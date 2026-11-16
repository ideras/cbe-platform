//go:build linux

package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"exam-platform/internal/agentproto"
)

type rig struct {
	agent  *Agent
	work   string
	socket string
	memory string
}

// startAgent runs an agent in this process, on a folder of its own and a socket of its own.
func startAgent(t *testing.T, change func(*Config)) *rig {
	t.Helper()
	shortDir, err := os.MkdirTemp("", "cbe")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(shortDir) })
	work := filepath.Join(shortDir, "work")
	if err := os.Mkdir(work, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := Config{Socket: filepath.Join(shortDir, "a.sock"), Work: work, Home: filepath.Join(shortDir, "home"), Heartbeat: 100 * time.Millisecond,
		KillGrace: 300 * time.Millisecond, MemoryEvents: filepath.Join(shortDir, "memory.events")}
	if _, err := os.Stat("/bin/bash"); err != nil {
		cfg.Shell = "/bin/sh"
	}
	if err := os.WriteFile(cfg.MemoryEvents, []byte("low 0\nhigh 0\nmax 0\noom 0\noom_kill 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if change != nil {
		change(&cfg)
	}
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = a.Serve(ctx); close(done) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("the agent did not stop")
		}
	})
	r := &rig{agent: a, work: work, socket: cfg.Socket, memory: cfg.MemoryEvents}
	// A client that connects before the agent has accepted is queued by the kernel, so there is nothing to wait for but the agent.
	waitFor(t, "the agent to listen", a.Ready)
	return r
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func (r *rig) dial(t *testing.T) *agentproto.Client {
	t.Helper()
	c, err := agentproto.Dial(r.socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

const wait = 10 * time.Second

func do(t *testing.T, c *agentproto.Client, request agentproto.Request) agentproto.Message {
	t.Helper()
	reply, err := c.Do(request, wait)
	if err != nil {
		t.Fatalf("%s: %v", request.Head().Type, err)
	}
	return reply
}

func doFails(t *testing.T, c *agentproto.Client, request agentproto.Request, want string) agentproto.Message {
	t.Helper()
	reply, err := c.Do(request, wait)
	if agentproto.CodeOf(err) != want {
		t.Fatalf("%s: error = %v, want %s", request.Head().Type, err, want)
	}
	return reply
}

// event waits for a message from the agent that matches.
func event(t *testing.T, c *agentproto.Client, what string, match func(agentproto.Message) bool) agentproto.Message {
	t.Helper()
	deadline := time.After(wait)
	for {
		select {
		case m, open := <-c.Events:
			if !open {
				t.Fatalf("the connection closed while waiting for %s", what)
			}
			if match(m) {
				return m
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func ofType(kind string) func(agentproto.Message) bool {
	return func(m agentproto.Message) bool { return m.Type == kind }
}

// output collects a terminal's output until it contains want.
func output(t *testing.T, c *agentproto.Client, term uint32, want string) string {
	t.Helper()
	var got strings.Builder
	deadline := time.After(wait)
	for {
		select {
		case out, open := <-c.Terminal:
			if !open {
				t.Fatalf("the connection closed; output so far %q, waiting for %q", got.String(), want)
			}
			if out.Term == term {
				got.Write(out.Data)
				if strings.Contains(got.String(), want) {
					return got.String()
				}
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %q in the output of terminal %d; got %q", want, term, got.String())
		}
	}
}

func needTool(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s is not installed", name)
	}
}
