package dashboard

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestBuiltServerSettingsAndUnlockFromTheDashboard drives the monitoring settings and the lock through the dashboard and
// the real server binary: the settings are read and saved, a save another proctor got ahead of is refused in words the
// Settings page recognizes, a student's exam locks, and the proctor unlocks it with the time back.
func TestBuiltServerSettingsAndUnlockFromTheDashboard(t *testing.T) {
	binary := os.Getenv("EXAM_SERVER_BINARY")
	if binary == "" {
		t.Skip("set EXAM_SERVER_BINARY for built-server integration")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "admin-token"), []byte(secret+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := restrictTokenACL(filepath.Join(dir, "admin-token")); err != nil {
		t.Fatal(err)
	}
	server := "http://127.0.0.1:" + strconv.Itoa(port)
	cmd := exec.Command(binary, "-bind", "127.0.0.1:"+strconv.Itoa(port), "-database", filepath.Join(dir, "exam.db"), "-screenshots", filepath.Join(dir, "screenshots"), "-admin-token-file", filepath.Join(dir, "admin-token"))
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cmd.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() { cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			cmd.Process.Kill()
			<-done
		}
	})
	client := &http.Client{Timeout: time.Second}
	wait(t, func() bool {
		resp, err := client.Get(server + "/healthz")
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == 200
	})

	var chosen string
	opts := DefaultOptions()
	opts.Server, opts.Token, opts.Poll = server, secret, 20*time.Millisecond
	opts.Choose = func(FileDialog) (string, error) { return chosen, nil }
	s, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.ServiceShutdown)
	if _, err := s.Connect(server); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool { v := s.Snapshot(); return v.CatalogLoaded && v.ClassesLoaded && !v.Stale })

	// The settings: the built-in defaults, then a save, then a save another proctor got ahead of.
	defaults, err := s.Settings()
	if err != nil || defaults.CaptureInterval != 5000 || defaults.LockAwayAfter != 300000 || defaults.IncidentCaptures != 60 || defaults.UpdatedAt != "" {
		t.Fatalf("default settings = %+v, %v", defaults, err)
	}
	values := defaults.SettingsValues
	values.LockAwayAfter, values.DefaultGrace = 120000, 45000
	saved, err := s.SaveSettings(values, defaults.UpdatedAt)
	if err != nil || saved.LockAwayAfter != 120000 || saved.IncidentCaptures != 24 || saved.UpdatedAt == "" {
		t.Fatalf("saved settings = %+v, %v (the per-absence count is worked out: (120 - 5) / 5 + 1)", saved, err)
	}
	if _, err := s.SaveSettings(values, defaults.UpdatedAt); err == nil || !strings.HasPrefix(err.Error(), "Another proctor saved the settings first.") {
		t.Fatalf("a stale save = %v", err)
	}
	if again, err := s.Settings(); err != nil || again.UpdatedAt != saved.UpdatedAt || again.LockAwayAfter != 120000 {
		t.Fatalf("settings read back = %+v, %v", again, err)
	}

	// A class and an exam without a grace of its own: it takes the default grace of the settings.
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	chosen = write("Class.csv", "canvas_id,account_number,name,email,sis_user_id\n1,0001,Ada,ada@x.invalid,S1\n")
	if _, err := s.ChooseClassFile(); err != nil {
		t.Fatal(err)
	}
	class, err := s.CreateClass("Class")
	if err != nil {
		t.Fatal(err)
	}
	chosen = write("Quiz.md", "---\ntitle: Quiz\nduration: 10m\n---\n## One\n{{q1}}\n")
	if preview, err := s.ChooseExam(class.ID); err != nil || !preview.Valid || preview.GraceSeconds != 45 {
		t.Fatalf("exam preview = %+v, %v", preview, err)
	}
	imported, err := s.ImportExam(class.ID, "quiz-pin", false)
	if err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool { v := s.Snapshot(); return len(v.Catalog.Exams) == 1 && !v.Busy })
	if _, err := s.Publish(imported.ExamID, "", false, true); err != nil {
		t.Fatal(err)
	}

	// A student logs in and the application locks the exam.
	body, _ := json.Marshal(map[string]string{"account_number": "0001", "exam_pin": "quiz-pin"})
	resp, err := http.Post(server+"/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var login struct {
		Token  string `json:"session_token"`
		Policy struct {
			LockAway int64 `json:"lock_away_after_ms"`
		} `json:"telemetry_policy"`
	}
	json.NewDecoder(resp.Body).Decode(&login)
	resp.Body.Close()
	if login.Token == "" || login.Policy.LockAway != 120000 {
		t.Fatalf("login = %+v: the exam must follow the settings it was published with", login)
	}
	request, _ := http.NewRequest(http.MethodPost, server+"/event", strings.NewReader(`{"event_type":"away_lock","detail":{"reason":"away"}}`))
	request.Header.Set("Authorization", "Bearer "+login.Token)
	request.Header.Set("Content-Type", "application/json")
	if resp, err := client.Do(request); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("away_lock = %v, %v", resp, err)
	} else {
		resp.Body.Close()
	}

	// The proctor sees the lock, in the roster and in the exam list, and unlocks with the time back.
	wait(t, func() bool {
		v := s.Snapshot()
		return len(v.Catalog.Exams) == 1 && v.Catalog.Exams[0].TotalSessions == 1
	})
	if _, err := s.SelectExam(imported.ExamID); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool {
		v := s.Snapshot()
		return v.Loaded && len(v.Roster.Sessions) == 1 && v.Roster.Sessions[0].Locked != nil && len(v.Catalog.Exams) == 1 && v.Catalog.Exams[0].LockedSessions == 1
	})
	row := s.Snapshot().Roster.Sessions[0]
	if row.Locked.Reason != "away" || s.Snapshot().Roster.Policy.LockAway != 120000 {
		t.Fatalf("locked row = %+v", row)
	}
	if _, err := s.Unlock(row.ID, "", true, true); err == nil {
		t.Fatal("an unlock without a reason was sent")
	}
	time.Sleep(1100 * time.Millisecond)
	result, err := s.Unlock(row.ID, "the student came back", true, true)
	if err != nil || result.SecondsAdded < 1 || !result.DeadlineAt.After(row.DeadlineAt) {
		t.Fatalf("unlock = %+v, %v", result, err)
	}
	wait(t, func() bool {
		v := s.Snapshot()
		return !v.Busy && len(v.Roster.Sessions) == 1 && v.Roster.Sessions[0].Locked == nil && len(v.Roster.Sessions[0].Locks) == 2 && v.Catalog.Exams[0].LockedSessions == 0
	})
	if _, err := s.Unlock(row.ID, "again", false, true); err == nil {
		t.Fatal("an exam that is not locked was unlocked")
	}
}
