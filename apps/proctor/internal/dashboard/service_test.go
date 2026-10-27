package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const examID = "11111111-1111-4111-8111-111111111111"
const sessionID = "22222222-2222-4222-8222-222222222222"
const otherExam = "33333333-3333-4333-8333-333333333333"
const unusedExam = "66666666-6666-4666-8666-666666666666"
const classID = "44444444-4444-4444-8444-444444444444"
const otherClass = "55555555-5555-4555-8555-555555555555"
const secret = "private-proctor-test-credential-not-for-assets"

var at = time.Date(2035, 1, 1, 0, 0, 0, 0, time.UTC)

func row() Session {
	return Session{ID: sessionID, ExamID: examID, Student: Student{"0012", "Ada <untrusted>"}, RosterActive: true, Status: "active", PersistedStatus: "active", Remaining: 89, Presence: Presence{Monitoring: true, Connectivity: "healthy", Hostname: "desk-a", Focus: Focus{State: "unknown"}}}
}
func roster() Roster {
	return Roster{ExamID: examID, ServerTime: at, Sessions: []Session{row()}, Missing: []Student{{"0003", "No session"}}}
}
func catalog() Catalog {
	return Catalog{ServerTime: at, CurrentExamID: examID, CurrentClassID: classID, CurrentClassName: "Mathematics 10A", Exams: []ExamEntry{
		{ID: examID, ClassID: classID, ClassName: "Mathematics 10A", Title: "Exam one", ExerciseCount: 3, Navigation: "free", DurationSeconds: 600, GraceSeconds: 30, MinVolumePercent: 80, CreatedAt: at, Published: true, ActiveSessions: 1, TotalSessions: 1},
		{ID: otherExam, ClassID: classID, ClassName: "Mathematics 10A", Title: "Exam two", ExerciseCount: 1, Navigation: "forward_only", DurationSeconds: 60, GraceSeconds: 30, MinVolumePercent: 80, CreatedAt: at, TotalSessions: 4},
		{ID: unusedExam, ClassID: otherClass, ClassName: "History 11B", Title: "Never given", ExerciseCount: 2, Navigation: "free", DurationSeconds: 60, GraceSeconds: 30, MinVolumePercent: 80, CreatedAt: at},
	}}
}

// classList is what the fake servers report: two classes, the first with two exams, one of them published, and the second
// with one exam nobody has taken.
func classList() ClassList {
	return ClassList{Classes: []ClassEntry{
		{ID: otherClass, Name: "History 11B", StudentCount: 24, ExamCount: 1, CreatedAt: at},
		{ID: classID, Name: "Mathematics 10A", StudentCount: 5, ExamCount: 2, CreatedAt: at, PublishedExam: &ClassExam{ExamID: examID, Title: "Exam one"}},
	}}
}

// classRoster is the five-student roster the fake servers give for the first class.
func classRoster(id string) ClassRoster {
	roster := ClassRoster{ID: id, Name: "Mathematics 10A", Total: 5, Students: []Student{}}
	for i, name := range []string{"Ada Demo", "Bruno Demo", "Carla Demo", "Diego Demo", "Elena Demo"} {
		roster.Students = append(roster.Students, Student{Account: fmt.Sprintf("%04d", i+1), Name: name})
	}
	return roster
}

