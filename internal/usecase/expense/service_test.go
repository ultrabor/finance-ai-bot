package expense_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"finance-ai-bot/internal/domain"
	"finance-ai-bot/internal/testutil/memstore"
	"finance-ai-bot/internal/usecase/expense"
	"finance-ai-bot/internal/usecase/user"
)

const (
	alice int64 = 111
	bob   int64 = 222
)

func setup(t *testing.T) *expense.Service {
	t.Helper()
	store := memstore.New()
	users := user.NewService(store.Users())
	for _, id := range []int64{alice, bob} {
		if _, err := users.Upsert(context.Background(), domain.User{TelegramID: id, FirstName: "u"}); err != nil {
			t.Fatal(err)
		}
	}
	return expense.NewService(store.Expenses(), time.UTC)
}

func TestUserScopeIsolation(t *testing.T) {
	ctx := context.Background()
	svc := setup(t)

	// В UserScope владелец берётся из области, а не из входных данных.
	bobs, err := svc.Create(ctx, expense.UserScope(bob), expense.CreateParams{
		UserID: alice, Amount: 120000, Category: domain.CategoryTransport, Comment: "такси",
	})
	if err != nil {
		t.Fatal(err)
	}
	if bobs.UserID != bob {
		t.Fatalf("owner = %d, want %d", bobs.UserID, bob)
	}

	aliceScope := expense.UserScope(alice)
	if _, err := svc.Get(ctx, aliceScope, bobs.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Get foreign: err = %v, want ErrNotFound", err)
	}
	amount := domain.Money(1)
	if _, err := svc.Update(ctx, aliceScope, bobs.ID, domain.ExpensePatch{Amount: &amount}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Update foreign: err = %v, want ErrNotFound", err)
	}
	if _, err := svc.Delete(ctx, aliceScope, bobs.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Delete foreign: err = %v, want ErrNotFound", err)
	}

	// Фильтр по чужому user_id в UserScope перекрывается областью.
	bobID := bob
	res, err := svc.List(ctx, aliceScope, domain.ExpenseFilter{UserID: &bobID})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 0 || res.Summary.Count != 0 {
		t.Errorf("alice sees %d foreign expenses", len(res.Items))
	}

	// Глава семьи видит всё.
	res, err = svc.List(ctx, expense.FamilyScope(), domain.ExpenseFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Summary.Count != 1 {
		t.Errorf("family count = %d, want 1", res.Summary.Count)
	}
}

func TestZeroScopeDenied(t *testing.T) {
	svc := setup(t)
	_, err := svc.List(context.Background(), expense.Scope{}, domain.ExpenseFilter{})
	if err == nil {
		t.Fatal("zero scope must be rejected")
	}
}

func TestCreateValidation(t *testing.T) {
	ctx := context.Background()
	svc := setup(t)
	scope := expense.UserScope(alice)
	future := time.Now().UTC().AddDate(0, 0, 3)

	tests := []struct {
		name  string
		in    expense.CreateParams
		field string
	}{
		{"zero amount", expense.CreateParams{Amount: 0, Category: domain.CategoryFood}, "amount"},
		{"negative amount", expense.CreateParams{Amount: -5, Category: domain.CategoryFood}, "amount"},
		{"bad category", expense.CreateParams{Amount: 100, Category: "beer"}, "category"},
		{"future date", expense.CreateParams{Amount: 100, Category: domain.CategoryFood, SpentAt: future}, "spent_at"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.Create(ctx, scope, tt.in)
			var ve *domain.ValidationError
			if !errors.As(err, &ve) || ve.Field != tt.field {
				t.Fatalf("err = %v, want validation error on %q", err, tt.field)
			}
		})
	}
}

func TestCreateDefaultsToToday(t *testing.T) {
	svc := setup(t)
	e, err := svc.Create(context.Background(), expense.UserScope(alice), expense.CreateParams{
		Amount: 35000, Category: domain.CategoryFood, Comment: "  кофе ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !e.SpentAt.Equal(svc.Today()) {
		t.Errorf("spent_at = %v, want today %v", e.SpentAt, svc.Today())
	}
	if e.Comment != "кофе" {
		t.Errorf("comment = %q, want trimmed", e.Comment)
	}
}

func TestListPeriodValidation(t *testing.T) {
	svc := setup(t)
	from := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, -1)
	_, err := svc.List(context.Background(), expense.FamilyScope(), domain.ExpenseFilter{From: &from, To: &to})
	var ve *domain.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("err = %v, want validation error", err)
	}
}

func TestUpdateValidation(t *testing.T) {
	ctx := context.Background()
	svc := setup(t)
	scope := expense.UserScope(alice)
	e, err := svc.Create(ctx, scope, expense.CreateParams{Amount: 100, Category: domain.CategoryFood})
	if err != nil {
		t.Fatal(err)
	}

	zero, tooBig := domain.Money(0), domain.MaxMoney+1
	beer := domain.Category("beer")
	future, ancient := time.Now().UTC().AddDate(0, 0, 3), time.Date(1999, 12, 31, 0, 0, 0, 0, time.UTC)
	long := strings.Repeat("я", expense.MaxCommentLen+1)
	tests := []struct {
		name  string
		patch domain.ExpensePatch
		field string
	}{
		{"empty patch", domain.ExpensePatch{}, "body"},
		{"zero amount", domain.ExpensePatch{Amount: &zero}, "amount"},
		{"too big amount", domain.ExpensePatch{Amount: &tooBig}, "amount"},
		{"bad category", domain.ExpensePatch{Category: &beer}, "category"},
		{"future date", domain.ExpensePatch{SpentAt: &future}, "spent_at"},
		{"ancient date", domain.ExpensePatch{SpentAt: &ancient}, "spent_at"},
		{"long comment", domain.ExpensePatch{Comment: &long}, "comment"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.Update(ctx, scope, e.ID, tt.patch)
			var ve *domain.ValidationError
			if !errors.As(err, &ve) || ve.Field != tt.field {
				t.Fatalf("err = %v, want validation error on %q", err, tt.field)
			}
		})
	}
}

