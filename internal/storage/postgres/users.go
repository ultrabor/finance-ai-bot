package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"finance-ai-bot/internal/domain"
)

type UserRepo struct {
	pool *pgxpool.Pool
}

func NewUserRepo(pool *pgxpool.Pool) *UserRepo {
	return &UserRepo{pool: pool}
}

func (r *UserRepo) Upsert(ctx context.Context, u domain.User) (domain.User, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO users (telegram_id, username, first_name, last_name)
		VALUES ($1, NULLIF($2, ''), $3, NULLIF($4, ''))
		ON CONFLICT (telegram_id) DO UPDATE SET
			username   = EXCLUDED.username,
			first_name = EXCLUDED.first_name,
			last_name  = EXCLUDED.last_name,
			updated_at = now()
		RETURNING telegram_id, COALESCE(username, ''), first_name, COALESCE(last_name, ''), created_at`,
		u.TelegramID, u.Username, u.FirstName, u.LastName)
	var out domain.User
	if err := row.Scan(&out.TelegramID, &out.Username, &out.FirstName, &out.LastName, &out.CreatedAt); err != nil {
		return domain.User{}, fmt.Errorf("upsert user: %w", err)
	}
	return out, nil
}

func (r *UserRepo) List(ctx context.Context) ([]domain.User, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT telegram_id, COALESCE(username, ''), first_name, COALESCE(last_name, ''), created_at
		FROM users ORDER BY first_name, telegram_id`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	users, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.User, error) {
		var u domain.User
		err := row.Scan(&u.TelegramID, &u.Username, &u.FirstName, &u.LastName, &u.CreatedAt)
		return u, err
	})
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	return users, nil
}
