//go:build !linux

package sysvolume

import "context"

// Watch has no change notification on this platform: the caller keeps polling.
func (System) Watch(context.Context, func()) error { return errWatchUnsupported }
