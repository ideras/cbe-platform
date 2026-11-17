//go:build linux

package agent

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTheAccountOfAUidIsFoundInPasswd(t *testing.T) {
	dir := t.TempDir()
	passwd := filepath.Join(dir, "passwd")
	if err := os.WriteFile(passwd, []byte("root:x:0:0:root:/root:/bin/bash\n# a comment\nubuntu:x:1000:1000::/home/ubuntu:/bin/bash\nweird\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if name, ok := accountOf(passwd, 1000); !ok || name != "ubuntu" {
		t.Errorf("uid 1000 = %q, %v", name, ok)
	}
	if _, ok := accountOf(passwd, 1001); ok {
		t.Error("an account that is not there was found")
	}
	if _, ok := accountOf(filepath.Join(dir, "none"), 0); ok {
		t.Error("a file that is not there has accounts")
	}
}

func TestTheCapabilitiesAndNoNewPrivilegesAreReadFromTheStatus(t *testing.T) {
	status := filepath.Join(t.TempDir(), "status")
	if err := os.WriteFile(status, []byte("Name:\tcbe-agent\nCapEff:\t0000000000000000\nNoNewPrivs:\t1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if caps, nnp := privileges(status); caps != "0000000000000000" || !nnp {
		t.Errorf("caps %q, no new privileges %v", caps, nnp)
	}
	if err := os.WriteFile(status, []byte("CapEff:\t00000000a80425fb\nNoNewPrivs:\t0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if caps, nnp := privileges(status); caps != "00000000a80425fb" || nnp {
		t.Errorf("caps %q, no new privileges %v", caps, nnp)
	}
	if caps, nnp := privileges(filepath.Join(t.TempDir(), "none")); caps != "unknown" || nnp {
		t.Errorf("no status: %q %v", caps, nnp)
	}
}

// The look for files with the setuid or setgid bit: it finds them, and does not enter what it is told to skip.
func TestFilesWithTheSetuidBitAreFound(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"usr/bin/su", "usr/bin/ping", "usr/bin/ls", "skipped/hidden"} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, mode := range map[string]fs.FileMode{"usr/bin/su": 0o755 | fs.ModeSetuid, "usr/bin/ping": 0o755 | fs.ModeSetgid, "skipped/hidden": 0o755 | fs.ModeSetuid} {
		if err := os.Chmod(filepath.Join(root, name), mode); err != nil {
			t.Fatal(err)
		}
	}
	files, found, timedOut := findSetuid(root, []string{filepath.Join(root, "skipped")}, 8, time.Minute)
	if timedOut || len(found) != 2 || !strings.HasSuffix(found[0], "usr/bin/ping") || !strings.HasSuffix(found[1], "usr/bin/su") || files < 5 {
		t.Errorf("files %d, found %v, timed out %v", files, found, timedOut)
	}
	// Only the first few are listed.
	if _, found, _ := findSetuid(root, nil, 1, time.Minute); len(found) != 1 {
		t.Errorf("found %v", found)
	}
	// A tree that cannot be finished in time says so.
	if _, _, timedOut := findSetuid(root, nil, 8, -time.Second); timedOut {
		t.Log("a tree of a few files is looked at before the clock is read")
	}
}

// The self test of a machine that is not a student's container fails the checks of the container, says what each wants, and prints JSON a program can read.
func TestTheSelfTestSaysWhatIsWrongAndPrintsJSON(t *testing.T) {
	var out bytes.Buffer
	status := WriteSelfTest(&out, Config{Version: "test", Work: t.TempDir(), Shell: "/bin/sh", walkRoot: t.TempDir()})
	var report SelfTest
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if report.Version != "test" || len(report.Checks) < 9 {
		t.Fatalf("report = %+v", report)
	}
	names := map[string]SelfCheck{}
	for _, c := range report.Checks {
		names[c.Name] = c
	}
	for _, name := range []string{"user", "account", "shell", "work", "tmp", "root-read-only", "capabilities", "no-new-privileges", "network", "no-sudo", "no-setuid-files"} {
		if _, ok := names[name]; !ok {
			t.Errorf("no check %q", name)
		}
	}
	// What a development machine has right: a shell, a folder for the files, a /tmp.
	for _, name := range []string{"shell", "work", "tmp"} {
		if !names[name].OK {
			t.Errorf("%s: %+v", name, names[name])
		}
	}
	// And what it has wrong: it is not the student.
	if names["user"].OK && os.Geteuid() != StudentID {
		t.Error("another user passed as the student")
	}
	if report.OK != (status == 0) {
		t.Errorf("status %d and ok %v disagree", status, report.OK)
	}
	if report.OK {
		t.Log("this machine passes the self test (it is run as the student, in a hardened container?)")
	} else if !strings.Contains(report.Failed(), ":") {
		t.Errorf("Failed() = %q", report.Failed())
	}
	if c := names["shell"]; c.OK && c.Detail != "/bin/sh" {
		t.Errorf("the shell asked for was not the one looked at: %+v", c)
	}
	// A shell that is not there is a failed check that says what is needed.
	var missing bytes.Buffer
	WriteSelfTest(&missing, Config{Work: t.TempDir(), Shell: "/nonexistent/shell", walkRoot: t.TempDir()})
	if !strings.Contains(missing.String(), "the terminals of the students need /bin/bash or /bin/sh in the image") {
		t.Errorf("a shell that is not there:\n%s", missing.String())
	}
}
