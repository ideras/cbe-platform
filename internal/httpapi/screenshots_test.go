package httpapi

import (
	"bytes"
	"encoding/json"
	"exam-platform/internal/domain"
	"exam-platform/internal/service"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func apiPNG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 8, 6))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func imageRouter(t *testing.T, f *apiFixture) http.Handler {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	shots, err := service.NewScreenshots(f.store, f.sessions, root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { shots.Close() })
	return New(f.clock, f.sessions, f.logger, Options{AdminToken: adminCredential, Proctor: f.sessions, Screenshots: shots})
}

const adminCredential = "private-test-admin-not-for-student"

func uploadImage(t *testing.T, h http.Handler, token string, event int64, data []byte, extra map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var b bytes.Buffer
	writer := multipart.NewWriter(&b)
	writer.WriteField("event_log_id", strconv.FormatInt(event, 10))
	writer.WriteField("client_captured_at", "2099-01-01T00:00:00Z")
	for k, v := range extra {
		writer.WriteField(k, v)
	}
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", `form-data; name="image"; filename="../../escape.png"`)
	header.Set("Content-Type", "image/png")
	if bytes.HasPrefix(data, []byte{0xff, 0xd8}) {
		header.Set("Content-Type", "image/jpeg")
	}
	part, err := writer.CreatePart(header)
	if err != nil {
		t.Fatal(err)
	}
	part.Write(data)
	writer.Close()
	r := httptest.NewRequest("POST", "/screenshot", &b)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestScreenshotHTTPAuthenticationValidationServingAndRetention(t *testing.T) {
	f := newAPIFixture(t)
	h := imageRouter(t, f)
	a, b := f.login(t, "0012"), f.login(t, "0034")
	at := f.clock.Now()
	f.sessions.Heartbeat(t.Context(), a.SessionToken, "pc", "192.0.2.1", domain.Sound{})
	loss, err := f.sessions.ClientEvent(t.Context(), a.SessionToken, service.EventInput{Type: "focus_lost"})
	if err != nil {
		t.Fatal(err)
	}
	data := apiPNG(t)
	if w := uploadImage(t, h, "", loss.EventLogID, data, nil); w.Code != 401 {
		t.Fatal("unauthenticated upload")
	}
	early := uploadImage(t, h, a.SessionToken, loss.EventLogID, data, nil)
	if early.Code != 425 || early.Header().Get("Retry-After") == "" {
		t.Fatalf("missing server threshold scheduling: %d", early.Code)
	}
	f.clock.Set(at.Add(21 * time.Second))
	for _, test := range []struct {
		token string
		event int64
		data  []byte
		extra map[string]string
		code  int
	}{{b.SessionToken, loss.EventLogID, data, nil, 404}, {a.SessionToken, loss.EventLogID + 999, data, nil, 404}, {a.SessionToken, loss.EventLogID, []byte("invalid PNG"), nil, 400}, {a.SessionToken, loss.EventLogID, data, map[string]string{"file_path": "../../evil"}, 400}, {a.SessionToken, loss.EventLogID, make([]byte, service.MaxScreenshotBytes+1), nil, 413}} {
		w := uploadImage(t, h, test.token, test.event, test.data, test.extra)
		if w.Code != test.code {
			t.Fatalf("upload status %d want %d: %s", w.Code, test.code, w.Body.String())
		}
	}
	w := uploadImage(t, h, a.SessionToken, loss.EventLogID, data, nil)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var shot service.ScreenshotResult
	if err := json.Unmarshal(w.Body.Bytes(), &shot); err != nil {
		t.Fatal(err)
	}
	if shot.ID <= 0 || shot.Width != 8 || shot.CapturedAt.Year() == 2099 || shot.ClientCapturedAt == nil {
		t.Fatal("untrusted capture time became receipt authority")
	}
	if strings.Contains(w.Body.String(), "file_path") || strings.Contains(w.Body.String(), "escape.png") {
		t.Fatal("image path leaked")
	}
	// Later captures of the same focus loss are numbered; the student application sends them as JPEG.
	var photo bytes.Buffer
	if err := jpeg.Encode(&photo, image.NewRGBA(image.Rect(0, 0, 8, 6)), nil); err != nil {
		t.Fatal(err)
	}
	second := uploadImage(t, h, a.SessionToken, loss.EventLogID, photo.Bytes(), map[string]string{"capture_index": "1"})
	if second.Code != 200 || !strings.Contains(second.Body.String(), `"capture_index":1`) || !strings.Contains(second.Body.String(), `"content_type":"image/jpeg"`) {
		t.Fatalf("second capture = %d %s", second.Code, second.Body.String())
	}
	for _, index := range []string{"-1", "x", ""} {
		if w := uploadImage(t, h, a.SessionToken, loss.EventLogID, data, map[string]string{"capture_index": index}); w.Code != 400 {
			t.Fatalf("capture index %q = %d", index, w.Code)
		}
	}
	repeated := uploadImage(t, h, a.SessionToken, loss.EventLogID, data, nil)
	if repeated.Code != 200 || !strings.Contains(repeated.Body.String(), `"duplicate":true`) {
		t.Fatal("idempotent upload failed")
	}
	for _, path := range []string{fmt.Sprintf("/admin/screenshot/%d", shot.ID), fmt.Sprintf("/admin/screenshot/%d/thumbnail", shot.ID)} {
		for _, credential := range []string{"", "Bearer " + a.SessionToken} {
			if r := request(h, "GET", path, "", credential); r.Code != 401 {
				t.Fatal("unauthorized image access")
			}
		}
		full := request(h, "GET", path, "", "Bearer "+adminCredential)
		if full.Code != 200 || full.Header().Get("Content-Type") != "image/jpeg" || full.Header().Get("Cache-Control") != "no-store" || full.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatal("protected image serving failed")
		}
		if _, err := jpeg.DecodeConfig(full.Body); err != nil {
			t.Fatal(err)
		}
	}
	if w := request(h, "GET", "/screenshots/"+a.SessionID+"/1.png", "", ""); w.Code != 404 {
		t.Fatal("public screenshot route")
	}
	timeline := request(h, "GET", "/admin/session/"+a.SessionID+"/timeline", "", "Bearer "+adminCredential)
	if timeline.Code != 200 || !strings.Contains(timeline.Body.String(), `"screenshot_id"`) || strings.Contains(timeline.Body.String(), "file_path") {
		t.Fatal("contextual protected metadata missing")
	}
	for _, path := range []string{"/admin/screenshot/1", "/admin/screenshot/../sessions", "/admin/purge-screenshots/" + apiExamID} {
		if w := request(h, "POST", path, `{"confirm":true}`, ""); w.Code != 401 {
			t.Fatal("admin method/path authorization bypass")
		}
	}
	if w := request(h, "POST", "/admin/purge-screenshots/"+apiExamID, `{"confirm":false}`, "Bearer "+adminCredential); w.Code != 400 {
		t.Fatal("purge lacked confirmation")
	}
	for range 2 {
		if w := request(h, "POST", "/admin/purge-screenshots/"+apiExamID, `{"confirm":true}`, "Bearer "+adminCredential); w.Code != 200 || !strings.Contains(w.Body.String(), `"complete":true`) {
			t.Fatal("purge failed")
		}
	}
	if w := request(h, "GET", fmt.Sprintf("/admin/screenshot/%d", shot.ID), "", "Bearer "+adminCredential); w.Code != 404 {
		t.Fatal("purged image still accessible")
	}
	if w := uploadImage(t, h, a.SessionToken, loss.EventLogID, data, nil); w.Code != 403 {
		t.Fatal("purged image resurrected")
	}
}
func TestScreenshotHTTPNarrowPostFinalizationException(t *testing.T) {
	f := newAPIFixture(t)
	h := imageRouter(t, f)
	a := f.login(t, "0012")
	now := f.clock.Now()
	f.sessions.Heartbeat(t.Context(), a.SessionToken, "pc", "192.0.2.1", domain.Sound{})
	loss, _ := f.sessions.ClientEvent(t.Context(), a.SessionToken, service.EventInput{Type: "focus_lost"})
	f.clock.Set(now.Add(21 * time.Second))
	if _, err := f.sessions.Submit(t.Context(), a.SessionToken, map[string]string{}); err != nil {
		t.Fatal(err)
	}
	if w := uploadImage(t, h, a.SessionToken, loss.EventLogID, apiPNG(t), nil); w.Code != 200 {
		t.Fatalf("pending eligible image locked: %d %s", w.Code, w.Body.String())
	}
	for _, path := range []string{"/event", "/heartbeat", "/autosave"} {
		if w := request(h, "POST", path, `{}`, "Bearer "+a.SessionToken); w.Code != 403 {
			t.Fatal("ordinary terminal writes reopened")
		}
	}
	f.clock.Set(now.Add(81*time.Second + time.Nanosecond))
	if w := uploadImage(t, h, a.SessionToken, loss.EventLogID, apiPNG(t), nil); w.Code != 403 {
		t.Fatal("terminal image window widened")
	}
}
