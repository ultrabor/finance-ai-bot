// Package user — сценарии работы с членами семьи.
package user

import (
	"context"
	"strings"

	"finance-ai-bot/internal/domain"
)

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) Upsert(ctx context.Context, u domain.User) (domain.User, error) {
	if u.TelegramID <= 0 {
		return domain.User{}, domain.NewValidationError("telegram_id", "некорректный Telegram ID")
	}
	u.FirstName = strings.TrimSpace(u.FirstName)
	u.LastName = strings.TrimSpace(u.LastName)
	u.Username = strings.TrimPrefix(strings.TrimSpace(u.Username), "@")
	return s.repo.Upsert(ctx, u)
}

func (s *Service) List(ctx context.Context) ([]domain.User, error) {
	return s.repo.List(ctx)
}
