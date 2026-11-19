// Package imagetest tries an installed image the way a student uses it (decision D-CB-10). It asks the runner for a workspace made from the image,
// as the exam server does (container.create, seed and start, with a student's limits), drives the agent in it through the runner (agent.connect),
// runs the checks every image must pass and the course's own (a checks file in the image's folder), and removes the workspace whatever happened.
// It is `cbe-portalctl image test`, which replaced images/test.sh.
package imagetest

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"exam-platform/internal/agentproto"
	"exam-platform/internal/runner"
)

// Runner is what the test needs of the runner: the calls the exam server makes for a student's workspace. *runner.Client is one.
type Runner interface {
	Ping(ctx context.Context) error
	ImageInspect(ctx context.Context, name string) (runner.ImageStatus, error)
	Create(ctx context.Context, req runner.CreateRequest) (runner.Status, error)
	Seed(ctx context.Context, session string, bundle io.Reader) (runner.Status, error)
	Start(ctx context.Context, session string) (runner.Status, error)
	Stop(ctx context.Context, session string) (runner.Status, error)
	Status(ctx context.Context, session string) (runner.Status, error)
	Remove(ctx context.Context, session string) error
	ConnectAgent(ctx context.Context, session string) (net.Conn, error)
}

var _ Runner = (*runner.Client)(nil)

// Exam is the exam the test's workspaces belong to. The exam server's exams are UUIDs, so it never takes one of them for its own, and its sessions
// are named image-test-<random>.
const Exam = "image-test"

// Resources are the limits of the test's workspace: a student's, small, so that the limits are reached quickly.
var Resources = runner.Resources{CPU: 1, MemoryMB: 256, DiskMB: 20}

// Options say what to test and how.
type Options struct {
	// Image is the name the runner approves (cbe/cpp20).
	Image  string
	Runner Runner
	// Checks are the course's own (ParseChecks), run after the checks every image must pass.
	Checks []Check
	// RunRoot is the runner's run_root: after a clean stop the agent's socket must be gone from it. Empty: not looked at.
	RunRoot string
	// Out is where the report goes.
	Out io.Writer
}

// Result is what the test came to. Failures name the checks that failed.
type Result struct {
	Passed, Failed int
	Failures       []string
}

// OK says every check passed.
func (r Result) OK() bool { return r.Failed == 0 && r.Passed > 0 }

// Run tests the image. An error is something that stopped the test (the runner does not answer, does not approve the image, cannot make the
// workspace); a check that fails is in the Result.
func Run(ctx context.Context, o Options) (Result, error) {
	return run(ctx, o, allGroups)
}

// group is a set of checks run together under a title; a check returns why it failed, or nil.
type group struct {
	title  string
	checks []check
}

type check struct {
	name string
	run  func(*tester) error
}

// The groups, by name, in the order they run: the course's own checks come before the limits, which leave the workspace full and its memory spent,
// and the stop comes last.
const (
	groupAgent    = "agent"
	groupStudent  = "student"
	groupFiles    = "files"
	groupTerminal = "terminals"
	groupCourse   = "course"
	groupLimits   = "limits"
	groupStop     = "stop"
)

var allGroups = []string{groupAgent, groupStudent, groupFiles, groupTerminal, groupCourse, groupLimits, groupStop}

