// Package agent — ИИ-агент, выполняющий CRUD над тратами пользователя по тексту на естественном языке.
package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"finance-ai-bot/internal/agent/llm"
	"finance-ai-bot/internal/agent/prompts"
	"finance-ai-bot/internal/agent/tools"
	"finance-ai-bot/internal/domain"
	"finance-ai-bot/internal/usecase/expense"
)

const (
	fallbackReply = "Не получилось обработать запрос. Попробуйте сформулировать иначе, например: «кофе 350»."
	// unconfirmedReply — модель дважды заявила об изменении, не вызвав инструмент.
	unconfirmedReply = "Ничего не сохранено: не получилось выполнить запрос. Попробуйте ещё раз, например: «такси 1200 вчера»."
	// unconfirmedNudge — подсказка модели, которая ответила «Записал…» без вызова инструмента.
	unconfirmedNudge = "Ты ответил, что изменил траты, но в этом ходе не вызвал ни одного инструмента — ничего не сохранено. " +
		"Если пользователь просит записать, изменить или удалить трату, вызови нужный инструмент. Если данных не хватает — переспроси."
)

// changeVerbs — начала ответов, которыми модель подтверждает изменение трат.
var changeVerbs = []string{"записал", "изменил", "исправил", "удалил"}

// ExpenseUseCase — сценарии учёта трат, которые агент вызывает через инструменты.
type ExpenseUseCase = tools.ExpenseUseCase

type Agent struct {
	llm      llm.Client
	tools    *tools.Set
	loc      *time.Location
	maxSteps int
	now      func() time.Time
	log      *slog.Logger
}

type Config struct {
	Location *time.Location
	MaxSteps int
}

func New(client llm.Client, expenses ExpenseUseCase, cfg Config, log *slog.Logger) *Agent {
	return &Agent{
		llm:      client,
		tools:    tools.NewSet(expenses, cfg.Location, log),
		loc:      cfg.Location,
		maxSteps: cfg.MaxSteps,
		now:      time.Now,
		log:      log,
	}
}

// Reply обрабатывает сообщение пользователя и возвращает ответ для него.
//
// userID берётся только из Telegram-апдейта и замыкается в области доступа инструментов:
// модель не передаёт и не может подменить владельца трат.
func (a *Agent) Reply(ctx context.Context, userID int64, history []domain.ChatMessage, text string) (string, error) {
	system, err := prompts.System(prompts.NewCalendar(a.now(), a.loc))
	if err != nil {
		return "", err
	}
	var done journal
	turn := tools.Turn{Scope: expense.UserScope(userID), Journal: &done}

	msgs := make([]llm.Message, 0, len(history)+2)
	msgs = append(msgs, llm.Message{Role: llm.RoleSystem, Content: system})
	for _, m := range history {
		msgs = append(msgs, llm.Message{Role: m.Role, Content: m.Content})
	}
	msgs = append(msgs, llm.Message{Role: llm.RoleUser, Content: text})

	var nudged bool
	for step := range a.maxSteps {
		resp, err := a.llm.Chat(ctx, msgs, a.tools.Definitions())
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
			if reply, final := a.textReply(ctx, userID, resp.Content, &done, nudged); final {
				return reply, nil
			}
			nudged = true
			msgs = append(msgs, llm.Message{Role: llm.RoleSystem, Content: unconfirmedNudge})
			continue
		}

		for i := range resp.ToolCalls {
			call := &resp.ToolCalls[i]
			normalizeToolCall(call, step, i)
			result := a.tools.Run(ctx, turn, *call)
			a.log.DebugContext(ctx, "tool call",
				"user_id", userID, "tool", call.Function.Name, "args", call.Function.Arguments, "result", result)
			msgs = append(msgs, llm.Message{Role: llm.RoleTool, ToolCallID: call.ID, Content: result})
		}
	}

	a.log.WarnContext(ctx, "agent step limit reached", "user_id", userID, "max_steps", a.maxSteps)
	return done.summaryOr(fallbackReply), nil
}

// textReply разбирает ответ модели без вызовов инструментов.
// final == false — модель заявила об изменении, которого не было, и ей нужна ещё попытка.
func (a *Agent) textReply(ctx context.Context, userID int64, content string, done *journal, nudged bool) (reply string, final bool) {
	reply = strings.TrimSpace(content)
	switch {
	case reply == "":
		return done.summaryOr(fallbackReply), true
	// Модель может скопировать «Записал…» из истории, ничего не сохранив.
	case !done.empty() || !claimsChange(reply):
		return reply, true
	case nudged:
		a.log.WarnContext(ctx, "model claimed a change without calling a tool", "user_id", userID)
		return unconfirmedReply, true
	}
	return "", false
}

// claimsChange — ответ начинается с подтверждения изменения («Записал…», «Готово, удалил…»).
func claimsChange(reply string) bool {
	s := strings.ToLower(strings.TrimSpace(reply))
	if rest, ok := strings.CutPrefix(s, "готово"); ok {
		s = strings.TrimLeft(rest, " ,.!:—-")
	}
	for _, v := range changeVerbs {
		if strings.HasPrefix(s, v) {
			return true
		}
	}
	return false
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

// journal — изменения трат за один ход агента; реализует tools.Journal.
type journal struct {
	entries []string
}

func (j *journal) Record(action string, e domain.Expense) {
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
