// Команда api — REST API веб-панели главы семьи.
package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"
	_ "time/tzdata" // часовые пояса в образе scratch

	"finance-ai-bot/internal/adapter/storage/postgres"
	"finance-ai-bot/internal/app"
	"finance-ai-bot/internal/config"
	"finance-ai-bot/internal/transport/httpapi"
	"finance-ai-bot/internal/usecase/expense"
	"finance-ai-bot/internal/usecase/user"
)

func main() {
	// Режим проверки здоровья для healthcheck в docker compose: в образе scratch нет curl.
	if len(os.Args) > 1 && os.Args[1] == "-healthcheck" {
		os.Exit(healthcheck())
	}
	if err := run(); err != nil {
		slog.Error("api stopped", "error", err)
		os.Exit(1)
	}
}

func healthcheck() int {
	port := cmp.Or(os.Getenv("HTTP_PORT"), "8080")
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + port + "/healthz")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "status", resp.StatusCode)
		return 1
	}
	return 0
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := app.NewLogger(cfg.AppEnv, cfg.LogLevel)
	loc, err := cfg.Location()
	if err != nil {
		return err
	}

	ctx, stop := app.SignalContext()
	defer stop()

	pool, err := postgres.Connect(ctx, cfg.Postgres.DSN())
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := postgres.Migrate(ctx, pool, log); err != nil {
		return err
	}

	handler := httpapi.New(
		expense.NewService(postgres.NewExpenseRepo(pool), loc),
		user.NewService(postgres.NewUserRepo(pool)),
		pool,
		log,
	)
	srv := &http.Server{
		Addr:              cfg.HTTP.Addr(),
		Handler:           handler.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("http server started", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
	}

	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
