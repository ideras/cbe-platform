//go:build windows

package sessionwatch

import (
	"context"
	"time"

	"golang.org/x/sys/windows"
)

// Watch calls report(false, reason) when the student's session stops being the one on screen and report(true, "") when it is
// again. It is meant to be started where the session is on screen, and runs until ctx ends.
//
// The session is on screen while it is the one attached to the console. A switch of user (Fast User Switching) attaches the
// other user's session to it, and so does a remote client taking the session over, so both are reported without the window
// changing its focus. A session that is not on the console when the application starts, a remote one, is not watched.
func Watch(ctx context.Context, report func(active bool, reason Reason)) {
	var own uint32
	if err := windows.ProcessIdToSessionId(windows.GetCurrentProcessId(), &own); err != nil {
		return
	}
	probe := &consoleProbe{own: own}
	poll(ctx, 500*time.Millisecond, func() (bool, bool) {
		return probe.onScreen(windows.WTSGetActiveConsoleSessionId())
	}, withReason(report, OtherSession))
}
