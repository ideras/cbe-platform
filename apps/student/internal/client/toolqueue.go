package client

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// The calculator's events travel on a queue of their own (D12), never the monitor's focus queue: a burst of
// calculations cannot delay a focus loss or its screenshots. The queue holds at most 1,000 events and retries
// with the monitor's backoff; like the monitor it gives an event up after its attempts (counted as dropped) and
// does not retry 401 or 403. The queue is memory only, like all calculator state: an evaluation is never shown
// without being queued, but a crash or a forced close while offline loses what was queued (Appendix B).
const toolQueueLimit = 1000

type toolEvent struct {
	kind   string
	detail map[string]any
}

type toolQueue struct {
	ctx                context.Context
	http               *http.Client
	mu                 sync.Mutex
	queue              []*toolEvent
	base, token        string
	active             bool
	wake               chan struct{}
	retryMin, retryMax time.Duration
	dropped            int
}

func newToolQueue(ctx context.Context, httpClient *http.Client, retryMin, retryMax time.Duration) *toolQueue {
	return &toolQueue{ctx: ctx, http: httpClient, wake: make(chan struct{}, 1), retryMin: retryMin, retryMax: retryMax}
}

// start aims the queue at the session the student logged in to; a later login re-aims it.
func (q *toolQueue) start(base, token string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.base, q.token, q.active = base, token, true
	signal(q.wake)
}

// enqueue appends an event, or answers false when the queue is full: the caller refuses the evaluation instead
// of dropping the record (D12).
func (q *toolQueue) enqueue(kind string, detail map[string]any) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.queue) >= toolQueueLimit {
		return false
	}
	q.queue = append(q.queue, &toolEvent{kind: kind, detail: detail})
	signal(q.wake)
	return true
}

// full says the queue cannot take another record, so evaluations are refused until it drains.
func (q *toolQueue) full() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.queue) >= toolQueueLimit
}

// queued is how many events wait to be sent.
func (q *toolQueue) queued() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.queue)
}

func (q *toolQueue) run() {
	for {
		select {
		case <-q.ctx.Done():
			return
		case <-q.wake:
		}
		for {
			q.mu.Lock()
			if !q.active || len(q.queue) == 0 {
				q.mu.Unlock()
				break
			}
			event := q.queue[0]
			base, token := q.base, q.token
			q.mu.Unlock()

			var err error
			backoff := q.retryMin
			for attempt := 0; attempt < 3; attempt++ {
				if q.ctx.Err() != nil {
					return
				}
				var reply eventAck
				err = requestJSON(q.ctx, q.http, base, token, http.MethodPost, "/event", struct {
					Type   string         `json:"event_type"`
					Detail map[string]any `json:"detail"`
				}{event.kind, event.detail}, &reply)
				if err == nil && (reply.ID <= 0 || reply.ServerTime.IsZero()) {
					err = &apiFailure{message: "Event acknowledgment is invalid."}
				}
				if err == nil || !retryable(err) || attempt+1 == 3 {
					break
				}
				timer := time.NewTimer(backoff)
				select {
				case <-q.ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
				backoff = min(backoff*2, q.retryMax)
			}
			q.mu.Lock()
			if len(q.queue) > 0 && q.queue[0] == event {
				q.queue = q.queue[1:]
			}
			if err != nil {
				q.dropped++
			}
			q.mu.Unlock()
		}
	}
}
