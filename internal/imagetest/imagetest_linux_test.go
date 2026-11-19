//go:build linux

package imagetest

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"exam-platform/internal/workspace/workspacetest"
)

// fakeRunner is the exam server's fake runner, whose agents are the real agent in this process (no container): what a check needs of a
// container (PID 1, no network, the limits) is not true of it, and the test does not run the checks that would act on this machine itself (a
// search of /, a memory hog, a fork storm).
type fakeRunner struct{ *workspacetest.Runner }

func (fakeRunner) Ping(context.Context) error { return nil }

func newFake(t *testing.T) fakeRunner {
	r := workspacetest.New(t.TempDir())
	t.Cleanup(r.Close)
	return fakeRunner{r}
}

func calls(r fakeRunner, op string) []string {
	var found []string
	for _, c := range r.Calls {
		if strings.HasPrefix(c, op+" ") {
			found = append(found, strings.TrimPrefix(c, op+" "))
		}
	}
	return found
}

func TestAWorkspaceIsMadeAsAStudentsIsTestedAndRemoved(t *testing.T) {
	r := newFake(t)
	var out bytes.Buffer
	checks, err := ParseChecks("# check: the course's tool answers\necho tool ok\n# check: the course's other tool fails\necho before\nfalse\necho never\n")
	if err != nil {
		t.Fatal(err)
	}
	result, err := run(context.Background(), Options{Image: "cbe/cpp20", Runner: r, Checks: checks, Out: &out}, []string{groupAgent, groupFiles, groupTerminal, groupCourse, groupStop})
	if err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	report := out.String()
	for _, want := range []string{
		"Testing cbe/cpp20 (image fake) in a workspace of its own, image-test-",
		"== the agent, through the runner\n  ok    it answers through the runner, in protocol 1\n",
		// The fake's agent is not a container's first process: the check says so.
		"  FAIL  it is the first process of the workspace (PID 1)\n        the agent says it is not PID 1",
		"  ok    its heartbeat arrives\n",
		"  ok    create, read, write, move and delete\n",
		"  ok    a link made in a terminal leads nowhere outside /work\n",
		"  ok    a command's exit status comes back\n",
		"  ok    a terminal has the size it was opened with\n",
		"  ok    a file made in a terminal is an event\n",
		"== the course's own checks (checks)\n  ok    the course's tool answers\n  FAIL  the course's other tool fails\n        checks, line 3: status 1; it printed:\n        before\n",
		"== stopping\n  ok    a stop ends the agent at once (SIGTERM), with status 0\n",
		"The workspace image-test-",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("the report lacks %q:\n%s", want, report)
		}
	}
	if strings.Contains(report, "never") || strings.Contains(report, "socket is gone") {
		t.Errorf("a course check goes on after a command failed, or the socket was looked for with no run folder:\n%s", report)
	}
	if result.OK() || result.Failed < 2 || result.Passed < 8 || !strings.Contains(strings.Join(result.Failures, "|"), "the course's other tool fails") {
		t.Errorf("%+v", result)
	}
	// The workspace is the runner's like a student's: made, seeded, started, and removed at the end; its session and exam are not the server's.
	created := calls(r, "create")
	if len(created) != 1 || !strings.HasPrefix(created[0], "image-test-") || len(calls(r, "seed")) != 1 || len(calls(r, "start")) != 1 {
		t.Errorf("calls: %v", r.Calls)
	}
	if removed := calls(r, "remove"); len(removed) != 1 || removed[0] != created[0] {
		t.Errorf("the workspace was not removed: %v", r.Calls)
	}
}

func TestAnImageTheRunnerDoesNotApproveIsNotTested(t *testing.T) {
	r := newFake(t)
	_, err := Run(context.Background(), Options{Image: "cbe/nothing", Runner: r})
	if err == nil || !strings.Contains(err.Error(), "the runner does not approve cbe/nothing") || len(r.Calls) != 0 {
		t.Errorf("%v %v", err, r.Calls)
	}
}

func TestAnInterruptedTestStillRemovesTheWorkspace(t *testing.T) {
	r := newFake(t)
	ctx, cancel := context.WithCancel(context.Background())
	checks, _ := ParseChecks("# check: a long one\nsleep 30\n")
	go func() {
		time.Sleep(2 * time.Second)
		cancel()
	}()
	started := time.Now()
	_, err := run(ctx, Options{Image: "cbe/cpp20", Runner: r, Checks: checks}, []string{groupCourse})
	if err == nil || time.Since(started) > 20*time.Second {
		t.Errorf("an interrupted test: %v after %s", err, time.Since(started))
	}
	if created, removed := calls(r, "create"), calls(r, "remove"); len(created) != 1 || len(removed) != 1 || removed[0] != created[0] {
		t.Errorf("the workspace was not removed: %v", r.Calls)
	}
}
