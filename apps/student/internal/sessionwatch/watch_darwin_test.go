//go:build darwin

package sessionwatch

import "testing"

// The first run on a real Mac: from the console, the window server says the process's login session is on it. Without a window
// server session (over SSH, say) or off the console, the test says so and stops.
//
//	go test ./internal/sessionwatch -run TestTheRealConsole -v
func TestTheRealConsoleSessionIsTheOneOfTheProcess(t *testing.T) {
	on, known := sessionOnConsole()
	t.Logf("on the console = %v, known = %v", on, known)
	if !known {
		t.Skip("the window server does not say which session is on the console (no window server session?)")
	}
	if !on {
		t.Skip("this process's login session is not the one on the console")
	}
}
