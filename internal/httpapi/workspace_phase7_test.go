//go:build linux

package httpapi

import (
	"archive/tar"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"exam-platform/internal/runner"
	"exam-platform/internal/workspace"
	"exam-platform/internal/workspace/workspacetest"
)

// ---------------------------------------------------------------------------------------------------------------------------------
// Phase 7: an exam's own image. These go through the proctor's routes, the publisher with its gate and the real database, with the fake
// runner standing in for cbe-runner (the real one is run by deploy/workspace/test-image.sh).

var libraryImage = map[string]string{
	"Containerfile":   "FROM cbe/cpp20\nUSER root\nRUN apt-get install -y libfoo-dev=1.0-1\nCOPY extra/lib.txt /opt/lib.txt\nUSER 1000:1000\n",
	"extra/lib.txt":   "THE LIBRARY",
	"extra/other.txt": "other",
}

// imageFixture is an exam with its own image, imported and not published.
func imageFixture(t *testing.T) *workspaceFixture {
	t.Helper()
	return newWorkspaceFixtureWith(t, nil, fixtureOptions{image: libraryImage, unpublished: true})
}

func (f *workspaceFixture) imageView(t *testing.T, log bool) workspace.ImageView {
	t.Helper()
	path := "/admin/workspace/exam/" + f.examID + "/image"
	if log {
		path += "?log=1"
	}
	w := request(f.router, "GET", path, "", "Bearer "+testAdminToken)
	if w.Code != http.StatusOK {
		t.Fatalf("image = %d %s", w.Code, w.Body.String())
	}
	return decoded[workspace.ImageView](t, w.Body.Bytes())
}

func (f *workspaceFixture) startBuild(t *testing.T, body string) (int, []byte) {
	t.Helper()
	return f.post(t, "/admin/workspace/exam/"+f.examID+"/image/build", body)
}

// built builds the exam's image and waits for the build to end.
func (f *workspaceFixture) built(t *testing.T) workspace.ImageView {
	t.Helper()
	if status, body := f.startBuild(t, ""); status != http.StatusAccepted {
		t.Fatalf("build = %d %s", status, body)
	}
	return f.waitBuild(t)
}

// waitBuild waits for the latest build of the exam's image to be over.
func (f *workspaceFixture) waitBuild(t *testing.T) workspace.ImageView {
	t.Helper()
	var view workspace.ImageView
	eventually(t, "the build to end", func() bool {
		view = f.imageView(t, false)
		return view.State != workspace.ImageQueued && view.State != workspace.ImageBuilding
	})
	return view
}

func (f *workspaceFixture) publishRoute(t *testing.T) (int, string) {
	t.Helper()
	status, body := f.post(t, "/admin/exam/"+f.examID+"/publish", `{"expected_current":"","allow_active":true}`)
	return status, string(body)
}

var sixteenHex = regexp.MustCompile(`^[0-9a-f]{16}$`)

func tarLink(name, target string) tar.Header {
	return tar.Header{Name: name, Typeflag: tar.TypeSymlink, Linkname: target, Mode: 0o777}
}

