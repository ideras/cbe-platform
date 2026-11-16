//go:build windows

package remotewatch

import (
	"golang.org/x/sys/windows/registry"
)

// machineFacts reads what the firmware told Windows at boot (SMBIOS), which is kept under HARDWARE\DESCRIPTION\System\BIOS and readable by anyone. The
// processor's hypervisor flag is not used: Windows with virtualization-based security or the Hyper-V role runs on a hypervisor on a real computer too.
func machineFacts() vmFacts {
	var facts vmFacts
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `HARDWARE\DESCRIPTION\System\BIOS`, registry.QUERY_VALUE)
	if err != nil {
		return facts
	}
	defer key.Close()
	for _, name := range []string{"SystemManufacturer", "SystemProductName", "SystemVersion", "BaseBoardManufacturer", "BaseBoardProduct", "BIOSVendor"} {
		if text, _, err := key.GetStringValue(name); err == nil && text != "" {
			facts.strings = append(facts.strings, text)
		}
	}
	return facts
}
