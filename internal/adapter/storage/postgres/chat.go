package postgres

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"finance-ai-bot/internal/domain"
)

// ChatRepo хранит историю диалога пользователя с ботом.
type ChatRepo struct {
	pool *pgxpool.Pool
}

func NewChatRepo(pool *pgxpool.Pool) *ChatRepo {
	return &ChatRepo{pool: pool}
}

// chatRetention — сколько хранить историю: агенту нужны минуты, остальное — для разбора жалоб.
const chatRetention = "7 days"

// Append сохраняет реплики и заодно удаляет устаревшие реплики пользователя, чтобы таблица не росла бесконечно.
func (r *ChatRepo) Append(ctx context.Context, userID int64, msgs ...domain.ChatMessage) error {
	batch := &pgx.Batch{}
	batch.Queue(`DELETE FROM chat_messages WHERE user_id = $1 AND created_at < now() - $2::interval`,
		userID, chatRetention)
	for _, m := range msgs {
		batch.Queue(`INSERT INTO chat_messages (user_id, role, content) VALUES ($1, $2, $3)`,
			userID, m.Role, m.Content)
	}
	if err := r.pool.SendBatch(ctx, batch).Close(); err != nil {
		return fmt.Errorf("append chat messages: %w", err)
	}
	return nil
}

// Recent возвращает до limit последних сообщений не старше since в хронологическом порядке.
func (r *ChatRepo) Recent(ctx context.Context, userID int64, since time.Time, limit int) ([]domain.ChatMessage, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT role, content FROM chat_messages
		WHERE user_id = $1 AND created_at >= $2
		ORDER BY created_at DESC, id DESC LIMIT $3`,
		userID, since, limit)
	if err != nil {
		return nil, fmt.Errorf("recent chat messages: %w", err)
	}
	msgs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.ChatMessage, error) {
		var m domain.ChatMessage
		err := row.Scan(&m.Role, &m.Content)
		return m, err
	})
	if err != nil {
		return nil, fmt.Errorf("recent chat messages: %w", err)
	}
	slices.Reverse(msgs)
	return msgs, nil
}
