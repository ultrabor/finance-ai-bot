package telegram

import (
	"context"
	"log/slog"
	"runtime/debug"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/go-telegram/bot/models"

	"finance-ai-bot/internal/domain"
)

const (
	historyLimit  = 10
	historyWindow = 30 * time.Minute
	replyTimeout  = 90 * time.Second
	maxMessageLen = 2000
	// Telegram ограничивает текст 4096 символами в UTF-16.
	telegramMaxLen = 4096
)

type handler struct {
	agent   Agent
	users   UserRegistry
	history ChatHistory
	allowed map[int64]bool
	log     *slog.Logger
}

func newHandler(allowedIDs []int64, agent Agent, users UserRegistry, history ChatHistory, log *slog.Logger) *handler {
	h := &handler{
		agent:   agent,
		users:   users,
		history: history,
		allowed: make(map[int64]bool, len(allowedIDs)),
		log:     log,
	}
	for _, id := range allowedIDs {
		h.allowed[id] = true
	}
	return h
}

// handle обрабатывает один апдейт. Сообщения одного пользователя вызываются последовательно (dispatcher).
func (h *handler) handle(parent context.Context, out sender, update *models.Update) {
	ctx, cancel := context.WithTimeout(parent, replyTimeout)
	defer cancel()

	defer func() {
		// Паника в агенте или хранилище не должна ронять бота для всей семьи.
		if r := recover(); r != nil {
			h.log.Error("panic while handling update", "panic", r, "stack", string(debug.Stack()))
			if msg := update.Message; msg != nil {
				h.send(ctx, out, msg.Chat.ID, replyFailure)
			}
		}
	}()

	if edited := update.EditedMessage; edited != nil {
		if h.accepts(edited) && h.isFamily(edited.From.ID) {
			h.send(ctx, out, edited.Chat.ID, replyEdited)
		}
		return
	}

	msg := update.Message
	if msg == nil || !h.accepts(msg) {
		return
	}
	from := msg.From

	if !h.isFamily(from.ID) {
		h.log.Warn("message from non-family user", "user_id", from.ID, "username", from.Username)
		h.send(ctx, out, msg.Chat.ID, replyNotFamily)
		return
	}

	text := strings.TrimSpace(msg.Text)
	switch {
	case text == "":
		h.send(ctx, out, msg.Chat.ID, replyTextOnly)
		return
	case isCommand(text, "start", "help"):
		h.registerUser(ctx, from)
		h.send(ctx, out, msg.Chat.ID, greeting)
		return
	case len([]rune(text)) > maxMessageLen:
		h.send(ctx, out, msg.Chat.ID, replyTooLong)
		return
	}

	if !h.registerUser(ctx, from) {
		h.send(ctx, out, msg.Chat.ID, replyFailure)
		return
	}

	stopTyping := keepTyping(ctx, out, msg.Chat.ID)
	defer stopTyping()

	history, err := h.history.Recent(ctx, from.ID, time.Now().Add(-historyWindow), historyLimit)
	if err != nil {
		h.log.Error("load chat history", "user_id", from.ID, "error", err)
	}

	start := time.Now()
	reply, err := h.agent.Reply(ctx, from.ID, history, text)
	if err != nil {
		h.log.Error("agent reply", "user_id", from.ID, "error", err, "duration", time.Since(start))
		h.send(ctx, out, msg.Chat.ID, replyLLMDown)
		return
	}
	h.log.Info("message handled", "user_id", from.ID, "duration", time.Since(start))

	// Отдельный таймаут: ответ (в том числе сводка сделанного) должен попасть в историю,
	// даже если время на обработку уже истекло.
	saveCtx, cancelSave := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancelSave()
	if err := h.history.Append(saveCtx, from.ID,
		domain.ChatMessage{Role: domain.ChatRoleUser, Content: text},
		domain.ChatMessage{Role: domain.ChatRoleAssistant, Content: reply},
	); err != nil {
		h.log.Error("save chat history", "user_id", from.ID, "error", err)
	}
	h.send(ctx, out, msg.Chat.ID, reply)
}

// accepts пропускает только личные сообщения от людей: в группе ответ с тратами увидят все участники.
func (h *handler) accepts(msg *models.Message) bool {
	return msg.From != nil && !msg.From.IsBot && msg.Chat.Type == models.ChatTypePrivate
}

func (h *handler) isFamily(userID int64) bool {
	return len(h.allowed) == 0 || h.allowed[userID]
}

func (h *handler) registerUser(ctx context.Context, u *models.User) bool {
	_, err := h.users.Upsert(ctx, domain.User{
		TelegramID: u.ID,
		Username:   u.Username,
		FirstName:  u.FirstName,
		LastName:   u.LastName,
	})
	if err != nil {
		h.log.Error("register user", "user_id", u.ID, "error", err)
		return false
	}
	return true
}

func (h *handler) send(ctx context.Context, out sender, chatID int64, text string) {
	// Ответ отправляем даже если время на обработку истекло.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := out.Send(ctx, chatID, truncateUTF16(text, telegramMaxLen)); err != nil {
		h.log.Error("send telegram message", "chat_id", chatID, "error", err)
	}
}

// keepTyping показывает «печатает…», пока агент думает (статус живёт ~5 секунд).
func keepTyping(ctx context.Context, out sender, chatID int64) func() {
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(4 * time.Second)
		defer ticker.Stop()
		for {
			out.Typing(ctx, chatID)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return cancel
}

// isCommand распознаёт /cmd, /cmd@BotName и /cmd с параметром.
func isCommand(text string, names ...string) bool {
	if !strings.HasPrefix(text, "/") {
		return false
	}
	cmd, _, _ := strings.Cut(strings.Fields(text)[0], "@")
	for _, n := range names {
		if cmd == "/"+n {
			return true
		}
	}
	return false
}

// truncateUTF16 обрезает текст до limit единиц UTF-16 (так считает длину Telegram).
func truncateUTF16(text string, limit int) string {
	if len(utf16.Encode([]rune(text))) <= limit {
		return text
	}
	var (
		b    strings.Builder
		size int
	)
	for _, r := range text {
		n := utf16.RuneLen(r)
		if size+n > limit-1 {
			break
		}
		b.WriteRune(r)
		size += n
	}
	return b.String() + "…"
}
