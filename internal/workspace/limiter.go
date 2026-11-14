package workspace

import (
	"context"
	"sync"
	"time"
)

// bucket is a token bucket: it lets rate units a second through, in bursts of up to burst.
type bucket struct {
	mu     sync.Mutex
	rate   float64
	burst  float64
	tokens float64
	last   time.Time
}

func newBucket(rate, burst float64) *bucket {
	return &bucket{rate: rate, burst: burst, tokens: burst, last: time.Now()}
}

func (b *bucket) refill(now time.Time) {
	b.tokens = min(b.burst, b.tokens+now.Sub(b.last).Seconds()*b.rate)
	b.last = now
}

// allow takes n tokens if there are that many, and says whether it did.
func (b *bucket) allow(n float64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refill(time.Now())
	if b.tokens < n {
		return false
	}
	b.tokens -= n
	return true
}

// wait takes n tokens, waiting for them as long as it must. A request larger than the burst is let through once the bucket is full.
func (b *bucket) wait(ctx context.Context, n float64) error {
	for {
		b.mu.Lock()
		b.refill(time.Now())
		need := min(n, b.burst)
		if b.tokens >= need {
			b.tokens -= need
			b.mu.Unlock()
			return nil
		}
		delay := time.Duration((need - b.tokens) / b.rate * float64(time.Second))
		b.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(max(delay, time.Millisecond)):
		}
	}
}

// Limits of what one student's connection may do (plan, Phase 3 task 7: per-student rate limits and output throttling).
const (
	// Control messages from the student: ordinary use is a few a second, an autosave a second, a burst when a folder is opened.
	controlRate  = 100
	controlBurst = 200
	// Bytes of control messages a second (file contents), and keystrokes a second.
	controlBytesRate  = 4 << 20
	controlBytesBurst = 8 << 20
	inputRate         = 256 << 10
	inputBurst        = 512 << 10
	// Terminal output to the student: a program that prints without end is held to this.
	outputRate  = 2 << 20
	outputBurst = 1 << 20
	// MaxClientMessage is the largest WebSocket message the student may send.
	MaxClientMessage = 2 << 20
)
