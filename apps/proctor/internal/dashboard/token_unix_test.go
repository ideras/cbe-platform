//go:build !windows

package dashboard

// restrictTokenACL is a no-op where Unix mode bits already define privacy.
func restrictTokenACL(string) error { return nil }
