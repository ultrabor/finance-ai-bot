package user

import (
	"context"

	"finance-ai-bot/internal/domain"
)

// Repository — хранилище членов семьи; реализуется адаптерами storage.
type Repository interface {
	// Upsert создаёт члена семьи или обновляет его имя.
	Upsert(ctx context.Context, u domain.User) (domain.User, error)
	List(ctx context.Context) ([]domain.User, error)
}