// The whole flow: import with an image folder, see it is not built, be refused a publication, build it, read the log with the base's digest,
// publish, and have the containers and the grading runs made from that image.
func TestAnExamWithItsOwnImageIsBuiltBeforeItIsPublishedAndUsedByItsContainers(t *testing.T) {
	f := imageFixture(t)
	view := f.imageView(t, false)
	if view.State != workspace.ImageNotBuilt || view.Ready || view.Image != "cbe/cpp20" || view.Build != nil {
		t.Fatalf("before any build: %+v", view)
	}
	// The exam list says it is a workspace exam with two TODO items and an image that is not built.
	list := request(f.router, "GET", "/admin/exams", "", "Bearer "+testAdminToken)
	var catalog struct {
		Exams []struct {
			ID    string               `json:"exam_id"`
			Kind  string               `json:"kind"`
			Todos *int                 `json:"todos"`
			Image *workspace.ImageView `json:"image"`
		} `json:"exams"`
	}
	if list.Code != http.StatusOK || json.Unmarshal(list.Body.Bytes(), &catalog) != nil {
		t.Fatalf("catalog = %d %s", list.Code, list.Body.String())
	}
	found := false
	for _, entry := range catalog.Exams {
		if entry.ID == f.examID {
			found = true
			if entry.Kind != "workspace" || entry.Todos == nil || *entry.Todos != 2 || entry.Image == nil || entry.Image.State != workspace.ImageNotBuilt || entry.Image.Ready {
				t.Errorf("the exam in the list: %+v %v %+v", entry, entry.Todos, entry.Image)
			}
		}
	}
	if !found {
		t.Fatal("the exam is not in the list")
	}
	// It cannot be published yet, and the refusal says why.
	if status, body := f.publishRoute(t); status != http.StatusConflict || !strings.Contains(body, "has not been built") {
		t.Fatalf("publishing an exam whose image is not built: %d %s", status, body)
	}

	view = f.built(t)
	if view.State != workspace.ImageReady || !view.Ready || view.Build == nil || view.Build.State != workspace.BuildOK || !sixteenHex.MatchString(view.Build.Hash) ||
		view.Build.BaseDigest != workspacetest.BaseDigest || view.Build.BaseName != "cbe/cpp20" || view.Build.Cached || len(view.Builds) != 1 {
		t.Fatalf("after the build: %+v %+v", view, view.Build)
	}
	if view.Present == nil || !*view.Present {
		t.Errorf("the runner was not asked whether it has the image: %v", view.Present)
	}
	// The log records the base image's digest (the plan's acceptance) and what the context had; the list of builds has no logs.
	withLog := f.imageView(t, true)
	if !strings.Contains(withLog.Build.Log, "Base image: cbe/cpp20 digest "+workspacetest.BaseDigest) || len(f.runner.Builds) != 1 {
		t.Errorf("the log: %q (%d builds)", withLog.Build.Log, len(f.runner.Builds))
	}
	if view.Builds[0].Log != "" {
		t.Error("the list of builds carries a log")
	}
	call := f.runner.Builds[0]
	if call.Request.Exam != f.examID || call.Request.Image != "cbe/cpp20" || call.Files["Containerfile"] != libraryImage["Containerfile"] || call.Files["extra/lib.txt"] != "THE LIBRARY" {
		t.Errorf("what the runner was asked to build: %+v", call.Request)
	}
	one := request(f.router, "GET", fmt.Sprintf("/admin/workspace/exam/%s/image/build/%d", f.examID, view.Build.ID), "", "Bearer "+testAdminToken)
	if one.Code != http.StatusOK || !strings.Contains(one.Body.String(), "Base image: cbe/cpp20") {
		t.Errorf("one build: %d %s", one.Code, one.Body.String())
	}

	// Published, and a student's container is made from the exam's own image.
	if status, body := f.publishRoute(t); status != http.StatusOK {
		t.Fatalf("publishing: %d %s", status, body)
	}
	login := f.loginReady(t, "0012")
	if got := f.runner.CreatedBuilds[login.SessionID]; got != view.Build.Hash {
		t.Errorf("the container was made from build %q, want %q", got, view.Build.Hash)
	}
	// A grading run is made from it too: the hidden tests may need the library.
	f.runner.GradeHook = func(_ context.Context, call workspacetest.GradeCall) (runner.GradeResult, error) {
		return runner.GradeResult{Stdout: `{"t1": {"passed": true}}`}, nil
	}
	submitted := f.submitted(t, "0034", "int main(void){return 0;}\n")
	if status, body := f.post(t, "/admin/workspace/session/"+submitted.SessionID+"/grade", ""); status != http.StatusAccepted {
		t.Fatalf("grading: %d %s", status, body)
	}
	f.gradedWith(t, submitted.SessionID)
	if len(f.runner.Gradings) != 1 || f.runner.Gradings[0].Request.Build != view.Build.Hash || f.runner.Gradings[0].Request.Image != "cbe/cpp20" {
		t.Errorf("the grading run: %+v", f.runner.Gradings)
	}
	if got := f.runner.CreatedBuilds[submitted.SessionID]; got != view.Build.Hash {
		t.Errorf("the second student's container: %q", got)
	}
}

// Phase 8: the Containerfile's warnings are shown and kept unless the exam says image_warnings: off, and then the runner is told not to make them.
func TestTheWarningsOfAnImageAreKeptUnlessTheExamSwitchesThemOff(t *testing.T) {
	unpinned := map[string]string{"Containerfile": "FROM cbe/cpp20\nUSER root\nRUN apt-get install -y libfoo-dev\nUSER 1000:1000\n"}
	on := newWorkspaceFixtureWith(t, nil, fixtureOptions{image: unpinned, unpublished: true})
	view := on.built(t)
	if view.Build == nil || len(view.Build.Warnings) != 1 || on.runner.Builds[0].Request.NoWarnings {
		t.Fatalf("by default: %+v %+v", view.Build, on.runner.Builds[0].Request)
	}
	off := newWorkspaceFixtureWith(t, func(d *workspace.Document) { no := false; d.Workspace.ImageWarnings = &no }, fixtureOptions{image: unpinned, unpublished: true})
	view = off.built(t)
	if view.State != workspace.ImageReady || view.Build == nil || len(view.Build.Warnings) != 0 || !off.runner.Builds[0].Request.NoWarnings {
		t.Errorf("with the warnings off: %+v %+v", view.Build, off.runner.Builds[0].Request)
	}
}

