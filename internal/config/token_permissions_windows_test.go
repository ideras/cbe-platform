package config

import (
	"os/exec"
	"testing"

	"golang.org/x/sys/windows"
)

func makeTokenPublic(path string) error {
	return exec.Command("icacls", path, "/grant:r", "*S-1-1-0:(R)").Run()
}

// restrictTokenACL replaces a test token file's inherited ACL with an explicitly private one: the current account
// alone. Files that only inherit a profile or temporary directory's ACL can carry extra access-granting entries
// (for example application-container SIDs) on some Windows images, which the strict credential policy rejects.
func restrictTokenACL(path string) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	return exec.Command("icacls", path, "/inheritance:r", "/grant:r", "*"+user.User.Sid.String()+":F").Run()
}

func TestWindowsTokenACL(t *testing.T) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, sddl string
		accept     bool
	}{
		{"private", "O:SYD:(A;;FA;;;SY)(A;;FA;;;BA)(A;;FA;;;" + user.User.Sid.String() + ")", true},
		{"everyone read", "O:SYD:(A;;FA;;;SY)(A;;FR;;;WD)", false},
		{"authenticated users write", "O:SYD:(A;;FA;;;SY)(A;;FW;;;AU)", false},
		{"untrusted owner", "O:WDD:(A;;FA;;;SY)", false},
		{"missing dacl", "O:SY", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sd, err := windows.SecurityDescriptorFromString(tc.sddl)
			if err != nil {
				t.Fatal(err)
			}
			err = checkTokenDescriptor(sd, user.User.Sid)
			if (err == nil) != tc.accept {
				t.Fatalf("accepted = %v, err = %v", err == nil, err)
			}
		})
	}
}
