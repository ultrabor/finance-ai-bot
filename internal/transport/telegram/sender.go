package telegram

import (
	"context"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

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
