//go:build !windows

package config

import "os"

func makeTokenPublic(path string) error { return os.Chmod(path, 0644) }
