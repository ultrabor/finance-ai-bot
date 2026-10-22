// Команда bot — Telegram-бот с ИИ-агентом учёта трат.
package main

import (
	"log/slog"
	"os"
	_ "time/tzdata" // часовые пояса в образе scratch

	"finance-ai-bot/internal/adapter/llm/openai"
	"finance-ai-bot/internal/adapter/storage/postgres"
	"finance-ai-bot/internal/agent"
	"finance-ai-bot/internal/app"
	"finance-ai-bot/internal/config"
	"finance-ai-bot/internal/transport/telegram"
	"finance-ai-bot/internal/usecase/expense"
	"finance-ai-bot/internal/usecase/user"
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
	if err := cfg.ValidateBot(); err != nil {
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

	expenses := expense.NewService(postgres.NewExpenseRepo(pool), loc)
	client := openai.New(openai.Config{
		BaseURL:     cfg.LLM.BaseURL,
		APIKey:      cfg.LLM.APIKey,
		Model:       cfg.LLM.Model,
		Temperature: cfg.LLM.Temperature,
		Timeout:     cfg.LLM.Timeout,
	})
	ai := agent.New(client, expenses, agent.Config{Location: loc, MaxSteps: cfg.LLM.MaxSteps}, log)

	bot, err := telegram.New(
		telegram.Config{Token: cfg.Telegram.Token, AllowedIDs: cfg.Telegram.AllowedIDs},
		ai,
		user.NewService(postgres.NewUserRepo(pool)),
		postgres.NewChatRepo(pool),
		log,
	)
	if err != nil {
		return err
	}

	log.Info("llm configured", "base_url", cfg.LLM.BaseURL, "model", cfg.LLM.Model)
	bot.Start(ctx)
	log.Info("shutting down")
	return nil
}
