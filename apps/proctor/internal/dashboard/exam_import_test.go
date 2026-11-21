package dashboard

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"exam-platform/shared/examdoc"
)

// These helpers run both steps for tests focused on the existing validation and upload behavior.
func (s *Service) previewTraditionalFixture(classID string) (ImportPreview, error) {
	source, err := s.ChooseExamSource(classID)
	if err != nil || source.Cancelled {
		return ImportPreview{}, err
	}
	preview, err := s.PrepareExamImport(classID, source.ID, examdoc.KindQuestion)
	if preview.Traditional == nil {
		return ImportPreview{}, err
	}
	return *preview.Traditional, err
}

func (s *Service) previewWorkspaceFixture(classID string) (WorkspacePreview, error) {
	source, err := s.ChooseExamSource(classID)
	if err != nil || source.Cancelled {
		return WorkspacePreview{}, err
	}
	preview, err := s.PrepareExamImport(classID, source.ID, examdoc.KindWorkspace)
	if preview.Workspace == nil {
		return WorkspacePreview{}, err
	}
	return *preview.Workspace, err
}

func (s *Service) importFixtureID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.examImport == nil {
		return ""
	}
	return s.examImport.id
}

func TestExamImportDetectsOnlyTheSelectedFilename(t *testing.T) {
	f := newPublishFixture(t, examID)
	traditional := writeTemp(t, "midterm.md", []byte(examDocument))
	if err := os.WriteFile(filepath.Join(filepath.Dir(traditional), workspaceManifest), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, file, kind string
	}{
		{"traditional next to workspace.md", traditional, examdoc.KindQuestion},
		{"canonical manifest", filepath.Join("..", "..", "..", "..", "testdata", "workspace", "lab-listas", workspaceManifest), examdoc.KindWorkspace},
		{"malformed manifest", filepath.Join(filepath.Dir(traditional), workspaceManifest), examdoc.KindWorkspace},
		{"cancelled picker", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f.chosen = tc.file
			got, err := f.ChooseExamSource(classID)
			if err != nil || got.Kind != tc.kind || got.Cancelled != (tc.file == "") || (tc.file != "" && (got.File != filepath.Base(tc.file) || got.ID == "")) {
				t.Fatalf("selection = %+v, %v", got, err)
			}
		})
	}
	if len(f.server.seen("POST", examRoute)) != 0 || len(f.server.seen("POST", "/admin/class/"+classID+"/workspace-exam")) != 0 {
		t.Fatal("detection sent an upload")
	}
}

