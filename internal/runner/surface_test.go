package runner

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// appendixE is the runner's whole surface: the operations of Appendix E of the plan, as the routes that serve them (and the ping the exam server's
// supervisor asks first). The runner is the one process that holds the container runtime, so a route added here is a new thing the exam server
// (and whoever takes the exam server over) can ask the runtime for: this test fails until the list, Appendix E and docs/workspace/deployment.md
// say so (Phase 8, security checklist: "the runner accepts only the operations in Appendix E").
var appendixE = []string{
	"GET /v1/ping",
	"POST /v1/containers",                   // container.create
	"PUT /v1/containers/{session}/seed",     // container.seed (P3-4)
	"POST /v1/containers/{session}/start",   // container.start
	"POST /v1/containers/{session}/pause",   // container.pause
	"POST /v1/containers/{session}/unpause", // container.unpause
	"POST /v1/containers/{session}/stop",    // container.stop
	"POST /v1/containers/{session}/remove",  // container.remove
	"GET /v1/containers/{session}",          // container.status
	"GET /v1/containers/{session}/stats",    // container.stats
	"GET /v1/exams/{exam}/stats",            // container.stats of an exam
	"GET /v1/containers/{session}/archive",  // container.archive
	"GET /v1/containers/{session}/agent",    // agent.connect
	"GET /v1/images",                        // image.inspect
	"POST /v1/grading",                      // grading.run
	"POST /v1/images/build",                 // image.build
}

func TestTheRunnerServesExactlyTheRoutesOfAppendixE(t *testing.T) {
	source, err := os.ReadFile("api.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	var found []string
	for _, match := range regexp.MustCompile(`mux\.HandleFunc\("([A-Z]+ [^"]+)"(\+action)?`).FindAllStringSubmatch(text, -1) {
		if match[2] == "" {
			found = append(found, match[1])
			continue
		}
		// The container actions are made in a loop over a map of their names.
		loop := regexp.MustCompile(`range map\[string\]func\([^)]*\) \(Status, error\)\{([^}]*)\}`).FindStringSubmatch(text)
		if loop == nil {
			t.Fatal("the loop that makes the container actions is not where this test looks for it")
		}
		for _, action := range regexp.MustCompile(`"(\w+)":`).FindAllStringSubmatch(loop[1], -1) {
			found = append(found, match[1]+action[1])
		}
	}
	if regexp.MustCompile(`mux\.Handle\(|\bhttp\.Handle(Func)?\(`).MatchString(text) {
		t.Error("a route is made in a way this test does not read; use mux.HandleFunc with a literal pattern")
	}
	want := append([]string(nil), appendixE...)
	sort.Strings(found)
	sort.Strings(want)
	if strings.Join(found, "\n") != strings.Join(want, "\n") {
		t.Errorf("the runner's routes are not Appendix E's.\nserved:\n  %s\nAppendix E:\n  %s", strings.Join(found, "\n  "), strings.Join(want, "\n  "))
	}
}
