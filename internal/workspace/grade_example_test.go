package workspace_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"exam-platform/internal/workspace"
)

// The example package's grade script is run here on the host (the course image has g++ too): a known-good submission, a partial one and the
// untouched starter are scored, and what it prints is what the grader accepts. The same script is run in a real grading container by
// deploy/workspace/test-server.sh.
const example = "../../testdata/workspace"

var exampleTodos = []workspace.Todo{{ID: "t1", Points: pts(10)}, {ID: "t2", Points: pts(15)}, {ID: "t3", Points: pts(15)}}

func copyTree(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.Walk(from, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, path)
		target := filepath.Join(to, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
	if err != nil {
		t.Fatal(err)
	}
}

// runGrade makes a /work of the starter with the files of overlay (a folder of testdata, or "") and extra files on top, and runs grade there.
func runGrade(t *testing.T, overlay string, extra map[string]string) (stdout, stderr string, err error) {
	t.Helper()
	if _, lookErr := exec.LookPath("g++"); lookErr != nil {
		t.Skip("g++ is not installed")
	}
	work := t.TempDir()
	copyTree(t, filepath.Join(example, "lab-listas/project"), work)
	if overlay != "" {
		copyTree(t, filepath.Join(example, "lab-listas-submissions", overlay), work)
	}
	for name, content := range extra {
		if err := os.WriteFile(filepath.Join(work, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	command := exec.Command("sh", filepath.Join(example, "lab-listas/grading/grade"))
	command.Env = append(os.Environ(), "WORK="+work)
	var out, errs bytes.Buffer
	command.Stdout, command.Stderr = &out, &errs
	err = command.Run()
	return out.String(), errs.String(), err
}

func TestTheExampleGradeScoresAKnownGoodAndAKnownBadSubmission(t *testing.T) {
	for _, test := range []struct {
		name, overlay string
		total         float64
		passed        map[string]bool
		points        map[string]float64
	}{
		{"a good submission", "good", 40, map[string]bool{"t1": true, "t2": true, "t3": true}, map[string]float64{"t1": 10, "t2": 15, "t3": 15}},
		{"a partial one", "partial", 16, map[string]bool{"t1": true, "t2": false, "t3": false}, map[string]float64{"t1": 10, "t2": 6, "t3": 0}},
		{"the starter as it was given", "", 0, map[string]bool{"t1": false, "t2": false, "t3": false}, map[string]float64{"t1": 0, "t2": 0, "t3": 0}},
	} {
		stdout, stderr, err := runGrade(t, test.overlay, nil)
		if err != nil {
			t.Fatalf("%s: %v\n%s", test.name, err, stderr)
		}
		results, total, max, err := workspace.ParseGrade(stdout, exampleTodos)
		if err != nil || total != test.total || max == nil || *max != 40 || len(results) != 3 {
			t.Fatalf("%s: %v %v %v\n%s", test.name, total, max, err, stdout)
		}
		for _, result := range results {
			if result.Passed != test.passed[result.TodoID] || result.Points != test.points[result.TodoID] || !result.Known || result.Log == "" {
				t.Errorf("%s: %+v", test.name, result)
			}
		}
		// Standard output is the JSON alone, and says nothing of the hidden cases.
		if strings.Contains(stdout, "hidden") || strings.Contains(stdout, "HIDDEN") || strings.Contains(stdout, "FAILED:") {
			t.Errorf("%s: standard output has more than the JSON:\n%s", test.name, stdout)
		}
	}
}

// A submission that does not compile, or that crashes, still gets a result for every TODO (the grader's failure is for a grade script that
// breaks, not for a student who does).
func TestTheExampleGradeAnswersForSubmissionsThatDoNotCompileOrCrash(t *testing.T) {
	broken := "#include \"list.hpp\"\nthis is not C++\n"
	stdout, _, err := runGrade(t, "good", map[string]string{"src/list.cpp": broken})
	if err != nil {
		t.Fatal(err)
	}
	results, total, _, err := workspace.ParseGrade(stdout, exampleTodos)
	if err != nil || total != 0 || len(results) != 3 || results[0].Log != "src/list.cpp does not compile" || results[0].Passed || results[1].Passed || results[2].Passed {
		t.Errorf("does not compile: %v %+v %v\n%s", total, results, err, stdout)
	}
	crashing := strings.Replace(readText(t, filepath.Join(example, "lab-listas-submissions/good/src/list.cpp")), "head_ = new Node{value, head_};", "*(volatile int*)0 = 1;", 1)
	stdout, stderr, err := runGrade(t, "good", map[string]string{"src/list.cpp": crashing})
	if err != nil {
		t.Fatalf("a crash in the student's code is a result: %v\n%s", err, stderr)
	}
	results, _, _, err = workspace.ParseGrade(stdout, exampleTodos)
	if err != nil || results[0].Passed || !strings.Contains(stderr, "crashed or did not end") {
		t.Errorf("a crash: %v %+v\n%s", err, results, stderr)
	}
}

func readText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
