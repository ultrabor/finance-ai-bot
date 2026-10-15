package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"finance-ai-bot/internal/domain"
	"finance-ai-bot/internal/service"
	"finance-ai-bot/internal/storage/memory"
)

const (
	alice int64 = 111
	bob   int64 = 222
)

func setup(t *testing.T) *service.Expenses {
	t.Helper()
	store := memory.New()
	users := service.NewUsers(store.Users())
	for _, id := range []int64{alice, bob} {
		if _, err := users.Upsert(context.Background(), domain.User{TelegramID: id, FirstName: "u"}); err != nil {
			t.Fatal(err)
		}
	}
	return service.NewExpenses(store.Expenses(), time.UTC)
}

func TestUserScopeIsolation(t *testing.T) {
	ctx := context.Background()
	svc := setup(t)

	// В UserScope владелец берётся из области, а не из входных данных.
	bobs, err := svc.Create(ctx, service.UserScope(bob), service.NewExpense{
		UserID: alice, Amount: 120000, Category: domain.CategoryTransport, Comment: "такси",
	})
	if err != nil {
		t.Fatal(err)
	}
	if bobs.UserID != bob {
		t.Fatalf("owner = %d, want %d", bobs.UserID, bob)
	}

	aliceScope := service.UserScope(alice)
	if _, err := svc.Get(ctx, aliceScope, bobs.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Get foreign: err = %v, want ErrNotFound", err)
	}
	amount := domain.Money(1)
	if _, err := svc.Update(ctx, aliceScope, bobs.ID, domain.ExpensePatch{Amount: &amount}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Update foreign: err = %v, want ErrNotFound", err)
	}
	if err := svc.Delete(ctx, aliceScope, bobs.ID); !errors.Is(err, domain.ErrNotFound) {
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
	res, err = svc.List(ctx, service.FamilyScope(), domain.ExpenseFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Summary.Count != 1 {
		t.Errorf("family count = %d, want 1", res.Summary.Count)
	}
}

func TestZeroScopeDenied(t *testing.T) {
	svc := setup(t)
	_, err := svc.List(context.Background(), service.Scope{}, domain.ExpenseFilter{})
	if err == nil {
		t.Fatal("zero scope must be rejected")
	}
}

func TestCreateValidation(t *testing.T) {
	ctx := context.Background()
	svc := setup(t)
	scope := service.UserScope(alice)
	future := time.Now().UTC().AddDate(0, 0, 3)

	tests := []struct {
		name  string
		in    service.NewExpense
		field string
	}{
		{"zero amount", service.NewExpense{Amount: 0, Category: domain.CategoryFood}, "amount"},
		{"negative amount", service.NewExpense{Amount: -5, Category: domain.CategoryFood}, "amount"},
		{"bad category", service.NewExpense{Amount: 100, Category: "beer"}, "category"},
		{"future date", service.NewExpense{Amount: 100, Category: domain.CategoryFood, SpentAt: future}, "spent_at"},
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
	e, err := svc.Create(context.Background(), service.UserScope(alice), service.NewExpense{
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
	_, err := svc.List(context.Background(), service.FamilyScope(), domain.ExpenseFilter{From: &from, To: &to})
	var ve *domain.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("err = %v, want validation error", err)
	}
}
