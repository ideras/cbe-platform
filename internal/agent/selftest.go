//go:build linux

package agent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// SelfCheck is one thing the agent looked at in the container it runs in.
type SelfCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

// SelfTest is what `cbe-agent -selftest` prints: whether the image, run the way a student's container is run, keeps the contract of a workspace image
// (images/README.md). The runner runs it on every image it builds, before it keeps the image; `cbe-portalctl image check` runs it on an author's computer.
type SelfTest struct {
	Version string      `json:"version"`
	OK      bool        `json:"ok"`
	Checks  []SelfCheck `json:"checks"`
}

// Failed lists what did not pass, as a sentence.
func (t SelfTest) Failed() string {
	var out []string
	for _, c := range t.Checks {
		if !c.OK {
			out = append(out, c.Name+": "+c.Detail)
		}
	}
	return strings.Join(out, "; ")
}

// walkLimit is how long the look for setuid files may take: a very large image is not a reason to hold up a build for ever.
const walkLimit = 40 * time.Second

// RunSelfTest looks at the container it runs in. It is read-only: it writes one file in /tmp, and removes it.
func RunSelfTest(cfg Config) SelfTest {
	cfg.fill()
	t := SelfTest{Version: cfg.Version, OK: true}
	add := func(name string, ok bool, detail string) {
		t.Checks = append(t.Checks, SelfCheck{Name: name, OK: ok, Detail: detail})
		if !ok {
			t.OK = false
		}
	}
	uid, gid := os.Geteuid(), os.Getegid()
	add("user", uid == StudentID && gid == StudentID, fmt.Sprintf("the agent runs as %d:%d, and it must run as %d:%d: the image must end with USER %d:%d", uid, gid, StudentID, StudentID, StudentID, StudentID))
	name, found := accountOf("/etc/passwd", StudentID)
	add("account", found && name == cfg.User, fmt.Sprintf("/etc/passwd names the user of uid %d %q, and it must be %q", StudentID, name, cfg.User))
	if info, err := os.Stat(cfg.Shell); err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
		add("shell", false, fmt.Sprintf("there is no shell at %s: the terminals of the students need /bin/bash or /bin/sh in the image", cfg.Shell))
	} else {
		add("shell", true, cfg.Shell)
	}
	if info, err := os.Stat(cfg.Work); err != nil || !info.IsDir() {
		add("work", false, fmt.Sprintf("%s is not a folder in the image: the student's files are mounted there", cfg.Work))
	} else {
		add("work", true, "")
	}
	add("tmp", tmpWritable("/tmp"), "/tmp cannot be written: the container is run with a small tmpfs there")
	add("root-read-only", !rootWritable(), "the root file system is writable: the container must be run with a read-only root")
	caps, noNewPrivs := privileges("/proc/self/status")
	add("capabilities", caps == "0000000000000000", "the container has capabilities ("+caps+"): they must all be dropped")
	add("no-new-privileges", noNewPrivs, "no-new-privileges is not set on the container")
	add("network", onlyLoopback(), "the container has a network interface other than the loopback one: it must run with no network")
	add("no-sudo", !exists("/usr/bin/sudo") && !exists("/usr/sbin/sudo") && !exists("/etc/sudoers"), "sudo is in the image: a student must have no way to become another user")
	root, skip := cfg.walkRoot, []string(nil)
	if root == "" {
		root, skip = "/", walkSkip
	}
	files, setuid, timedOut := findSetuid(root, skip, 8, walkLimit)
	switch {
	case len(setuid) > 0:
		add("no-setuid-files", false, "the image has files with the setuid or setgid bit, which are a way up: "+strings.Join(setuid, ", "))
	case timedOut:
		add("no-setuid-files", false, fmt.Sprintf("the look at the files did not end in %s (after %d files)", walkLimit, files))
	default:
		add("no-setuid-files", true, fmt.Sprintf("%d files looked at", files))
	}
	return t
}

// WriteSelfTest runs the self test and prints it as JSON. The status is 0 when every check passed.
func WriteSelfTest(w io.Writer, cfg Config) int {
	t := RunSelfTest(cfg)
	data, _ := json.MarshalIndent(t, "", "  ")
	fmt.Fprintln(w, string(data))
	if !t.OK {
		return 1
	}
	return 0
}

// StudentID is the uid and gid of the student in every workspace image (decision P0-30).
const StudentID = 1000

// walkSkip are the places the setuid look does not enter: what is not part of the image (the kernel's views, the device files, the mounts of the container).
var walkSkip = []string{"/proc", "/sys", "/dev", "/run", "/tmp", "/work"}

func exists(path string) bool { _, err := os.Lstat(path); return err == nil }

// accountOf is the name of the account with a uid in a passwd file.
func accountOf(passwd string, uid int) (string, bool) {
	file, err := os.Open(passwd)
	if err != nil {
		return "", false
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), ":")
		if len(fields) >= 3 && fields[2] == strconv.Itoa(uid) {
			return fields[0], true
		}
	}
	return "", false
}

func tmpWritable(dir string) bool {
	file, err := os.CreateTemp(dir, "cbe-selftest-*")
	if err != nil {
		return false
	}
	name := file.Name()
	file.Close()
	return os.Remove(name) == nil
}

// rootWritable says whether a file can be made in the root of the file system.
func rootWritable() bool {
	err := syscall.Mkdir("/cbe-selftest", 0o700)
	if err == nil {
		_ = syscall.Rmdir("/cbe-selftest")
		return true
	}
	return err != syscall.EROFS && err != syscall.EACCES && err != syscall.EPERM
}

// privileges reads the effective capabilities and the no-new-privileges flag of the process from its status file.
func privileges(status string) (effective string, noNewPrivs bool) {
	data, err := os.ReadFile(status)
	if err != nil {
		return "unknown", false
	}
	effective = "unknown"
	for _, line := range strings.Split(string(data), "\n") {
		switch {
		case strings.HasPrefix(line, "CapEff:"):
			effective = strings.TrimSpace(strings.TrimPrefix(line, "CapEff:"))
		case strings.HasPrefix(line, "NoNewPrivs:"):
			noNewPrivs = strings.TrimSpace(strings.TrimPrefix(line, "NoNewPrivs:")) == "1"
		}
	}
	return effective, noNewPrivs
}

func onlyLoopback() bool {
	interfaces, err := net.Interfaces()
	if err != nil {
		return false
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback == 0 && iface.Flags&net.FlagUp != 0 {
			return false
		}
	}
	return true
}

// findSetuid walks a tree and returns how many files it looked at, the first few regular files that have the setuid or setgid bit, and whether
// the time ran out first. skip are folders it does not enter.
func findSetuid(root string, skip []string, most int, limit time.Duration) (files int, found []string, timedOut bool) {
	deadline := time.Now().Add(limit)
	skipped := map[string]bool{}
	for _, dir := range skip {
		skipped[filepath.Clean(dir)] = true
	}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && skipped[path] {
			return filepath.SkipDir
		}
		files++
		if files%1024 == 0 && time.Now().After(deadline) {
			timedOut = true
			return filepath.SkipAll
		}
		if d.Type().IsRegular() {
			if info, err := d.Info(); err == nil && info.Mode()&(fs.ModeSetuid|fs.ModeSetgid) != 0 {
				if len(found) < most {
					found = append(found, path)
				}
			}
		}
		return nil
	})
	return files, found, timedOut
}
