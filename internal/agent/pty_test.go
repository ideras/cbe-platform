//go:build linux

package agent

import (
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"exam-platform/internal/agentproto"
)

func open(t *testing.T, c *agentproto.Client, cmd string) uint32 {
	t.Helper()
	reply := do(t, c, &agentproto.PTYOpen{Header: agentproto.Header{Type: agentproto.TypePTYOpen}, Cols: 80, Rows: 24, Cmd: cmd})
	if reply.Term == 0 {
		t.Fatalf("no terminal ID: %+v", reply)
	}
	return reply.Term
}

func exited(t *testing.T, c *agentproto.Client, term uint32) agentproto.Message {
	t.Helper()
	return event(t, c, "pty.exit", func(m agentproto.Message) bool { return m.Type == agentproto.TypePTYExit && m.Term == term })
}

func TestACommandRunsAndReportsItsExitCode(t *testing.T) {
	r := startAgent(t, nil)
	c := r.dial(t)
	for cmd, want := range map[string]int{"echo hello; exit 0": 0, "echo oops >&2; exit 3": 3, "false": 1, "exit 255": 255} {
		term := open(t, c, cmd)
		message := exited(t, c, term)
		if message.ExitCode == nil || *message.ExitCode != want || message.Signal != "" || message.OOMKilled {
			t.Errorf("%q: exit = %+v", cmd, message)
		}
		doClose(t, c, term)
	}
}

func doClose(t *testing.T, c *agentproto.Client, term uint32) {
	t.Helper()
	do(t, c, &agentproto.PTYClose{Header: agentproto.Header{Type: agentproto.TypePTYClose}, Term: term})
}

func TestASignalIsReported(t *testing.T) {
	r := startAgent(t, nil)
	c := r.dial(t)
	term := open(t, c, "kill -9 $$")
	message := exited(t, c, term)
	if message.ExitCode == nil || *message.ExitCode != 137 || message.Signal != "SIGKILL" {
		t.Fatalf("exit = %+v", message)
	}
}

// The plan's acceptance: a terminal runs make, streams its output, and reports the exit code.
func TestMakeRunsInATerminal(t *testing.T) {
	needTool(t, "make")
	r := startAgent(t, nil)
	c := r.dial(t)
	write(t, filepath.Join(r.work, "Makefile"), "all:\n\t@echo step one\n\t@sleep 0.2\n\t@echo step two\n\nfail:\n\t@echo failing\n\t@false\n")
	term := open(t, c, "make")
	out := output(t, c, term, "step two")
	if !strings.Contains(out, "step one") || strings.Index(out, "step one") > strings.Index(out, "step two") {
		t.Errorf("output = %q", out)
	}
	if message := exited(t, c, term); *message.ExitCode != 0 {
		t.Errorf("exit = %+v", message)
	}
	term = open(t, c, "make fail")
	output(t, c, term, "failing")
	message := exited(t, c, term)
	if *message.ExitCode != 2 {
		t.Errorf("exit = %+v", message)
	}
	// The work folder is the program's folder.
	term = open(t, c, "pwd; echo $HOME $USER $TERM")
	output(t, c, term, r.agent.cfg.Home+" student xterm-256color")
}

func TestAShellTakesInputAndResizes(t *testing.T) {
	r := startAgent(t, nil)
	c := r.dial(t)
	term := do(t, c, &agentproto.PTYOpen{Header: agentproto.Header{Type: agentproto.TypePTYOpen}, Cols: 80, Rows: 24}).Term
	if err := c.Input(term, []byte("stty size\n")); err != nil {
		t.Fatal(err)
	}
	output(t, c, term, "24 80")
	do(t, c, &agentproto.PTYResize{Header: agentproto.Header{Type: agentproto.TypePTYResize}, Term: term, Cols: 120, Rows: 40})
	if err := c.Input(term, []byte("stty size\n")); err != nil {
		t.Fatal(err)
	}
	output(t, c, term, "40 120")
	// Unicode and control characters go through as typed.
	if err := c.Input(term, []byte("echo ñandú-$((6*7))\n")); err != nil {
		t.Fatal(err)
	}
	output(t, c, term, "ñandú-42")
	if err := c.Input(term, []byte("exit 7\n")); err != nil {
		t.Fatal(err)
	}
	if message := exited(t, c, term); *message.ExitCode != 7 {
		t.Errorf("exit = %+v", message)
	}
	list := do(t, c, &agentproto.PTYList{Header: agentproto.Header{Type: agentproto.TypePTYList}})
	if len(list.Terminals) != 1 || !list.Terminals[0].Exited || *list.Terminals[0].ExitCode != 7 || list.Terminals[0].Cols != 120 || list.Terminals[0].Rows != 40 {
		t.Errorf("terminals = %+v", list.Terminals)
	}
}

