//go:build integration

// Интеграционные тесты SQL-репозиториев на настоящем Postgres.
// Запуск: make db && make test-integration (параметры подключения — POSTGRES_* из окружения).
package postgres

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"finance-ai-bot/internal/config"
	"finance-ai-bot/internal/domain"
)

type testDB struct {
	pool     *pgxpool.Pool
	expenses *ExpenseRepo
	users    *UserRepo
	chat     *ChatRepo
	alice    int64
	bob      int64
}

func setup(t *testing.T) *testDB {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("no database config (run via make test-integration): %v", err)
	}
	ctx := context.Background()
	pool, err := Connect(ctx, cfg.Postgres.DSN())
	if err != nil {
		t.Fatalf("database unavailable (start it with make db): %v", err)
	}
	t.Cleanup(pool.Close)
	if err := Migrate(ctx, pool, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatal(err)
	}

	// Тесты идут в ту же БД, что и приложение. Отрицательные ID не бывают у реальных
	// пользователей Telegram, поэтому тестовые пользователи с ними не пересекаются
	// (CHECK на telegram_id нет, репозитории валидацию сервиса не проходят).
	// Удаление каскадом чистит траты и историю.
	db := &testDB{
		pool:     pool,
		expenses: NewExpenseRepo(pool),
		users:    NewUserRepo(pool),
		chat:     NewChatRepo(pool),
		alice:    -(1_000_000_000 + rand.Int64N(1_000_000_000)),
	}
	db.bob = db.alice - 1
	// Очистку регистрируем до вставок: если вторая упадёт, первая не останется в базе.
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE telegram_id IN ($1, $2)`, db.alice, db.bob)
	})
	for _, u := range []domain.User{{TelegramID: db.alice, FirstName: "Алиса"}, {TelegramID: db.bob, FirstName: "Боб"}} {
		if _, err := db.users.Upsert(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func date(s string) time.Time {
	d, _ := domain.ParseDate(s)
	return d
}

func (db *testDB) create(t *testing.T, owner int64, amount domain.Money, cat domain.Category, spent, comment string) domain.Expense {
	t.Helper()
	e, err := db.expenses.Create(context.Background(), domain.Expense{
		UserID: owner, Amount: amount, Category: cat, SpentAt: date(spent), Comment: comment,
	})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestExpenseCRUDAndOwnership(t *testing.T) {
	db := setup(t)
	ctx := context.Background()
	e := db.create(t, db.alice, 35000, domain.CategoryFood, "2026-10-01", "кофе")
	if !e.SpentAt.Equal(date("2026-10-01")) || e.Comment != "кофе" || e.CreatedAt.IsZero() {
		t.Fatalf("created = %+v", e)
	}

	// Чужой владелец не видит, не меняет и не удаляет.
	if _, err := db.expenses.Get(ctx, e.ID, &db.bob); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("foreign get: %v", err)
	}
	amount := domain.Money(1)
	if _, err := db.expenses.Update(ctx, e.ID, &db.bob, domain.ExpensePatch{Amount: &amount}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("foreign update: %v", err)
	}
	if err := db.expenses.Delete(ctx, e.ID, &db.bob); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("foreign delete: %v", err)
	}

	// Частичное обновление: comment nil — без изменений, "" — очистка.
	newAmount := domain.Money(45000)
	got, err := db.expenses.Update(ctx, e.ID, &db.alice, domain.ExpensePatch{Amount: &newAmount})
	if err != nil || got.Amount != 45000 || got.Comment != "кофе" || got.Category != domain.CategoryFood {
		t.Fatalf("update amount: %+v, %v", got, err)
	}
	if !got.UpdatedAt.After(got.CreatedAt) {
		t.Error("updated_at not bumped")
	}
	empty := ""
	got, err = db.expenses.Update(ctx, e.ID, nil, domain.ExpensePatch{Comment: &empty})
	if err != nil || got.Comment != "" || got.Amount != 45000 {
		t.Fatalf("clear comment: %+v, %v", got, err)
	}

	if err := db.expenses.Delete(ctx, e.ID, &db.alice); err != nil {
		t.Fatal(err)
	}
	if _, err := db.expenses.Get(ctx, e.ID, nil); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("after delete: %v", err)
	}
}

func TestListFiltersAndSummary(t *testing.T) {
	db := setup(t)
	ctx := context.Background()
	db.create(t, db.alice, 35000, domain.CategoryFood, "2026-10-01", "Кофе с собой")
	db.create(t, db.alice, 120000, domain.CategoryTransport, "2026-10-02", "такси 100% быстро")
	db.create(t, db.alice, 50000, domain.CategoryFood, "2026-09-30", "обед_офис")
	db.create(t, db.bob, 999900, domain.CategoryShopping, "2026-10-01", "телевизор")

	from, to := date("2026-10-01"), date("2026-10-31")
	items, sum, err := db.expenses.List(ctx, domain.ExpenseFilter{UserID: &db.alice, From: &from, To: &to, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Comment != "такси 100% быстро" {
		t.Errorf("first page = %+v", items)
	}
	// Итоги считаются по всей выборке, а не по странице.
	if sum.Count != 2 || sum.Total != 155000 {
		t.Errorf("summary = %+v", sum)
	}
	// Порядок итогов: по сумме, затем по категории.
	if len(sum.ByCategory) != 2 || sum.ByCategory[0].Category != domain.CategoryTransport {
		t.Errorf("by category = %+v", sum.ByCategory)
	}

	search := func(q string) int {
		_, s, err := db.expenses.List(ctx, domain.ExpenseFilter{UserID: &db.alice, Query: q, Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		return s.Count
	}
	for q, want := range map[string]int{
		"кофе": 1, // регистр не важен, в том числе для кириллицы
		"100%": 1, // % экранируется
		"%":    1,
		"_":    1, // _ экранируется: совпадает только «обед_офис»
		"чай":  0,
	} {
		if got := search(q); got != want {
			t.Errorf("search %q = %d, want %d", q, got, want)
		}
	}

	byCreated, _, err := db.expenses.List(ctx, domain.ExpenseFilter{UserID: &db.alice, Sort: domain.SortByCreatedAt, Limit: 1})
	if err != nil || byCreated[0].Comment != "обед_офис" {
		t.Errorf("latest added = %+v, %v", byCreated, err)
	}
}

func TestConstraintErrorsMapped(t *testing.T) {
	db := setup(t)
	ctx := context.Background()
	var ve *domain.ValidationError

	// setup берёт ID из (-2e9-1, -1e9], сюда — из (-4e9, -3e9]: такого пользователя точно нет.
	unknown := db.alice - 2_000_000_000
	_, err := db.expenses.Create(ctx, domain.Expense{UserID: unknown, Amount: 1, Category: domain.CategoryFood, SpentAt: date("2026-10-01")})
	if !errors.As(err, &ve) || ve.Field != "user_id" {
		t.Errorf("unknown user: %v", err)
	}
	_, err = db.expenses.Create(ctx, domain.Expense{UserID: db.alice, Amount: 1, Category: "beer", SpentAt: date("2026-10-01")})
	if !errors.As(err, &ve) {
		t.Errorf("bad category: %v", err)
	}
}

func TestUsersAndChat(t *testing.T) {
	db := setup(t)
	ctx := context.Background()

	u, err := db.users.Upsert(ctx, domain.User{TelegramID: db.alice, FirstName: "Алиса", Username: "alice"})
	if err != nil || u.Username != "alice" {
		t.Fatalf("upsert: %+v, %v", u, err)
	}
	users, err := db.users.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	pos := map[int64]int{}
	for i, u := range users {
		pos[u.TelegramID] = i
	}
	if pos[db.alice] > pos[db.bob] {
		t.Error("users must be ordered by first name")
	}

	if err := db.chat.Append(ctx, db.alice,
		domain.ChatMessage{Role: domain.ChatRoleUser, Content: "кофе 350"},
		domain.ChatMessage{Role: domain.ChatRoleAssistant, Content: "Записал"},
	); err != nil {
		t.Fatal(err)
	}
	msgs, err := db.chat.Recent(ctx, db.alice, time.Now().Add(-time.Minute), 10)
	if err != nil || len(msgs) != 2 || msgs[0].Content != "кофе 350" || msgs[1].Role != domain.ChatRoleAssistant {
		t.Errorf("recent = %+v, %v", msgs, err)
	}
	if msgs, _ := db.chat.Recent(ctx, db.bob, time.Now().Add(-time.Minute), 10); len(msgs) != 0 {
		t.Errorf("bob sees alice's history: %+v", msgs)
	}
}

func TestChatRetention(t *testing.T) {
	db := setup(t)
	ctx := context.Background()

	for _, id := range []int64{db.alice, db.bob} {
		if _, err := db.pool.Exec(ctx, `INSERT INTO chat_messages (user_id, role, content, created_at)
			VALUES ($1, 'user', 'старое', now() - interval '8 days')`, id); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.chat.Append(ctx, db.alice, domain.ChatMessage{Role: domain.ChatRoleUser, Content: "новое"}); err != nil {
		t.Fatal(err)
	}

	count := func(id int64) (n int) {
		if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM chat_messages WHERE user_id = $1`, id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count(db.alice); n != 1 {
		t.Errorf("alice messages = %d, want 1 (old one removed)", n)
	}
	// Чистка затрагивает только автора новой реплики.
	if n := count(db.bob); n != 1 {
		t.Errorf("bob messages = %d, want 1", n)
	}
}
