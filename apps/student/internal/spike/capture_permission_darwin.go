//go:build darwin

package spike

/*
#cgo LDFLAGS: -framework CoreGraphics
#include <CoreGraphics/CoreGraphics.h>
*/
import "C"

func platformCapturePermission(request bool) CapturePermission {
	granted := bool(C.CGPreflightScreenCaptureAccess())
	if request && !granted {
		granted = bool(C.CGRequestScreenCaptureAccess())
	}
	if !granted {
		return CapturePermission{Supported: true, Message: "Screen Recording permission is not granted. In System Settings > Privacy & Security > Screen Recording (or Screen & System Audio Recording), allow the packaged cbe-portal-student app, then fully quit/relaunch it. Notify the instructor; editing requires a successful retest. Before relaunch, wait for saves and capture-health reports to be acknowledged. A real packaged-Mac grant/deny test is required."}
	}
	return CapturePermission{Supported: true, Granted: true, Message: "Screen Recording permission is granted. All attached monitors and packaged-app relaunch behavior still require instructor verification."}
}
