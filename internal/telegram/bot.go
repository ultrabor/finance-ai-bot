// Package telegram связывает Telegram-бота с ИИ-агентом.
package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"finance-ai-bot/internal/domain"
	"finance-ai-bot/internal/usecase/user"
)

const (
	historyLimit  = 10
	historyWindow = 30 * time.Minute
	replyTimeout  = 90 * time.Second
	maxMessageLen = 2000
	// Telegram ограничивает текст 4096 символами в UTF-16.
	telegramMaxLen = 4096
)

const greeting = `Привет! Я веду учёт ваших трат. Пишите обычным текстом:

• кофе 350
• такси 1200 вчера
• сколько потратил на еду в этом месяце
• покажи траты за неделю
• последнее было не 350, а 450
• удали такси

Ваши траты видите только вы и глава семьи в веб-панели.`

const (
	replyNotFamily = "Этот бот доступен только членам семьи."
	replyTextOnly  = "Пока я понимаю только текст. Напишите, например: «кофе 350»."
	replyTooLong   = "Сообщение слишком длинное, напишите короче."
	replyEdited    = "Изменённые сообщения я не обрабатываю. Напишите исправление новым сообщением, например: «последнее было не 350, а 450»."
	replyFailure   = "Что-то пошло не так, попробуйте ещё раз чуть позже."
	replyLLMDown   = "Не удалось связаться с ИИ-ассистентом. Попробуйте ещё раз через минуту."
	replyBusy      = "Слишком много сообщений подряд — дождитесь ответа на предыдущие."
	replyRestart   = "Бот перезапускается — отправьте сообщение ещё раз через минуту."
)

// noticeTimeout ограничивает служебные уведомления (переполнение, перезапуск):
// они не должны надолго задерживать остановку.
const noticeTimeout = 5 * time.Second

// Agent — ИИ-агент, отвечающий на сообщение пользователя.
type Agent interface {
	Reply(ctx context.Context, userID int64, history []domain.ChatMessage, text string) (string, error)
}

// ChatHistory хранит последние реплики диалога.
type ChatHistory interface {
	Append(ctx context.Context, userID int64, msgs ...domain.ChatMessage) error
	Recent(ctx context.Context, userID int64, since time.Time, limit int) ([]domain.ChatMessage, error)
}

// UserRegistry регистрирует члена семьи при первом сообщении.
type UserRegistry interface {
	Upsert(ctx context.Context, u domain.User) (domain.User, error)
}

type Bot struct {
	api        *tgbot.Bot
	out        sender
	handler    *handler
	dispatcher *dispatcher
	log        *slog.Logger
	// notices — фоновые служебные уведомления; Start дожидается их перед выходом.
	notices sync.WaitGroup
}

type Config struct {
	Token      string
	AllowedIDs []int64
}

func New(cfg Config, agent Agent, users *user.Service, history ChatHistory, log *slog.Logger) (*Bot, error) {
	b := &Bot{handler: newHandler(cfg.AllowedIDs, agent, users, history, log), log: log}
	b.dispatcher = newDispatcher(func(u *models.Update) {
		// Обработка не зависит от контекста поллинга: при остановке начатое доводим до ответа.
		b.handler.handle(context.Background(), b.out, u)
	})
	b.dispatcher.onDrop = b.droppedOnStop

	api, err := tgbot.New(cfg.Token,
		// Один синхронный воркер только раскладывает апдейты по очередям пользователей —
		// так сохраняется порядок сообщений, а сама обработка идёт в dispatcher.
		tgbot.WithNotAsyncHandlers(),
		tgbot.WithWorkers(1),
		tgbot.WithDefaultHandler(func(_ context.Context, _ *tgbot.Bot, u *models.Update) { b.route(u) }),
		tgbot.WithErrorsHandler(func(err error) { log.Error("telegram", "error", err) }),
	)
	if err != nil {
		return nil, fmt.Errorf("create telegram bot: %w", err)
	}
	b.api = api
	b.out = &telegramSender{api: api}
	return b, nil
}

// Start запускает long polling и блокируется до отмены ctx, затем дожидается
// сообщений, уже взятых в обработку; не начатые выкидываются (их просят прислать снова).
func (b *Bot) Start(ctx context.Context) {
	b.log.Info("telegram bot started", "family_whitelist", len(b.handler.allowed))
	b.api.Start(ctx)
	b.log.Info("waiting for in-flight messages")
	b.stop()
}

func (b *Bot) stop() {
	b.dispatcher.close()
	b.notices.Wait()
}

func (b *Bot) route(u *models.Update) {
	msg := u.Message
	if msg == nil {
		msg = u.EditedMessage
	}
	if msg == nil || msg.From == nil {
		return
	}
	// route работает в единственном воркере библиотеки — здесь нельзя ждать сеть.
	switch b.dispatcher.dispatch(msg.From.ID, u) {
	case dispatchFull:
		b.log.Warn("user queue is full, message dropped", "user_id", msg.From.ID)
		if b.handler.accepts(msg) && b.handler.isFamily(msg.From.ID) {
			b.notify(msg.Chat.ID, replyBusy)
		}
	case dispatchFullNotified:
		b.log.Warn("user queue is full, message dropped", "user_id", msg.From.ID)
	case dispatchClosed:
		b.log.Warn("bot is stopping, message dropped", "user_id", msg.From.ID)
	}
}

// droppedOnStop просит пользователя повторить сообщения, выкинутые при остановке.
func (b *Bot) droppedOnStop(userID int64, dropped []*models.Update) {
	b.log.Warn("queued messages dropped on shutdown", "user_id", userID, "count", len(dropped))
	for _, u := range dropped {
		if msg := u.Message; msg != nil && b.handler.accepts(msg) && b.handler.isFamily(msg.From.ID) {
			b.notify(msg.Chat.ID, replyRestart)
			return
		}
	}
}

// notify отправляет служебное уведомление в фоне с коротким таймаутом.
func (b *Bot) notify(chatID int64, text string) {
	b.notices.Add(1)
	go func() {
		defer b.notices.Done()
		ctx, cancel := context.WithTimeout(context.Background(), noticeTimeout)
		defer cancel()
		if err := b.out.Send(ctx, chatID, text); err != nil {
			b.log.Error("send telegram notice", "chat_id", chatID, "error", err)
		}
	}()
}

// sender — отправка сообщений; в тестах подменяется.
type sender interface {
	Send(ctx context.Context, chatID int64, text string) error
	Typing(ctx context.Context, chatID int64)
}

type telegramSender struct{ api *tgbot.Bot }

func (s *telegramSender) Send(ctx context.Context, chatID int64, text string) error {
	_, err := s.api.SendMessage(ctx, &tgbot.SendMessageParams{ChatID: chatID, Text: text})
	return err
}

func (s *telegramSender) Typing(ctx context.Context, chatID int64) {
	_, _ = s.api.SendChatAction(ctx, &tgbot.SendChatActionParams{ChatID: chatID, Action: models.ChatActionTyping})
}

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
