//go:build darwin

package remotewatch

import "golang.org/x/sys/unix"

// machineFacts asks the kernel whether it runs under a hypervisor (kern.hv_vmm_present, set for the virtual machines of Apple's framework and of
// the products built on it) and what the computer's model is called.
func machineFacts() vmFacts {
	var facts vmFacts
	if value, err := unix.SysctlUint32("kern.hv_vmm_present"); err == nil && value == 1 {
		facts.hypervisor = true
	}
	if model, err := unix.Sysctl("hw.model"); err == nil && model != "" {
		facts.strings = append(facts.strings, model)
	}
	return facts
}
