//go:build eval

// Eval-набор: прогон сценариев из ТЗ против реальной LLM.
// Запуск: make eval (LLM_BASE_URL, LLM_API_KEY, LLM_MODEL берутся из окружения).
package agent

import (
	"cmp"
	"context"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"finance-ai-bot/internal/domain"
	"finance-ai-bot/internal/llm"
	"finance-ai-bot/internal/service"
	"finance-ai-bot/internal/storage/memory"
)

type evalTurn struct {
	say   string
	check func(t *testing.T, reply string, all []domain.Expense, today time.Time)
}

func TestEval(t *testing.T) {
	model := os.Getenv("LLM_MODEL")
	if model == "" {
		t.Skip("LLM_MODEL is not set")
	}
	var temperature *float64
	if v, err := strconv.ParseFloat(os.Getenv("LLM_TEMPERATURE"), 64); err == nil {
		temperature = &v
	}
	client := llm.NewOpenAIClient(llm.OpenAIConfig{
		BaseURL:     os.Getenv("LLM_BASE_URL"),
		APIKey:      os.Getenv("LLM_API_KEY"),
		Model:       model,
		Temperature: temperature,
		Timeout:     90 * time.Second,
	})

	loc, err := time.LoadLocation(cmp.Or(os.Getenv("APP_TIMEZONE"), "Europe/Moscow"))
	if err != nil {
		t.Fatal(err)
	}

	store := memory.New()
	ctx := context.Background()
	if _, err := service.NewUsers(store.Users()).Upsert(ctx, domain.User{TelegramID: alice, FirstName: "Алиса"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.NewUsers(store.Users()).Upsert(ctx, domain.User{TelegramID: bob, FirstName: "Боб"}); err != nil {
		t.Fatal(err)
	}
	expenses := service.NewExpenses(store.Expenses(), loc)
	// Чужая трата: агент Алисы не должен её видеть.
	if _, err := expenses.Create(ctx, service.UserScope(bob), service.NewExpense{
		Amount: 999900, Category: domain.CategoryShopping, Comment: "телевизор",
	}); err != nil {
		t.Fatal(err)
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	a := New(client, expenses, Config{Location: loc, MaxSteps: 8}, log)

	find := func(all []domain.Expense, comment string) *domain.Expense {
		for i := range all {
			if strings.Contains(all[i].Comment, comment) {
				return &all[i]
			}
		}
		return nil
	}
	contains := func(t *testing.T, reply string, variants ...string) {
		t.Helper()
		norm := strings.ReplaceAll(reply, " ", " ")
		for _, v := range variants {
			if strings.Contains(norm, v) {
				return
			}
		}
		t.Errorf("reply %q contains none of %q", reply, variants)
	}

	turns := []evalTurn{
		{"кофе 350", func(t *testing.T, _ string, all []domain.Expense, today time.Time) {
			e := find(all, "кофе")
			if e == nil || e.Amount != 35000 || e.Category != domain.CategoryFood || !e.SpentAt.Equal(today) {
				t.Errorf("coffee = %+v", e)
			}
		}},
		{"такси 1200 вчера", func(t *testing.T, _ string, all []domain.Expense, today time.Time) {
			e := find(all, "такси")
			if e == nil || e.Amount != 120000 || e.Category != domain.CategoryTransport || !e.SpentAt.Equal(today.AddDate(0, 0, -1)) {
				t.Errorf("taxi = %+v", e)
			}
		}},
		// Последней добавлена поездка на такси, но исправить нужно трату на 350 — кофе.
		{"последнее было не 350, а 450", func(t *testing.T, _ string, all []domain.Expense, _ time.Time) {
			if e := find(all, "кофе"); e == nil || e.Amount != 45000 || e.Comment != "кофе" {
				t.Errorf("coffee after fix = %+v", e)
			}
			if e := find(all, "такси"); e == nil || e.Amount != 120000 {
				t.Errorf("taxi must stay unchanged: %+v", e)
			}
		}},
		{"аптека 780", func(t *testing.T, _ string, all []domain.Expense, _ time.Time) {
			if e := find(all, "аптек"); e == nil || e.Category != domain.CategoryHealth {
				t.Errorf("pharmacy = %+v", e)
			}
		}},
		{"сколько потратил на еду в этом месяце", func(t *testing.T, reply string, _ []domain.Expense, _ time.Time) {
			contains(t, reply, "450")
		}},
		{"покажи траты за неделю", func(t *testing.T, reply string, _ []domain.Expense, _ time.Time) {
			contains(t, reply, "1200", "1 200")
			if strings.Contains(reply, "телевизор") {
				t.Error("foreign expense leaked")
			}
		}},
		{"удали такси", func(t *testing.T, _ string, all []domain.Expense, _ time.Time) {
			if find(all, "такси") != nil {
				t.Error("taxi was not deleted")
			}
			if len(all) != 2 {
				t.Errorf("expenses left = %d, want 2", len(all))
			}
		}},
	}

	var history []domain.ChatMessage
	for _, turn := range turns {
		t.Run(turn.say, func(t *testing.T) {
			// «Сегодня» фиксируем до запроса, чтобы прогон около полуночи не давал ложных падений.
			today := expenses.Today()
			reply, err := a.Reply(ctx, alice, history, turn.say)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("> %s\n< %s", turn.say, reply)
			history = append(history,
				domain.ChatMessage{Role: domain.ChatRoleUser, Content: turn.say},
				domain.ChatMessage{Role: domain.ChatRoleAssistant, Content: reply})

			res, err := expenses.List(ctx, service.UserScope(alice), domain.ExpenseFilter{Limit: 100})
			if err != nil {
				t.Fatal(err)
			}
			turn.check(t, reply, res.Items, today)
		})
	}
}
