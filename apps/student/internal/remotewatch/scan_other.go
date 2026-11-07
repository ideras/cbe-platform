//go:build !linux && !windows && !darwin

package remotewatch

// processNames has nothing to read on this platform.
func processNames() ([]string, bool) { return nil, false }
