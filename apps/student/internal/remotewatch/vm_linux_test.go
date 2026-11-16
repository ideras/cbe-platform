//go:build linux

package remotewatch

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, text := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// What the files of /sys and /proc say is read as the classifier needs it: the firmware's descriptions, and the processor's hypervisor flag.
func TestTheDescriptionsAndTheFlagAreReadFromSysAndProc(t *testing.T) {
	sys, proc := t.TempDir(), t.TempDir()
	writeFiles(t, sys, map[string]string{
		"class/dmi/id/sys_vendor": "innotek GmbH\n", "class/dmi/id/product_name": "VirtualBox\n", "class/dmi/id/board_vendor": "Oracle Corporation\n", "class/dmi/id/bios_vendor": "\n",
	})
	writeFiles(t, proc, map[string]string{"cpuinfo": "processor\t: 0\nmodel name\t: Intel\nflags\t\t: fpu vme sse2 hypervisor lahf_lm\n\nprocessor\t: 1\nflags\t\t: fpu\n"})
	facts := readMachineFacts(sys, proc)
	if !facts.hypervisor {
		t.Error("the hypervisor flag was not found")
	}
	if len(facts.strings) != 3 || facts.strings[0] != "innotek GmbH" || facts.strings[1] != "VirtualBox" {
		t.Errorf("strings = %q (an empty description is left out)", facts.strings)
	}
	if finding, ok := classify(facts); !ok || finding.Name != "VirtualBox" {
		t.Errorf("finding = %+v, %v", finding, ok)
	}

	// A real computer: its makers' words, and no flag (a word that only contains it is not the flag).
	realSys, realProc := t.TempDir(), t.TempDir()
	writeFiles(t, realSys, map[string]string{"class/dmi/id/sys_vendor": "Dell Inc.\n", "class/dmi/id/product_name": "Precision M4700\n"})
	writeFiles(t, realProc, map[string]string{"cpuinfo": "flags\t\t: fpu vme nothypervisorish sse2\n"})
	facts = readMachineFacts(realSys, realProc)
	if facts.hypervisor {
		t.Error("a flag was found that is not there")
	}
	if finding, ok := classify(facts); ok {
		t.Errorf("a real computer was taken for %+v", finding)
	}
}

// Files that are not there say nothing: a machine with no /sys/class/dmi (a container, an old kernel) is not taken for a virtual one.
func TestMissingFilesSayNothing(t *testing.T) {
	facts := readMachineFacts(t.TempDir(), t.TempDir())
	if facts.hypervisor || len(facts.strings) != 0 {
		t.Fatalf("facts = %+v", facts)
	}
}

// This computer, whatever it is: reading it must work and give a finding or none, and never panic.
func TestThisComputerCanBeLookedAt(t *testing.T) {
	finding, ok := classify(machineFacts())
	t.Logf("this computer: virtual machine = %v %+v", ok, finding)
}
