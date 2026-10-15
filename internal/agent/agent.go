// Package agent — ИИ-агент, выполняющий CRUD над тратами пользователя по тексту на естественном языке.
package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"finance-ai-bot/internal/domain"
	"finance-ai-bot/internal/llm"
	"finance-ai-bot/internal/service"
)

const fallbackReply = "Не получилось обработать запрос. Попробуйте сформулировать иначе, например: «кофе 350»."

type Agent struct {
	llm      llm.Client
	expenses *service.Expenses
	loc      *time.Location
	maxSteps int
	now      func() time.Time
	log      *slog.Logger
	tools    []llm.Tool
}

type Config struct {
	Location *time.Location
	MaxSteps int
}

func New(client llm.Client, expenses *service.Expenses, cfg Config, log *slog.Logger) *Agent {
	return &Agent{
		llm:      client,
		expenses: expenses,
		loc:      cfg.Location,
		maxSteps: cfg.MaxSteps,
		now:      time.Now,
		log:      log,
		tools:    toolDefinitions(),
	}
}

// Reply обрабатывает сообщение пользователя и возвращает ответ для него.
//
// userID берётся только из Telegram-апдейта и замыкается в области доступа инструментов:
// модель не передаёт и не может подменить владельца трат.
func (a *Agent) Reply(ctx context.Context, userID int64, history []domain.ChatMessage, text string) (string, error) {
	scope := service.UserScope(userID)

	msgs := make([]llm.Message, 0, len(history)+2)
	msgs = append(msgs, llm.Message{Role: llm.RoleSystem, Content: systemPrompt(a.now(), a.loc)})
	for _, m := range history {
		msgs = append(msgs, llm.Message{Role: m.Role, Content: m.Content})
	}
	msgs = append(msgs, llm.Message{Role: llm.RoleUser, Content: text})

	var done journal
	for step := range a.maxSteps {
		resp, err := a.llm.Chat(ctx, msgs, a.tools)
		if err != nil {
			err = fmt.Errorf("agent step %d: %w", step, err)
			// Изменения уже сохранены — сообщаем о них, иначе пользователь повторит запрос и создаст дубль.
			if !done.empty() {
				a.log.ErrorContext(ctx, "llm failed after changes were applied", "user_id", userID, "error", err)
				return done.summary(), nil
			}
			return "", err
		}
		resp.Role = llm.RoleAssistant
		msgs = append(msgs, resp)

		if len(resp.ToolCalls) == 0 {
			reply := strings.TrimSpace(resp.Content)
			if reply == "" {
				return done.summaryOr(fallbackReply), nil
			}
			return reply, nil
		}

		for i := range resp.ToolCalls {
			call := &resp.ToolCalls[i]
			normalizeToolCall(call, step, i)
			result := a.runTool(ctx, scope, &done, *call)
			a.log.DebugContext(ctx, "tool call",
				"user_id", userID, "tool", call.Function.Name, "args", call.Function.Arguments, "result", result)
			msgs = append(msgs, llm.Message{Role: llm.RoleTool, ToolCallID: call.ID, Content: result})
		}
	}

	a.log.WarnContext(ctx, "agent step limit reached", "user_id", userID, "max_steps", a.maxSteps)
	return done.summaryOr(fallbackReply), nil
}

// normalizeToolCall дополняет поля, которые некоторые совместимые провайдеры не присылают,
// но требуют в следующем запросе (id для связи с ответом инструмента, type).
func normalizeToolCall(call *llm.ToolCall, step, idx int) {
	if call.Type == "" {
		call.Type = "function"
	}
	if call.ID == "" {
		call.ID = fmt.Sprintf("call_%d_%d", step, idx)
	}
}

// journal — изменения трат за один ход агента.
type journal struct {
	entries []string
}

func (j *journal) record(action string, e domain.Expense) {
	what := e.Comment
	if what == "" {
		what = e.Category.Label()
	}
	j.entries = append(j.entries, fmt.Sprintf("%s: %s — %s (%s), %s",
		action, what, e.Amount, e.Category.Label(), e.SpentAt.Format("02.01.2006")))
}

func (j *journal) empty() bool { return len(j.entries) == 0 }

func (j *journal) summary() string {
	return "Готово, но ассистент не смог сформулировать ответ. Сделано:\n• " + strings.Join(j.entries, "\n• ")
}

func (j *journal) summaryOr(fallback string) string {
	if j.empty() {
		return fallback
	}
	return j.summary()
}
