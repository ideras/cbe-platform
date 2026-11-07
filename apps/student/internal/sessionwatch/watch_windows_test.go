//go:build windows

package sessionwatch

import (
	"testing"

	"golang.org/x/sys/windows"
)

// The first run on a real Windows computer: from the console, the process's session is the one attached to it, so the probe
// says it is on screen. A process in a remote session is not on the console, and the test says so and stops.
//
//	go test ./internal/sessionwatch -run TestTheRealConsole -v
func TestTheRealConsoleSessionIsTheOneOfTheProcess(t *testing.T) {
	var own uint32
	if err := windows.ProcessIdToSessionId(windows.GetCurrentProcessId(), &own); err != nil {
		t.Fatal(err)
	}
	console := windows.WTSGetActiveConsoleSessionId()
	t.Logf("own session = %d, console session = %d", own, console)
	if console != own {
		t.Skipf("this process is not on the console (a remote session?): own = %d, console = %d", own, console)
	}
	if on, known := (&consoleProbe{own: own}).onScreen(console); !known || !on {
		t.Fatalf("on = %v, known = %v", on, known)
	}
}
