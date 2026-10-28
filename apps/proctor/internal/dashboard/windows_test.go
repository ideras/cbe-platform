package dashboard

import (
	"bytes"
	"image"
	"image/jpeg"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWindowsAreNamedPerPurposeAndCarryNoCredential(t *testing.T) {
	s, _ := start(t, func(w http.ResponseWriter, r *http.Request) { reply(w, roster()) })
	var opened []WindowRequest
	s.opts.OpenWindow = func(request WindowRequest) error { opened = append(opened, request); return nil }
	wait(t, func() bool { return len(s.Snapshot().Roster.Sessions) == 1 && s.Snapshot().CatalogLoaded })

	if err := s.OpenMonitoring(); err != nil {
		t.Fatal(err)
	}
	if err := s.OpenReview(sessionID); err != nil {
		t.Fatal(err)
	}
	if err := s.OpenImage([]int64{7, 8, 9}, nil, 1); err != nil {
		t.Fatal(err)
	}
	if len(opened) != 3 {
		t.Fatalf("opened = %+v", opened)
	}
	if monitoring := opened[0]; monitoring.Name != "monitoring" || monitoring.URL != "/#/monitoring" || monitoring.Title != "Monitoring — Exam one (Mathematics 10A)" {
		t.Fatalf("monitoring window = %+v", monitoring)
	}
	if review := opened[1]; review.Name != "review-"+sessionID || review.URL != "/#/review/"+sessionID || review.Title != "Review — Ada <untrusted> (0012)" {
		t.Fatalf("review window = %+v", review)
	}
	pictures := opened[2]
	query, err := url.ParseQuery(strings.TrimPrefix(pictures.URL, "/#/image?"))
	if pictures.Name != "image-7" || pictures.Title != "Screenshot #8" || err != nil || query.Get("set") != "7,8,9" || query.Get("at") != "1" {
		t.Fatalf("image window = %+v", pictures)
	}
	for _, request := range opened {
		if strings.Contains(request.URL+request.Title, secret) {
			t.Fatal("a window request carries the credential")
		}
	}

	for _, bad := range []func() error{
		func() error { return s.OpenReview("not-a-session") },
		func() error { return s.OpenImage(nil, nil, 0) },
		func() error { return s.OpenImage([]int64{7}, nil, 1) },
		func() error { return s.OpenImage([]int64{7, -1}, nil, 0) },
		func() error { return s.OpenImage(make([]int64, maxIncidentImages+1), nil, 0) },
	} {
		if err := bad(); err == nil {
			t.Fatal("an invalid window request was accepted")
		}
	}
	if len(opened) != 3 {
		t.Fatal("an invalid request opened a window")
	}
	s.opts.OpenWindow = nil
	if err := s.OpenMonitoring(); err == nil {
		t.Fatal("a build without windows offered one")
	}
}

func TestSaveImageWritesTheOriginalBytesPrivately(t *testing.T) {
	var original bytes.Buffer
	if err := jpeg.Encode(&original, image.NewRGBA(image.Rect(0, 0, 3, 2)), nil); err != nil {
		t.Fatal(err)
	}
	s, _ := start(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/admin/screenshot/7" {
			w.Header().Set("Content-Type", "image/jpeg")
			w.Write(original.Bytes())
			return
		}
		reply(w, roster())
	})
	target := filepath.Join(t.TempDir(), "evidence.jpg")
	var asked SaveDialog
	s.opts.Save = func(dialog SaveDialog) (string, error) { asked = dialog; return target, nil }
	path, err := s.SaveImage(7)
	if err != nil || path != target {
		t.Fatalf("save = %q, %v", path, err)
	}
	if asked.Filename != "screenshot-7.jpg" || asked.Pattern != "*.jpg" {
		t.Fatalf("dialog = %+v", asked)
	}
	written, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(written, original.Bytes()) {
		t.Fatal("the saved file is not the server's image")
	}
	if info, _ := os.Stat(target); runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("saved with mode %v", info.Mode().Perm())
	}
	// The image window shows the same picture as a data URL.
	if shown, err := s.Image(7, false); err != nil || !strings.HasPrefix(shown.Data, "data:image/jpeg;base64,") {
		t.Fatalf("image = %v", err)
	}

	s.opts.Save = func(SaveDialog) (string, error) { return "", nil }
	if path, err := s.SaveImage(7); err != nil || path != "" {
		t.Fatalf("cancelled save = %q, %v", path, err)
	}
	if _, err := s.SaveImage(8); err == nil {
		t.Fatal("a missing image was saved")
	}
}

func TestReviewWindowsKeepWorkingWhenAnotherExamIsMonitored(t *testing.T) {
	s, _ := start(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/timeline"):
			reply(w, Page{ServerTime: at, Session: row(), Items: []Item{}})
		default:
			reply(w, roster())
		}
	})
	if _, err := s.SelectExam(otherExam); err != nil {
		t.Fatal(err)
	}
	if page, err := s.Timeline(sessionID, ""); err != nil || page.Session.ExamID != examID {
		t.Fatalf("history of a session of another exam = %+v, %v", page, err)
	}
}
