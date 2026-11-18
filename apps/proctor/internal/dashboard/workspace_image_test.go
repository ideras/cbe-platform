package dashboard

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const imageRoute = "/admin/workspace/exam/" + examID + "/image"

// copyPackage copies the example package with its own image to a folder of its own, so that a test may change it.
func copyPackage(t *testing.T) string {
	t.Helper()
	src := filepath.Join("..", "..", "..", "..", "testdata", "workspace", "lab-image")
	dst := t.TempDir()
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
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
	return dst
}

// parts reads the multipart body of an import into name -> content.
func parts(t *testing.T, header string, body []byte) map[string][]byte {
	t.Helper()
	_, params, err := mime.ParseMediaType(header)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]byte{}
	reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		out[part.FormName()], _ = io.ReadAll(part)
	}
}

func sha(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// The proctor chooses a package's workspace.md, sees what the compiler made of it (its own image, warnings and all), chooses a PIN, and what
// goes to the server is the document with the PIN and the three bundles; the package is used up by the import.
func TestAWorkspacePackageIsPreviewedAndImportedWithItsImageBundle(t *testing.T) {
	f := newPublishFixture(t, "")
	dir := copyPackage(t)
	f.chosen = filepath.Join(dir, "workspace.md")
	// What was sent is read from the fake server's own record of its requests, which is kept under its lock.
	sent := func() []recorded { return f.server.seen("POST", "/admin/class/"+classID+"/workspace-exam") }
	f.server.setReply(func(w http.ResponseWriter, r *http.Request, body []byte) bool {
		if r.Method == "POST" && r.URL.Path == "/admin/class/"+classID+"/workspace-exam" {
			w.WriteHeader(http.StatusCreated)
			reply(w, map[string]string{"exam_id": otherExam})
			return true
		}
		return false
	})
	preview, err := f.ChooseWorkspaceExam(classID)
	if err != nil || !preview.Valid || len(preview.Problems) != 0 {
		t.Fatalf("%+v %v", preview, err)
	}
	if preview.Title != "Laboratory – a library the course image does not have" || preview.DurationSeconds != 3600 || preview.Image != "cbe/cpp20" || !preview.Terminal ||
		preview.Run != "make run" || preview.Check != "make check" || preview.MemoryMB != 1024 || preview.Points == nil || *preview.Points != 10 {
		t.Errorf("the preview: %+v", preview)
	}
	if len(preview.Todos) != 1 || preview.Todos[0].ID != "t1" || preview.Todos[0].Text != "make run prints hello, <your name> using libgreet" || *preview.Todos[0].Points != 10 {
		t.Errorf("the TODO items: %+v", preview.Todos)
	}
	if !preview.OwnImage || preview.ImageFiles != 3 || !preview.HasGrading || preview.GradingFiles != 1 || preview.StarterFiles != 2 || len(preview.ImageWarnings) != 0 {
		t.Errorf("the bundles: %+v", preview)
	}
	if preview.Folder != filepath.Base(dir) {
		t.Errorf("the folder: %q", preview.Folder)
	}
	if len(sent()) != 0 {
		t.Fatal("previewing stored something")
	}

	// A PIN is needed, and a bad one is refused before anything is sent.
	for _, pin := range []string{"", "   ", "two\nlines", strings.Repeat("x", 257)} {
		if _, err := f.ImportWorkspaceExam(classID, pin); err == nil {
			t.Errorf("the PIN %q was taken", pin)
		}
	}
	if len(sent()) != 0 {
		t.Fatal("a refused PIN sent the exam")
	}
	// A package previewed for another class is not imported into this one.
	if _, err := f.ImportWorkspaceExam(otherClass, "pin-1234"); err == nil || !strings.Contains(err.Error(), "Choose a workspace package for this class first") {
		t.Errorf("another class: %v", err)
	}
	result, err := f.ImportWorkspaceExam(classID, "pin-1234")
	if err != nil || result.ExamID != otherExam {
		t.Fatalf("%+v %v", result, err)
	}
	if sent := sent(); len(sent) != 1 || !strings.HasPrefix(sent[0].ContentType, "multipart/form-data") || sent[0].Authorization != "Bearer "+secret {
		t.Fatalf("what was sent: %+v", sent)
	}
	got := parts(t, sent()[0].ContentType, sent()[0].Body)
	if len(got) != 4 || len(got["starter"]) == 0 || len(got["grading"]) == 0 || len(got["image"]) == 0 {
		t.Fatalf("the parts: %v", got)
	}
	var document map[string]any
	if err := json.Unmarshal(got["document"], &document); err != nil {
		t.Fatal(err)
	}
	if document["pin"] != "pin-1234" || document["kind"] != "workspace" || document["image_bundle"] != sha(got["image"]) || document["starter_bundle"] != sha(got["starter"]) || document["grading_bundle"] != sha(got["grading"]) {
		t.Errorf("the document: %v", document)
	}
	// The package is used up: importing again would be the same exam twice.
	if _, err := f.ImportWorkspaceExam(classID, "pin-1234"); err == nil || len(sent()) != 1 {
		t.Errorf("a second import: %v (%d sent)", err, len(sent()))
	}
}

func TestAPackageWithoutAnImageOrGradingSendsOnlyWhatItHas(t *testing.T) {
	f := newPublishFixture(t, "")
	dir := copyPackage(t)
	for _, name := range []string{"image", "grading"} {
		if err := os.RemoveAll(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	f.chosen = filepath.Join(dir, "workspace.md")
	f.server.setReply(func(w http.ResponseWriter, r *http.Request, body []byte) bool {
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/workspace-exam") {
			w.WriteHeader(http.StatusCreated)
			reply(w, map[string]string{"exam_id": otherExam})
			return true
		}
		return false
	})
	preview, err := f.ChooseWorkspaceExam(classID)
	if err != nil || !preview.Valid || preview.OwnImage || preview.HasGrading || preview.ImageFiles != 0 {
		t.Fatalf("%+v %v", preview, err)
	}
	if _, err := f.ImportWorkspaceExam(classID, "pin-1234"); err != nil {
		t.Fatal(err)
	}
	post := f.server.seen("POST", "/admin/class/"+classID+"/workspace-exam")
	if len(post) != 1 {
		t.Fatalf("the import was sent %d times", len(post))
	}
	got := parts(t, post[0].ContentType, post[0].Body)
	if len(got) != 2 || got["image"] != nil || got["grading"] != nil {
		t.Errorf("the parts of a package with no image and no grading: %v", got)
	}
	var document map[string]any
	_ = json.Unmarshal(got["document"], &document)
	if _, found := document["image_bundle"]; found {
		t.Errorf("the document names an image bundle: %v", document)
	}
}

func TestAPackageThatDoesNotCompileIsShownItsProblemsAndStoresNothing(t *testing.T) {
	f := newPublishFixture(t, "")
	dir := copyPackage(t)
	if err := os.WriteFile(filepath.Join(dir, "image", "Containerfile"), []byte("FROM ubuntu:24.04\nRUN --network=host true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.chosen = filepath.Join(dir, "workspace.md")
	preview, err := f.ChooseWorkspaceExam(classID)
	if err != nil || preview.Valid || len(preview.Problems) < 2 {
		t.Fatalf("%+v %v", preview, err)
	}
	joined := strings.Join(preview.Problems, "\n")
	for _, want := range []string{"image/Containerfile line 1: FROM ubuntu:24.04: the only image", "image/Containerfile line 2: RUN --network=host is not allowed"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the problems do not say %q:\n%s", want, joined)
		}
	}
	if _, err := f.ImportWorkspaceExam(classID, "pin-1234"); err == nil {
		t.Error("a package with problems was imported")
	}
	// Cancelling the dialog is not an error and keeps nothing.
	f.chosen = ""
	if cancelled, err := f.ChooseWorkspaceExam(classID); err != nil || !cancelled.Cancelled {
		t.Errorf("cancelled: %+v %v", cancelled, err)
	}
	if _, err := f.ChooseWorkspaceExam("not an id"); err == nil {
		t.Error("a class that is not an ID")
	}
}

// What the server refuses on import is said, every problem of it, and the package is used up.
func TestWhatTheServerRefusesOnImportIsSaid(t *testing.T) {
	f := newPublishFixture(t, "")
	f.chosen = filepath.Join(copyPackage(t), "workspace.md")
	f.server.setReply(func(w http.ResponseWriter, r *http.Request, body []byte) bool {
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/workspace-exam") {
			problemsReply(w, http.StatusBadRequest, "invalid workspace exam", "this server does not have the course image cbe/cpp20", "image/Containerfile line 4: VOLUME is not allowed")
			return true
		}
		return false
	})
	if preview, err := f.ChooseWorkspaceExam(classID); err != nil || !preview.Valid {
		t.Fatalf("%+v %v", preview, err)
	}
	_, err := f.ImportWorkspaceExam(classID, "pin-1234")
	if err == nil || !strings.Contains(err.Error(), "does not have the course image cbe/cpp20") || !strings.Contains(err.Error(), "VOLUME is not allowed") {
		t.Fatalf("the refusal: %v", err)
	}
	if _, err := f.ImportWorkspaceExam(classID, "pin-1234"); err == nil || !strings.Contains(err.Error(), "Choose a workspace package") {
		t.Errorf("the package was kept after the server refused it: %v", err)
	}
}

func goodBuild(id int64, state string) ImageBuild {
	b := ImageBuild{ID: id, State: state, BaseName: "cbe/cpp20", BaseDigest: "sha256:" + strings.Repeat("b", 64), Warnings: []string{}, QueuedAt: at, DurationMS: 1500}
	if state == "ok" {
		b.Hash = "0123456789abcdef"
	}
	return b
}

func TestAnExamsImageIsBuiltAndReadThroughTheServer(t *testing.T) {
	f := newPublishFixture(t, "")
	ready := ExamImage{ExamID: examID, State: "ready", Image: "cbe/cpp20", Ready: true, Build: ptrBuild(goodBuild(3, "ok")), Builds: []ImageBuild{goodBuild(3, "ok")}}
	withLog := goodBuild(3, "ok")
	withLog.Log = "Base image: cbe/cpp20 is localhost/cbe/cpp20:latest\n            digest sha256:bbbb\nVerified: made from cbe/cpp20\n"
	var askedMu sync.Mutex
	var asked []string
	f.server.setReply(func(w http.ResponseWriter, r *http.Request, body []byte) bool {
		askedMu.Lock()
		asked = append(asked, r.Method+" "+r.URL.RequestURI())
		askedMu.Unlock()
		switch {
		case r.Method == "GET" && r.URL.Path == imageRoute:
			view := ready
			if r.URL.Query().Get("log") == "1" {
				b := withLog
				view.Build = &b
			}
			reply(w, view)
		case r.Method == "POST" && r.URL.Path == imageRoute+"/build":
			var request map[string]bool
			_ = json.Unmarshal(body, &request)
			w.WriteHeader(http.StatusAccepted)
			queued := ExamImage{ExamID: examID, State: "queued", Image: "cbe/cpp20", Build: ptrBuild(goodBuild(4, "queued")), Builds: []ImageBuild{goodBuild(4, "queued")}}
			if request["rebuild"] {
				queued.Build.Rebuild = true
			}
			reply(w, queued)
		case r.Method == "GET" && r.URL.Path == imageRoute+"/build/3":
			reply(w, withLog)
		case r.Method == "GET" && r.URL.Path == imageRoute+"/build/5":
			wrong := withLog
			wrong.ID = 99
			reply(w, wrong)
		case r.Method == "GET" && r.URL.Path == imageRoute+"/build/6":
			bad := withLog
			bad.ID, bad.State, bad.Hash = 6, "ok", ""
			reply(w, bad)
		default:
			return false
		}
		return true
	})
	view, err := f.ExamImage(examID, false, 0)
	if err != nil || view.State != "ready" || !view.Ready || view.Build == nil || view.Build.Hash != "0123456789abcdef" || len(view.Builds) != 1 || view.Build.Log != "" {
		t.Fatalf("%+v %v", view, err)
	}
	logged, err := f.ExamImage(examID, true, 0)
	if err != nil || !strings.Contains(logged.Build.Log, "digest sha256:bbbb") {
		t.Fatalf("with the log: %+v %v", logged, err)
	}
	started, err := f.BuildExamImage(examID, true)
	if err != nil || started.State != "queued" || !started.Build.Rebuild {
		t.Fatalf("starting a build: %+v %v", started, err)
	}
	one, err := f.ExamImageBuild(examID, 3)
	if err != nil || one.ID != 3 || !strings.Contains(one.Log, "Verified") {
		t.Fatalf("one build: %+v %v", one, err)
	}
	// What is not what was asked for, or makes no sense, is an error.
	if _, err := f.ExamImageBuild(examID, 5); err == nil {
		t.Error("another build was taken for the one asked for")
	}
	if _, err := f.ExamImageBuild(examID, 6); err == nil {
		t.Error("a build that is ok with no name was taken")
	}
	for _, bad := range []func() error{
		func() error { _, err := f.ExamImage("not an id", false, 0); return err },
		func() error { _, err := f.BuildExamImage("not an id", false); return err },
		func() error { _, err := f.ExamImageBuild(examID, 0); return err },
		func() error { _, err := f.ExamImageBuild("nope", 3); return err },
	} {
		if err := bad(); err == nil {
			t.Error("a bad selection was sent to the server")
		}
	}
	askedMu.Lock()
	defer askedMu.Unlock()
	for _, line := range asked {
		if strings.Contains(line, "not%20an") || strings.Contains(line, "nope") {
			t.Errorf("a bad ID went to the server: %s", line)
		}
	}
}

func ptrBuild(b ImageBuild) *ImageBuild { return &b }

func TestTheServersAnswersAboutAnImageAreChecked(t *testing.T) {
	// What the fake server answers, changed by the test between requests.
	var mu sync.Mutex
	var servedValue any
	statusValue := http.StatusOK
	answer := func(status int, value any) {
		mu.Lock()
		defer mu.Unlock()
		statusValue, servedValue = status, value
	}
	f := newPublishFixture(t, "")
	f.server.setReply(func(w http.ResponseWriter, r *http.Request, body []byte) bool {
		if strings.HasPrefix(r.URL.Path, imageRoute) {
			mu.Lock()
			status, value := statusValue, servedValue
			mu.Unlock()
			w.WriteHeader(status)
			reply(w, value)
			return true
		}
		return false
	})
	ok := goodBuild(1, "ok")
	for name, view := range map[string]any{
		"an unknown state":                    map[string]any{"exam_id": examID, "state": "great", "build": nil, "builds": []any{}},
		"another exam":                        ExamImage{ExamID: otherExam, State: "none"},
		"ready with no build":                 ExamImage{ExamID: examID, State: "ready", Ready: true},
		"a build in a state that is not one":  ExamImage{ExamID: examID, State: "ready", Build: &ImageBuild{ID: 1, State: "great", QueuedAt: at}},
		"an ok build with no name":            ExamImage{ExamID: examID, State: "ready", Build: &ImageBuild{ID: 1, State: "ok", QueuedAt: at}},
		"a build with a name that is no name": ExamImage{ExamID: examID, State: "ready", Build: &ImageBuild{ID: 1, State: "ok", Hash: "../../x", QueuedAt: at}},
		"a build with no number":              ExamImage{ExamID: examID, State: "ready", Build: &ImageBuild{State: "ok", Hash: "0123456789abcdef", QueuedAt: at}},
		"a log that is too large":             ExamImage{ExamID: examID, State: "ready", Build: &ImageBuild{ID: 1, State: "ok", Hash: "0123456789abcdef", QueuedAt: at, Log: strings.Repeat("x", 2<<20+1)}},
		"a state with no build to go with it": ExamImage{ExamID: examID, State: "failed"},
		"a negative time":                     ExamImage{ExamID: examID, State: "ready", Build: &ImageBuild{ID: 1, State: "ok", Hash: "0123456789abcdef", QueuedAt: at, DurationMS: -1}},
	} {
		answer(http.StatusOK, view)
		if got, err := f.ExamImage(examID, false, 0); err == nil {
			t.Errorf("%s was taken: %+v", name, got)
		}
	}
	// Building: an answer that says there is nothing to build, or that is not a build, is not taken for one.
	for name, view := range map[string]ExamImage{
		"none":             {ExamID: examID, State: "none"},
		"no build with it": {ExamID: examID, State: "queued"},
		"another exam's":   {ExamID: otherExam, State: "queued", Build: &ok},
	} {
		answer(http.StatusAccepted, view)
		if got, err := f.BuildExamImage(examID, false); err == nil {
			t.Errorf("an answer that is %s was taken: %+v", name, got)
		}
	}
	// The server's own refusal comes through in its words.
	answer(http.StatusConflict, map[string]string{"error": "this exam's image is already waiting to be built or being built"})
	if _, err := f.BuildExamImage(examID, false); err == nil || !strings.Contains(err.Error(), "already waiting to be built or being built") {
		t.Errorf("the server's refusal: %v", err)
	}
	answer(http.StatusConflict, map[string]string{"error": "this exam has no image folder: it runs in its course image, so there is nothing to build"})
	if _, err := f.BuildExamImage(examID, false); err == nil || !strings.Contains(err.Error(), "no image folder") {
		t.Errorf("an exam with no image folder: %v", err)
	}
}

func TestTheCatalogCarriesTheWorkspaceFieldsAndRefusesInconsistentOnes(t *testing.T) {
	three := 3
	workspaceEntry := func(c *Catalog) {
		c.Exams[1].Kind, c.Exams[1].Todos = "workspace", &three
		c.Exams[1].Image = &ExamImage{State: "not_built", Image: "cbe/cpp20", Builds: []ImageBuild{}}
	}
	good := catalog()
	workspaceEntry(&good)
	if err := validateCatalog(good); err != nil {
		t.Fatalf("a workspace exam in the catalog: %v", err)
	}
	if err := validateCatalog(catalog()); err != nil {
		t.Fatalf("a catalog of question exams, as before: %v", err)
	}
	for name, change := range map[string]func(c *Catalog){
		"an unknown kind":                  func(c *Catalog) { workspaceEntry(c); c.Exams[1].Kind = "other" },
		"a question exam with an image":    func(c *Catalog) { c.Exams[1].Image = &ExamImage{State: "none"} },
		"a question exam with todos":       func(c *Catalog) { c.Exams[1].Todos = &three },
		"a workspace exam with no todos":   func(c *Catalog) { workspaceEntry(c); c.Exams[1].Todos = nil },
		"a workspace exam with no image":   func(c *Catalog) { workspaceEntry(c); c.Exams[1].Image = nil },
		"a negative number of todos":       func(c *Catalog) { workspaceEntry(c); minus := -1; c.Exams[1].Todos = &minus },
		"an image in a state that is none": func(c *Catalog) { workspaceEntry(c); c.Exams[1].Image.State = "great" },
	} {
		c := catalog()
		change(&c)
		if validateCatalog(c) == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// The log of a build that is going is read from an offset, with where the build is; what the server answers is checked.
func TestTheLogOfABuildThatIsGoingIsAskedForFromAnOffset(t *testing.T) {
	f := newPublishFixture(t, "")
	var mu sync.Mutex
	var queries []string
	running := ImageBuild{ID: 4, State: "running", BaseName: "cbe/cpp20", Warnings: []string{}, QueuedAt: at, Live: true, Log: "STEP 2/9: RUN make\n", LogFrom: 120, LogNext: 140, Step: 2, Steps: 9, StepText: "RUN make", LastOutputAt: &at}
	f.server.setReply(func(w http.ResponseWriter, r *http.Request, body []byte) bool {
		if r.URL.Path != imageRoute {
			return false
		}
		mu.Lock()
		queries = append(queries, r.URL.RawQuery)
		mu.Unlock()
		reply(w, ExamImage{ExamID: examID, State: "building", Image: "cbe/cpp20", Build: &running, Builds: []ImageBuild{running}})
		return true
	})
	view, err := f.ExamImage(examID, true, 120)
	if err != nil || view.Build == nil || !view.Build.Live || view.Build.LogFrom != 120 || view.Build.LogNext != 140 || view.Build.Step != 2 || view.Build.Steps != 9 || view.Build.StepText != "RUN make" || view.Build.LastOutputAt == nil {
		t.Fatalf("%+v %v", view.Build, err)
	}
	if _, err := f.ExamImage(examID, false, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ExamImage(examID, true, -1); err == nil {
		t.Error("a negative offset was sent")
	}
	mu.Lock()
	got := append([]string{}, queries...)
	mu.Unlock()
	if len(got) != 2 || got[0] != "log=1&log_from=120" || got[1] != "" {
		t.Errorf("what was asked: %q", got)
	}
	// Answers that make no sense are not taken for one.
	for name, mutate := range map[string]func(b *ImageBuild){
		"an offset that goes back":        func(b *ImageBuild) { b.LogNext = 10 },
		"a negative offset":               func(b *ImageBuild) { b.LogFrom, b.LogNext = -5, 0 },
		"more steps done than there are":  func(b *ImageBuild) { b.Step, b.Steps = 9, 4 },
		"a negative step":                 func(b *ImageBuild) { b.Step = -1 },
		"a step of a thousand characters": func(b *ImageBuild) { b.StepText = strings.Repeat("x", 2000) },
		"a live build that has ended":     func(b *ImageBuild) { b.State, b.Hash = "ok", "0123456789abcdef" },
	} {
		bad := running
		mutate(&bad)
		if validBuild(bad) {
			t.Errorf("%s was accepted", name)
		}
	}
}
