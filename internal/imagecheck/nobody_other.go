//go:build !unix

package imagecheck

import "os/exec"

// asNobody does nothing where there is no root: the agent, a Linux program, does not run there anyway.
func asNobody(*exec.Cmd) {}
