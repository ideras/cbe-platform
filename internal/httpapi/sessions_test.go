package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"exam-platform/internal/domain"
	"exam-platform/internal/service"
	"exam-platform/internal/storage"
)

type stubSessions struct {
	login        func(context.Context, string, string) (service.LoginResult, error)
	authenticate func(context.Context, string) (service.Principal, error)
}

func (s stubSessions) Login(ctx context.Context, account, pin string) (service.LoginResult, error) {
	if s.login != nil {
		return s.login(ctx, account, pin)
	}
	return service.LoginResult{}, service.ErrInvalidCredentials
}
func (s stubSessions) Authenticate(ctx context.Context, token string) (service.Principal, error) {
	if s.authenticate != nil {
		return s.authenticate(ctx, token)
	}
	return service.Principal{}, service.ErrInvalidToken
}

func (s stubSessions) GetExam(context.Context, string) (service.ExamView, error) {
	return service.ExamView{}, service.ErrInvalidToken
}
func (s stubSessions) Autosave(context.Context, string, domain.Answers) (service.SaveResult, error) {
	return service.SaveResult{}, service.ErrInvalidToken
}
func (s stubSessions) Submit(context.Context, string, domain.Answers) (service.SubmitResult, error) {
	return service.SubmitResult{}, service.ErrInvalidToken
}
func (s stubSessions) Advance(context.Context, string, int, domain.Answers) (service.AdvanceResult, error) {
	return service.AdvanceResult{}, service.ErrInvalidToken
}

func (s stubSessions) Heartbeat(context.Context, string, string, string, domain.Sound) (service.HeartbeatResult, error) {
	return service.HeartbeatResult{}, service.ErrInvalidToken
}
func (s stubSessions) ClientEvent(context.Context, string, service.EventInput) (service.EventResult, error) {
	return service.EventResult{}, service.ErrInvalidToken
}

type apiClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *apiClock) Now() time.Time    { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *apiClock) Set(now time.Time) { c.mu.Lock(); defer c.mu.Unlock(); c.now = now }

type apiFixture struct {
	clock    *apiClock
	store    *storage.Store
	sessions *service.Sessions
	router   http.Handler
	logger   *slog.Logger
	logs     *bytes.Buffer
}

const apiExamID = "00000000-0000-4000-8000-000000000001"
const apiClassID = "00000000-0000-4000-8000-0000000000c1"

// newAPIFixture has the test exam published, as after the proctor clicked Publish.
func newAPIFixture(t *testing.T) *apiFixture {
	t.Helper()
	f := newUnpublishedAPIFixture(t)
	f.publish(t, apiExamID)
	return f
}

// publish admits the students of the exam's class to it through the service the proctor dashboard uses.
func (f *apiFixture) publish(t *testing.T, id string) {
	t.Helper()
	publisher := service.NewPublisher(f.store, f.clock)
	catalog, err := publisher.Catalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := publisher.Publish(t.Context(), id, catalog.CurrentExamID, true); err != nil {
		t.Fatal(err)
	}
}

