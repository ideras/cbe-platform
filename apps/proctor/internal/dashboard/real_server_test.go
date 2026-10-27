package dashboard

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"mime/multipart"
	"net"
	"net/http"
	"net/textproto"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestBuiltServerManualExamWorkflow(t *testing.T) {
	binary := os.Getenv("EXAM_SERVER_BINARY")
	if binary == "" {
		t.Skip("set EXAM_SERVER_BINARY for built-server integration")
	}
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	dir := filepath.Join(t.TempDir(), "demo")
	cmd := exec.Command("python3", filepath.Join(root, "scripts/manual_exam.py"), "init", "--directory", dir, "--port", strconv.Itoa(port))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("demo initialization: %v %s", err, out)
	}
	var profile struct {
		Server  string `json:"server"`
		ExamID  string `json:"exam_id"`
		ClassID string `json:"class_id"`
	}
	data, err := os.ReadFile(filepath.Join(dir, "profile.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &profile); err != nil {
		t.Fatal(err)
	}
	token, err := ReadCredential(filepath.Join(dir, "admin-token"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out, []byte(token)) {
		t.Fatal("demo printed its credential")
	}
	cmd = exec.Command(binary, "-bind", "127.0.0.1:"+strconv.Itoa(port), "-database", filepath.Join(dir, "exam.db"), "-screenshots", filepath.Join(dir, "screenshots"), "-admin-token-file", filepath.Join(dir, "admin-token"))
	if err = cmd.Start(); err != nil {
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
	httpClient := &http.Client{Timeout: time.Second}
	wait(t, func() bool {
		resp, err := httpClient.Get(profile.Server + "/healthz")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == 200
	})
	ctx := t.Context()
	call := func(auth, path string, body any, result any) {
		t.Helper()
		if err := getJSON(ctx, httpClient, profile.Server, auth, "POST", path, body, result); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
	}
	put := func(auth, path string, body any, result any) {
		t.Helper()
		if err := getJSON(ctx, httpClient, profile.Server, auth, "PUT", path, body, result); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
	}
	// The monitoring settings are stored on the server now, and the exam freezes them when it is published.
	var settings map[string]any
	put(token, "/admin/settings", map[string]any{"heartbeat_interval_ms": 5000, "overdue_threshold_ms": 10000, "focus_threshold_ms": 1000,
		"alert_interval_ms": 2000, "capture_interval_ms": 5000, "lock_away_after_ms": 0, "lock_no_contact_after_ms": 0,
		"incident_captures": 30, "session_captures": 200, "default_grace_ms": 30000}, &settings)
	var published map[string]any
	call(token, "/admin/exam/"+profile.ExamID+"/publish", map[string]string{}, &published) // what the dashboard's Publish does
	var login struct {
		Token string `json:"session_token"`
		ID    string `json:"session_id"`
	}
	call("", "/login", map[string]string{"account_number": "0001", "exam_pin": "demo-only"}, &login)
	var ignored map[string]any
	call(login.Token, "/heartbeat", map[string]string{"hostname": "synthetic-demo-client"}, &ignored)
	call(login.Token, "/event", map[string]any{"event_type": "app_started"}, &ignored)
	var loss struct {
		ID int64 `json:"event_log_id"`
	}
	call(login.Token, "/event", map[string]any{"event_type": "focus_lost"}, &loss)
	time.Sleep(1100 * time.Millisecond) // the focus threshold is one second
	for _, text := range []string{"first synthetic save", "second synthetic save 🧪"} {
		var ack map[string]any
		call(login.Token, "/autosave", map[string]any{"answers": map[string]string{"1b.answer": text}}, &ack)
	}
	var submitted map[string]any
	call(login.Token, "/submit", map[string]any{"answers": map[string]string{"1b.answer": "explicit final"}}, &submitted)
	var multipartBody bytes.Buffer
	writer := multipart.NewWriter(&multipartBody)
	if err = writer.WriteField("event_log_id", strconv.FormatInt(loss.ID, 10)); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreatePart(textproto.MIMEHeader{"Content-Disposition": {`form-data; name="image"; filename="synthetic.png"`}, "Content-Type": {"image/png"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = png.Encode(part, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	writer.Close()
	req, err := http.NewRequestWithContext(ctx, "POST", profile.Server+"/screenshot", &multipartBody)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+login.Token)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	uploaded, err := httpClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var photo Screenshot
	if err = json.NewDecoder(uploaded.Body).Decode(&photo); err != nil {
		t.Fatal(err)
	}
	uploaded.Body.Close()
	if uploaded.StatusCode != 200 || photo.ID <= 0 {
		t.Fatal("pending image upload failed", uploaded.StatusCode)
	}
	opts := DefaultOptions()
	opts.Server = profile.Server
	opts.Token = token
	opts.ExamID = profile.ExamID
	opts.Poll = 20 * time.Millisecond
	s, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer s.ServiceShutdown()
	if _, err = s.Connect(profile.Server); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool { return s.Snapshot().Loaded })
	v := s.Snapshot()
	if len(v.Roster.Sessions) != 1 || len(v.Roster.Missing) != 4 || v.Roster.Sessions[0].Status != "submitted" {
		t.Fatal("roster did not preserve terminal and missing students")
	}
	page, err := s.Timeline(login.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	versions := 0
	foundFinal := false
	for _, item := range page.Items {
		if item.Version != nil {
			versions++
			if item.Version.ID == page.Session.Final.VersionID {
				foundFinal = item.Version.Answers["1b.answer"] == "explicit final"
			}
		}
	}
	if versions != 3 || !foundFinal {
		t.Fatal("all snapshots/designated final not available", versions)
	}
	foundImage := false
	for _, item := range page.Items {
		if item.Event != nil && item.Event.ID == loss.ID {
			for _, meta := range item.Event.Screenshots {
				foundImage = meta.ID == photo.ID && meta.EventID == loss.ID
			}
		}
	}
	if !foundImage {
		t.Fatal("image not linked to its originating focus-loss event")
	}
	for _, thumb := range []bool{true, false} {
		im, err := s.Image(photo.ID, thumb)
		if err != nil || im.Data == "" {
			t.Fatal("protected image read after submission", err)
		}
	}
	var other struct {
		Token string `json:"session_token"`
		ID    string `json:"session_id"`
	}
	call("", "/login", map[string]string{"account_number": "0002", "exam_pin": "demo-only"}, &other)
	var saved map[string]any
	call(other.Token, "/autosave", map[string]any{"answers": map[string]string{"2.code": "retained autosave"}}, &saved)
	s.Refresh()
	wait(t, func() bool { return len(s.Snapshot().Roster.Sessions) == 2 })
	forced, err := s.ForceSubmit(other.ID, "Synthetic manual intervention", true)
	if err != nil || forced.Status != "force_submitted" {
		t.Fatal(forced, err)
	}
	var repeated map[string]any
	call(other.Token, "/submit", map[string]any{"answers": map[string]string{"2.code": "must not replace"}}, &repeated)
	if repeated["final_version_id"] != float64(forced.VersionID) {
		t.Fatal("later student submit changed final")
	}
	s.Refresh()
	wait(t, func() bool {
		v := s.Snapshot()
		for _, r := range v.Roster.Sessions {
			if r.ID == other.ID {
				return r.Status == "force_submitted" && r.Final != nil
			}
		}
		return false
	})
	page, err = s.Timeline(other.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range page.Items {
		if item.Version != nil && item.Version.IsFinal && item.Version.Answers["2.code"] != "retained autosave" {
			t.Fatal("force-submit lost committed work")
		}
	}
	purged, err := s.PurgeScreenshots(profile.ExamID, true)
	if err != nil || !purged.Complete || purged.Deleted != 1 {
		t.Fatal("real purge", purged, err)
	}
	if _, err = s.Image(photo.ID, true); err == nil {
		t.Fatal("purged image remained readable")
	}
	page, err = s.Timeline(login.ID, "")
	if err != nil || page.Session.Final == nil {
		t.Fatal("purge lost final history", err)
	}
	afterVersions := 0
	for _, item := range page.Items {
		if item.Version != nil {
			afterVersions++
		}
		if item.Event != nil && len(item.Event.Screenshots) > 0 {
			t.Fatal("purged image remained in timeline")
		}
	}
	if afterVersions != versions {
		t.Fatal("purge changed answer history")
	}
	data, _ = json.Marshal(s.Snapshot())
	if bytes.Contains(data, []byte(token)) || bytes.Contains(data, []byte(login.Token)) || bytes.Contains(data, []byte(other.Token)) {
		t.Fatal("credential in public view")
	}
	s.ServiceShutdown()
	resp, err := httpClient.Get(profile.Server + "/healthz")
	if err != nil {
		t.Fatal("dashboard closure stopped server")
	}
	resp.Body.Close()
}
