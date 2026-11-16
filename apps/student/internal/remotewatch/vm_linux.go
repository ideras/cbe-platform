//go:build linux

package remotewatch

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// machineFacts reads the firmware's descriptions from /sys (SMBIOS, readable by anyone) and the processor's hypervisor flag from /proc.
func machineFacts() vmFacts { return readMachineFacts("/sys", "/proc") }

func readMachineFacts(sys, proc string) vmFacts {
	var facts vmFacts
	for _, name := range []string{"sys_vendor", "product_name", "product_version", "board_vendor", "board_name", "bios_vendor"} {
		if data, err := os.ReadFile(filepath.Join(sys, "class", "dmi", "id", name)); err == nil {
			if text := strings.TrimSpace(string(data)); text != "" {
				facts.strings = append(facts.strings, text)
			}
		}
	}
	// Xen says what it is here, whatever the firmware says.
	if data, err := os.ReadFile(filepath.Join(sys, "hypervisor", "type")); err == nil {
		if text := strings.TrimSpace(string(data)); text != "" {
			facts.strings = append(facts.strings, text)
		}
	}
	// A kernel that runs under a hypervisor says so among the processor's flags; the host of a virtual machine does not.
	if file, err := os.Open(filepath.Join(proc, "cpuinfo")); err == nil {
		defer file.Close()
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 64<<10), 1<<20)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "flags") {
				continue
			}
			for _, flag := range strings.Fields(line) {
				if flag == "hypervisor" {
					facts.hypervisor = true
				}
			}
			break
		}
	}
	return facts
}