// newUnpublishedAPIFixture stores the test exam but admits nobody.
func newUnpublishedAPIFixture(t *testing.T) *apiFixture {
	t.Helper()
	f := &apiFixture{clock: &apiClock{now: fixedClock{}.Now()}, logs: &bytes.Buffer{}}
	f.logger = slog.New(slog.NewJSONHandler(f.logs, nil))
	var err error
	f.store, err = storage.Open(t.Context(), filepath.Join(t.TempDir(), "exam.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.store.Close() })
	// The monitoring settings are stored before the exam is published, so the exam freezes them.
	storeSettings(f.store, f.clock, testPolicy())
	if err := f.store.CreateClass(t.Context(), domain.Class{ID: apiClassID, Name: "Test class", CreatedAt: f.clock.Now()}, []domain.Student{{AccountNumber: "0012", Name: "Ada", Email: "ada@example.invalid"}, {AccountNumber: "0034", Name: "Grace"}}); err != nil {
		t.Fatal(err)
	}
	if err := f.store.CreateExam(t.Context(), domain.Exam{ID: apiExamID, ClassID: apiClassID, Title: "Test", PIN: "exam-secret", DurationSeconds: 60, GraceSeconds: 30, CreatedAt: f.clock.Now()}); err != nil {
		t.Fatal(err)
	}
	f.sessions = testSessions(f.store, f.clock)
	f.router = New(f.clock, f.sessions, f.logger)
	return f
}

func request(handler http.Handler, method, path, body, authorization string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if authorization != "" {
		r.Header.Set("Authorization", authorization)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func (f *apiFixture) login(t *testing.T, account string) loginResponse {
	t.Helper()
	body, err := json.Marshal(loginRequest{AccountNumber: account, ExamPIN: "exam-secret"})
	if err != nil {
		t.Fatal(err)
	}
	w := request(f.router, "POST", "/login", string(body), "")
	if w.Code != http.StatusOK {
		t.Fatalf("login = %d %s", w.Code, w.Body.String())
	}
	var result loginResponse
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestLoginCredentialErrorsHaveIdenticalPublicResponse(t *testing.T) {
	f := newAPIFixture(t)
	unknown := request(f.router, "POST", "/login", `{"account_number":"missing","exam_pin":"exam-secret"}`, "")
	wrongPIN := request(f.router, "POST", "/login", `{"account_number":"0012","exam_pin":"wrong"}`, "")
	if unknown.Code != http.StatusUnauthorized || wrongPIN.Code != unknown.Code || wrongPIN.Body.String() != unknown.Body.String() {
		t.Fatalf("credential distinction: %d %q / %d %q", unknown.Code, unknown.Body.String(), wrongPIN.Code, wrongPIN.Body.String())
	}
	result := f.login(t, "0012")
	if !result.DeadlineAt.Equal(f.clock.Now().Add(time.Minute)) || result.ServerTime != f.clock.Now() {
		t.Fatalf("login time = %+v", result)
	}
	if strings.Contains(f.logs.String(), "exam-secret") || strings.Contains(f.logs.String(), result.SessionToken) {
		t.Fatal("credentials logged")
	}
	if unknown.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("credential response can be cached")
	}
}

func TestBearerAuthenticationAndOwnSessionOnly(t *testing.T) {
	f := newAPIFixture(t)
	first := f.login(t, "0012")
	f.clock.Set(f.clock.Now().Add(5 * time.Second))
	other := f.login(t, "0034")
	for _, header := range []string{"", "Bearer", "Bearer ", "Basic " + first.SessionToken, "Bearer invalid-token", "Bearer " + first.SessionToken + " extra", "Bearer " + first.SessionToken + ",Bearer " + other.SessionToken} {
		w := request(f.router, "GET", "/session-status", "", header)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("malformed/invalid bearer accepted: %d", w.Code)
		}
		if w.Header().Get("WWW-Authenticate") == "" {
			t.Fatal("missing authentication challenge")
		}
	}
	r := httptest.NewRequest("GET", "/session-status", nil)
	r.Header.Add("Authorization", "Bearer "+first.SessionToken)
	r.Header.Add("Authorization", "Bearer "+other.SessionToken)
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatal("multiple bearer headers accepted")
	}
	w = request(f.router, "GET", "/session-status?session_id="+other.SessionID, "", "bearer "+first.SessionToken)
	var status statusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || !status.DeadlineAt.Equal(first.DeadlineAt) || status.SecondsRemaining != 55 {
		t.Fatalf("cross-session status = %+v, %d", status, w.Code)
	}
	if strings.Contains(w.Body.String(), first.SessionToken) || strings.Contains(w.Body.String(), other.SessionToken) {
		t.Fatal("status leaked tokens")
	}
	w = request(f.router, "GET", "/session-status?token="+first.SessionToken, "", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatal("query parameter authorized a session")
	}
}

func TestStatusBoundariesAndClientClockSkew(t *testing.T) {
	f := newAPIFixture(t)
	login := f.login(t, "0012")
	start := login.ServerTime
	for _, test := range []struct {
		elapsed   time.Duration
		state     domain.SessionStatus
		remaining int64
		pending   bool
	}{
		{0, domain.Active, 60, false}, {60*time.Second - time.Nanosecond, domain.Active, 1, false},
		{60 * time.Second, domain.Grace, 0, false}, {89 * time.Second, domain.Grace, 0, false},
		{90 * time.Second, domain.Submitted, 0, false}, {time.Hour, domain.Submitted, 0, false},
	} {
		f.clock.Set(start.Add(test.elapsed))
		for _, clientDate := range []string{"Mon, 01 Jan 2001 00:00:00 GMT", "Sun, 01 Jan 2040 00:00:00 GMT"} {
			r := httptest.NewRequest("GET", "/session-status?server_time=2099-01-01T00:00:00Z", nil)
			r.Header.Set("Date", clientDate)
			r.Header.Set("Authorization", "Bearer "+login.SessionToken)
			w := httptest.NewRecorder()
			f.router.ServeHTTP(w, r)
			var status statusResponse
			if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
				t.Fatal(err)
			}
			if w.Code != http.StatusOK || status.Status != test.state || status.SecondsRemaining != test.remaining || status.FinalizationPending != test.pending || !status.ServerTime.Equal(f.clock.Now()) || !status.GraceUntil.Equal(start.Add(90*time.Second)) {
				t.Fatalf("status = %+v, %d", status, w.Code)
			}
		}
	}
	w := request(f.router, "POST", "/login", `{"account_number":"0012","exam_pin":"exam-secret"}`, "")
	if w.Code != http.StatusConflict {
		t.Fatalf("expired resume = %d %s", w.Code, w.Body.String())
	}
}

func TestMiddlewareTerminalExceptionsAreNarrow(t *testing.T) {
	f := newAPIFixture(t)
	login := f.login(t, "0012")
	f.clock.Set(login.DeadlineAt.Add(30 * time.Second))
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := PrincipalFromContext(r.Context())
		if !ok || principal.Session.ID != login.SessionID {
			t.Error("missing token-scoped principal")
		}
		w.WriteHeader(http.StatusNoContent)
	})
	guard := StudentAuth(f.sessions, f.logger, next)
	for _, test := range []struct {
		method, path string
		allowed      bool
	}{
		{"GET", "/session-status", true}, {"HEAD", "/session-status", true}, {"POST", "/submit", true},
		{"POST", "/session-status", false}, {"GET", "/submit", false}, {"POST", "/submit/other", false},
		{"POST", "/autosave", false}, {"GET", "/exam", false}, {"POST", "/heartbeat", false},
		{"POST", "/event", false}, {"POST", "/screenshot", true}, {"GET", "/admin/sessions", false},
	} {
		w := request(guard, test.method, test.path, "", "Bearer "+login.SessionToken)
		expected := http.StatusForbidden
		if test.allowed {
			expected = http.StatusNoContent
		}
		if w.Code != expected {
			t.Fatalf("%s %s = %d; want %d", test.method, test.path, w.Code, expected)
		}
	}
	w := request(f.router, "POST", "/submit", `{"answers":{}}`, "Bearer "+login.SessionToken)
	if w.Code != http.StatusOK {
		t.Fatal("repeated submit did not reach its idempotent handler")
	}
}

