//go:build !linux && !windows && !darwin

package sessionwatch

import "context"

// Watch does nothing here: there is no record of which session is on screen to watch on this platform.
func Watch(context.Context, func(active bool, reason Reason)) {}