// Nothing of the image reaches a student: the view of the workspace has no image and no Containerfile.
func TestTheStudentNeverSeesTheImageOrItsContainerfile(t *testing.T) {
	f := imageFixture(t)
	f.built(t)
	if status, body := f.publishRoute(t); status != http.StatusOK {
		t.Fatalf("publishing: %d %s", status, body)
	}
	login := f.loginReady(t, "0012")
	w := request(f.router, "GET", "/workspace", "", "Bearer "+login.SessionToken)
	if w.Code != http.StatusOK {
		t.Fatalf("view = %d", w.Code)
	}
	for _, secret := range []string{"Containerfile", "libfoo", "THE LIBRARY", "cbe-exam", "image_bundle", hashOf(f.image), "base_digest"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Errorf("the student's view has %q: %s", secret, w.Body.String())
		}
	}
}

func TestAFailedBuildBlocksPublishingAndAGoodImageIsNotLostToAFailedRebuild(t *testing.T) {
	f := imageFixture(t)
	f.runner.BuildHook = func(context.Context, workspacetest.BuildCall) (runner.BuildResult, error) {
		return runner.BuildResult{Failure: runner.FailBuild, Message: `building at STEP "RUN make": exit status 2`, BaseName: "cbe/cpp20", BaseDigest: workspacetest.BaseDigest,
			Log: "STEP 2/3: RUN make\ncc: error: no such file\n", Warnings: []string{"image/Containerfile line 3: apt-get installs x without a version"}}, nil
	}
	view := f.built(t)
	if view.State != workspace.ImageFailed || view.Ready || view.Build.Failure != workspace.BuildFailBuild || !strings.Contains(view.Build.Message, "exit status 2") || len(view.Build.Warnings) != 1 {
		t.Fatalf("a build that failed: %+v %+v", view, view.Build)
	}
	if log := f.imageView(t, true).Build.Log; !strings.Contains(log, "no such file") {
		t.Errorf("the log of a failed build: %q", log)
	}
	if status, body := f.publishRoute(t); status != http.StatusConflict || !strings.Contains(body, "build failed") && !strings.Contains(body, "failed") {
		t.Errorf("publishing after a failed build: %d %s", status, body)
	}
	// Every way a build can end badly is recorded with its code; an unknown code is the runner's.
	for _, c := range []struct{ code, want string }{
		{runner.FailContainerfile, workspace.BuildFailContainerfile}, {runner.FailBase, workspace.BuildFailBase}, {runner.FailTimeout, workspace.BuildFailTimeout},
		{runner.FailVerify, workspace.BuildFailVerify}, {"something new", workspace.BuildFailRunner},
	} {
		f.runner.BuildHook = func(context.Context, workspacetest.BuildCall) (runner.BuildResult, error) {
			return runner.BuildResult{Failure: c.code, Message: "said " + c.code}, nil
		}
		if got := f.built(t); got.Build.Failure != c.want || got.Build.Message != "said "+c.code {
			t.Errorf("%s: %+v", c.code, got.Build)
		}
	}
	// A runner that does not answer, or answers with nonsense, is a failed build too, not a hung one.
	f.runner.BuildHook = func(context.Context, workspacetest.BuildCall) (runner.BuildResult, error) {
		return runner.BuildResult{}, fmt.Errorf("the runner is down")
	}
	if got := f.built(t); got.State != workspace.ImageFailed || got.Build.Failure != workspace.BuildFailRunner || !strings.Contains(got.Build.Message, "the runner is down") {
		t.Errorf("a runner that is down: %+v", got.Build)
	}
	f.runner.BuildHook = func(context.Context, workspacetest.BuildCall) (runner.BuildResult, error) {
		return runner.BuildResult{OK: true, Hash: "not a hash"}, nil
	}
	if got := f.built(t); got.State != workspace.ImageFailed || got.Build.Failure != workspace.BuildFailRunner {
		t.Errorf("an answer with no usable name: %+v", got.Build)
	}

	// The proctor fixes it: a good build makes the exam publishable, and a later failed rebuild does not take the image away.
	f.runner.BuildHook = nil
	if good := f.built(t); good.State != workspace.ImageReady || !good.Ready {
		t.Fatalf("a good build: %+v", good)
	}
	f.runner.BuildHook = func(context.Context, workspacetest.BuildCall) (runner.BuildResult, error) {
		return runner.BuildResult{Failure: runner.FailBuild, Message: "this time it failed"}, nil
	}
	again := f.built(t)
	if again.State != workspace.ImageFailed || !again.Ready {
		t.Errorf("a failed rebuild of an image that was built: %+v", again)
	}
	if status, body := f.publishRoute(t); status != http.StatusOK {
		t.Errorf("publishing with a good image and a failed rebuild: %d %s", status, body)
	}
}