func run(ctx context.Context, o Options, groups []string) (Result, error) {
	out := o.Out
	if out == nil {
		out = io.Discard
	}
	say := func(format string, args ...any) { fmt.Fprintf(out, format+"\n", args...) }

	// A runner that was just restarted (image install --restart) takes a moment to answer.
	deadline := time.Now().Add(30 * time.Second)
	for {
		err := o.Runner.Ping(ctx)
		if err == nil {
			break
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return Result{}, fmt.Errorf("the runner does not answer: %w", err)
		}
		sleep(ctx, 500*time.Millisecond)
	}
	image, err := o.Runner.ImageInspect(ctx, o.Image)
	switch {
	case err != nil:
		return Result{}, fmt.Errorf("the runner cannot say whether it has %s: %w", o.Image, err)
	case !image.Approved:
		return Result{}, fmt.Errorf("the runner does not approve %s: install it with image install (a new name needs the runner restarted)", o.Image)
	case !image.Present:
		return Result{}, fmt.Errorf("the runner approves %s, and the container runtime does not have it: install it again with image install --replace", o.Image)
	}

	id := make([]byte, 6)
	if _, err := rand.Read(id); err != nil {
		return Result{}, err
	}
	session := "image-test-" + hex.EncodeToString(id)
	say("Testing %s (image %s) in a workspace of its own, %s, made by the runner as a student's is\n", o.Image, shortID(image.ID), session)
	// The workspace is removed whatever happened, and even when the test was interrupted (ctx is then done, and the removal has a context of its own).
	defer func() {
		removing, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := o.Runner.Remove(removing, session); err != nil {
			say("\nwarning: the workspace %s could not be removed (%v): the runner removes it when asked again (container.remove)", session, err)
			return
		}
		say("\nThe workspace %s was removed.", session)
	}()

	if _, err := o.Runner.Create(ctx, runner.CreateRequest{Session: session, Exam: Exam, Image: o.Image, Resources: Resources}); err != nil {
		return Result{}, fmt.Errorf("the runner did not make the workspace: %w", err)
	}
	if _, err := o.Runner.Seed(ctx, session, starter()); err != nil {
		return Result{}, fmt.Errorf("the runner did not put the starter in /work: %w", err)
	}
	if _, err := o.Runner.Start(ctx, session); err != nil {
		return Result{}, fmt.Errorf("the runner did not start the workspace: %w", err)
	}
	if err := waitAgent(ctx, o.Runner, session); err != nil {
		return Result{}, err
	}
	conn, err := o.Runner.ConnectAgent(ctx, session)
	if err != nil {
		return Result{}, fmt.Errorf("the runner did not connect to the agent: %w", err)
	}
	t := &tester{ctx: ctx, o: o, session: session, c: agentproto.NewClient(conn)}
	defer t.c.Close()

	var result Result
	for _, name := range groups {
		g := groupOf(name, o)
		if len(g.checks) == 0 {
			continue
		}
		say("== %s", g.title)
		for _, c := range g.checks {
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			err := c.run(t)
			if ctx.Err() != nil {
				say("  (interrupted: %s)", c.name)
				return result, ctx.Err()
			}
			if err != nil {
				result.Failed++
				result.Failures = append(result.Failures, c.name)
				say("  FAIL  %s", c.name)
				for _, line := range strings.Split(strings.TrimRight(err.Error(), "\n"), "\n") {
					say("        %s", line)
				}
				continue
			}
			result.Passed++
			say("  ok    %s", c.name)
		}
	}
	say("\n%d passed, %d failed", result.Passed, result.Failed)
	return result, nil
}

// waitAgent waits until the runner has heard the agent of the new workspace, as the exam server waits.
func waitAgent(ctx context.Context, r Runner, session string) error {
	deadline := time.Now().Add(60 * time.Second)
	for {
		status, err := r.Status(ctx, session)
		if err == nil && status.State == runner.StateRunning && status.Agent.Connected && !status.Agent.LastSeen.IsZero() {
			return nil
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			if err != nil {
				return fmt.Errorf("the agent of the workspace did not come up: %w", err)
			}
			return fmt.Errorf("the agent of the workspace did not come up within a minute (the container is %s): the image's entrypoint must be the agent", status.State)
		}
		sleep(ctx, 200*time.Millisecond)
	}
}

// starter is the bundle put in /work, as an exam's starter is: one file.
func starter() io.Reader {
	var buffer bytes.Buffer
	w := tar.NewWriter(&buffer)
	text := []byte("A workspace of cbe-portalctl image test.\n")
	_ = w.WriteHeader(&tar.Header{Name: "README.txt", Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(text))})
	_, _ = w.Write(text)
	_ = w.Close()
	return &buffer
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

