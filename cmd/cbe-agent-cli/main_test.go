//go:build linux

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"exam-platform/internal/agent"
)

// The harness is tested against a real agent in this process: it is what the later phases' tests use, so it has to be right.
func startAgent(t *testing.T) (socket, work string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "cli")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	work = filepath.Join(dir, "work")
	if err := os.Mkdir(work, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := agent.Config{Socket: filepath.Join(dir, "a.sock"), Work: work, Home: filepath.Join(dir, "home"), Heartbeat: 100 * time.Millisecond, Shell: "/bin/sh"}
	a, err := agent.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = a.Serve(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	for range 500 {
		if _, err := os.Stat(cfg.Socket); err == nil {
			time.Sleep(20 * time.Millisecond)
			return cfg.Socket, work
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no socket")
	return
}

func cli(t *testing.T, socket, stdin string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = run(append([]string{"-socket", socket, "-timeout", "10s"}, args...), strings.NewReader(stdin), &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestFileCommands(t *testing.T) {
	socket, work := startAgent(t)
	steps := []struct {
		args   []string
		stdin  string
		code   int
		stdout string // a substring
		stderr string
	}{
		{[]string{"mkdir", "src"}, "", 0, "", ""},
		{[]string{"create", "src/a.txt"}, "hello\n", 0, "sha256:", ""},
		{[]string{"cat", "src/a.txt"}, "", 0, "hello\n", ""},
		{[]string{"write", "src/a.txt"}, "changed\n", 0, "sha256:", ""},
		{[]string{"cat", "src/a.txt"}, "", 0, "changed\n", ""},
		{[]string{"ls", "src"}, "", 0, "file          8  a.txt", ""},
		{[]string{"ls"}, "", 0, "dir           0  src", ""},
		{[]string{"mv", "src/a.txt", "src/b.txt"}, "", 0, "", ""},
		{[]string{"cat", "src/a.txt"}, "", 1, "", "NOT_FOUND"},
		{[]string{"create", "src/b.txt"}, "", 1, "", "EXISTS"},
		{[]string{"cat", "../../etc/passwd"}, "", 1, "", "BAD_PATH"},
		{[]string{"rm", "src"}, "", 0, "", ""},
		{[]string{"ls", "src"}, "", 1, "", "NOT_FOUND"},
		{[]string{"-json", "ls"}, "", 0, `"ok":true`, ""},
		{[]string{"raw", `{"type":"agent.ping"}`}, "", 0, `"type":"agent.ping"`, ""},
		{[]string{"raw", `{"type":"fs.nonsense"}`}, "", 1, "BAD_REQUEST", ""},
		{[]string{"status"}, "", 0, `"type":"agent.hello"`, ""},
	}
	for _, step := range steps {
		code, stdout, stderr := cli(t, socket, step.stdin, step.args...)
		if code != step.code || !strings.Contains(stdout, step.stdout) || !strings.Contains(stderr, step.stderr) {
			t.Errorf("%v: code %d, stdout %q, stderr %q", step.args, code, stdout, stderr)
		}
	}
	if _, err := os.Stat(filepath.Join(work, "src")); !os.IsNotExist(err) {
		t.Error("rm left the folder")
	}
}

func TestExecRunsACommandAndReturnsItsExitCode(t *testing.T) {
	socket, work := startAgent(t)
	if err := os.WriteFile(filepath.Join(work, "greeting.txt"), []byte("from a file"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ := cli(t, socket, "", "exec", "cat", "greeting.txt", "&&", "echo", "done")
	if code != 0 || !strings.Contains(stdout, "from a file") || !strings.Contains(stdout, "done") {
		t.Errorf("exec = %d, %q", code, stdout)
	}
	if code, stdout, _ := cli(t, socket, "", "exec", "echo", "bye;", "exit", "42"); code != 42 || !strings.Contains(stdout, "bye") {
		t.Errorf("exec exit = %d, %q", code, stdout)
	}
	if code, _, _ := cli(t, socket, "", "exec", "kill", "-9", "$$"); code != 137 {
		t.Errorf("exec of a killed command = %d", code)
	}
	if code, _, _ := cli(t, socket, "", "exec", "-cwd", "missing", "ls"); code != 1 {
		t.Errorf("exec in a missing folder = %d", code)
	}
	_, size, _ := cli(t, socket, "", "exec", "-cols", "132", "-rows", "50", "stty", "size")
	if !strings.Contains(size, "50 132") {
		t.Errorf("stty size = %q", size)
	}
}

func TestWatchTermsAndClose(t *testing.T) {
	socket, work := startAgent(t)
	go func() {
		time.Sleep(300 * time.Millisecond)
		_ = os.WriteFile(filepath.Join(work, "seen.txt"), []byte("x"), 0o644)
	}()
	code, stdout, _ := cli(t, socket, "", "watch", "-n", "1", "-for", "5s")
	if code != 0 || !strings.Contains(stdout, `"kind":"create"`) || !strings.Contains(stdout, `"path":"seen.txt"`) {
		t.Errorf("watch = %d, %q", code, stdout)
	}
	if code, stdout, _ := cli(t, socket, "", "terms"); code != 0 || stdout != "" {
		t.Errorf("terms = %d, %q", code, stdout)
	}
	if code, _, stderr := cli(t, socket, "", "close", "9"); code != 1 || !strings.Contains(stderr, "NOT_FOUND") {
		t.Errorf("close = %d, %q", code, stderr)
	}
}

func TestMistakesInTheCommandLine(t *testing.T) {
	socket, _ := startAgent(t)
	for _, args := range [][]string{{}, {"nonsense"}, {"cat"}, {"mv", "a"}, {"attach", "x"}, {"exec"}, {"raw", "not json"}, {"ls", "a", "b"}} {
		if code, _, _ := cli(t, socket, "", args...); code != 2 {
			t.Errorf("%v exited with %d, want 2", args, code)
		}
	}
	var out, errOut bytes.Buffer
	if code := run([]string{"-socket", "/no/such/socket", "ls"}, strings.NewReader(""), &out, &errOut); code != 1 || errOut.Len() == 0 {
		t.Errorf("no agent: %d, %q", code, errOut.String())
	}
}
