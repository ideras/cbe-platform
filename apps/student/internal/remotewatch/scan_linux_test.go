//go:build linux

package remotewatch

import (
	"os/exec"
	"slices"
	"testing"
	"time"
)

// The real /proc of this computer: a program that is running is among the names, under the name it was started as.
func TestTheRealProcessListHasARunningProgram(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skip("sleep cannot be started:", err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		names, ok := processNames()
		if !ok {
			t.Fatal("the process list could not be read")
		}
		if slices.Contains(names, "sleep") {
			// And the list matches it like any other program.
			if got := matchTools(names, []string{"sle*"}); len(got) == 0 {
				t.Fatalf("a running program was not matched: %v", got)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("a running program is not in the process list")
}