func TestUpdateNormalizesFields(t *testing.T) {
	ctx := context.Background()
	svc := setup(t)
	scope := expense.UserScope(alice)
	e, err := svc.Create(ctx, scope, expense.CreateParams{Amount: 100, Category: domain.CategoryFood})
	if err != nil {
		t.Fatal(err)
	}

	amount, cat, comment := domain.Money(250), domain.CategoryHealth, "  аптека "
	// Время суток отбрасывается: в БД хранится только дата.
	when := time.Date(2026, 1, 15, 18, 45, 0, 0, time.UTC)
	got, err := svc.Update(ctx, scope, e.ID, domain.ExpensePatch{
		Amount: &amount, Category: &cat, SpentAt: &when, Comment: &comment,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Amount != amount || got.Category != cat || got.Comment != "аптека" ||
		!got.SpentAt.Equal(time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("updated = %+v", got)
	}
}

func TestSummaryRespectsScopeAndCategory(t *testing.T) {
	ctx := context.Background()
	svc := setup(t)
	for _, in := range []struct {
		owner  int64
		amount domain.Money
		cat    domain.Category
	}{
		{alice, 100, domain.CategoryFood},
		{alice, 200, domain.CategoryTransport},
		{bob, 400, domain.CategoryFood},
	} {
		if _, err := svc.Create(ctx, expense.UserScope(in.owner), expense.CreateParams{Amount: in.amount, Category: in.cat}); err != nil {
			t.Fatal(err)
		}
	}

	sum, err := svc.Summary(ctx, expense.UserScope(alice), domain.ExpenseFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Count != 2 || sum.Total != 300 || len(sum.ByCategory) != 2 {
		t.Errorf("alice summary = %+v", sum)
	}

	food := domain.CategoryFood
	sum, err = svc.Summary(ctx, expense.FamilyScope(), domain.ExpenseFilter{Category: &food})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Count != 2 || sum.Total != 500 {
		t.Errorf("family food summary = %+v", sum)
	}

	beer := domain.Category("beer")
	if _, err := svc.Summary(ctx, expense.FamilyScope(), domain.ExpenseFilter{Category: &beer}); err == nil {
		t.Error("unknown category in filter must be rejected")
	}
	if _, err := svc.Summary(ctx, expense.Scope{}, domain.ExpenseFilter{}); err == nil {
		t.Error("zero scope must be rejected")
	}
}

func TestListPagingDefaults(t *testing.T) {
	ctx := context.Background()
	svc := setup(t)
	scope := expense.UserScope(alice)
	for range expense.DefaultListLimit + 1 {
		if _, err := svc.Create(ctx, scope, expense.CreateParams{Amount: 1, Category: domain.CategoryOther}); err != nil {
			t.Fatal(err)
		}
	}

	res, err := svc.List(ctx, scope, domain.ExpenseFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != expense.DefaultListLimit || res.Summary.Count != expense.DefaultListLimit+1 {
		t.Errorf("default page: %d items of %d", len(res.Items), res.Summary.Count)
	}

	res, err = svc.List(ctx, scope, domain.ExpenseFilter{Limit: expense.MaxListLimit + 100, Offset: expense.DefaultListLimit})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 {
		t.Errorf("offset page: %d items, want 1", len(res.Items))
	}

	for name, f := range map[string]domain.ExpenseFilter{
		"negative offset": {Offset: -1},
		"unknown sort":    {Sort: "amount"},
	} {
		var ve *domain.ValidationError
		if _, err := svc.List(ctx, scope, f); !errors.As(err, &ve) {
			t.Errorf("%s: err = %v, want validation error", name, err)
		}
	}
}

func TestZeroScopeDeniedEverywhere(t *testing.T) {
	ctx := context.Background()
	svc := setup(t)
	e, err := svc.Create(ctx, expense.UserScope(alice), expense.CreateParams{Amount: 1, Category: domain.CategoryFood})
	if err != nil {
		t.Fatal(err)
	}
	amount := domain.Money(2)
	checks := map[string]error{}
	_, checks["Create"] = svc.Create(ctx, expense.Scope{}, expense.CreateParams{UserID: alice, Amount: 1, Category: domain.CategoryFood})
	_, checks["Get"] = svc.Get(ctx, expense.Scope{}, e.ID)
	_, checks["Update"] = svc.Update(ctx, expense.Scope{}, e.ID, domain.ExpensePatch{Amount: &amount})
	_, checks["Delete"] = svc.Delete(ctx, expense.Scope{}, e.ID)
	for name, err := range checks {
		if err == nil {
			t.Errorf("%s with zero scope must be rejected", name)
		}
	}
}

func TestFamilyScopeRequiresUserID(t *testing.T) {
	_, err := setup(t).Create(context.Background(), expense.FamilyScope(), expense.CreateParams{Amount: 1, Category: domain.CategoryFood})
	var ve *domain.ValidationError
	if !errors.As(err, &ve) || ve.Field != "user_id" {
		t.Fatalf("err = %v, want validation error on user_id", err)
	}
}
