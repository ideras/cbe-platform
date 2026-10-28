package config

import (
	"errors"
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// A null or unsupported ACL is rejected. Only the running account, SYSTEM and
// local Administrators may be granted access; deny entries are conservative.
func checkPrivateToken(file *os.File, _ os.FileInfo) error {
	sd, err := windows.GetSecurityInfo(windows.Handle(file.Fd()), windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("cannot inspect Windows token ACL: %w", err)
	}
	if sd == nil {
		return errors.New("cannot inspect Windows token ACL")
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return fmt.Errorf("cannot identify Windows account: %w", err)
	}
	return checkTokenDescriptor(sd, user.User.Sid)
}

func checkTokenDescriptor(sd *windows.SECURITY_DESCRIPTOR, user *windows.SID) error {
	rejected := errors.New("token ACL must grant access only to the current account, SYSTEM and local Administrators")
	if sd == nil || user == nil || !user.IsValid() {
		return rejected
	}
	trusted := func(sid *windows.SID) bool {
		return sid != nil && sid.IsValid() && (sid.Equals(user) || sid.IsWellKnown(windows.WinLocalSystemSid) || sid.IsWellKnown(windows.WinBuiltinAdministratorsSid))
	}
	owner, _, err := sd.Owner()
	if err != nil || !trusted(owner) {
		return rejected
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil {
		return rejected
	}
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(acl, i, &ace); err != nil || ace == nil {
			return rejected
		}
		if ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 {
			continue
		}
		switch ace.Header.AceType {
		case windows.ACCESS_DENIED_ACE_TYPE:
			continue
		case windows.ACCESS_ALLOWED_ACE_TYPE:
			if ace.Header.AceSize < 16 {
				return rejected
			}
			sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
			if !trusted(sid) {
				return rejected
			}
		default:
			// Conditional and object-specific ACEs need separate interpretation.
			return rejected
		}
	}
	return nil
}
