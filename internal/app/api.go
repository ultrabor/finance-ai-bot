package app

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"finance-ai-bot/internal/adapter/storage/postgres"
	"finance-ai-bot/internal/config"
	"finance-ai-bot/internal/transport/httpapi"
	"finance-ai-bot/internal/usecase/expense"
	"finance-ai-bot/internal/usecase/user"
)

const shutdownTimeout = 10 * time.Second

// RunAPI запускает REST API веб-панели и блокируется до отмены ctx.
func RunAPI(ctx context.Context, cfg *config.Config) error {
	rt, err := newRuntime(ctx, cfg)
	if err != nil {
		return err
	}
	defer rt.close()

	handler := httpapi.New(
		expense.NewService(postgres.NewExpenseRepo(rt.pool), rt.loc),
		user.NewService(postgres.NewUserRepo(rt.pool)),
		rt.pool,
		rt.log,
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
		rt.log.Info("http server started", "addr", srv.Addr)
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

	rt.log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// Healthcheck проверяет /healthz запущенного API и возвращает код выхода процесса.
// Нужен healthcheck'у docker compose: в образе scratch нет curl.
func Healthcheck() int {
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
