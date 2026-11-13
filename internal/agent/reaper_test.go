//go:build linux

package agent

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestTheReaperReportsHowProgramsEnd(t *testing.T) {
	r := NewReaper(false)
	for _, test := range []struct {
		script string
		code   int
		signal syscall.Signal
	}{{"exit 0", 0, 0}, {"exit 7", 7, 0}, {"kill -TERM $$", 143, syscall.SIGTERM}, {"kill -9 $$", 137, syscall.SIGKILL}} {
		_, status, err := r.Start("/bin/sh", []string{"sh", "-c", test.script}, &syscall.ProcAttr{Env: os.Environ(), Files: []uintptr{0, 1, 2}})
		if err != nil {
			t.Fatal(err)
		}
		select {
		case end := <-status:
			if end.Code != test.code || end.Signal != test.signal {
				t.Errorf("%q ended with %+v", test.script, end)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%q: no status", test.script)
		}
	}
	if _, _, err := r.Start("/no/such/program", []string{"x"}, &syscall.ProcAttr{}); err == nil {
		t.Error("a missing program started")
	}
}

// As PID 1 the agent is the parent of every orphan. This runs the reaper in a process of its own that has made itself the reaper of
// its descendants (what PID 1 is), starts a program that leaves an orphan behind, and looks for zombies.
func TestAnOrphanedProgramIsReaped(t *testing.T) {
	if os.Getenv("CBE_REAPER_HELPER") == "1" {
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestReaperHelper$", "-test.v")
	cmd.Env = append(os.Environ(), "CBE_REAPER_HELPER=1")
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "REAPER-OK") {
		t.Fatalf("the helper failed: %v\n%s", err, out)
	}
}

func TestReaperHelper(t *testing.T) {
	if os.Getenv("CBE_REAPER_HELPER") != "1" {
		t.Skip("run by TestAnOrphanedProgramIsReaped")
	}
	// Orphans of this process's descendants come to it, as they would to PID 1.
	if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0); err != nil {
		t.Skipf("no subreaper: %v", err)
	}
	r := NewReaper(true)
	defer r.Stop()
	_, status, err := r.Start("/bin/sh", []string{"sh", "-c", "(sleep 0.3; exit 4) & (sleep 0.1) & exit 3"}, &syscall.ProcAttr{Env: os.Environ(), Files: []uintptr{0, 1, 2}})
	if err != nil {
		t.Fatal(err)
	}
	if end := <-status; end.Code != 3 {
		t.Fatalf("the program ended with %+v", end)
	}
	// The two background jobs are orphans of this process now. Within a few seconds neither is a zombie, and none is left.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if children() == "" {
			t.Log("REAPER-OK")
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("children are left: %s", children())
}

// children lists this process's children that exist, by process ID and state.
func children() string {
	var found []string
	entries, _ := os.ReadDir("/proc")
	me := strconv.Itoa(os.Getpid())
	for _, entry := range entries {
		data, err := os.ReadFile("/proc/" + entry.Name() + "/stat")
		if err != nil {
			continue
		}
		// pid (comm) state ppid ...: the command may hold spaces and parentheses, so read after the last ")".
		text := string(data)
		fields := strings.Fields(text[strings.LastIndex(text, ")")+1:])
		if len(fields) > 1 && fields[1] == me {
			found = append(found, entry.Name()+":"+fields[0])
		}
	}
	return strings.Join(found, " ")
}