func TestLoginBodyValidation(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	calls := 0
	sessions := stubSessions{login: func(context.Context, string, string) (service.LoginResult, error) {
		calls++
		return service.LoginResult{}, nil
	}}
	router := New(fixedClock{}, sessions, logger)
	for _, body := range []string{"", "null", "[]", "not JSON", "{", `{"account_number":12}`, `{"account_number":"0012","exam_pin":"pin","server_time":"2099"}`, `{} {}`, "{} garbage"} {
		w := request(router, "POST", "/login", body, "")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("bad input = %d for %q", w.Code, body)
		}
	}
	w := request(router, "POST", "/login", `{"account_number":"`+strings.Repeat("x", 9000)+`","exam_pin":"pin"}`, "")
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("body limit = %d", w.Code)
	}
	r := httptest.NewRequest("POST", "/login", strings.NewReader("{}"))
	r.Header.Set("Content-Type", "text/plain")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("content type = %d", w.Code)
	}
	if calls != 0 {
		t.Fatal("invalid body reached service")
	}
	if request(router, "GET", "/login", "", "").Code != http.StatusMethodNotAllowed {
		t.Fatal("GET login allowed")
	}
	if request(router, "POST", "/session-status", "{}", "").Code != http.StatusMethodNotAllowed {
		t.Fatal("POST status allowed")
	}
}

func TestServiceFailuresAreSanitized(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, test := range []struct {
		err     error
		status  int
		message string
	}{
		{service.ErrNoCurrentExam, http.StatusServiceUnavailable, "no current exam is available"},
		{service.ErrAdmissionClosed, http.StatusForbidden, "exam admission is closed"},
		{errors.New("private database details"), http.StatusInternalServerError, "internal server error"},
	} {
		sessions := stubSessions{login: func(context.Context, string, string) (service.LoginResult, error) {
			return service.LoginResult{}, test.err
		}}
		w := request(New(fixedClock{}, sessions, logger), "POST", "/login", `{"account_number":"0012","exam_pin":"pin"}`, "")
		var body struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if w.Code != test.status || body.Error != test.message {
			t.Fatalf("error = %d %q", w.Code, body.Error)
		}
	}
}

func TestStudentAndAdminCredentialsAreSeparate(t *testing.T) {
	f := newAPIFixture(t)
	login := f.login(t, "0012")
	adminToken := strings.Repeat("a", 64)
	w := request(f.router, "GET", "/session-status", "", "Bearer "+adminToken)
	if w.Code != http.StatusUnauthorized {
		t.Fatal("admin token authorized student session")
	}
	admin := AdminAuth(adminToken, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	if request(admin, "GET", "/admin/test", "", "Bearer "+login.SessionToken).Code != http.StatusUnauthorized {
		t.Fatal("student token authorized admin action")
	}
}

func TestRealHTTPLoginAndResumeFlow(t *testing.T) {
	f := newAPIFixture(t)
	server := httptest.NewServer(f.router)
	defer server.Close()
	login := func() loginResponse {
		response, err := server.Client().Post(server.URL+"/login", "application/json", strings.NewReader(`{"account_number":"0012","exam_pin":"exam-secret"}`))
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var result loginResponse
		if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK {
			t.Fatalf("login = %d", response.StatusCode)
		}
		return result
	}
	first := login()
	f.clock.Set(first.ServerTime.Add(10 * time.Second))
	resume := login()
	if first.SessionID != resume.SessionID || first.SessionToken != resume.SessionToken || !first.DeadlineAt.Equal(resume.DeadlineAt) {
		t.Fatal("real HTTP resume reset session")
	}
	r, err := http.NewRequest("GET", server.URL+"/session-status", nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer "+first.SessionToken)
	response, err := server.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var status statusResponse
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || status.SecondsRemaining != 50 {
		t.Fatalf("real HTTP status = %+v", status)
	}
}
