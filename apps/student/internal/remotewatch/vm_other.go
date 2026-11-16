//go:build !linux && !windows && !darwin

package remotewatch

// machineFacts has nothing to read on this platform: it is never said to be a virtual machine.
func machineFacts() vmFacts { return vmFacts{} }