func TestATerminalOpensInAFolder(t *testing.T) {
	r := startAgent(t, nil)
	c := r.dial(t)
	write(t, filepath.Join(r.work, "src", "main.c"), "x")
	term := do(t, c, &agentproto.PTYOpen{Header: agentproto.Header{Type: agentproto.TypePTYOpen}, Cols: 80, Rows: 24, Cmd: "ls", Cwd: "src"}).Term
	output(t, c, term, "main.c")
	doFails(t, c, &agentproto.PTYOpen{Header: agentproto.Header{Type: agentproto.TypePTYOpen}, Cols: 80, Rows: 24, Cwd: "missing"}, agentproto.CodeNotFound)
	write(t, filepath.Join(r.work, "file"), "x")
	doFails(t, c, &agentproto.PTYOpen{Header: agentproto.Header{Type: agentproto.TypePTYOpen}, Cols: 80, Rows: 24, Cwd: "file"}, agentproto.CodeBadPath)
	_ = os.Symlink("/etc", filepath.Join(r.work, "evil"))
	doFails(t, c, &agentproto.PTYOpen{Header: agentproto.Header{Type: agentproto.TypePTYOpen}, Cols: 80, Rows: 24, Cwd: "evil"}, agentproto.CodeBadPath)
}

func TestAtMostFourTerminals(t *testing.T) {
	r := startAgent(t, nil)
	c := r.dial(t)
	var terms []uint32
	for range agentproto.MaxTerminals {
		terms = append(terms, open(t, c, "sleep 30"))
	}
	doFails(t, c, &agentproto.PTYOpen{Header: agentproto.Header{Type: agentproto.TypePTYOpen}, Cols: 80, Rows: 24}, agentproto.CodeLimit)
	// Closing one makes room, and the program on it is gone.
	doClose(t, c, terms[0])
	message := exited(t, c, terms[0])
	if message.Signal != "SIGHUP" && message.Signal != "SIGKILL" {
		t.Errorf("a closed terminal's program ended with %+v", message)
	}
	_ = open(t, c, "sleep 30")
	doFails(t, c, &agentproto.PTYClose{Header: agentproto.Header{Type: agentproto.TypePTYClose}, Term: terms[0]}, agentproto.CodeNotFound)
	doFails(t, c, &agentproto.PTYResize{Header: agentproto.Header{Type: agentproto.TypePTYResize}, Term: 99, Cols: 80, Rows: 24}, agentproto.CodeNotFound)
	doFails(t, c, &agentproto.PTYAttach{Header: agentproto.Header{Type: agentproto.TypePTYAttach}, Term: 99}, agentproto.CodeNotFound)
}

func TestAClosedTerminalKillsAProgramThatIgnoresHangup(t *testing.T) {
	r := startAgent(t, nil)
	c := r.dial(t)
	term := open(t, c, "trap '' HUP; echo ready; while :; do sleep 0.05; done")
	output(t, c, term, "ready")
	doClose(t, c, term)
	message := exited(t, c, term)
	if message.Signal != "SIGKILL" {
		t.Fatalf("exit = %+v", message)
	}
}

// Terminals live on when the connection goes: a new connection attaches and sees what the terminal printed.
func TestAReconnectingClientAttachesAndSeesTheOutput(t *testing.T) {
	r := startAgent(t, nil)
	first := r.dial(t)
	term := open(t, first, "echo before; read line; echo got:$line")
	output(t, first, term, "before")
	first.Close()
	waitFor(t, "the first connection to go", func() bool { return len(r.agent.connections()) == 0 })

	second := r.dial(t)
	list := do(t, second, &agentproto.PTYList{Header: agentproto.Header{Type: agentproto.TypePTYList}})
	if len(list.Terminals) != 1 || list.Terminals[0].Term != term || list.Terminals[0].Exited {
		t.Fatalf("terminals = %+v", list.Terminals)
	}
	do(t, second, &agentproto.PTYAttach{Header: agentproto.Header{Type: agentproto.TypePTYAttach}, Term: term})
	output(t, second, term, "before")
	if err := second.Input(term, []byte("again\n")); err != nil {
		t.Fatal(err)
	}
	output(t, second, term, "got:again")
	exited(t, second, term)
	// A terminal that has ended can still be attached, and its output is replayed.
	third := r.dial(t)
	reply := do(t, third, &agentproto.PTYAttach{Header: agentproto.Header{Type: agentproto.TypePTYAttach}, Term: term})
	if !reply.Exited || *reply.ExitCode != 0 {
		t.Errorf("attach = %+v", reply)
	}
	output(t, third, term, "got:again")
}

