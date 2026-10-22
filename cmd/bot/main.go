// Команда bot — Telegram-бот с ИИ-агентом учёта трат.
package main

import (
	"log/slog"
	"os"
	_ "time/tzdata" // часовые пояса в образе scratch

	"finance-ai-bot/internal/app"
	"finance-ai-bot/internal/config"
)

func main() {
	if err := run(); err != nil {
		slog.Error("bot stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := app.SignalContext()
	defer stop()
	return app.RunBot(ctx, cfg)
}
