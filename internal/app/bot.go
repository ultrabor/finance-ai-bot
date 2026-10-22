package app

import (
	"context"

	"finance-ai-bot/internal/adapter/llm/openai"
	"finance-ai-bot/internal/adapter/storage/postgres"
	"finance-ai-bot/internal/agent"
	"finance-ai-bot/internal/agent/prompts"
	"finance-ai-bot/internal/config"
	"finance-ai-bot/internal/transport/telegram"
	"finance-ai-bot/internal/usecase/expense"
	"finance-ai-bot/internal/usecase/user"
)

// RunBot запускает Telegram-бота с ИИ-агентом и блокируется до отмены ctx.
func RunBot(ctx context.Context, cfg *config.Config) error {
	if err := cfg.ValidateBot(); err != nil {
		return err
	}
	rt, err := newRuntime(ctx, cfg)
	if err != nil {
		return err
	}
	defer rt.close()

	client := openai.New(openai.Config{
		BaseURL:     cfg.LLM.BaseURL,
		APIKey:      cfg.LLM.APIKey,
		Model:       cfg.LLM.Model,
		Temperature: cfg.LLM.Temperature,
		Timeout:     cfg.LLM.Timeout,
	})
	agentPrompts, err := prompts.Load(cfg.LLM.PromptPath)
	if err != nil {
		return err
	}
	expenses := expense.NewService(postgres.NewExpenseRepo(rt.pool), rt.loc)
	ai, err := agent.New(client, expenses, agent.Config{Location: rt.loc, MaxSteps: cfg.LLM.MaxSteps, Prompts: agentPrompts}, rt.log)
	if err != nil {
		return err
	}

	bot, err := telegram.New(
		telegram.Config{Token: cfg.Telegram.Token, AllowedIDs: cfg.Telegram.AllowedIDs},
		ai,
		user.NewService(postgres.NewUserRepo(rt.pool)),
		postgres.NewChatRepo(rt.pool),
		rt.log,
	)
	if err != nil {
		return err
	}

	rt.log.Info("llm configured", "base_url", cfg.LLM.BaseURL, "model", cfg.LLM.Model,
		"prompt", cfg.LLM.PromptPath)
	bot.Start(ctx)
	rt.log.Info("shutting down")
	return nil
}
