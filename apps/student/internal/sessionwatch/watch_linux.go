package sessionwatch

import (
	"context"
	"time"
)

// activeConsole names the console that is on screen, "tty2" for instance. The kernel keeps it for every virtual console, so it
// is the same under X11 and Wayland, and under every display manager.
const activeConsole = "/sys/class/tty/tty0/active"

// Watch calls report(false, reason) when the student's session stops being the one on screen and report(true, "") when it is
// again. It is meant to be started where the session is on screen, and runs until ctx ends.
func Watch(ctx context.Context, report func(active bool, reason Reason)) {
	watch(ctx, activeConsole, 500*time.Millisecond, withReason(report, OtherConsole))
}