// serveAdminState answers the exam catalog and student count so a test only has to describe the roster it cares about.
func serveAdminState(w http.ResponseWriter, r *http.Request) bool {
	switch {
	case r.Method == "GET" && r.URL.Path == "/admin/exams":
		reply(w, catalog())
		return true
	case r.Method == "GET" && r.URL.Path == "/admin/classes":
		reply(w, classList())
		return true
	case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/admin/class/") && strings.HasSuffix(r.URL.Path, "/students"):
		reply(w, classRoster(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/admin/class/"), "/students")))
		return true
	}
	return false
}
func reply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
func wait(t *testing.T, fn func() bool) {
	t.Helper()
	until := time.Now().Add(5 * time.Second)
	for time.Now().Before(until) {
		if fn() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition did not become true")
}
func httptestServer(t *testing.T, fake *adminServer) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+secret {
			http.Error(w, "raw credential rejected "+secret, 401)
			return
		}
		fake.handler(w, r)
	}))
	t.Cleanup(server.Close)
	return server
}
func start(t *testing.T, h http.HandlerFunc) (*Service, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+secret {
			http.Error(w, "raw credential rejected "+secret, 401)
			return
		}
		if !serveAdminState(w, r) {
			h(w, r)
		}
	}))
	t.Cleanup(server.Close)
	opts := DefaultOptions()
	opts.Server = server.URL
	opts.Token = secret
	opts.ExamID = examID
	opts.Poll = 15 * time.Millisecond
	opts.Timeout = time.Second
	s, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.ServiceShutdown)
	if _, err = s.Connect(server.URL); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool { return s.Snapshot().Loaded })
	return s, server
}
func TestPollingKeepsStaleRosterAndRedactsErrors(t *testing.T) {
	var fail atomic.Bool
	var active, maxActive atomic.Int32
	s, _ := start(t, func(w http.ResponseWriter, r *http.Request) {
		n := active.Add(1)
		defer active.Add(-1)
		if n > maxActive.Load() {
			maxActive.Store(n)
		}
		select {
		case <-time.After(25 * time.Millisecond):
		case <-r.Context().Done():
			return
		}
		if fail.Load() {
			http.Error(w, secret, 500)
			return
		}
		reply(w, roster())
	})
	first := s.Snapshot()
	if first.Stale || first.LastSuccess == nil || len(first.Roster.Missing) != 1 {
		t.Fatal(first)
	}
	fail.Store(true)
	s.Refresh()
	wait(t, func() bool { return s.Snapshot().Stale })
	v := s.Snapshot()
	if len(v.Roster.Sessions) != 1 || v.Roster.Sessions[0].Remaining != 89 || !v.LastSuccess.Equal(*first.LastSuccess) || strings.Contains(v.Error, secret) {
		t.Fatal("stale observations lost or sensitive error exposed")
	}
	if maxActive.Load() != 1 {
		t.Fatal("overlapping polls")
	}
	data, _ := json.Marshal(v)
	if bytes.Contains(data, []byte(secret)) {
		t.Fatal("credential leaked into view")
	}
	fail.Store(false)
	s.Refresh()
	wait(t, func() bool { return !s.Snapshot().Stale })
}
func TestForceRequiresConfirmationSerializesAndHonorsOriginalFinal(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var writes atomic.Int32
	s, _ := start(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			reply(w, roster())
			return
		}
		writes.Add(1)
		var body map[string]string
		if json.NewDecoder(r.Body).Decode(&body) != nil || body["reason"] != "Reason" {
			t.Error("invalid body")
		}
		entered <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		reply(w, map[string]any{"status": "submitted", "submitted_at": at, "final_version_id": 12, "version_seq": 3})
	})
	if _, err := s.ForceSubmit(sessionID, "Reason", false); err == nil || writes.Load() != 0 {
		t.Fatal("cancelled request was sent")
	}
	done := make(chan error, 1)
	go func() {
		v, err := s.ForceSubmit(sessionID, " Reason ", true)
		if err == nil && v.Status != "submitted" {
			err = context.Canceled
		}
		done <- err
	}()
	<-entered
	if _, err := s.ForceSubmit(sessionID, "Reason", true); err == nil {
		t.Fatal("duplicate concurrent action accepted")
	}
	if _, err := s.SelectExam(otherExam); err == nil {
		t.Fatal("selection changed during action")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if writes.Load() != 1 {
		t.Fatal(writes.Load())
	}
	if _, err := s.ForceSubmit(otherExam, "Reason", true); err == nil || writes.Load() != 1 {
		t.Fatal("wrong exam/session was sent")
	}
}
func TestTimelineImageAndPurge(t *testing.T) {
	var raw bytes.Buffer
	png.Encode(&raw, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	var images, purges atomic.Int32
	next := "opaque +/cursor="
	s, _ := start(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/admin/sessions":
			reply(w, roster())
		case strings.HasSuffix(r.URL.Path, "/timeline"):
			p := Page{ServerTime: at, Session: row(), Items: []Item{}}
			if r.URL.Query().Get("cursor") == "" {
				p.Next = &next
				p.Items = []Item{{ID: "event:31", Kind: "event", At: at, Event: &Event{ID: 31, Type: "focus_lost", Detail: map[string]any{"client": "untrusted"}, Screenshots: []Screenshot{{ID: 7, EventID: 31}}}}}
			} else {
				if r.URL.Query().Get("cursor") != next {
					t.Error("cursor changed")
				}
				p.Items = []Item{{ID: "answer_version:12", Kind: "answer_version", At: at, Version: &Version{ID: 12, Sequence: 3, Answers: map[string]string{"q1": "<script>text</script>"}}}}
			}
			reply(w, p)
		case strings.HasPrefix(r.URL.Path, "/admin/screenshot/"):
			images.Add(1)
			w.Header().Set("Content-Type", "image/png")
			w.Write(raw.Bytes())
		case strings.HasPrefix(r.URL.Path, "/admin/purge-screenshots/"):
			purges.Add(1)
			reply(w, Purge{ExamID: strings.TrimPrefix(r.URL.Path, "/admin/purge-screenshots/"), Deleted: 1, Failed: []int64{}, Complete: true})
		default:
			http.NotFound(w, r)
		}
	})
	p, err := s.Timeline(sessionID, "")
	if err != nil || p.Next == nil || p.Items[0].Event.Screenshots[0].EventID != 31 {
		t.Fatal(p, err)
	}
	p, err = s.Timeline(sessionID, *p.Next)
	if err != nil || p.Items[0].Version.Answers["q1"] != "<script>text</script>" || p.Next != nil {
		t.Fatal(p, err)
	}
	for _, thumb := range []bool{true, false} {
		im, err := s.Image(7, thumb)
		if err != nil || !strings.HasPrefix(im.Data, "data:image/png;base64,") {
			t.Fatal(err)
		}
	}
	if images.Load() != 2 {
		t.Fatal(images.Load())
	}
	if _, err = s.PurgeScreenshots(examID, false); err == nil || purges.Load() != 0 {
		t.Fatal("unconfirmed purge")
	}
	if _, err := s.PurgeScreenshots("99999999-9999-4999-8999-999999999999", true); err == nil || purges.Load() != 0 {
		t.Fatal("an exam that is not on the server's list was purged")
	}
	purg, err := s.PurgeScreenshots(examID, true)
	if err != nil || !purg.Complete || purges.Load() != 1 {
		t.Fatal(err)
	}
	// Any listed exam can be purged from the exam list, not only the monitored one.
	if purg, err := s.PurgeScreenshots(otherExam, true); err != nil || purg.ExamID != otherExam || purges.Load() != 2 {
		t.Fatalf("purge of another exam = %+v, %v", purg, err)
	}
}
func TestChangedExamCancelsOldPollAndHistory(t *testing.T) {
	var blocked atomic.Bool
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	defer close(release)
	s, _ := start(t, func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("exam_id")
		if strings.HasSuffix(r.URL.Path, "/timeline") {
			entered <- struct{}{}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			reply(w, Page{ServerTime: at, Session: row(), Items: []Item{}})
			return
		}
		if blocked.Load() && id == examID {
			entered <- struct{}{}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		rr := roster()
		rr.ExamID = id
		rr.Sessions[0].ExamID = id
		reply(w, rr)
	})
	done := make(chan error, 1)
	go func() { _, err := s.Timeline(sessionID, ""); done <- err }()
	<-entered
	blocked.Store(true)
	s.Refresh()
	<-entered
	if _, err := s.SelectExam(otherExam); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Fatal("old history was accepted")
	}
	wait(t, func() bool { v := s.Snapshot(); return v.Loaded && v.Roster.ExamID == otherExam })
	if s.Snapshot().Stale {
		t.Fatal("old poll overwrote new selection")
	}
}
func TestRedirectAndMalformedResponsesDoNotExposeCredential(t *testing.T) {
	var forwarded atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Add(1) }))
	defer target.Close()
	var bad atomic.Bool
	s, _ := start(t, func(w http.ResponseWriter, r *http.Request) {
		if bad.Load() {
			http.Redirect(w, r, target.URL, http.StatusFound)
			return
		}
		reply(w, roster())
	})
	bad.Store(true)
	s.Refresh()
	wait(t, func() bool { return s.Snapshot().Stale })
	if forwarded.Load() != 0 {
		t.Fatal("credential forwarded on redirect")
	}
}
func TestInvalidTimelineImageAndAcknowledgments(t *testing.T) {
	var mode atomic.Int32
	s, _ := start(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/admin/sessions":
			if mode.Load() == 4 {
				reply(w, map[string]any{})
				return
			}
			reply(w, roster())
		case strings.HasSuffix(r.URL.Path, "/timeline"):
			p := Page{ServerTime: at, Session: row(), Items: []Item{{ID: "event:1", Kind: "event", At: at, Event: &Event{ID: 1, Screenshots: []Screenshot{{ID: 7, EventID: 99}}}}}}
			reply(w, p)
		case strings.HasPrefix(r.URL.Path, "/admin/screenshot/"):
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("not png"))
		default:
			reply(w, map[string]any{})
		}
	})
	if _, err := s.Timeline(sessionID, ""); err == nil {
		t.Fatal("mismatched image accepted")
	}
	if _, err := s.Image(7, true); err == nil {
		t.Fatal("invalid image accepted")
	}
	if _, err := s.ForceSubmit(sessionID, "reason", true); err == nil {
		t.Fatal("empty final ack accepted")
	}
	if _, err := s.PurgeScreenshots(examID, true); err == nil {
		t.Fatal("empty purge ack accepted")
	}
	mode.Store(4)
	s.Refresh()
	wait(t, func() bool { return s.Snapshot().Stale && strings.Contains(s.Snapshot().Error, "Invalid roster") })
	if len(s.Snapshot().Roster.Sessions) != 1 {
		t.Fatal("invalid response replaced observations")
	}
}
func TestCredentialFileAndShutdown(t *testing.T) {
	file := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(file, []byte(secret+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadCredential(file); err != nil || got != secret {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		os.Chmod(file, 0644)
		if _, err := ReadCredential(file); err == nil {
			t.Fatal("public credential file accepted")
		}
		os.Chmod(file, 0600)
		link := file + "-link"
		if err := os.Symlink(file, link); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadCredential(link); err == nil {
			t.Fatal("credential symlink accepted")
		}
	}
	s, server := start(t, func(w http.ResponseWriter, r *http.Request) { reply(w, roster()) })
	s.ServiceShutdown()
	if _, err := s.Connect(server.URL); err == nil {
		t.Fatal("reopened after close")
	}
	client := &http.Client{Timeout: time.Second}
	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatal("closing proctor stopped server", err)
	}
	resp.Body.Close()
}
