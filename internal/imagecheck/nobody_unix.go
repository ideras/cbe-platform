//go:build unix

package imagecheck

import (
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"syscall"
)

// asNobody makes a command run as the account nobody when this program is root.
func asNobody(cmd *exec.Cmd) {
	if os.Geteuid() != 0 {
		return
	}
	uid, gid := uint32(65534), uint32(65534)
	if u, err := user.Lookup("nobody"); err == nil {
		if n, err := strconv.ParseUint(u.Uid, 10, 32); err == nil {
			uid = uint32(n)
		}
		if n, err := strconv.ParseUint(u.Gid, 10, 32); err == nil {
			gid = uint32(n)
		}
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uid, Gid: gid, Groups: []uint32{}}}
}