func TestOutputGoesOnlyToConnectionsThatAreAttached(t *testing.T) {
	r := startAgent(t, nil)
	a, b := r.dial(t), r.dial(t)
	term := open(t, a, "echo only-for-a; sleep 1")
	output(t, a, term, "only-for-a")
	select {
	case out := <-b.Terminal:
		t.Fatalf("a connection that never attached got output: %q", out.Data)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestInputToAMissingTerminalIsReported(t *testing.T) {
	r := startAgent(t, nil)
	c := r.dial(t)
	if err := c.Input(42, []byte("x")); err != nil {
		t.Fatal(err)
	}
	m := event(t, c, "agent.error", ofType(agentproto.TypeSessionErr))
	if m.Code != agentproto.CodeNotFound || m.Term != 42 {
		t.Errorf("message = %+v", m)
	}
}

// The memory limit's kill is read from the cgroup's own counter, because the runtime's flag is never set (P0-8).
func TestAMemoryKillIsReported(t *testing.T) {
	r := startAgent(t, nil)
	c := r.dial(t)
	term := open(t, c, "printf 'oom_kill 1\\n' > "+r.memory+"; kill -9 $$")
	message := exited(t, c, term)
	if !message.OOMKilled || message.Signal != "SIGKILL" {
		t.Fatalf("exit = %+v", message)
	}
	term = open(t, c, "exit 0")
	if message := exited(t, c, term); message.OOMKilled {
		t.Errorf("a later command was blamed for an earlier kill: %+v", message)
	}
	if hb := event(t, c, "a heartbeat", ofType(agentproto.TypeHeartbeat)); hb.OOMKills != 1 {
		t.Errorf("heartbeat = %+v", hb)
	}
}

// A program that prints without end is held back by a connection that does not read: the agent's memory does not grow, because the
// reading of the terminal waits until the connection has taken what it was given.
func TestARunawayProgramIsHeldByAConnectionThatDoesNotRead(t *testing.T) {
	r := startAgent(t, nil)
	conn, err := net.Dial("unix", r.socket)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	request, _ := agentproto.Encode(agentproto.PTYOpen{Header: agentproto.Header{ID: 1, Type: agentproto.TypePTYOpen}, Cols: 80, Rows: 24, Cmd: "yes"})
	if err := agentproto.WriteFrame(conn, agentproto.Frame{Kind: agentproto.KindJSON, Data: request}); err != nil {
		t.Fatal(err)
	}
	// Never read from conn. Two seconds of `yes` is gigabytes if anything keeps it.
	time.Sleep(2 * time.Second)
	runtime.GC()
	runtime.ReadMemStats(&after)
	if grown := int64(after.HeapAlloc) - int64(before.HeapAlloc); grown > 64<<20 {
		t.Fatalf("the agent's heap grew by %d MiB while nobody read the output", grown>>20)
	}
	// And the agent still answers everyone else.
	other := r.dial(t)
	list := do(t, other, &agentproto.PTYList{Header: agentproto.Header{Type: agentproto.TypePTYList}})
	if len(list.Terminals) != 1 || list.Terminals[0].Exited {
		t.Fatalf("terminals = %+v", list.Terminals)
	}
	doClose(t, other, list.Terminals[0].Term)
}

func TestTheChildEnvironmentIsTheAgentsOwn(t *testing.T) {
	t.Setenv("CBE_SECRET_FROM_RUNNER", "do-not-pass")
	r := startAgent(t, func(c *Config) { c.Env = []string{"COURSE=cpp20"} })
	c := r.dial(t)
	term := open(t, c, "env | sort")
	out := output(t, c, term, "TMPDIR=/tmp")
	if strings.Contains(out, "CBE_SECRET_FROM_RUNNER") || !strings.Contains(out, "COURSE=cpp20") || !strings.Contains(out, "LANG=C.UTF-8") {
		t.Errorf("environment = %q", out)
	}
}
