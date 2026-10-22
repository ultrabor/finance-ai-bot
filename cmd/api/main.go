// Команда api — REST API веб-панели главы семьи.
package main

import (
	"log/slog"
	"os"
	_ "time/tzdata" // часовые пояса в образе scratch

	"finance-ai-bot/internal/app"
	"finance-ai-bot/internal/config"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "-healthcheck" {
		os.Exit(app.Healthcheck())
	}
	if err := run(); err != nil {
		slog.Error("api stopped", "error", err)
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
	return app.RunAPI(ctx, cfg)
}
