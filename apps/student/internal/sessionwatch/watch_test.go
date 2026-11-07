package sessionwatch

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"
)

type reports struct {
	mu   sync.Mutex
	list []bool
}

func (r *reports) add(active bool) { r.mu.Lock(); r.list = append(r.list, active); r.mu.Unlock() }
func (r *reports) all() []bool     { r.mu.Lock(); defer r.mu.Unlock(); return slices.Clone(r.list) }

func eventually(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("the condition was not met in time")
}

func writeConsole(t *testing.T, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}

// The student switches to a text console and back: the session is reported as off screen, and as on screen again.
func TestASwitchToAnotherConsoleAndBackIsReported(t *testing.T) {
	path := filepath.Join(t.TempDir(), "active")
	writeConsole(t, path, "tty2\n")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var got reports
	go watch(ctx, path, 2*time.Millisecond, got.add)
	time.Sleep(30 * time.Millisecond)
	if all := got.all(); len(all) != 0 {
		t.Fatalf("reports while the session was on screen: %v", all)
	}
	writeConsole(t, path, "tty3\n")
	eventually(t, func() bool { return slices.Equal(got.all(), []bool{false}) })
	// Switching between other consoles is not the session coming back, and nothing is reported twice.
	writeConsole(t, path, "tty4\n")
	time.Sleep(30 * time.Millisecond)
	writeConsole(t, path, "tty2\n")
	eventually(t, func() bool { return slices.Equal(got.all(), []bool{false, true}) })
}

// Without a record of the console there is nothing to say: no report, rather than a report that the session is off screen.
func TestNothingIsReportedWhenTheConsoleCannotBeRead(t *testing.T) {
	var got reports
	done := make(chan struct{})
	go func() {
		watch(t.Context(), filepath.Join(t.TempDir(), "missing"), time.Millisecond, got.add)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the watch kept running without a record to read")
	}
	if all := got.all(); len(all) != 0 {
		t.Fatalf("reports = %v", all)
	}
}

// A record that cannot be read for a moment does not make the session look off screen.
func TestAMomentWithoutTheRecordIsNotASwitch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "active")
	writeConsole(t, path, "tty2")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var got reports
	go watch(ctx, path, 2*time.Millisecond, got.add)
	time.Sleep(20 * time.Millisecond)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	writeConsole(t, path, "tty2")
	time.Sleep(30 * time.Millisecond)
	if all := got.all(); len(all) != 0 {
		t.Fatalf("reports = %v", all)
	}
}

// source is an answer to "is the session on screen?" that a test changes while the poll runs.
type source struct {
	mu        sync.Mutex
	on, known bool
}

func (s *source) set(on, known bool) { s.mu.Lock(); s.on, s.known = on, known; s.mu.Unlock() }
func (s *source) ask() (bool, bool)  { s.mu.Lock(); defer s.mu.Unlock(); return s.on, s.known }

// The session leaves the screen and comes back: each is reported once, and a moment when the system cannot say changes nothing.
func TestPollReportsTheSessionLeavingAndComingBack(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	src := &source{on: true, known: true}
	var got reports
	go poll(ctx, 2*time.Millisecond, src.ask, got.add)
	time.Sleep(30 * time.Millisecond)
	if all := got.all(); len(all) != 0 {
		t.Fatalf("reports while the session was on screen: %v", all)
	}
	src.set(false, true)
	eventually(t, func() bool { return slices.Equal(got.all(), []bool{false}) })
	// Not knowing is not the session coming back, nor leaving again.
	src.set(true, false)
	time.Sleep(30 * time.Millisecond)
	src.set(false, true)
	time.Sleep(30 * time.Millisecond)
	src.set(true, true)
	eventually(t, func() bool { return slices.Equal(got.all(), []bool{false, true}) })
}

// A session that is not on screen when the watch begins is not one it can say anything about: it returns, with no report.
func TestPollDoesNotWatchASessionThatIsNotOnScreenAtTheStart(t *testing.T) {
	var got reports
	done := make(chan struct{})
	go func() {
		poll(t.Context(), time.Millisecond, (&source{on: false, known: true}).ask, got.add)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the watch kept running for a session that was not on screen")
	}
	if all := got.all(); len(all) != 0 {
		t.Fatalf("reports = %v", all)
	}
}

// A system that cannot answer at the very start is waited for: the first answer decides, and the watch begins from it.
func TestPollWaitsForTheFirstAnswer(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	src := &source{}
	var got reports
	go poll(ctx, 2*time.Millisecond, src.ask, got.add)
	time.Sleep(30 * time.Millisecond)
	src.set(true, true)
	time.Sleep(30 * time.Millisecond)
	src.set(false, true)
	eventually(t, func() bool { return slices.Equal(got.all(), []bool{false}) })
}

// The watch ends with its context.
func TestPollStopsWhenItsContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		poll(ctx, time.Millisecond, (&source{on: true, known: true}).ask, func(bool) {})
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the watch did not stop")
	}
}

// The reports say why the session left, and nothing when it is back.
func TestReportsSayWhyTheSessionLeft(t *testing.T) {
	type report struct {
		active bool
		reason Reason
	}
	var got []report
	say := withReason(func(active bool, reason Reason) { got = append(got, report{active, reason}) }, OtherSession)
	say(false)
	say(true)
	say(false)
	want := []report{{false, OtherSession}, {true, ""}, {false, OtherSession}}
	if !slices.Equal(got, want) {
		t.Fatalf("reports = %v, want %v", got, want)
	}
}
