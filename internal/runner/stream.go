package runner

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// The answer of image.build is a stream (decision P7-10), so that the proctor can read the log of a build while it goes: `application/x-ndjson`, one
// JSON object per line, each one of
//
//	{"log": "text"}                    what the build printed since the last message
//	{"result": {...}}                  the build's result, whatever way it ended: the last message
//	{"error": "words", "status": 500}  a failure of the runner's own after the stream began: the last message
//
// A request the runner cannot take is refused before the stream begins, with an HTTP status and {"error": ...} as every other operation.

const (
	// streamEvery is how long what a build printed waits to be sent with what follows it, and streamBytes how much waits at most.
	streamEvery = 250 * time.Millisecond
	streamBytes = 16 << 10
)

// streamWriter sends what is written to it as {"log"} messages, a few times a second at most, and ends the stream with the result or the failure.
type streamWriter struct {
	mu     sync.Mutex
	w      http.ResponseWriter
	buf    []byte
	timer  *time.Timer
	began  bool
	closed bool
}

func newStreamWriter(w http.ResponseWriter) *streamWriter { return &streamWriter{w: w} }

// begin sends the status and the headers, once.
func (s *streamWriter) begin() {
	if s.began {
		return
	}
	s.began = true
	s.w.Header().Set("Content-Type", "application/x-ndjson")
	s.w.Header().Set("Cache-Control", "no-store")
	s.w.Header().Set("X-Content-Type-Options", "nosniff")
	s.w.WriteHeader(http.StatusOK)
}

// send writes one message and sends it on its way. The connection may be gone: the build is stopped by its request's context, not by this.
func (s *streamWriter) send(message any) {
	s.begin()
	_ = json.NewEncoder(s.w).Encode(message)
	_ = http.NewResponseController(s.w).Flush()
}

func (s *streamWriter) flushLocked() {
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	if s.closed || len(s.buf) == 0 {
		return
	}
	text := string(s.buf)
	s.buf = s.buf[:0]
	s.send(map[string]string{"log": text})
}

func (s *streamWriter) timerFired() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.timer = nil
	s.flushLocked()
}

func (s *streamWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return len(p), nil
	}
	s.buf = append(s.buf, p...)
	switch {
	case len(s.buf) >= streamBytes:
		s.flushLocked()
	case s.timer == nil:
		s.timer = time.AfterFunc(streamEvery, s.timerFired)
	}
	return len(p), nil
}

// started says the stream has begun: something was sent, so the status can no longer change.
func (s *streamWriter) started() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flushLocked()
	return s.began
}

// finish sends what is left and the result.
func (s *streamWriter) finish(result BuildResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flushLocked()
	s.send(map[string]any{"result": result})
}

// fail sends what is left and the failure.
func (s *streamWriter) fail(status int, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flushLocked()
	s.send(map[string]any{"error": message, "status": status})
}

// close stops the timer: nothing is written once the handler has returned.
func (s *streamWriter) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
}
