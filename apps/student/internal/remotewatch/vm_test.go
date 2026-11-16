package remotewatch

import (
	"strings"
	"testing"
)

// What the firmware of each hypervisor's guests says of itself, and what real computers say, are told apart; the hypervisor is named when it is known.
func TestTheFirmwareNamesTheHypervisorOfAVirtualMachine(t *testing.T) {
	for _, test := range []struct {
		name  string
		facts vmFacts
		want  string // the hypervisor, or "" for a real computer
	}{
		{"VirtualBox", vmFacts{strings: []string{"innotek GmbH", "VirtualBox", "1.2", "Oracle Corporation", "VirtualBox", "innotek GmbH"}}, "VirtualBox"},
		{"VMware", vmFacts{strings: []string{"VMware, Inc.", "VMware Virtual Platform", "None"}}, "VMware"},
		{"VMware, a newer name", vmFacts{strings: []string{"VMware, Inc.", "VMware7,1"}}, "VMware"},
		{"Hyper-V guest", vmFacts{strings: []string{"Microsoft Corporation", "Virtual Machine", "Hyper-V UEFI Release v4.1"}}, "Hyper-V"},
		{"QEMU", vmFacts{strings: []string{"QEMU", "Standard PC (Q35 + ICH9, 2009)", "pc-q35-8.2"}}, "QEMU/KVM"},
		{"KVM by name", vmFacts{strings: []string{"KVM", "Virtual Machine"}}, "QEMU/KVM"},
		{"Parallels", vmFacts{strings: []string{"Parallels Software International Inc.", "Parallels Virtual Platform"}}, "Parallels"},
		{"Xen", vmFacts{strings: []string{"Xen", "HVM domU"}}, "Xen"},
		{"Apple's framework", vmFacts{strings: []string{"VirtualMac2,1"}, hypervisor: true}, "Apple Virtualization"},
		{"Google Cloud", vmFacts{strings: []string{"Google", "Google Compute Engine"}}, "Google Cloud"},
		// The name wins over the flag, and a flag with no name is a virtual machine of an unknown kind.
		{"VirtualBox, with the flag", vmFacts{strings: []string{"VirtualBox"}, hypervisor: true}, "VirtualBox"},
		{"a flag and no name", vmFacts{strings: []string{"Acme", "Cloud Server 9"}, hypervisor: true}, "unknown"},
		// Real computers: the makers' own words, a Surface (also Microsoft's), and a CPU that has "xen" in a longer word.
		{"a Dell", vmFacts{strings: []string{"Dell Inc.", "Precision M4700", "A18", "Dell Inc.", "0GWHNR", "Dell Inc."}}, ""},
		{"a Surface", vmFacts{strings: []string{"Microsoft Corporation", "Surface Laptop 5", "124I:00036T:000M:0300000D:11B:1F:2P:3"}}, ""},
		{"a Lenovo", vmFacts{strings: []string{"LENOVO", "20XW", "ThinkPad X1 Carbon Gen 9"}}, ""},
		{"a Xeon workstation", vmFacts{strings: []string{"HP", "Z840 Workstation", "Xeon E5-2690"}}, ""},
		{"a Mac", vmFacts{strings: []string{"MacBookPro18,3"}}, ""},
		{"nothing said", vmFacts{}, ""},
	} {
		finding, ok := classify(test.facts)
		if test.want == "" {
			if ok {
				t.Errorf("%s: a real computer was taken for %+v", test.name, finding)
			}
			continue
		}
		if !ok || finding.Kind != KindVirtualMachine || finding.Name != test.want {
			t.Errorf("%s: found %+v (%v), want a virtual machine named %q", test.name, finding, ok, test.want)
		}
	}
}

// The detail is the firmware's own description, cut short, for the instructor to read; a flag alone has none.
func TestTheDetailIsWhatTheFirmwareSaidAndIsCut(t *testing.T) {
	finding, ok := classify(vmFacts{strings: []string{"  QEMU  ", "Standard PC"}})
	if !ok || finding.Detail != "QEMU" {
		t.Fatalf("finding = %+v", finding)
	}
	finding, _ = classify(vmFacts{strings: []string{"VMware " + strings.Repeat("é", 100)}})
	if len(finding.Detail) > maxDetail || !strings.HasPrefix(finding.Detail, "VMware ") {
		t.Fatalf("the detail is %d bytes: %q", len(finding.Detail), finding.Detail)
	}
	if finding, _ = classify(vmFacts{hypervisor: true}); finding.Detail != "" {
		t.Fatalf("a flag alone has a detail: %+v", finding)
	}
}

// A virtual machine is one more finding in every look, and it is reported once it has held for two, like any other.
func TestAVirtualMachineIsInEveryLookAndIsReportedLikeAnyFinding(t *testing.T) {
	vm := Finding{Kind: KindVirtualMachine, Name: "VirtualBox", Detail: "VirtualBox"}
	look := look(func() []string { return nil }, []Finding{vm})
	for i := 0; i < 3; i++ {
		if got := look(); len(got) != 1 || got[0] != vm {
			t.Fatalf("look %d = %v", i, got)
		}
	}
	got := runPoll(t, [][]Finding{{vm}, {vm}, {vm}}, 6)
	if want := [][]Finding{{vm}}; !equal(got, want) {
		t.Fatalf("reports = %v, want %v", got, want)
	}
	// A real computer has none, and nothing is reported.
	if got := look2(nil); len(got) != 0 {
		t.Fatalf("a real computer: %v", got)
	}
}

func look2(vm []Finding) []Finding { return look(func() []string { return nil }, vm)() }
