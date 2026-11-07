//go:build darwin

package remotewatch

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// processNames are the names of the running programs, from ps (-c: the name of the program, not its path and arguments).
func processNames() ([]string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/bin/ps", "-axco", "comm=").Output()
	if err != nil {
		return nil, false
	}
	var names []string
	for _, line := range strings.Split(string(out), "\n") {
		if name := strings.TrimSpace(line); name != "" {
			names = append(names, name)
		}
	}
	return names, true
}
