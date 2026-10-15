// Package postgres реализует хранилище на PostgreSQL (pgx).
package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"finance-ai-bot/migrations"
)

// Connect открывает пул и ждёт готовности БД.
func Connect(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for {
		if err = pool.Ping(pingCtx); err == nil {
			return pool, nil
		}
		select {
		case <-pingCtx.Done():
			pool.Close()
			return nil, fmt.Errorf("ping postgres: %w", err)
		case <-time.After(time.Second):
		}
	}
}

// Migrate накатывает встроенные миграции. Advisory lock позволяет
// безопасно запускать его одновременно из api и bot.
func Migrate(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) error {
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()

	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return fmt.Errorf("create migration locker: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS, goose.WithSessionLocker(locker))
	if err != nil {
		return fmt.Errorf("create migration provider: %w", err)
	}
	results, err := provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	for _, r := range results {
		log.Info("migration applied", "version", r.Source.Version, "duration", r.Duration)
	}
	return nil
}
