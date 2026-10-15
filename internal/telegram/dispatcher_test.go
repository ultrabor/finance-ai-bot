package telegram

import (
	"sync"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"
)

func update(userID int64, text string) *models.Update {
	return &models.Update{Message: &models.Message{From: &models.User{ID: userID}, Text: text}}
}

// Сообщения одного пользователя обрабатываются по порядку, и медленный пользователь не блокирует других.
func TestDispatcherOrderAndIsolation(t *testing.T) {
	var (
		mu      sync.Mutex
		got     = map[int64][]string{}
		release = make(chan struct{})
		fastHit = make(chan struct{})
	)
	d := newDispatcher(func(u *models.Update) {
		id := u.Message.From.ID
		if id == 1 {
			<-release // «медленный» пользователь ждёт LLM
		} else {
			close(fastHit)
		}
		mu.Lock()
		got[id] = append(got[id], u.Message.Text)
		mu.Unlock()
	})

	for _, text := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"} {
		if d.dispatch(1, update(1, text)) != dispatchQueued {
			t.Fatalf("dispatch %s rejected", text)
		}
	}
	d.dispatch(2, update(2, "other"))

	select {
	case <-fastHit:
	case <-time.After(5 * time.Second):
		t.Fatal("user 2 blocked by user 1")
	}
	// Закрываем только после того, как всё поставленное обработано: close выкидывает не начатое.
	for range 10 {
		release <- struct{}{}
	}
	d.close()

	if want := "abcdefghij"; joined(got[1]) != want {
		t.Errorf("user 1 order = %s, want %s", joined(got[1]), want)
	}
	if len(got[2]) != 1 {
		t.Errorf("user 2 got %v", got[2])
	}
}

func TestDispatcherQueueLimit(t *testing.T) {
	started := make(chan struct{}, 100)
	block := make(chan struct{})
	d := newDispatcher(func(*models.Update) {
		started <- struct{}{}
		<-block
	})
	d.queueSize = 3

	d.dispatch(1, update(1, "x"))
	<-started // первое сообщение в обработке
	for i := range d.queueSize {
		if r := d.dispatch(1, update(1, "x")); r != dispatchQueued {
			t.Fatalf("dispatch %d = %v, want queued", i, r)
		}
	}
	if r := d.dispatch(1, update(1, "x")); r != dispatchFull {
		t.Errorf("first overflow = %v, want dispatchFull", r)
	}
	if r := d.dispatch(1, update(1, "x")); r != dispatchFullNotified {
		t.Errorf("second overflow = %v, want dispatchFullNotified", r)
	}
	close(block)
	d.close()
	if d.dispatch(1, update(1, "late")) != dispatchClosed {
		t.Error("dispatch after close must be rejected")
	}
}

// Простаивающая горутина завершается и удаляет очередь; следующее сообщение
// того же пользователя создаёт её заново и порядок сохраняется.
func TestDispatcherIdleExit(t *testing.T) {
	processed := make(chan string, 10)
	d := newDispatcher(func(u *models.Update) { processed <- u.Message.Text })
	d.idleTimeout = time.Millisecond

	d.dispatch(1, update(1, "a"))
	if got := <-processed; got != "a" {
		t.Fatalf("processed %q", got)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		d.mu.Lock()
		_, alive := d.queues[1]
		d.mu.Unlock()
		if !alive {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("idle queue was not removed")
		}
		time.Sleep(time.Millisecond)
	}

	for _, text := range []string{"b", "c", "d"} {
		if d.dispatch(1, update(1, text)) != dispatchQueued {
			t.Fatalf("dispatch %s rejected", text)
		}
	}
	var got []string
	for range 3 {
		got = append(got, <-processed)
	}
	if joined(got) != "bcd" {
		t.Errorf("order after idle exit = %v", got)
	}
	d.close()
}

// close выкидывает не начатые сообщения и ждёт сообщение, которое уже в обработке.
func TestDispatcherCloseDropsQueued(t *testing.T) {
	var (
		processed []string
		started   = make(chan struct{})
		release   = make(chan struct{})
		dropped   = make(chan []string, 1)
	)
	d := newDispatcher(func(u *models.Update) {
		if u.Message.Text == "a" {
			close(started)
			<-release
		}
		processed = append(processed, u.Message.Text)
	})
	d.onDrop = func(userID int64, us []*models.Update) {
		var texts []string
		for _, u := range us {
			texts = append(texts, u.Message.Text)
		}
		dropped <- texts
	}

	d.dispatch(1, update(1, "a"))
	<-started
	for _, text := range []string{"b", "c", "d"} {
		d.dispatch(1, update(1, text))
	}

	closed := make(chan struct{})
	go func() {
		d.close()
		close(closed)
	}()
	if got := <-dropped; joined(got) != "bcd" {
		t.Errorf("dropped = %v, want b c d", got)
	}
	select {
	case <-closed:
		t.Fatal("close returned before in-flight message finished")
	default:
	}
	close(release)
	<-closed

	if joined(processed) != "a" {
		t.Errorf("processed = %v, want only a", processed)
	}
}

func joined(ss []string) string {
	out := ""
	for _, s := range ss {
		out += s
	}
	return out
}