func shortID(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// tester is the agent of the test's workspace, as the test talks to it. The messages that arrive while it waits for one are kept in seen, for the
// checks that look for them later (fs.event, agent.hb).
type tester struct {
	ctx     context.Context
	o       Options
	session string
	c       *agentproto.Client
	seen    []agentproto.Message
}

// terminal is what a command run in a terminal came to.
type terminal struct {
	code      int
	output    string
	oomKilled bool
}

// run runs a command in a terminal of the given size (sh -c, in /work) and waits for it to end, at most limit.
func (t *tester) run(command string, cols, rows int, limit time.Duration) (terminal, error) {
	reply, err := t.c.Do(&agentproto.PTYOpen{Header: agentproto.Header{Type: agentproto.TypePTYOpen}, Cols: cols, Rows: rows, Cmd: command}, 20*time.Second)
	if err != nil {
		return terminal{}, fmt.Errorf("pty.open: %w", err)
	}
	closeTerm := func() {
		_, _ = t.c.Do(&agentproto.PTYClose{Header: agentproto.Header{Type: agentproto.TypePTYClose}, Term: reply.Term}, 10*time.Second)
	}
	var output bytes.Buffer
	keep := func(data []byte) {
		if output.Len() < 1<<20 {
			output.Write(data)
		}
	}
	timeout := time.After(limit)
	for {
		select {
		case o, open := <-t.c.Terminal:
			if !open {
				return terminal{}, agentproto.ErrClosed
			}
			if o.Term == reply.Term {
				keep(o.Data)
			}
		case m, open := <-t.c.Events:
			if !open {
				return terminal{}, agentproto.ErrClosed
			}
			if m.Type != agentproto.TypePTYExit || m.Term != reply.Term {
				t.keep(m)
				continue
			}
			// What the terminal printed last may come after its end.
			for drained := false; !drained; {
				select {
				case o := <-t.c.Terminal:
					if o.Term == reply.Term {
						keep(o.Data)
					}
				case <-time.After(50 * time.Millisecond):
					drained = true
				}
			}
			closeTerm()
			code := -1
			if m.ExitCode != nil {
				code = *m.ExitCode
			}
			return terminal{code: code, output: strings.ReplaceAll(output.String(), "\r", ""), oomKilled: m.OOMKilled}, nil
		case <-timeout:
			closeTerm()
			return terminal{}, fmt.Errorf("it did not end within %s; it printed:\n%s", limit, tail(strings.ReplaceAll(output.String(), "\r", ""), 8))
		case <-t.ctx.Done():
			closeTerm()
			return terminal{}, t.ctx.Err()
		}
	}
}

// sh runs a command in an ordinary terminal and says what it printed, or why it did not end with status 0.
func (t *tester) sh(command string) (string, error) {
	result, err := t.run(command, 120, 30, time.Minute)
	if err != nil {
		return "", err
	}
	if result.code != 0 {
		return result.output, fmt.Errorf("%s: status %d\n%s", command, result.code, tail(result.output, 8))
	}
	return result.output, nil
}

func (t *tester) keep(m agentproto.Message) {
	t.seen = append(t.seen, m)
	if len(t.seen) > 2000 {
		t.seen = t.seen[len(t.seen)-1000:]
	}
}

// await finds a message that matches among those seen since from, or waits for one at most d.
func (t *tester) await(from int, d time.Duration, match func(agentproto.Message) bool) (agentproto.Message, bool) {
	for _, m := range t.seen[min(from, len(t.seen)):] {
		if match(m) {
			return m, true
		}
	}
	timeout := time.After(d)
	for {
		select {
		case m, open := <-t.c.Events:
			if !open {
				return agentproto.Message{}, false
			}
			t.keep(m)
			if match(m) {
				return m, true
			}
		case <-t.c.Terminal:
		case <-timeout:
			return agentproto.Message{}, false
		case <-t.ctx.Done():
			return agentproto.Message{}, false
		}
	}
}

func (t *tester) do(request agentproto.Request) (agentproto.Message, error) {
	return t.c.Do(request, 20*time.Second)
}

func header(kind string) agentproto.Header { return agentproto.Header{Type: kind} }

// tail is the last n lines of text.
func tail(text string, n int) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// expect says that what a command printed is not what it should be.
func expect(printed, want string) error {
	return fmt.Errorf("it printed %q, and should have printed %s", strings.TrimSpace(printed), want)
}

// socketGone says whether the agent's socket is gone from the runner's folder of the session.
func socketGone(runRoot, session string) bool {
	_, err := os.Lstat(filepath.Join(runRoot, session, "agent.sock"))
	return errors.Is(err, os.ErrNotExist)
}
