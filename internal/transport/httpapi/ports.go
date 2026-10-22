package httpapi

import (
	"context"

	"github.com/google/uuid"

	"finance-ai-bot/internal/domain"
	"finance-ai-bot/internal/usecase/expense"
)

// ExpenseUseCase — сценарии учёта трат для веб-панели.
type ExpenseUseCase interface {
	Create(ctx context.Context, scope expense.Scope, in expense.CreateParams) (domain.Expense, error)
	Get(ctx context.Context, scope expense.Scope, id uuid.UUID) (domain.Expense, error)
	List(ctx context.Context, scope expense.Scope, f domain.ExpenseFilter) (expense.ListResult, error)
	Update(ctx context.Context, scope expense.Scope, id uuid.UUID, p domain.ExpensePatch) (domain.Expense, error)
	Delete(ctx context.Context, scope expense.Scope, id uuid.UUID) (domain.Expense, error)
}

// UserUseCase — управление членами семьи.
type UserUseCase interface {
	Upsert(ctx context.Context, u domain.User) (domain.User, error)
	List(ctx context.Context) ([]domain.User, error)
}

// Pinger проверяет доступность зависимостей (БД) для /healthz.
type Pinger interface {
	Ping(ctx context.Context) error
}