func TestBuildsAreOneAtATimePerExamAndTheSameFilesAreACacheHit(t *testing.T) {
	f := imageFixture(t)
	release := make(chan struct{})
	var once sync.Once
	f.runner.BuildHook = func(ctx context.Context, call workspacetest.BuildCall) (runner.BuildResult, error) {
		select {
		case <-release:
		case <-ctx.Done():
		}
		f.runner.Have(call.Request.Exam, "0123456789abcdef")
		return runner.BuildResult{OK: true, Hash: "0123456789abcdef", ImageID: "sha256:x", BaseName: "cbe/cpp20", BaseDigest: workspacetest.BaseDigest, Log: "the first log\n"}, nil
	}
	if status, _ := f.startBuild(t, ""); status != http.StatusAccepted {
		t.Fatal(status)
	}
	eventually(t, "the build to start", func() bool { return f.imageView(t, false).State == workspace.ImageBuilding })
	if status, body := f.startBuild(t, ""); status != http.StatusConflict || !strings.Contains(string(body), "already") {
		t.Errorf("a second build while one goes: %d %s", status, body)
	}
	if status, body := f.publishRoute(t); status != http.StatusConflict || !strings.Contains(body, "is being built") {
		t.Errorf("publishing while it builds: %d %s", status, body)
	}
	once.Do(func() { close(release) })
	if view := f.waitBuild(t); view.State != workspace.ImageReady {
		t.Fatalf("%+v", view)
	}
	// The runner says the image was there already: a cache hit, whose log holds the log of the build that made it.
	f.runner.BuildHook = func(context.Context, workspacetest.BuildCall) (runner.BuildResult, error) {
		return runner.BuildResult{OK: true, Cached: true, Hash: "0123456789abcdef", ImageID: "sha256:x", BaseName: "cbe/cpp20", BaseDigest: workspacetest.BaseDigest, Log: "nothing was built\n"}, nil
	}
	cached := f.built(t)
	if !cached.Build.Cached || cached.Build.Hash != "0123456789abcdef" || len(cached.Builds) != 2 {
		t.Fatalf("a cache hit: %+v", cached.Build)
	}
	if log := f.imageView(t, true).Build.Log; !strings.Contains(log, "nothing was built") || !strings.Contains(log, "the first log") || !strings.Contains(log, "the log of the build that made this image") {
		t.Errorf("the log of a cache hit: %q", log)
	}
	// A rebuild is asked of the runner as one.
	f.runner.BuildHook = nil
	if status, _ := f.startBuild(t, `{"rebuild": true}`); status != http.StatusAccepted {
		t.Fatal(status)
	}
	f.waitBuild(t)
	if last := f.runner.Builds[len(f.runner.Builds)-1]; !last.Request.Rebuild {
		t.Errorf("the rebuild was not asked as one: %+v", last.Request)
	}
	if status, _ := f.startBuild(t, `{"rebuild": "yes"}`); status != http.StatusBadRequest {
		t.Errorf("a body that is not the request: %d", status)
	}
}

func TestAnImageTheRunnerLostBlocksPublishingAndStudents(t *testing.T) {
	f := imageFixture(t)
	view := f.built(t)
	f.runner.Forget(f.examID, view.Build.Hash)
	if got := f.imageView(t, false); got.State != workspace.ImageMissing || got.Ready || got.Present == nil || *got.Present {
		t.Errorf("an image that was removed: %+v", got)
	}
	if status, body := f.publishRoute(t); status != http.StatusConflict || !strings.Contains(body, "does not have the exam's image") {
		t.Errorf("publishing without the image: %d %s", status, body)
	}
	// Building again puts it back.
	f.built(t)
	if status, body := f.publishRoute(t); status != http.StatusOK {
		t.Errorf("publishing after building again: %d %s", status, body)
	}
}

