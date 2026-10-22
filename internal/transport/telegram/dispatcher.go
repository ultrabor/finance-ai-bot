package telegram

import (
	"sync"
	"time"

	"github.com/go-telegram/bot/models"
)

const (
	// defaultQueueSize — сколько необработанных сообщений одного пользователя держим в очереди.
	defaultQueueSize = 20
	// defaultIdleTimeout — через сколько простоя горутина пользователя завершается.
	defaultIdleTimeout = time.Minute
)

// dispatchResult — итог постановки апдейта в очередь.
type dispatchResult int

const (
	dispatchQueued dispatchResult = iota
	// dispatchFull — очередь переполнена, пользователя ещё не предупреждали в этом эпизоде.
	dispatchFull
	// dispatchFullNotified — очередь переполнена, предупреждение уже отправлено.
	dispatchFullNotified
	dispatchClosed
)

type userQueue struct {
	ch chan *models.Update
	// overflowNotified — пользователя уже предупредили о переполнении; сбрасывается,
	// когда очередь разобрана до конца.
	overflowNotified bool
}

// dispatcher раскладывает апдейты по очередям пользователей: сообщения одного
// пользователя обрабатываются строго по порядку, разные пользователи — параллельно,
// и поток сообщений одного не задерживает остальных.
type dispatcher struct {
	process func(*models.Update)
	// onDrop вызывается при остановке для апдейтов, которые так и не начали обрабатывать.
	onDrop      func(userID int64, dropped []*models.Update)
	queueSize   int
	idleTimeout time.Duration

	mu     sync.Mutex
	queues map[int64]*userQueue
	closed bool
	wg     sync.WaitGroup
}

func newDispatcher(process func(*models.Update)) *dispatcher {
	return &dispatcher{
		process:     process,
		onDrop:      func(int64, []*models.Update) {},
		queueSize:   defaultQueueSize,
		idleTimeout: defaultIdleTimeout,
		queues:      map[int64]*userQueue{},
	}
}

// dispatch ставит апдейт в очередь пользователя.
func (d *dispatcher) dispatch(userID int64, u *models.Update) dispatchResult {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return dispatchClosed
	}
	q, ok := d.queues[userID]
	if !ok {
		q = &userQueue{ch: make(chan *models.Update, d.queueSize)}
		d.queues[userID] = q
		d.wg.Add(1)
		go d.run(userID, q)
	}
	select {
	case q.ch <- u:
		return dispatchQueued
	default:
		if q.overflowNotified {
			return dispatchFullNotified
		}
		q.overflowNotified = true
		return dispatchFull
	}
}

func (d *dispatcher) run(userID int64, q *userQueue) {
	defer d.wg.Done()
	idle := time.NewTimer(d.idleTimeout)
	defer idle.Stop()
	for {
		select {
		case u, ok := <-q.ch:
			if !ok {
				return
			}
			d.mu.Lock()
			if len(q.ch) == 0 {
				// Очередь разобрана — эпизод переполнения закончился.
				q.overflowNotified = false
			}
			d.mu.Unlock()
			d.process(u)
			idle.Reset(d.idleTimeout)
		case <-idle.C:
			// Удаляем очередь под блокировкой: dispatch не сможет положить в неё новый апдейт.
			d.mu.Lock()
			if len(q.ch) == 0 {
				delete(d.queues, userID)
				d.mu.Unlock()
				return
			}
			d.mu.Unlock()
			idle.Reset(d.idleTimeout)
		}
	}
}

// close перестаёт принимать апдейты, выкидывает ещё не начатые и ждёт,
// пока доработают сообщения, уже взятые в обработку.
func (d *dispatcher) close() {
	type drop struct {
		userID  int64
		updates []*models.Update
	}
	var drops []drop

	d.mu.Lock()
	if !d.closed {
		d.closed = true
		for userID, q := range d.queues {
			var dropped []*models.Update
		drain:
			for {
				// Без блокировки: горутина пользователя может забрать апдейт параллельно.
				select {
				case u := <-q.ch:
					dropped = append(dropped, u)
				default:
					break drain
				}
			}
			close(q.ch)
			if len(dropped) > 0 {
				drops = append(drops, drop{userID, dropped})
			}
		}
	}
	d.mu.Unlock()

	for _, dr := range drops {
		d.onDrop(dr.userID, dr.updates)
	}
	d.wg.Wait()
}
