//go:build !darwin

package spike

func platformCapturePermission(bool) CapturePermission {
	return CapturePermission{Supported: true, Granted: true, Message: "Screen capture will be attempted at the sustained-unfocus threshold. Device/monitor verification is required; protected or unavailable displays can fail. Notify the instructor about any capture failure."}
}