func TestAnExamWithoutAnImageIsNotAffectedByAny_OfThis(t *testing.T) {
	f := newWorkspaceFixtureWith(t, nil, fixtureOptions{unpublished: true})
	view := f.imageView(t, false)
	if view.State != workspace.ImageNone || !view.Ready || view.Build != nil {
		t.Errorf("an exam with no image folder: %+v", view)
	}
	if status, body := f.startBuild(t, ""); status != http.StatusConflict || !strings.Contains(string(body), "no image folder") {
		t.Errorf("building an exam with no image folder: %d %s", status, body)
	}
	if status, body := f.publishRoute(t); status != http.StatusOK {
		t.Errorf("publishing: %d %s", status, body)
	}
	login := f.loginReady(t, "0012")
	if got, found := f.runner.CreatedBuilds[login.SessionID]; !found || got != "" {
		t.Errorf("a container of an exam with no image folder was made from build %q", got)
	}
	if len(f.runner.Builds) != 0 {
		t.Error("something was built")
	}
}

func TestAnImportWithABadImageBundleIsRefusedAndStoresNothing(t *testing.T) {
	good := "FROM cbe/cpp20\nRUN true\n"
	cases := map[string]struct {
		image   map[string]string
		mangle  func(d *workspace.Document, parts map[string][]byte)
		problem string
	}{
		"another base":     {image: map[string]string{"Containerfile": "FROM ubuntu:24.04\n"}, problem: "the only image a workspace exam's Containerfile may start from"},
		"host networking":  {image: map[string]string{"Containerfile": "FROM cbe/cpp20\nRUN --network=host true\n"}, problem: "host would let the build reach"},
		"a volume":         {image: map[string]string{"Containerfile": "FROM cbe/cpp20\nVOLUME /x\n"}, problem: "VOLUME is not allowed"},
		"no Containerfile": {image: map[string]string{"setup.sh": "x"}, problem: "no file called Containerfile"},
		"a Dockerfile":     {image: map[string]string{"Dockerfile": good}, problem: "no file called Containerfile"},
		"a link out": {image: map[string]string{"Containerfile": good}, mangle: func(d *workspace.Document, p map[string][]byte) {
			p["image"] = tarOf(t, map[string]string{"Containerfile": good}, tarLink("x", "../../etc/passwd"))
			d.ImageBundle = hashOf(p["image"])
		}, problem: "image bundle"},
		"a hash that is not the bundle's": {image: map[string]string{"Containerfile": good}, mangle: func(d *workspace.Document, p map[string][]byte) { d.ImageBundle = hashOf([]byte("other")) }, problem: "image bundle does not match the hash"},
		"no hash in the document":         {image: map[string]string{"Containerfile": good}, mangle: func(d *workspace.Document, p map[string][]byte) { d.ImageBundle = "" }, problem: "image_bundle must be sha256:"},
		"a hash with no bundle":           {image: map[string]string{"Containerfile": good}, mangle: func(d *workspace.Document, p map[string][]byte) { delete(p, "image") }, problem: "names an image bundle but none came with it"},
		"a bundle that is not a tar": {image: map[string]string{"Containerfile": good}, mangle: func(d *workspace.Document, p map[string][]byte) {
			p["image"] = []byte("FROM cbe/cpp20\n")
			d.ImageBundle = hashOf(p["image"])
		}, problem: "not a tar archive"},
	}
	for name, c := range cases {
		f := newWorkspaceFixtureWith(t, nil, fixtureOptions{noImport: true})
		image := tarOf(t, c.image)
		doc := document(f.starter, f.grading)
		doc.ImageBundle = hashOf(image)
		parts := map[string][]byte{"image": image}
		if c.mangle != nil {
			c.mangle(&doc, parts)
		}
		body, contentType := importBody(t, doc, f.starter, f.grading, parts)
		w := requestWithType(f.router, "POST", "/admin/class/"+apiClassID+"/workspace-exam", body, contentType, "Bearer "+testAdminToken)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), c.problem) {
			t.Errorf("%s: %d %s", name, w.Code, w.Body.String())
		}
		catalog := request(f.router, "GET", "/admin/exams", "", "Bearer "+testAdminToken)
		if strings.Contains(catalog.Body.String(), `"workspace"`) {
			t.Errorf("%s: an exam was stored", name)
		}
	}
	// An image part is one of the parts the form may hold; others are still refused.
	f := newWorkspaceFixtureWith(t, nil, fixtureOptions{noImport: true})
	body, contentType := importBody(t, document(f.starter, f.grading), f.starter, f.grading, map[string][]byte{"containers": []byte("x")})
	if w := requestWithType(f.router, "POST", "/admin/class/"+apiClassID+"/workspace-exam", body, contentType, "Bearer "+testAdminToken); w.Code != http.StatusBadRequest ||
		!strings.Contains(w.Body.String(), "document, starter, grading and image") {
		t.Errorf("an unknown part: %d %s", w.Code, w.Body.String())
	}
}

