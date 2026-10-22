// Package app — composition root: сборка зависимостей и запуск бинарников.
// Только здесь конкретные адаптеры связываются с use case и транспортом.
package app

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"finance-ai-bot/internal/adapter/storage/postgres"
	"finance-ai-bot/internal/config"
)

// NewLogger создаёт slog-логгер: JSON в production, текст при разработке.
func NewLogger(env, level string) *slog.Logger {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(strings.ToUpper(level))); err != nil {
		lvl = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: lvl}
	if env == "production" {
		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, opts))
}

// SignalContext отменяется по SIGINT/SIGTERM.
func SignalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

// runtime — общее для бинарников окружение: логгер, часовой пояс семьи и пул БД с применёнными миграциями.
type runtime struct {
	log  *slog.Logger
	loc  *time.Location
	pool *pgxpool.Pool
}

func newRuntime(ctx context.Context, cfg *config.Config) (*runtime, error) {
	log := NewLogger(cfg.AppEnv, cfg.LogLevel)
	loc, err := cfg.Location()
	if err != nil {
		return nil, err
	}
	pool, err := postgres.Connect(ctx, cfg.Postgres.DSN())
	if err != nil {
		return nil, err
	}
	if err := postgres.Migrate(ctx, pool, log); err != nil {
		pool.Close()
		return nil, err
	}
	return &runtime{log: log, loc: loc, pool: pool}, nil
}

func (r *runtime) close() { r.pool.Close() }
