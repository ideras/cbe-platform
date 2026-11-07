package sessionwatch

import (
	"context"
	"flag"
	"testing"
	"time"
)

var live = flag.Duration("live", 0, "watch the real system for this long, printing each report (TestLiveWatch)")

// Watching the real system by hand, on any platform: start it, switch to another user (or on Linux to a text console) and back,
// and read the reports with their times. It does nothing unless -live is given.
//
//	go test ./internal/sessionwatch -run TestLiveWatch -v -live 2m
func TestLiveWatch(t *testing.T) {
	if *live <= 0 {
		t.Skip("give -live <duration> to watch the real system")
	}
	ctx, cancel := context.WithTimeout(t.Context(), *live)
	defer cancel()
	stamp := func() string { return time.Now().Format("15:04:05.000") }
	t.Logf("%s watching for %v: switch to another user and back", stamp(), *live)
	started := time.Now()
	Watch(ctx, func(active bool, reason Reason) {
		if active {
			t.Logf("%s the session is on screen again", stamp())
			return
		}
		t.Logf("%s the session left the screen: %s", stamp(), reason)
	})
	if time.Since(started) < *live-time.Second {
		t.Logf("%s the watch ended on its own: the system has nothing to say about this session (a remote session, or no record of the console)", stamp())
		return
	}
	t.Logf("%s done", stamp())
}
