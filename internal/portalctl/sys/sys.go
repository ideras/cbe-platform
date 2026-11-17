// Package sys is everything cbe-portalctl does to the machine it runs on, behind one interface: so that what it would do can be shown
// (a dry run), and tested without a machine to change (Fake keeps a small simulated one).
package sys

import (
	"io/fs"
)

// Info is what is known of a path.
type Info struct {
	Exists bool
	IsDir  bool
	IsLink bool
	// Mode is the permission bits, with setuid, setgid and sticky (as 02710 is the runner's folder).
	Mode     fs.FileMode
	UID, GID int
	Size     int64
	// Target is where a symbolic link points.
	Target string
}

// User is an account.
type User struct {
	Name     string
	UID, GID int
	Home     string
}

// System is the machine.
type System interface {
	// IsRoot says whether the program runs as root.
	IsRoot() bool
	// Arch is the machine's architecture in Go's words (amd64, arm64).
	Arch() string
	// Run runs a program and returns what it printed on standard output. An error carries its exit status and what it printed on standard
	// error.
	Run(name string, args ...string) (string, error)
	// RunAs runs a program as another account, with the environment given.
	RunAs(user string, env []string, name string, args ...string) (string, error)
	// LookPath says where a program is, or that it is not.
	LookPath(name string) (string, bool)
	ReadFile(path string) ([]byte, error)
	// WriteFile writes a file whole and in one step (a file that is read while it is written is never seen half written), with the mode.
	WriteFile(path string, data []byte, mode fs.FileMode) error
	Stat(path string) Info
	// ReadDir lists the names in a folder.
	ReadDir(path string) ([]string, error)
	MkdirAll(path string, mode fs.FileMode) error
	Chown(path string, uid, gid int) error
	Chmod(path string, mode fs.FileMode) error
	Symlink(target, link string) error
	Remove(path string) error
	// RemoveAll removes a folder and everything in it. Only the uninstaller's removal of data uses it.
	RemoveAll(path string) error
	Lookup(name string) (User, bool)
	LookupGroup(name string) (gid int, ok bool)
	// FreeBytes is the room left on the filesystem that holds path.
	FreeBytes(path string) (int64, error)
}
