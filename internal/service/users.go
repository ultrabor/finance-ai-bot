package service

import (
	"context"
	"strings"

	"finance-ai-bot/internal/domain"
)

type UserRepository interface {
	// Upsert создаёт члена семьи или обновляет его имя.
	Upsert(ctx context.Context, u domain.User) (domain.User, error)
	List(ctx context.Context) ([]domain.User, error)
}

type Users struct {
	repo UserRepository
}

func NewUsers(repo UserRepository) *Users {
	return &Users{repo: repo}
}

func (s *Users) Upsert(ctx context.Context, u domain.User) (domain.User, error) {
	if u.TelegramID <= 0 {
		return domain.User{}, domain.NewValidationError("telegram_id", "некорректный Telegram ID")
	}
	u.FirstName = strings.TrimSpace(u.FirstName)
	u.LastName = strings.TrimSpace(u.LastName)
	u.Username = strings.TrimPrefix(strings.TrimSpace(u.Username), "@")
	return s.repo.Upsert(ctx, u)
}

func (s *Users) List(ctx context.Context) ([]domain.User, error) {
	return s.repo.List(ctx)
}
