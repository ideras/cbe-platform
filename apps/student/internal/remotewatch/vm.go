package remotewatch

import (
	"strings"
	"unicode"
)

// KindVirtualMachine is the exam program running inside a virtual machine. It is not remote access: the window of a virtual machine is on a
// host that the guest cannot see, so what the student does there (another window, another program, a second screen) is not seen by the exam
// program, which can only tell the instructor that this is the case.
const KindVirtualMachine = "virtual_machine"

// vmFacts is what the system says about the machine the program runs on, in a form the platforms share.
type vmFacts struct {
	// strings are the descriptions the firmware gives (SMBIOS): the manufacturer and the product of the system and of the board, the maker of the firmware,
	// or the model the platform names. They come from the machine, which a virtual machine's maker fills in and its user can change, so they are one more
	// sign and no proof (the same as the rest of this package).
	strings []string
	// hypervisor is that the system says it runs under a hypervisor itself (the processor's flag, or the platform's own word for it). It is left false where
	// the flag is also set on a real computer: Windows with virtualization-based security runs on top of Hyper-V, so there it says nothing.
	hypervisor bool
}

// signatures are the parts of those descriptions that name a hypervisor, in the order they are tried, and the name each gives. The product comes
// before the maker where both are told: Hyper-V's guests say "Microsoft Corporation" of themselves, and so do real computers.
var signatures = []struct {
	needle, name string
	// word is that the needle must be a whole word of the description: a short one (xen, kvm) is part of other words.
	word bool
}{
	{"virtualbox", "VirtualBox", false}, {"innotek", "VirtualBox", false},
	{"vmware", "VMware", false},
	{"parallels", "Parallels", false},
	{"qemu", "QEMU/KVM", false}, {"kvm", "QEMU/KVM", true}, {"bochs", "QEMU/KVM", true},
	{"xen", "Xen", true},
	{"bhyve", "bhyve", false},
	{"hyper-v", "Hyper-V", false},
	{"virtualmac", "Apple Virtualization", false}, {"apple virtual", "Apple Virtualization", false},
	{"google compute engine", "Google Cloud", false},
	{"amazon ec2", "Amazon EC2", false},
	{"openstack", "OpenStack", false},
	{"digitalocean", "DigitalOcean", false},
}

// classify says whether the machine is a virtual one, as a finding: Name is the hypervisor when it is known ("VirtualBox") and "unknown" when the system
// says it is virtual and not whose, and Detail is the description that told it, for the instructor to read.
func classify(facts vmFacts) (Finding, bool) {
	lower := make([]string, len(facts.strings))
	for i, text := range facts.strings {
		lower[i] = strings.ToLower(strings.TrimSpace(text))
	}
	for _, signature := range signatures {
		for i, text := range lower {
			if mentions(text, signature.needle, signature.word) {
				return Finding{Kind: KindVirtualMachine, Name: signature.name, Detail: truncate(strings.TrimSpace(facts.strings[i]), maxDetail)}, true
			}
		}
	}
	// Hyper-V's guests are the one hypervisor whose firmware says only "Virtual Machine" (of a "Microsoft Corporation"); a real computer is never
	// called that.
	for i, text := range lower {
		if text == "virtual machine" {
			return Finding{Kind: KindVirtualMachine, Name: "Hyper-V", Detail: truncate(strings.TrimSpace(facts.strings[i]), maxDetail)}, true
		}
	}
	if facts.hypervisor {
		return Finding{Kind: KindVirtualMachine, Name: "unknown"}, true
	}
	return Finding{}, false
}

// mentions says whether a description (in lower case) has the needle in it, as a whole word when word is set.
func mentions(text, needle string, word bool) bool {
	if !word {
		return strings.Contains(text, needle)
	}
	for _, part := range strings.FieldsFunc(text, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if part == needle {
			return true
		}
	}
	return false
}

// virtualMachine is the finding for the machine this program runs on, if it is a virtual one. What the machine is does not change while the program
// runs, so it is looked at once. It is a variable so that a test can say what the machine is.
var virtualMachine = func() []Finding {
	if finding, ok := classify(machineFacts()); ok {
		return []Finding{finding}
	}
	return nil
}
