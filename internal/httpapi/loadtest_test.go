//go:build linux

package httpapi

import (
	"context"
	"strings"
	"testing"
	"time"

	"exam-platform/internal/loadtest"
	"exam-platform/internal/workspace"
)

// The load test's simulated students (internal/loadtest, Phase 8) against the exam server's real routes, a real database and a real agent: the
// whole class logs in, edits, saves, builds, takes part in a storm, submits and leaves, and every measurement is there.
func TestTheLoadTestsStudentsTakeAWorkspaceExam(t *testing.T) {
	f := newWorkspaceFixtureWith(t, func(d *workspace.Document) { d.Workspace.Terminal = true }, fixtureOptions{controlRate: 1000, starter: map[string]string{
		"Makefile":      "all:\n\tmkdir -p build && cp src/unit0.cpp build/copy.txt\n",
		"src/":          "",
		"src/unit0.cpp": "int unit0() { return 0; }\n",
		"src/unit1.cpp": "int unit1() { return 1; }\n",
	}})
	var logs []string
	result, err := loadtest.Run(context.Background(), loadtest.Config{
		Server: f.server.URL, PIN: "exam-secret", Accounts: []string{"0012", "0034"},
		Duration: 6 * time.Second, Ramp: time.Second, ThinkMin: 200 * time.Millisecond, ThinkMax: 400 * time.Millisecond, SaveGap: 100 * time.Millisecond,
		Heartbeat: time.Second, Storms: []time.Duration{3 * time.Second}, SampleEvery: time.Second, Seed: 1,
		Sample: func() map[string]float64 { return map[string]float64{"one": 1} },
		Logf:   func(format string, args ...any) { logs = append(logs, format) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Ready != 2 {
		t.Fatalf("%d of 2 students were ready; log: %v", result.Ready, logs)
	}
	counts := map[string]int{}
	failures := map[string]int{}
	for _, op := range result.Summary.Ops {
		counts[op.Op], failures[op.Op] = op.Count, op.Failures
		if op.Failures > 0 {
			t.Errorf("%s failed %d times: %v", op.Op, op.Failures, op.Errors)
		}
	}
	for _, op := range []string{loadtest.OpLogin, loadtest.OpReady, loadtest.OpList, loadtest.OpOpen, loadtest.OpSave, loadtest.OpEcho, loadtest.OpBuild, loadtest.OpStorm, loadtest.OpSubmit, loadtest.OpHeartbeat} {
		if counts[op] == 0 {
			t.Errorf("nothing was measured for %q: %v", op, counts)
		}
	}
	if counts[loadtest.OpStorm] != 2 || counts[loadtest.OpSubmit] != 2 || counts[loadtest.OpLogin] != 2 {
		t.Errorf("each student logs in once, builds in the storm once and submits once: %v", counts)
	}
	if len(result.Series) < 3 {
		t.Errorf("the machine was read %d times", len(result.Series))
	}
	if len(result.Summary.Minutes) == 0 {
		t.Error("no autosaves by minute")
	}
	var markdown strings.Builder
	result.Summary.Markdown(&markdown)
	if markdown.Len() == 0 {
		t.Error("the report is empty")
	}
	t.Log("\n" + markdown.String())
}
