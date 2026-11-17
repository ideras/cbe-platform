package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"exam-platform/internal/workspace"
)

const ownBaseContainerfile = "FROM docker.io/library/python:3.12-slim@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\nRUN pip install numpy==2.1.3\n"

// ownBaseFixture is an exam with a base image of its own: no image: in its document, and an image bundle whose Containerfile starts FROM Python.
func ownBaseFixture(t *testing.T) *workspaceFixture {
	t.Helper()
	return newWorkspaceFixtureWith(t, func(d *workspace.Document) { d.Workspace.Image = "" }, fixtureOptions{image: map[string]string{"Containerfile": ownBaseContainerfile}, unpublished: true})
}

// The whole flow for an exam with a base of its own: imported with no course image, not built, refused a publication, built (the base is the file's), published,
// and its students' containers and its grading runs made from the build and from nothing else.
func TestAnExamWithABaseOfItsOwnIsImportedBuiltPublishedAndRun(t *testing.T) {
	f := ownBaseFixture(t)
	view := f.imageView(t, false)
	if view.State != workspace.ImageNotBuilt || view.Ready || view.Image != "" || !view.OwnBase || view.Build != nil {
		t.Fatalf("before any build: %+v", view)
	}
	if status, body := f.publishRoute(t); status == http.StatusOK || !strings.Contains(body, "image") {
		t.Errorf("publishing an exam whose image is not built: %d %s", status, body)
	}
	view = f.built(t)
	if view.State != workspace.ImageReady || !view.Ready || !view.OwnBase || view.Build == nil || view.Build.Hash == "" {
		t.Fatalf("after the build: %+v", view)
	}
	if view.Build.BaseName != "docker.io/library/python:3.12-slim@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" {
		t.Errorf("the base the build names: %q", view.Build.BaseName)
	}
	if len(f.runner.Builds) != 1 || f.runner.Builds[0].Request.Image != "" {
		t.Errorf("the build the runner was asked for: %+v", f.runner.Builds)
	}
	if status, body := f.publishRoute(t); status != http.StatusOK {
		t.Fatalf("publishing: %d %s", status, body)
	}
	login := f.loginReady(t, "0012")
	if got := f.runner.CreatedBuilds[login.SessionID]; got != view.Build.Hash {
		t.Errorf("the container was made from build %q, not %q", got, view.Build.Hash)
	}
}

// What the server checks when such an exam is imported: it has an image bundle, and the Containerfile breaks none of the rules.
func TestAnExamWithABaseOfItsOwnIsHeldToTheRulesWhenItIsImported(t *testing.T) {
	for name, test := range map[string]struct {
		image   map[string]string
		problem string
	}{
		"a volume":        {map[string]string{"Containerfile": ownBaseContainerfile + "VOLUME /data\n"}, "VOLUME is not allowed"},
		"a variable":      {map[string]string{"Containerfile": "ARG V=1\nFROM python:$V\n"}, "FROM cannot use a variable"},
		"not a reference": {map[string]string{"Containerfile": "FROM Python:3.12\n"}, "is not an image reference"},
		"host network":    {map[string]string{"Containerfile": ownBaseContainerfile + "RUN --network=host true\n"}, "host would let the build reach"},
	} {
		f := newWorkspaceFixtureWith(t, func(d *workspace.Document) { d.Workspace.Image = "" }, fixtureOptions{noImport: true})
		image := tarOf(t, test.image)
		doc := document(f.starter, f.grading)
		doc.Workspace.Image = ""
		doc.ImageBundle = hashOf(image)
		body, contentType := importBody(t, doc, f.starter, f.grading, map[string][]byte{"image": image})
		w := requestWithType(f.router, "POST", "/admin/class/"+apiClassID+"/workspace-exam", body, contentType, "Bearer "+testAdminToken)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), test.problem) {
			t.Errorf("%s: %d %s", name, w.Code, w.Body.String())
		}
	}
	// No image at all: neither a course image nor an image bundle.
	f := newWorkspaceFixtureWith(t, nil, fixtureOptions{noImport: true})
	doc := document(f.starter, f.grading)
	doc.Workspace.Image = ""
	body, contentType := importBody(t, doc, f.starter, f.grading, nil)
	w := requestWithType(f.router, "POST", "/admin/class/"+apiClassID+"/workspace-exam", body, contentType, "Bearer "+testAdminToken)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "workspace.image is missing, and the package has no image bundle") {
		t.Errorf("no image at all: %d %s", w.Code, w.Body.String())
	}
}
