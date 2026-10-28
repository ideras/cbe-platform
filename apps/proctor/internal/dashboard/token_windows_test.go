//go:build windows

package dashboard

import (
	"os/exec"

	"golang.org/x/sys/windows"
)

// restrictTokenACL replaces a test credential file's inherited ACL with an explicitly private one: the current
// account alone. Files that only inherit a profile or temporary directory's ACL can carry extra access-granting
// entries (for example application-container SIDs) on some Windows images, which the server's strict credential
// policy rejects. The server under test reads the file, so the test has to hand it a private one.
func restrictTokenACL(path string) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	return exec.Command("icacls", path, "/inheritance:r", "/grant:r", "*"+user.User.Sid.String()+":F").Run()
}
