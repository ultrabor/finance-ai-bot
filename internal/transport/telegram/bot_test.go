package telegram

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/go-telegram/bot/models"

	"finance-ai-bot/internal/domain"
)

type fakeSender struct {
	mu   sync.Mutex
	sent []string
	// block — если задан, Send ждёт его закрытия (медленный Telegram).
	block chan struct{}
	// sentCh — если задан, получает каждый отправляемый текст.
	sentCh chan string
}

func (s *fakeSender) Send(ctx context.Context, _ int64, text string) error {
	s.mu.Lock()
	s.sent = append(s.sent, text)
	s.mu.Unlock()
	if s.sentCh != nil {
		s.sentCh <- text
	}
	if s.block != nil {
		select {
		case <-s.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (s *fakeSender) count(text string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, t := range s.sent {
		if t == text {
			n++
		}
	}
	return n
}

func (s *fakeSender) Typing(context.Context, int64) {}

type fakeAgent struct {
	reply string
	err   error
	calls int
}

func (a *fakeAgent) Reply(context.Context, int64, []domain.ChatMessage, string) (string, error) {
	a.calls++
	return a.reply, a.err
}

type fakeHistory struct{ saved []domain.ChatMessage }

func (h *fakeHistory) Append(_ context.Context, _ int64, msgs ...domain.ChatMessage) error {
	h.saved = append(h.saved, msgs...)
	return nil
}

func (h *fakeHistory) Recent(context.Context, int64, time.Time, int) ([]domain.ChatMessage, error) {
	return nil, nil
}

type fakeUsers struct{ registered []int64 }

func (u *fakeUsers) Upsert(_ context.Context, user domain.User) (domain.User, error) {
	u.registered = append(u.registered, user.TelegramID)
	return user, nil
}

const familyID int64 = 111

type env struct {
	h       *handler
	out     *fakeSender
	agent   *fakeAgent
	history *fakeHistory
	users   *fakeUsers
}

func newEnv() *env {
	e := &env{out: &fakeSender{}, agent: &fakeAgent{reply: "Записал"}, history: &fakeHistory{}, users: &fakeUsers{}}
	e.h = newHandler([]int64{familyID}, e.agent, e.users, e.history, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return e
}

func message(from int64, chatType models.ChatType, text string) *models.Message {
	return &models.Message{
		From: &models.User{ID: from, FirstName: "u"},
		Chat: models.Chat{ID: from, Type: chatType},
		Text: text,
	}
}

func (e *env) handle(u *models.Update) { e.h.handle(context.Background(), e.out, u) }

func TestHandleMessage(t *testing.T) {
	e := newEnv()
	e.handle(&models.Update{Message: message(familyID, models.ChatTypePrivate, "кофе 350")})

	if e.agent.calls != 1 || len(e.out.sent) != 1 || e.out.sent[0] != "Записал" {
		t.Fatalf("calls=%d sent=%q", e.agent.calls, e.out.sent)
	}
	if len(e.history.saved) != 2 || e.history.saved[0].Content != "кофе 350" {
		t.Errorf("history = %+v", e.history.saved)
	}
	if len(e.users.registered) != 1 {
		t.Errorf("user not registered")
	}
}

func TestRejectsNonFamily(t *testing.T) {
	e := newEnv()
	e.handle(&models.Update{Message: message(999, models.ChatTypePrivate, "кофе 350")})
	if e.agent.calls != 0 || len(e.users.registered) != 0 || e.out.sent[0] != replyNotFamily {
		t.Fatalf("calls=%d registered=%v sent=%q", e.agent.calls, e.users.registered, e.out.sent)
	}
}

func TestIgnoresGroupChats(t *testing.T) {
	e := newEnv()
	e.handle(&models.Update{Message: message(familyID, models.ChatTypeGroup, "покажи траты")})
	if e.agent.calls != 0 || len(e.out.sent) != 0 {
		t.Fatalf("group message handled: calls=%d sent=%q", e.agent.calls, e.out.sent)
	}
}

func TestEditedMessageHint(t *testing.T) {
	e := newEnv()
	e.handle(&models.Update{EditedMessage: message(familyID, models.ChatTypePrivate, "кофе 450")})
	if e.agent.calls != 0 || len(e.out.sent) != 1 || e.out.sent[0] != replyEdited {
		t.Fatalf("calls=%d sent=%q", e.agent.calls, e.out.sent)
	}
}

func TestStartCommandVariants(t *testing.T) {
	for _, text := range []string{"/start", "/start@FamilyBot", "/start ref123", "/help"} {
		e := newEnv()
		e.handle(&models.Update{Message: message(familyID, models.ChatTypePrivate, text)})
		if e.agent.calls != 0 || len(e.out.sent) != 1 || e.out.sent[0] != greeting {
			t.Errorf("%q: calls=%d sent=%q", text, e.agent.calls, e.out.sent)
		}
	}
}

func TestAgentErrorNotSavedToHistory(t *testing.T) {
	e := newEnv()
	e.agent.err = errors.New("llm down")
	e.handle(&models.Update{Message: message(familyID, models.ChatTypePrivate, "кофе 350")})
	if len(e.history.saved) != 0 || e.out.sent[0] != replyLLMDown {
		t.Fatalf("history=%+v sent=%q", e.history.saved, e.out.sent)
	}
}

func TestTruncateUTF16(t *testing.T) {
	short := "привет"
	if truncateUTF16(short, 10) != short {
		t.Error("short text changed")
	}
	// Эмодзи занимает 2 единицы UTF-16: 3000 эмодзи = 6000 > 4096.
	long := strings.Repeat("😀", 3000)
	got := truncateUTF16(long, telegramMaxLen)
	if n := len(utf16.Encode([]rune(got))); n > telegramMaxLen {
		t.Errorf("len = %d UTF-16 units, want <= %d", n, telegramMaxLen)
	}
}

// panicAgent паникует на первом вызове, дальше отвечает нормально.
type panicAgent struct{ calls int }

func (a *panicAgent) Reply(context.Context, int64, []domain.ChatMessage, string) (string, error) {
	a.calls++
	if a.calls == 1 {
		panic("boom")
	}
	return "Записал", nil
}

func private(text string) *models.Update {
	return &models.Update{Message: message(familyID, models.ChatTypePrivate, text)}
}

// Паника в агенте не роняет очередь пользователя: он получает replyFailure,
// а следующее сообщение обрабатывается как обычно.
func TestPanicRecovered(t *testing.T) {
	e := newEnv()
	e.h.agent = &panicAgent{}
	e.out.sentCh = make(chan string, 2)
	d := newDispatcher(func(u *models.Update) { e.h.handle(context.Background(), e.out, u) })
	d.dispatch(familyID, private("кофе 350"))
	d.dispatch(familyID, private("такси 1200"))
	// close выкидывает не начатое — дожидаемся обоих ответов.
	<-e.out.sentCh
	<-e.out.sentCh
	d.close()

	if len(e.out.sent) != 2 || e.out.sent[0] != replyFailure || e.out.sent[1] != "Записал" {
		t.Fatalf("sent = %q", e.out.sent)
	}
	if len(e.history.saved) != 2 || e.history.saved[0].Content != "такси 1200" {
		t.Errorf("history = %+v", e.history.saved)
	}
}

// testBot собирает Bot без Telegram API; process блокируется, пока не отпустят release.
func testBot(out *fakeSender) (b *Bot, started chan string, release chan struct{}) {
	started = make(chan string, 100)
	release = make(chan struct{})
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	b = &Bot{out: out, log: log, handler: newHandler([]int64{familyID}, &fakeAgent{}, &fakeUsers{}, &fakeHistory{}, log)}
	b.dispatcher = newDispatcher(func(u *models.Update) {
		started <- u.Message.Text
		<-release
	})
	b.dispatcher.onDrop = b.droppedOnStop
	return b, started, release
}

// Переполнение очереди: одно предупреждение на эпизод, и route не ждёт отправки.
func TestRouteBusyNoticeOncePerOverflow(t *testing.T) {
	out := &fakeSender{block: make(chan struct{})}
	b, started, release := testBot(out)
	b.dispatcher.queueSize = 1

	b.route(private("1"))
	<-started // «1» в обработке, очередь пуста

	routed := make(chan struct{})
	go func() {
		defer close(routed)
		for _, text := range []string{"2", "3", "4", "5", "6", "7", "8", "9", "10"} {
			b.route(private(text)) // «2» в очереди, остальные выкинуты
		}
	}()
	select {
	case <-routed:
	case <-time.After(5 * time.Second):
		t.Fatal("route blocked on sending busy notice")
	}
	close(out.block)
	b.notices.Wait()
	if n := out.count(replyBusy); n != 1 {
		t.Fatalf("busy notices = %d, want 1", n)
	}

	// Очередь разобрана — новый эпизод переполнения снова предупреждает (один раз).
	release <- struct{}{}
	if got := <-started; got != "2" {
		t.Fatalf("started %q, want 2", got)
	}
	for _, text := range []string{"11", "12", "13"} {
		b.route(private(text))
	}
	b.notices.Wait()
	if n := out.count(replyBusy); n != 2 {
		t.Fatalf("busy notices = %d, want 2", n)
	}

	close(release)
	b.stop()
}

// При остановке не начатые сообщения выкидываются, пользователю один раз приходит
// просьба повторить, а начатое доводится до конца.
func TestStopDropsQueuedAndNotifies(t *testing.T) {
	out := &fakeSender{sentCh: make(chan string, 10)}
	b, started, release := testBot(out)

	b.route(private("1"))
	<-started
	for _, text := range []string{"2", "3", "4"} {
		b.route(private(text))
	}

	stopped := make(chan struct{})
	go func() {
		b.stop()
		close(stopped)
	}()
	if got := <-out.sentCh; got != replyRestart {
		t.Fatalf("sent %q, want restart notice", got)
	}
	select {
	case <-stopped:
		t.Fatal("stop returned before in-flight message finished")
	default:
	}
	close(release)
	<-stopped

	if len(started) != 0 {
		t.Errorf("dropped messages were processed: %d", len(started))
	}
	if n := out.count(replyRestart); n != 1 || len(out.sent) != 1 {
		t.Errorf("sent = %q", out.sent)
	}
}