func TestExamImportOverridesAndOneShotIdentity(t *testing.T) {
	f := newPublishFixture(t, examID)
	f.server.setReply(func(w http.ResponseWriter, r *http.Request, _ []byte) bool {
		if r.Method != "POST" || r.URL.Path != examRoute {
			return false
		}
		if r.URL.Query().Get("dry_run") == "true" {
			checkedExam(w)
		} else {
			_, _ = w.Write([]byte(`{"exam_id":"` + examID + `"}`))
		}
		return true
	})
	pick := func(path string) ExamImportSource {
		t.Helper()
		f.chosen = path
		source, err := f.ChooseExamSource(classID)
		if err != nil || source.ID == "" {
			t.Fatalf("selection = %+v, %v", source, err)
		}
		return source
	}
	normalPath := writeTemp(t, "exam.md", []byte(examDocument))
	manifest := filepath.Join(filepath.Dir(normalPath), workspaceManifest)
	if err := os.WriteFile(manifest, []byte(examDocument), 0600); err != nil {
		t.Fatal(err)
	}
	first := pick(normalPath)
	otherClass := "99999999-9999-4999-8999-999999999999"
	if _, err := f.PrepareExamImport(otherClass, first.ID, examdoc.KindWorkspace); err == nil {
		t.Fatal("prepared for the wrong class")
	}
	forced, err := f.PrepareExamImport(classID, first.ID, examdoc.KindWorkspace)
	if err != nil || forced.Workspace == nil || forced.Workspace.Valid || !strings.Contains(strings.Join(forced.Workspace.Problems, " "), workspaceManifest) {
		t.Fatalf("forced workspace = %+v, %v", forced.Workspace, err)
	}
	if _, err := f.ImportWorkspaceExam(classID, first.ID, "pin"); err == nil {
		t.Fatal("invalid workspace was importable")
	}
	if got := len(f.server.seen("POST", examRoute)); got != 0 {
		t.Fatalf("workspace override made %d traditional requests", got)
	}
	actualWorkspace := pick(filepath.Join("..", "..", "..", "..", "testdata", "workspace", "lab-listas", workspaceManifest))
	wrongKind, err := f.PrepareExamImport(classID, actualWorkspace.ID, examdoc.KindQuestion)
	if err != nil || wrongKind.Traditional == nil || wrongKind.Traditional.Valid || len(wrongKind.Traditional.Problems) == 0 {
		t.Fatalf("forced traditional = %+v, %v", wrongKind.Traditional, err)
	}
	second := pick(manifest)
	if second.Kind != examdoc.KindWorkspace {
		t.Fatal("fixture did not trigger workspace detection")
	}
	if _, err := f.ImportExam(classID, second.ID, "pin", false); err == nil {
		t.Fatal("detection alone made a draft importable")
	}
	preview, err := f.PrepareExamImport(classID, second.ID, examdoc.KindQuestion)
	if err != nil || preview.Traditional == nil || !preview.Traditional.Valid {
		t.Fatalf("traditional Markdown with a workspace filename = %+v, %v", preview.Traditional, err)
	}
	f.DiscardExamImport(classID, first.ID) // a late cancel cannot erase a newer draft
	if _, err := f.ImportExam(classID, first.ID, "pin", false); err == nil {
		t.Fatal("old selection consumed the new preview")
	}
	if _, err := f.ImportWorkspaceExam(classID, second.ID, "pin"); err == nil {
		t.Fatal("traditional preview was importable as workspace")
	}
	if got := len(f.server.seen("POST", examRoute)); got != 1 {
		t.Fatalf("expected only the dry run, got %d requests", got)
	}
	if _, err := f.ImportExam(classID, second.ID, "pin", false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ImportExam(classID, second.ID, "pin", false); err == nil || len(f.server.seen("POST", examRoute)) != 2 {
		t.Fatal("expected exactly one final upload")
	}
}

func TestExamImportWorkspaceDraftDoesNotSurviveReplacingOrReconnecting(t *testing.T) {
	f := newPublishFixture(t, examID)
	manifest := filepath.Join("..", "..", "..", "..", "testdata", "workspace", "lab-listas", workspaceManifest)
	stage := func() ExamImportSource {
		t.Helper()
		f.chosen = manifest
		source, err := f.ChooseExamSource(classID)
		if err != nil || source.Kind != examdoc.KindWorkspace {
			t.Fatalf("selection = %+v, %v", source, err)
		}
		preview, err := f.PrepareExamImport(classID, source.ID, source.Kind)
		if err != nil || preview.Workspace == nil || !preview.Workspace.Valid {
			t.Fatalf("workspace = %+v, %v", preview.Workspace, err)
		}
		return source
	}
	first := stage()
	f.chosen = ""
	if picked, err := f.ChooseExamSource(classID); err != nil || !picked.Cancelled {
		t.Fatalf("cancelled picker = %+v, %v", picked, err)
	}
	if _, err := f.ImportWorkspaceExam(classID, first.ID, "pin"); err == nil {
		t.Fatal("previous workspace survived a new picker")
	}
	second := stage()
	disconnected, err := f.Disconnect()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Connect(disconnected.Server); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ImportWorkspaceExam(classID, second.ID, "pin"); err == nil {
		t.Fatal("previous workspace survived reconnecting")
	}
	if got := len(f.server.seen("POST", "/admin/class/"+classID+"/workspace-exam")); got != 0 {
		t.Fatalf("validation or cancellation made %d uploads", got)
	}
}

func TestExamImportCancelDuringDryRunCannotRestage(t *testing.T) {
	f := newPublishFixture(t, examID)
	entered, release := make(chan struct{}), make(chan struct{})
	f.server.setReply(func(w http.ResponseWriter, r *http.Request, _ []byte) bool {
		if r.Method != "POST" || r.URL.Path != examRoute || r.URL.Query().Get("dry_run") != "true" {
			return false
		}
		close(entered)
		<-release
		checkedExam(w)
		return true
	})
	f.chosen = writeTemp(t, "midterm.md", []byte(examDocument))
	source, err := f.ChooseExamSource(classID)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := f.PrepareExamImport(classID, source.ID, source.Kind); done <- err }()
	<-entered
	f.DiscardExamImport(classID, source.ID)
	close(release)
	if err := <-done; err == nil {
		t.Fatal("late validation reported a current preview")
	}
	if _, err := f.ImportExam(classID, source.ID, "pin", false); err == nil || len(f.server.seen("POST", examRoute)) != 1 {
		t.Fatal("cancelled validation staged an import")
	}
}