func TestBuildsThatAStopInterruptedAreMarkedFailedWhenTheServerStartsAgain(t *testing.T) {
	f := imageFixture(t)
	ctx := context.Background()
	exam, err := f.repo.Exam(ctx, f.examID)
	if err != nil || exam.ImageHash == "" {
		t.Fatalf("%+v %v", exam, err)
	}
	queued, err := f.repo.CreateImageBuild(ctx, f.examID, exam.ImageHash, false, f.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.CreateImageBuild(ctx, f.examID, exam.ImageHash, false, f.clock.Now()); err != workspace.ErrBuildBusy {
		t.Errorf("two builds of one exam at once: %v", err)
	}
	running := queued
	if err := f.repo.StartImageBuild(ctx, running, f.clock.Now()); err != nil {
		t.Fatal(err)
	}
	// A builder that starts after a stop (it is not given the old build's job) marks what was going as interrupted.
	workspace.NewImageBuilder(f.manager).Recover(ctx)
	view := f.imageView(t, false)
	if view.State != workspace.ImageFailed || view.Build.Failure != workspace.BuildFailInterrupted || !strings.Contains(view.Build.Message, "build the image again") {
		t.Errorf("after a restart: %+v", view.Build)
	}
	if status, _ := f.startBuild(t, ""); status != http.StatusAccepted {
		t.Errorf("building again after an interruption: %d", status)
	}
	f.waitBuild(t)
}

func TestTheImageRoutesAreAdminOnlyAndRefuseWhatIsNotThere(t *testing.T) {
	f := imageFixture(t)
	for _, c := range []struct {
		method, path, body string
		token              string
		want               int
	}{
		{"GET", "/admin/workspace/exam/" + f.examID + "/image", "", "", http.StatusUnauthorized},
		{"POST", "/admin/workspace/exam/" + f.examID + "/image/build", "", "", http.StatusUnauthorized},
		{"GET", "/admin/workspace/exam/" + f.examID + "/image", "", "Bearer wrong", http.StatusUnauthorized},
		{"GET", "/admin/workspace/exam/00000000-0000-4000-8000-0000000000ff/image", "", "Bearer " + testAdminToken, http.StatusNotFound},
		{"POST", "/admin/workspace/exam/00000000-0000-4000-8000-0000000000ff/image/build", "", "Bearer " + testAdminToken, http.StatusNotFound},
		{"GET", "/admin/workspace/exam/" + f.examID + "/image/build/abc", "", "Bearer " + testAdminToken, http.StatusNotFound},
		{"GET", "/admin/workspace/exam/" + f.examID + "/image/build/9999", "", "Bearer " + testAdminToken, http.StatusNotFound},
		{"GET", "/admin/workspace/exam/" + f.examID + "/image/build/0", "", "Bearer " + testAdminToken, http.StatusNotFound},
		{"POST", "/admin/workspace/exam/" + f.examID + "/image/build", `{"unknown": 1}`, "Bearer " + testAdminToken, http.StatusBadRequest},
		{"DELETE", "/admin/workspace/exam/" + f.examID + "/image", "", "Bearer " + testAdminToken, http.StatusMethodNotAllowed},
	} {
		w := request(f.router, c.method, c.path, c.body, c.token)
		if w.Code != c.want {
			t.Errorf("%s %s = %d, want %d: %s", c.method, c.path, w.Code, c.want, w.Body.String())
		}
	}
	// The build of one exam is not readable through another's route.
	other := f.built(t)
	status := request(f.router, "GET", fmt.Sprintf("/admin/workspace/exam/%s/image/build/%d", "00000000-0000-4000-8000-0000000000ee", other.Build.ID), "", "Bearer "+testAdminToken)
	if status.Code != http.StatusNotFound {
		t.Errorf("a build read through another exam: %d", status.Code)
	}
}

// A question exam is untouched: the catalog has none of the workspace fields for it.
func TestAQuestionExamInTheCatalogHasNoWorkspaceFields(t *testing.T) {
	f := newWorkspaceFixtureWith(t, nil, fixtureOptions{unpublished: true})
	w := request(f.router, "GET", "/admin/exams", "", "Bearer "+testAdminToken)
	var catalog struct {
		Exams []map[string]json.RawMessage `json:"exams"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &catalog); err != nil || len(catalog.Exams) < 2 {
		t.Fatalf("%v %s", err, w.Body.String())
	}
	var question, workspaceExams int
	for _, entry := range catalog.Exams {
		var id string
		_ = json.Unmarshal(entry["exam_id"], &id)
		_, hasKind := entry["kind"]
		_, hasImage := entry["image"]
		_, hasTodos := entry["todos"]
		if id == f.examID {
			workspaceExams++
			if !hasKind || !hasImage || !hasTodos {
				t.Errorf("the workspace exam lacks its fields: %v", entry)
			}
			continue
		}
		question++
		if hasKind || hasImage || hasTodos {
			t.Errorf("a question exam has workspace fields: %v", entry)
		}
	}
	if question == 0 || workspaceExams != 1 {
		t.Errorf("%d question exams, %d workspace exams", question, workspaceExams)
	}
}

// ---------------------------------------------------------------------------------------------------------------------------------
// The log of a build while it goes (decision P7-10): the runner streams it, the server keeps it and serves it from an offset.

func (f *workspaceFixture) imageFrom(t *testing.T, from int64) workspace.ImageView {
	t.Helper()
	w := request(f.router, "GET", fmt.Sprintf("/admin/workspace/exam/%s/image?log=1&log_from=%d", f.examID, from), "", "Bearer "+testAdminToken)
	if w.Code != http.StatusOK {
		t.Fatalf("image = %d %s", w.Code, w.Body.String())
	}
	return decoded[workspace.ImageView](t, w.Body.Bytes())
}

func TestTheLogOfABuildIsReadWhileItGoesFromAnOffsetWithItsProgress(t *testing.T) {
	f := imageFixture(t)
	first, second := make(chan struct{}), make(chan struct{})
	f.runner.BuildHook = func(ctx context.Context, call workspacetest.BuildCall) (runner.BuildResult, error) {
		call.Log("Base image: cbe/cpp20 digest " + workspacetest.BaseDigest + "\n")
		call.Log("STEP 1/3: FROM cbe/cpp20\nSTEP 2/3: RUN make\n")
		select {
		case <-first:
		case <-ctx.Done():
			return runner.BuildResult{}, ctx.Err()
		}
		call.Log("make: compiling\nSTEP 3/3: USER 1000:1000\n")
		select {
		case <-second:
		case <-ctx.Done():
			return runner.BuildResult{}, ctx.Err()
		}
		call.Log("Verified.\n")
		f.runner.Have(call.Request.Exam, "0123456789abcdef")
		return runner.BuildResult{OK: true, Hash: "0123456789abcdef", ImageID: "sha256:x", BaseName: "cbe/cpp20", BaseDigest: workspacetest.BaseDigest,
			Log: "Base image: cbe/cpp20 digest " + workspacetest.BaseDigest + "\nSTEP 1/3: FROM cbe/cpp20\nSTEP 2/3: RUN make\nmake: compiling\nSTEP 3/3: USER 1000:1000\nVerified.\n"}, nil
	}
	if status, body := f.startBuild(t, ""); status != http.StatusAccepted {
		t.Fatalf("%d %s", status, body)
	}
	var one workspace.ImageView
	eventually(t, "the first part of the log", func() bool {
		one = f.imageFrom(t, 0)
		return one.State == workspace.ImageBuilding && one.Build != nil && strings.Contains(one.Build.Log, "STEP 2/3: RUN make")
	})
	b := one.Build
	if !b.Live || b.LogFrom != 0 || b.LogNext != int64(len(b.Log)) || b.Step != 2 || b.Steps != 3 || b.StepText != "RUN make" || b.LastOutputAt == nil || !strings.HasPrefix(b.Log, "Base image: cbe/cpp20 digest") {
		t.Fatalf("the build while it goes: %+v", b)
	}
	// A look with nothing new carries nothing; the list of builds never carries a log.
	if again := f.imageFrom(t, b.LogNext); again.Build.Log != "" || again.Build.LogNext != b.LogNext || again.Build.LogFrom != b.LogNext || again.Builds[0].Log != "" {
		t.Errorf("a look with nothing new: %+v", again.Build)
	}
	// What the build prints next arrives, and only that.
	close(first)
	var two workspace.ImageView
	eventually(t, "the second part of the log", func() bool {
		two = f.imageFrom(t, b.LogNext)
		return two.Build != nil && strings.Contains(two.Build.Log, "STEP 3/3")
	})
	if two.Build.Log != "make: compiling\nSTEP 3/3: USER 1000:1000\n" || two.Build.LogFrom != b.LogNext || two.Build.Step != 3 || !two.Build.Live {
		t.Errorf("what was new: %+v", two.Build)
	}
	// An offset the server does not have starts again from the beginning, and an offset that is not a number is refused.
	if stale := f.imageFrom(t, 1<<40); stale.Build.LogFrom != 0 || !strings.HasPrefix(stale.Build.Log, "Base image:") {
		t.Errorf("a stale offset: %+v", stale.Build)
	}
	for _, bad := range []string{"abc", "-1", "1.5"} {
		if w := request(f.router, "GET", "/admin/workspace/exam/"+f.examID+"/image?log=1&log_from="+bad, "", "Bearer "+testAdminToken); w.Code != http.StatusBadRequest {
			t.Errorf("log_from=%s: %d", bad, w.Code)
		}
	}
	// The look without the log asks nothing of the log but still says where the build is.
	if quiet := f.imageView(t, false); quiet.Build.Log != "" || quiet.Build.Step != 3 || quiet.Build.StepText != "USER 1000:1000" {
		t.Errorf("a look without the log: %+v", quiet.Build)
	}
	close(second)
	done := f.waitBuild(t)
	if done.State != workspace.ImageReady {
		t.Fatalf("%+v", done)
	}
	// When it has ended the log is the runner's, whole, from the start, and it is not live any more.
	end := f.imageFrom(t, b.LogNext)
	if end.Build.Live || end.Build.LogFrom != 0 || !strings.HasPrefix(end.Build.Log, "Base image:") || !strings.HasSuffix(end.Build.Log, "Verified.\n") || end.Build.Step != 0 || end.Build.LogNext != int64(len(end.Build.Log)) {
		t.Errorf("after the build: %+v", end.Build)
	}
}

// A build that goes on is written to the database as it goes, so a server that stops leaves the log up to there on a build marked interrupted.
func TestTheLogSoFarSurvivesAServerThatStopsDuringABuild(t *testing.T) {
	f := imageFixture(t)
	f.builder.PersistEvery = 20 * time.Millisecond
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	f.runner.BuildHook = func(ctx context.Context, call workspacetest.BuildCall) (runner.BuildResult, error) {
		call.Log("Base image: cbe/cpp20\nSTEP 1/2: RUN make\ncc: compiling main.c\n")
		select {
		case <-release:
		case <-ctx.Done():
		}
		return runner.BuildResult{}, ctx.Err()
	}
	if status, _ := f.startBuild(t, ""); status != http.StatusAccepted {
		t.Fatal(status)
	}
	ctx := context.Background()
	var id int64
	eventually(t, "the log to reach the database while the build goes", func() bool {
		builds, err := f.repo.ImageBuilds(ctx, f.examID, 1)
		if err != nil || len(builds) == 0 || builds[0].State != workspace.BuildRunning {
			return false
		}
		id = builds[0].ID
		full, err := f.repo.ImageBuild(ctx, id)
		return err == nil && strings.Contains(full.Log, "cc: compiling main.c")
	})
	// The server starts again: a new builder marks what was going as interrupted, and the log stays.
	workspace.NewImageBuilder(f.manager).Recover(ctx)
	full, err := f.repo.ImageBuild(ctx, id)
	if err != nil || full.State != workspace.BuildFailed || full.Failure != workspace.BuildFailInterrupted || !strings.Contains(full.Log, "STEP 1/2: RUN make") {
		t.Fatalf("%+v %v", full, err)
	}
	view := f.imageFrom(t, 0)
	if view.State != workspace.ImageFailed || view.Build.Live || !strings.Contains(view.Build.Log, "cc: compiling main.c") {
		t.Errorf("after the restart: %+v", view.Build)
	}
}

// A runner that fails in the middle of a build gives no result, and what the build had printed is the failed build's log.
func TestWhatABuildHadPrintedIsTheLogOfOneTheRunnerLost(t *testing.T) {
	f := imageFixture(t)
	f.runner.BuildHook = func(_ context.Context, call workspacetest.BuildCall) (runner.BuildResult, error) {
		call.Log("Base image: cbe/cpp20\nSTEP 1/2: RUN make\n")
		return runner.BuildResult{}, fmt.Errorf("the runner went away")
	}
	view := f.built(t)
	if view.State != workspace.ImageFailed || view.Build.Failure != workspace.BuildFailRunner || view.Build.Live {
		t.Fatalf("%+v", view.Build)
	}
	if log := f.imageFrom(t, 0).Build.Log; !strings.Contains(log, "STEP 1/2: RUN make") {
		t.Errorf("the log of the failed build: %q", log)
	}
}

// A build that is waiting has a live log with nothing in it yet.
func TestABuildThatIsWaitingHasNothingToReadYet(t *testing.T) {
	f := imageFixture(t)
	ctx := context.Background()
	exam, err := f.repo.Exam(ctx, f.examID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.CreateImageBuild(ctx, f.examID, exam.ImageHash, false, f.clock.Now()); err != nil {
		t.Fatal(err)
	}
	view := f.imageFrom(t, 0)
	if view.State != workspace.ImageQueued || !view.Build.Live || view.Build.Log != "" || view.Build.LogNext != 0 || view.Build.Step != 0 {
		t.Errorf("%+v", view.Build)
	}
}
