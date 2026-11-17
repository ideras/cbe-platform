//go:build linux

package sys

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

// OS is the machine the program runs on.
type OS struct{}

func (OS) IsRoot() bool { return os.Geteuid() == 0 }
func (OS) Arch() string { return runtime.GOARCH }

func (OS) Run(name string, args ...string) (string, error) {
	return run(exec.Command(name, args...), name, args)
}

func (OS) RunAs(account string, env []string, name string, args ...string) (string, error) {
	full := append([]string{"-u", account, "--", "env"}, env...)
	full = append(full, name)
	full = append(full, args...)
	return run(exec.Command("runuser", full...), name, args)
}

func run(cmd *exec.Cmd, name string, args []string) (string, error) {
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		text := strings.TrimSpace(errOut.String())
		if text == "" {
			text = strings.TrimSpace(out.String())
		}
		return out.String(), fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, text)
	}
	return out.String(), nil
}

func (OS) LookPath(name string) (string, bool) {
	path, err := exec.LookPath(name)
	return path, err == nil
}

func (OS) ReadFile(path string) ([]byte, error) { return os.ReadFile(path) }

func (OS) WriteFile(path string, data []byte, mode fs.FileMode) error {
	temp := fmt.Sprintf("%s.new-%s", path, randomSuffix())
	if err := os.WriteFile(temp, data, mode); err != nil {
		return err
	}
	if err := os.Chmod(temp, mode); err != nil { // the umask has been at the mode
		_ = os.Remove(temp)
		return err
	}
	if err := os.Rename(temp, path); err != nil {
		_ = os.Remove(temp)
		return err
	}
	return nil
}

func randomSuffix() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (OS) Stat(path string) Info {
	info, err := os.Lstat(path)
	if err != nil {
		return Info{}
	}
	out := Info{Exists: true, IsDir: info.IsDir(), IsLink: info.Mode()&os.ModeSymlink != 0, Size: info.Size(), Mode: permissions(info.Mode())}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		out.UID, out.GID = int(stat.Uid), int(stat.Gid)
	}
	if out.IsLink {
		out.Target, _ = os.Readlink(path)
		if target, err := os.Stat(path); err == nil {
			out.IsDir = target.IsDir()
		}
	}
	return out
}

func permissions(mode fs.FileMode) fs.FileMode {
	return mode & (fs.ModePerm | fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky)
}

func (OS) ReadDir(path string) ([]string, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(entries))
	for i, entry := range entries {
		names[i] = entry.Name()
	}
	return names, nil
}

func (OS) MkdirAll(path string, mode fs.FileMode) error { return os.MkdirAll(path, mode) }
func (OS) Chown(path string, uid, gid int) error        { return os.Lchown(path, uid, gid) }
func (OS) Chmod(path string, mode fs.FileMode) error    { return os.Chmod(path, mode) }
func (OS) Symlink(target, link string) error            { return os.Symlink(target, link) }
func (OS) Remove(path string) error                     { return os.Remove(path) }
func (OS) RemoveAll(path string) error                  { return os.RemoveAll(path) }

func (OS) Lookup(name string) (User, bool) {
	account, err := user.Lookup(name)
	if err != nil {
		return User{}, false
	}
	uid, _ := strconv.Atoi(account.Uid)
	gid, _ := strconv.Atoi(account.Gid)
	return User{Name: name, UID: uid, GID: gid, Home: account.HomeDir}, true
}

func (OS) LookupGroup(name string) (int, bool) {
	group, err := user.LookupGroup(name)
	if err != nil {
		return 0, false
	}
	gid, _ := strconv.Atoi(group.Gid)
	return gid, true
}

func (OS) FreeBytes(path string) (int64, error) {
	// The nearest folder that exists: the room of a folder that is still to be made is the room of what it will be made in.
	for path != "/" && !exists(path) {
		path = filepath.Dir(path)
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return int64(stat.Bavail) * int64(stat.Bsize), nil
}

func exists(path string) bool { _, err := os.Lstat(path); return err == nil }
