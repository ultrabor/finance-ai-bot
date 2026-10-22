package expense

import (
	"context"

	"github.com/google/uuid"

	"finance-ai-bot/internal/domain"
)

// Repository — хранилище трат; реализуется адаптерами storage.
type Repository interface {
	Create(ctx context.Context, e domain.Expense) (domain.Expense, error)
	// Get, Update, Delete при ownerID != nil работают только с тратами этого владельца,
	// иначе возвращают domain.ErrNotFound.
	Get(ctx context.Context, id uuid.UUID, ownerID *int64) (domain.Expense, error)
	// List возвращает страницу трат и агрегаты по всей выборке из одного снимка данных.
	List(ctx context.Context, f domain.ExpenseFilter) ([]domain.Expense, domain.ExpenseSummary, error)
	Summary(ctx context.Context, f domain.ExpenseFilter) (domain.ExpenseSummary, error)
	Update(ctx context.Context, id uuid.UUID, ownerID *int64, p domain.ExpensePatch) (domain.Expense, error)
	// Delete возвращает удалённую трату.
	Delete(ctx context.Context, id uuid.UUID, ownerID *int64) (domain.Expense, error)
}
