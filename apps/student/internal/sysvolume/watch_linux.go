//go:build linux

package sysvolume

import (
	"bufio"
	"context"
	"os/exec"
	"strings"
	"time"
)

// Watch calls changed when PulseAudio or PipeWire's pulse server reports that a sink or the default output changed, until
// ctx ends. It returns an error when the server cannot be watched (no pactl, or the connection ended); the caller keeps
// polling.
func (System) Watch(ctx context.Context, changed func()) error {
	cmd := exec.CommandContext(ctx, "pactl", "subscribe")
	cmd.WaitDelay = time.Second
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	lines := bufio.NewScanner(out)
	for lines.Scan() {
		// "Event 'change' on sink #3" is a volume or mute change; "on server" is a new default output.
		if line := lines.Text(); strings.Contains(line, " on sink #") || strings.Contains(line, " on server") {
			changed()
		}
	}
	err = cmd.Wait()
	if ctx.Err() != nil {
		return nil
	}
	if err == nil {
		err = lines.Err()
	}
	if err == nil {
		err = errWatchEnded
	}
	return err
}
