// Package sessionwatch says whether the student's session is the one on screen. A student who switches to another user's
// session, or on Linux to a text console (Ctrl+Alt+F3), leaves the exam window as it was, still with the focus, so nothing about
// the window shows it; the operating system's record of which session is on screen does. Each platform answers that one
// question its own way (the kernel's active console on Linux, the console session on Windows, the window server's session on
// macOS) and the loop that turns the answer into reports is the same.
package sessionwatch

import (
	"context"
	"os"
	"strings"
	"time"
)

// Reason says what took the session off screen, as far as the platform can tell. A report that the session is on screen has none.
type Reason string

const (
	// OtherConsole is a text console or another user's session: on Linux the kernel names the console that is on screen and no
	// more, so the two cannot be told apart.
	OtherConsole Reason = "other_console"
	// OtherSession is another session holding the console: another user signed in on the screen or, on Windows, a remote client
	// taking the session over.
	OtherSession Reason = "other_session"
)

// withReason turns the loop's plain reports into ones that say why the session left: the reason given, and none when it is back.
func withReason(report func(active bool, reason Reason), reason Reason) func(active bool) {
	return func(active bool) {
		if active {
			report(true, "")
			return
		}
		report(false, reason)
	}
}

// poll reports false when the session stops being on screen and true when it is again, each time that changes, until ctx ends.
// It asks onScreen every interval. onScreen's second result is false when the system cannot say at that moment, which changes
// nothing: no answer is not an answer that the session is off screen, it is no knowledge.
//
// The first answer it can get decides whether to watch at all. It is meant to be started where the session is on screen, so a
// session that is not on screen then (or a system that never answers) is not one it can say anything about, and poll returns
// without a report.
func poll(ctx context.Context, every time.Duration, onScreen func() (on, known bool), report func(active bool)) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	active, started := true, false
	for {
		if on, known := onScreen(); known {
			switch {
			case !started && !on:
				return
			case !started:
				started = true
			case on != active:
				active = on
				report(active)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// watch reports false when the console that was on screen when it began is no longer, and true when it is again, each time that
// changes, until ctx ends. It checks the file every interval. It reports nothing at all when the file cannot be read, as it
// cannot on a system without such a record: no report is not a report that the session is on screen, it is no knowledge.
func watch(ctx context.Context, path string, every time.Duration, report func(active bool)) {
	own, ok := read(path)
	if !ok {
		return
	}
	poll(ctx, every, func() (bool, bool) {
		current, ok := read(path)
		return current == own, ok
	}, report)
}

// read is the content of the file, trimmed; false when it cannot be read or is empty.
func read(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	value := strings.TrimSpace(string(data))
	return value, value != ""
}
