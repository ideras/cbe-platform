package dashboard

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An exam's pictures are read from the folder of the exam file when it is chosen and travel inside the compiled exam. The
// server keeps the Markdown as written, with the reference to the file and not its bytes, and nothing depends on the folder
// once the exam is previewed.
func TestImportedExamEmbedsItsPicturesAndKeepsTheReferenceInTheSource(t *testing.T) {
	f := newPublishFixture(t, examID)
	f.server.reply = func(w http.ResponseWriter, r *http.Request, body []byte) bool {
		switch {
		case r.Method == "POST" && r.URL.Path == examRoute && r.URL.Query().Get("dry_run") == "true":
			checkedExam(w)
		case r.Method == "POST" && r.URL.Path == examRoute:
			w.WriteHeader(http.StatusCreated)
			reply(w, map[string]string{"exam_id": otherExam})
		default:
			return false
		}
		return true
	}
	var figure bytes.Buffer
	if err := png.Encode(&figure, image.NewGray(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "img"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "img", "grey.png"), figure.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	const reference = "![A grey square](img/grey.png)"
	source := strings.Replace(examDocument, "Answer: {{x}}", reference+"\n\nAnswer: {{x}}", 1)
	f.chosen = filepath.Join(dir, "midterm.md")
	if err := os.WriteFile(f.chosen, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}

	preview, err := f.ChooseExam(classID)
	if err != nil || !preview.Valid || len(preview.Problems) != 0 {
		t.Fatalf("preview = %+v, %v", preview, err)
	}
	embedded := `<img src="data:image/png;base64,` + base64.StdEncoding.EncodeToString(figure.Bytes()) + `" alt="A grey square">`
	if body := preview.Content.Exercises[0].Body; !strings.Contains(body, embedded) {
		t.Fatalf("the previewed exercise lacks the embedded picture:\n%.300s", body)
	}

	// The folder goes away between the preview and the import.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ImportExam(classID, "announced-pin", false); err != nil {
		t.Fatal(err)
	}
	stored := sentExam(t, f.server.seen("POST", examRoute)[1].Body)
	if got, _ := stored["source_markdown"].(string); !strings.Contains(got, reference) || strings.Contains(got, "base64") {
		t.Errorf("source_markdown must keep the reference and not the bytes:\n%.400s", got)
	}
	content, _ := stored["content"].(map[string]any)
	exercises, _ := content["exercises"].([]any)
	if body, _ := exercises[0].(map[string]any)["body"].(string); !strings.Contains(body, embedded) {
		t.Errorf("the imported exercise lacks the embedded picture:\n%.300s", body)
	}
}

// A picture that cannot be found is a problem shown in the preview, before anything is sent.
func TestExamWithAMissingPictureShowsTheProblem(t *testing.T) {
	f := newPublishFixture(t, examID)
	f.chosen = writeTemp(t, "midterm.md", []byte(strings.Replace(examDocument, "Answer: {{x}}", "![Lost](lost.png)\n\nAnswer: {{x}}", 1)))
	preview, err := f.ChooseExam(classID)
	if err != nil || preview.Valid || len(preview.Problems) != 1 || !strings.Contains(preview.Problems[0], `image "lost.png": the file does not exist next to the exam`) {
		t.Fatalf("preview = %+v, %v", preview, err)
	}
	if len(f.server.seen("POST", examRoute)) != 0 {
		t.Fatal("something was sent to the server")
	}
}
