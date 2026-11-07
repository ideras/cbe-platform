//go:build darwin

package sessionwatch

/*
#cgo LDFLAGS: -framework ApplicationServices
#include <ApplicationServices/ApplicationServices.h>

// session_on_console says whether this login session is the one on the console: 1 for yes, 0 for no, and -1 when the window
// server does not say (no window server session, or an answer without the key).
static int session_on_console(void) {
	CFDictionaryRef session = CGSessionCopyCurrentDictionary();
	if (session == NULL) {
		return -1;
	}
	int result = -1;
	const void *value = CFDictionaryGetValue(session, kCGSessionOnConsoleKey);
	if (value != NULL && CFGetTypeID(value) == CFBooleanGetTypeID()) {
		result = CFBooleanGetValue((CFBooleanRef)value) ? 1 : 0;
	}
	CFRelease(session);
	return result;
}
*/
import "C"

import (
	"context"
	"time"
)

// Watch calls report(false, reason) when the student's session stops being the one on screen and report(true, "") when it is
// again. It is meant to be started where the session is on screen, and runs until ctx ends.
//
// The window server says whether a login session is the one on the console. A switch to another user (Fast User Switching) takes
// the console from the student's session, and the window keeps its focus, so it is reported here.
func Watch(ctx context.Context, report func(active bool, reason Reason)) {
	poll(ctx, 500*time.Millisecond, sessionOnConsole, withReason(report, OtherSession))
}

// sessionOnConsole is whether this process's login session is the one on the console; it is not known when the window server
// does not say.
func sessionOnConsole() (on, known bool) {
	switch C.session_on_console() {
	case 1:
		return true, true
	case 0:
		return false, true
	}
	return false, false
}
