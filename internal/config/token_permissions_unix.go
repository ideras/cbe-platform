//go:build !windows

package config

import (
	"fmt"
	"os"
)

func checkPrivateToken(_ *os.File, info os.FileInfo) error {
	if info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("admin token file must not be accessible by group or others (use chmod 600)")
	}
	return nil
}
