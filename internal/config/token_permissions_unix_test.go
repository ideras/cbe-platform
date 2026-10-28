//go:build !windows

package config

import "os"

func makeTokenPublic(path string) error { return os.Chmod(path, 0644) }

// restrictTokenACL is a no-op where Unix mode bits already define privacy.
func restrictTokenACL(string) error { return nil }
