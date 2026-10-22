// Package telegram связывает Telegram-бота с ИИ-агентом.
package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"finance-ai-bot/internal/domain"
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

func New(cfg Config, agent Agent, users UserRegistry, history ChatHistory, log *slog.Logger) (*Bot, error) {
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
