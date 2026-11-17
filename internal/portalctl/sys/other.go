//go:build !linux

package sys

import (
	"errors"
	"io/fs"
	"runtime"
)

// OS is not a machine cbe-portalctl can configure: it runs on Linux only. It is here so that the program builds everywhere, and says so.
type OS struct{}

var errLinuxOnly = errors.New("cbe-portalctl configures Linux servers only")

func (OS) IsRoot() bool                                              { return false }
func (OS) Arch() string                                              { return runtime.GOARCH }
func (OS) Run(string, ...string) (string, error)                     { return "", errLinuxOnly }
func (OS) RunAs(string, []string, string, ...string) (string, error) { return "", errLinuxOnly }
func (OS) LookPath(string) (string, bool)                            { return "", false }
func (OS) ReadFile(string) ([]byte, error)                           { return nil, errLinuxOnly }
func (OS) WriteFile(string, []byte, fs.FileMode) error               { return errLinuxOnly }
func (OS) Stat(string) Info                                          { return Info{} }
func (OS) ReadDir(string) ([]string, error)                          { return nil, errLinuxOnly }
func (OS) MkdirAll(string, fs.FileMode) error                        { return errLinuxOnly }
func (OS) Chown(string, int, int) error                              { return errLinuxOnly }
func (OS) Chmod(string, fs.FileMode) error                           { return errLinuxOnly }
func (OS) Symlink(string, string) error                              { return errLinuxOnly }
func (OS) Remove(string) error                                       { return errLinuxOnly }
func (OS) RemoveAll(string) error                                    { return errLinuxOnly }
func (OS) Lookup(string) (User, bool)                                { return User{}, false }
func (OS) LookupGroup(string) (int, bool)                            { return 0, false }
func (OS) FreeBytes(string) (int64, error)                           { return 0, errLinuxOnly }
