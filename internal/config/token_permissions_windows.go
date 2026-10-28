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
	// SIDs are not secrets, so rejections name the offending entry: a hosted failure then shows exactly which account
	// or ACE the policy refused instead of a generic verdict.
	account := "<no account>"
	if user != nil && user.IsValid() {
		account = user.String()
	}
	rejected := func(format string, args ...any) error {
		return fmt.Errorf("token ACL must grant access only to the current account, SYSTEM and local Administrators (current account %s: %s)", account, fmt.Sprintf(format, args...))
	}
	if sd == nil || user == nil || !user.IsValid() {
		return rejected("no security descriptor or account to compare with")
	}
	trusted := func(sid *windows.SID) bool {
		return sid != nil && sid.IsValid() && (sid.Equals(user) || sid.IsWellKnown(windows.WinLocalSystemSid) || sid.IsWellKnown(windows.WinBuiltinAdministratorsSid))
	}
	sidName := func(sid *windows.SID) string {
		if sid == nil || !sid.IsValid() {
			return "<invalid>"
		}
		return sid.String()
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return rejected("cannot read the token file's owner: %v", err)
	}
	if !trusted(owner) {
		return rejected("file is owned by %s", sidName(owner))
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil {
		return rejected("no access list to inspect")
	}
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(acl, i, &ace); err != nil || ace == nil {
			return rejected("cannot read access entry %d", i)
		}
		if ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 {
			continue
		}
		switch ace.Header.AceType {
		case windows.ACCESS_DENIED_ACE_TYPE:
			continue
		case windows.ACCESS_ALLOWED_ACE_TYPE:
			if ace.Header.AceSize < 16 {
				return rejected("malformed access entry %d", i)
			}
			sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
			if !trusted(sid) {
				return rejected("access entry %d grants %s", i, sidName(sid))
			}
		default:
			// Conditional and object-specific ACEs need separate interpretation.
			return rejected("access entry %d has unsupported type %d", i, ace.Header.AceType)
		}
	}
	return nil
}
