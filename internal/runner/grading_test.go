package runner

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// archive makes a tar of name -> content (a name ending in / is a folder; a content starting with "->" is a symbolic link; a name that
// ends in .sh or is "grade" can be run).
func archive(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := tar.NewWriter(&buf)
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sortStrings(names)
	for _, name := range names {
		content := entries[name]
		header := &tar.Header{Name: name, Mode: 0o644, Typeflag: tar.TypeReg, Size: int64(len(content))}
		switch {
		case strings.HasSuffix(name, "/"):
			header.Typeflag, header.Mode, header.Size = tar.TypeDir, 0o755, 0
		case strings.HasPrefix(content, "->"):
			header.Typeflag, header.Linkname, header.Size = tar.TypeSymlink, strings.TrimPrefix(content, "->"), 0
		case name == "grade" || strings.HasSuffix(name, ".sh"):
			header.Mode = 0o755
		}
		if err := w.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if header.Typeflag == tar.TypeReg {
			if _, err := w.Write([]byte(content)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func (r *rig) gradeRequest(id string) GradeRequest {
	return GradeRequest{Grading: id, Exam: "exam1", Image: "cbe/cpp20", TimeoutSeconds: 30, Resources: Resources{CPU: 1, MemoryMB: 512, DiskMB: 50}}
}

var theBundle = map[string]string{"grade": "#!/bin/sh\necho '{}'\n", "tests/cases.txt": "HIDDEN\n"}

func (r *rig) grade(t *testing.T, id string, submission map[string]string) (GradeResult, error) {
	t.Helper()
	return r.svc.Grade(context.Background(), r.gradeRequest(id), bytes.NewReader(archive(t, submission)), bytes.NewReader(archive(t, theBundle)))
}

// A grading container is made from the student's own hardening, has the two mounts, runs /grading/grade, and is not taken for a
// student's session; and what a student's container is given has no /grading in it.
func TestAGradingContainerIsHardenedLikeAStudentsAndHasOnlyTheTwoMounts(t *testing.T) {
	r := newRig(t)
	var seen ContainerSpec
	r.rt.run = func(_ context.Context, c *fakeContainer) (int, Output) {
		seen = c.spec
		return 0, Output{Stdout: []byte(`{"t1":{"passed":true}}`)}
	}
	result, err := r.grade(t, "g1", map[string]string{"src/a.cpp": "int a;\n"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Stdout != `{"t1":{"passed":true}}` || result.ExitCode != 0 || result.TimedOut {
		t.Errorf("result = %+v", result)
	}
	student := BuildSpec(r.cfg, r.request("s1"), Resources{CPU: 1, MemoryMB: 512, DiskMB: 50}, "ref")
	for what, same := range map[string]bool{
		"network": seen.NetworkMode == "none" && seen.NetworkMode == student.NetworkMode, "read-only root": seen.ReadonlyRootfs && student.ReadonlyRootfs,
		"capabilities": len(seen.CapDrop) == 1 && seen.CapDrop[0] == "ALL" && len(student.CapDrop) == 1, "no new privileges": len(seen.SecurityOpt) == 1 && seen.SecurityOpt[0] == student.SecurityOpt[0],
		"processes": seen.PidsLimit == student.PidsLimit, "memory": seen.MemoryBytes == student.MemoryBytes && seen.MemorySwap == seen.MemoryBytes, "cpu": seen.NanoCPUs == student.NanoCPUs,
		"user": seen.User == "1000:1000" && seen.User == student.User, "tmp": seen.Tmpfs["/tmp"] == student.Tmpfs["/tmp"],
	} {
		if !same {
			t.Errorf("the grading container's %s is not the student's: %+v", what, seen)
		}
	}
	if len(seen.Binds) != 2 || seen.Binds[0].Target != WorkPath || seen.Binds[0].ReadOnly || seen.Binds[1].Target != GradingPath || !seen.Binds[1].ReadOnly {
		t.Errorf("mounts = %+v", seen.Binds)
	}
	if seen.Binds[0].Source != filepath.Join(r.cfg.WorkRoot, "grade-g1") || seen.Binds[1].Source != filepath.Join(r.cfg.GradeRoot, "g1", "grading") {
		t.Errorf("mount sources = %+v", seen.Binds)
	}
	if len(seen.Entrypoint) != 1 || seen.Entrypoint[0] != "/grading/grade" || len(seen.Cmd) != 0 || seen.Labels[LabelManaged] != "" || seen.Labels[LabelSession] != "" || seen.Labels[LabelRole] != RoleGrading {
		t.Errorf("entrypoint, cmd or labels = %v %v %v", seen.Entrypoint, seen.Cmd, seen.Labels)
	}
	// A student's container never has the grading bundle: two mounts, neither of them /grading, and the image's own entrypoint.
	for _, bind := range student.Binds {
		if bind.Target == GradingPath || strings.Contains(bind.Source, r.cfg.GradeRoot) {
			t.Errorf("a student's container has the grading mount: %+v", bind)
		}
	}
	if len(student.Binds) != 2 || len(student.Entrypoint) != 0 || student.Labels[LabelRole] != "" {
		t.Errorf("a student's spec = %+v", student)
	}
}

// The submission is what is in /work (owned by the student), the bundle is on the host for the run only, and everything is gone after.
func TestAGradingRunGetsTheSubmissionAndTheBundleAndLeavesNothing(t *testing.T) {
	r := newRig(t)
	var inWork map[string]string
	var bundleSeen []string
	var modes = map[string]os.FileMode{}
	r.rt.run = func(_ context.Context, c *fakeContainer) (int, Output) {
		inWork = c.files
		root := filepath.Join(r.cfg.GradeRoot, "g2", "grading")
		_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				rel, _ := filepath.Rel(root, path)
				bundleSeen = append(bundleSeen, rel)
				modes[rel] = info.Mode().Perm()
			}
			return nil
		})
		return 0, Output{}
	}
	if _, err := r.grade(t, "g2", map[string]string{"src/a.cpp": "int a;\n", "Makefile": "all:\n"}); err != nil {
		t.Fatal(err)
	}
	if inWork["src/a.cpp"] != "int a;\n" || inWork["Makefile"] != "all:\n" {
		t.Errorf("/work = %v", inWork)
	}
	sortStrings(bundleSeen)
	if strings.Join(bundleSeen, ",") != "grade,tests/cases.txt" || (runtime.GOOS != "windows" && (modes["grade"] != 0o755 || modes["tests/cases.txt"] != 0o644)) {
		t.Errorf("the bundle on the host = %v %v", bundleSeen, modes)
	}
	r.rt.mu.Lock()
	left := len(r.rt.containers)
	r.rt.mu.Unlock()
	if left != 0 || len(r.quota.folders) != 0 {
		t.Errorf("left behind: %d containers, folders %v", left, r.quota.folders)
	}
	if entries, _ := os.ReadDir(r.cfg.GradeRoot); len(entries) != 0 {
		t.Errorf("the bundle's folder is still there: %v", entries)
	}
	if entries, _ := os.ReadDir(filepath.Join(r.cfg.StateRoot, "grading")); len(entries) != 0 {
		t.Errorf("the run's note is still there: %v", entries)
	}
	// The bundle never reaches the student's /work: the submission archive is all CopyTo was given.
	if _, found := inWork["grade"]; found {
		t.Error("the grading bundle is in /work")
	}
}

// A run that takes too long is stopped, and what it printed is returned; a run that fails is a result.
func TestARunThatTakesTooLongIsStoppedAndAFailureIsAResult(t *testing.T) {
	r := newRig(t)
	r.rt.run = func(ctx context.Context, c *fakeContainer) (int, Output) {
		<-ctx.Done()
		return 0, Output{}
	}
	req := r.gradeRequest("g3")
	req.TimeoutSeconds = 1
	started := time.Now()
	result, err := r.svc.Grade(context.Background(), req, bytes.NewReader(archive(t, map[string]string{"a": "x"})), bytes.NewReader(archive(t, theBundle)))
	if err != nil {
		t.Fatal(err)
	}
	if !result.TimedOut || time.Since(started) < time.Second || time.Since(started) > 5*time.Second {
		t.Errorf("result = %+v after %v", result, time.Since(started))
	}
	stopped := false
	for _, call := range r.rt.calls {
		stopped = stopped || strings.HasPrefix(call, "stop cbe-grade-exam1-g3")
	}
	if !stopped || len(r.rt.containers) != 0 || len(r.quota.folders) != 0 {
		t.Errorf("stopped %v, containers %d, folders %v", stopped, len(r.rt.containers), r.quota.folders)
	}
	// A crash and a kill for memory are results too, with what was printed.
	r.rt.run = func(context.Context, *fakeContainer) (int, Output) {
		return 139, Output{Stdout: []byte("partial"), Stderr: []byte("Segmentation fault")}
	}
	result, err = r.grade(t, "g4", map[string]string{"a": "x"})
	if err != nil || result.ExitCode != 139 || result.Stdout != "partial" || result.Stderr != "Segmentation fault" || result.TimedOut {
		t.Errorf("a crash: %+v %v", result, err)
	}
	r.rt.run = func(_ context.Context, c *fakeContainer) (int, Output) { c.oom = true; return 137, Output{} }
	if result, err = r.grade(t, "g5", map[string]string{"a": "x"}); err != nil || !result.OOMKilled || result.ExitCode != 137 {
		t.Errorf("a kill for memory: %+v %v", result, err)
	}
}

// What is printed is cut at the limit, and said to be.
func TestWhatARunPrintsIsCutAtTheLimit(t *testing.T) {
	r := newRig(t)
	r.cfg.GradeOutputBytes = 1 << 10
	r.svc.cfg = r.cfg
	r.rt.run = func(context.Context, *fakeContainer) (int, Output) {
		return 0, Output{Stdout: bytes.Repeat([]byte("x"), 5000), Stderr: []byte("short")}
	}
	result, err := r.grade(t, "g6", map[string]string{"a": "x"})
	if err != nil || len(result.Stdout) != 1<<10 || !result.StdoutTruncated || result.StderrTruncated || result.Stderr != "short" {
		t.Errorf("%d %v %v %v", len(result.Stdout), result.StdoutTruncated, result.StderrTruncated, err)
	}
}

// What a request may not ask for is refused before anything is made.
func TestAGradingRequestIsRefusedBeforeAnythingIsMade(t *testing.T) {
	r := newRig(t)
	bad := map[string]func(*GradeRequest){
		"an image that is not approved": func(q *GradeRequest) { q.Image = "docker.io/library/alpine" },
		"an ID with a slash":            func(q *GradeRequest) { q.Grading = "../x" },
		"an exam with a space":          func(q *GradeRequest) { q.Exam = "a b" },
		"a grading ID too long":         func(q *GradeRequest) { q.Grading = strings.Repeat("a", 49) },
		"a timeout over the maximum":    func(q *GradeRequest) { q.TimeoutSeconds = r.cfg.GradeMaxSeconds + 1 },
		"a negative timeout":            func(q *GradeRequest) { q.TimeoutSeconds = -5 },
		"memory over the maximum":       func(q *GradeRequest) { q.Resources.MemoryMB = 999999 },
	}
	for what, change := range bad {
		req := r.gradeRequest("g7")
		change(&req)
		_, err := r.svc.Grade(context.Background(), req, bytes.NewReader(archive(t, map[string]string{"a": "x"})), bytes.NewReader(archive(t, theBundle)))
		if err == nil || (!errors.Is(err, ErrBadRequest) && !errors.Is(err, ErrNotApproved)) {
			t.Errorf("%s: %v", what, err)
		}
	}
	if len(r.rt.specs) != 0 || len(r.quota.folders) != 0 {
		t.Errorf("a refused request made %d containers and %v", len(r.rt.specs), r.quota.folders)
	}
	// The bundle must be what the compiler makes, and have a grade that can be run.
	for what, bundle := range map[string]map[string]string{
		"no grade":                 {"tests/a": "x"},
		"a grade that cannot run":  {"grade": "x", "tests/a.txt": "y"},
		"an absolute name":         {"grade": "#!/bin/sh\n", "/etc/passwd": "x"},
		"a link out of the folder": {"grade": "#!/bin/sh\n", "l": "->../../x"},
	} {
		entries := bundle
		if what == "a grade that cannot run" {
			entries = map[string]string{"tests/a.txt": "y"}
		}
		data := archive(t, entries)
		if what == "a grade that cannot run" {
			// A "grade" without the executable bit.
			var buf bytes.Buffer
			w := tar.NewWriter(&buf)
			_ = w.WriteHeader(&tar.Header{Name: "grade", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg})
			_, _ = w.Write([]byte("x"))
			_ = w.Close()
			data = buf.Bytes()
		}
		_, err := r.svc.Grade(context.Background(), r.gradeRequest("g8"), bytes.NewReader(archive(t, map[string]string{"a": "x"})), bytes.NewReader(data))
		if !errors.Is(err, ErrBadRequest) {
			t.Errorf("%s: %v", what, err)
		}
	}
	if len(r.rt.specs) != 0 || len(r.quota.folders) != 0 {
		t.Errorf("a refused bundle made %d containers and %v", len(r.rt.specs), r.quota.folders)
	}
}

// What a student made that a container cannot be given is left out and named, and the run goes on.
func TestWhatAContainerCannotBeGivenIsLeftOutOfASubmissionAndNamed(t *testing.T) {
	r := newRig(t)
	var inWork map[string]string
	r.rt.run = func(_ context.Context, c *fakeContainer) (int, Output) { inWork = c.files; return 0, Output{} }
	result, err := r.grade(t, "g9", map[string]string{"src/a.cpp": "ok", "evil": "->/etc/passwd", "up": "->../../../x", "bad\\name": "x", ".cbe-state": "x"})
	if err != nil {
		t.Fatal(err)
	}
	sortStrings(result.Skipped)
	if strings.Join(result.Skipped, ",") != ".cbe-state,bad\\name,evil,up" {
		t.Errorf("skipped = %v", result.Skipped)
	}
	if inWork["src/a.cpp"] != "ok" {
		t.Errorf("/work = %v", inWork)
	}
	for name := range inWork {
		if name == "evil" || name == "up" || strings.Contains(name, "cbe-state") {
			t.Errorf("%s reached /work", name)
		}
	}
	// A submission over the limits is refused, though: too many files.
	r.cfg.Inodes = 100
	r.svc.cfg = r.cfg
	many := map[string]string{}
	for i := 0; i < 150; i++ {
		many[fmt.Sprintf("f%03d", i)] = "x"
	}
	if _, err := r.grade(t, "g10", many); !errors.Is(err, ErrBadRequest) {
		t.Errorf("too many files: %v", err)
	}
}

// At most GradeParallel runs go at once, whatever is asked, and every one is run.
func TestGradingIsBoundedAndEveryRunIsRun(t *testing.T) {
	r := newRig(t)
	r.cfg.GradeParallel = 3
	r.svc.gradeSlots = make(chan struct{}, 3)
	r.rt.run = func(ctx context.Context, c *fakeContainer) (int, Output) {
		time.Sleep(30 * time.Millisecond)
		return 0, Output{Stdout: []byte(c.spec.Labels[LabelGrading])}
	}
	const total = 40
	var wg sync.WaitGroup
	results := make([]string, total)
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := fmt.Sprintf("n%d", i)
			result, err := r.grade(t, id, map[string]string{"a": "x"})
			if err != nil {
				t.Errorf("%s: %v", id, err)
				return
			}
			results[i] = result.Stdout
		}()
	}
	wg.Wait()
	r.rt.mu.Lock()
	peak, left := r.rt.peak, len(r.rt.containers)
	r.rt.mu.Unlock()
	if peak > 3 || peak < 2 || left != 0 || len(r.quota.folders) != 0 {
		t.Errorf("peak %d, left %d, folders %v", peak, left, r.quota.folders)
	}
	for i, got := range results {
		if got != fmt.Sprintf("n%d", i) {
			t.Errorf("run %d got %q", i, got)
		}
	}
}

// A runner that was stopped in the middle of runs removes what they left when it starts.
func TestARestartedRunnerRemovesWhatStoppedRunsLeft(t *testing.T) {
	r := newRig(t)
	left := ContainerSpec{Name: GradeContainerName("exam1", "old"), Labels: map[string]string{LabelRole: RoleGrading, LabelGrading: "old", LabelExam: "exam1"}}
	if _, err := r.rt.Create(context.Background(), left); err != nil {
		t.Fatal(err)
	}
	student := BuildSpec(r.cfg, r.request("s1"), Resources{CPU: 1, MemoryMB: 64, DiskMB: 1}, "ref")
	if _, err := r.rt.Create(context.Background(), student); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(r.cfg.GradeRoot, "old", "grading"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(r.cfg.StateRoot, "grading"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.cfg.StateRoot, "grading", "old.json"), []byte(`{"grading":"old","exam":"exam1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	r.quota.folders = map[string]int{"grade-old": 50}
	count, err := r.svc.Reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("student containers = %d, want 1 (a grading container is not one)", count)
	}
	if _, found := r.rt.containers[left.Name]; found || len(r.quota.folders) != 0 {
		t.Errorf("a grading container or folder is still there: %v %v", found, r.quota.folders)
	}
	if _, found := r.rt.containers[student.Name]; !found {
		t.Error("the student's container was removed")
	}
	if entries, _ := os.ReadDir(r.cfg.GradeRoot); len(entries) != 0 {
		t.Errorf("the bundle's folder is still there: %v", entries)
	}
	if _, err := r.svc.readMeta("old"); err == nil {
		t.Error("a grading container was taken for a student's session")
	}
}

// The same through the API, with the multipart request the exam server sends.
func TestGradingRunOverTheAPI(t *testing.T) {
	r := newRig(t)
	r.rt.run = func(_ context.Context, c *fakeContainer) (int, Output) {
		return 0, Output{Stdout: []byte("{\"t1\":{\"passed\":true}}\n"), Stderr: []byte("note")}
	}
	client := r.api(t)
	result, err := client.Grade(context.Background(), r.gradeRequest("a1"), bytes.NewReader(archive(t, map[string]string{"src/x.cpp": "x"})), bytes.NewReader(archive(t, theBundle)))
	if err != nil || result.Grading != "a1" || !strings.Contains(result.Stdout, `"t1"`) || result.Stderr != "note" {
		t.Fatalf("%+v %v", result, err)
	}
	// A refused request comes back as the right status.
	req := r.gradeRequest("a2")
	req.Image = "docker.io/library/alpine"
	if _, err := client.Grade(context.Background(), req, bytes.NewReader(archive(t, map[string]string{"a": "x"})), bytes.NewReader(archive(t, theBundle))); !errors.Is(err, ErrNotApproved) {
		t.Errorf("an image that is not approved: %v", err)
	}
	if _, err := client.Grade(context.Background(), r.gradeRequest("a3"), bytes.NewReader(archive(t, map[string]string{"a": "x"})), bytes.NewReader(archive(t, map[string]string{"x": "y"}))); !errors.Is(err, ErrBadRequest) {
		t.Errorf("a bundle with no grade: %v", err)
	}
	// Without the secret there is no run.
	other := NewClient(client.socket, "not the secret")
	if _, err := other.Grade(context.Background(), r.gradeRequest("a4"), strings.NewReader(""), strings.NewReader("")); err == nil {
		t.Error("a run without the secret")
	}
	if specs := len(r.rt.specs); specs != 1 {
		t.Errorf("%d containers were made, want 1", specs)
	}
}
