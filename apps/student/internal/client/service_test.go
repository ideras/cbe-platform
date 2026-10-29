package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"exam-platform/shared/calc"
)

type writeRecord struct {
	path    string
	answers map[string]string
}
type telemetryRecord struct {
	path, kind string
	at         time.Time
	detail     map[string]any
	volume     *int
	muted      *bool
}
type screenshotRecord struct {
	event       int64
	index       int
	data        []byte
	at          time.Time
	contentType string
}
type fixture struct {
	screenshots                     []screenshotRecord
	onScreenshot                    func(http.ResponseWriter, *http.Request, screenshotRecord, int) bool
	telemetry                       []telemetryRecord
	onTelemetry                     func(http.ResponseWriter, *http.Request, telemetryRecord) bool
	mu                              sync.Mutex
	server                          *httptest.Server
	status                          Status
	token, pin                      string
	saved                           *Saved
	answers                         map[string]string
	writes                          []writeRecord
	active, maxActive               int
	loginCode, statusCode, examCode int
	logins                          int // requests to /login
	onWrite                         func(http.ResponseWriter, *http.Request, writeRecord, int) bool
	onStatus                        func(*http.Request)
	final                           Final
	minVolume                       *int // the exam volume the login reply advertises; nil like an older server
	navigation                      string
	examTools                       *calc.Tools // the exam's tools configuration the /exam reply carries; nil like an exam without the key
}

// sampleContent has two exercises: q1, a text answer whose body carries markup to keep inert, and q2, a code answer.
var sampleContent = Content{Instructions: "<p>Read carefully.</p>", Exercises: []Exercise{
	{ID: "1", Title: "First", Body: `<p>&lt;script&gt;not executable&lt;/script&gt;</p><div class="slot" data-slot="q1"></div>`, Slots: []Slot{{ID: "q1", Kind: "text", Lines: 4}}},
	{ID: "2", Title: "Second", Items: []Item{{ID: "2a", Title: "a", Body: `<div class="slot" data-slot="q2"></div>`, Slots: []Slot{{ID: "q2", Kind: "code", Language: "mips"}}}}},
}}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	clock := time.Date(2035, 1, 2, 3, 4, 5, 0, time.UTC)
	f := &fixture{token: strings.Repeat("private-bearer", 4), pin: " literal PIN ", answers: map[string]string{"q1": "restored", "q2": ""}, navigation: "free", status: Status{State: "active", ServerTime: clock, DeadlineAt: clock.Add(40 * time.Second), GraceUntil: clock.Add(60 * time.Second), SecondsRemaining: 40}}
	f.server = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.server.Close)
	return f
}
func respond(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
func (f *fixture) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	if r.URL.Path == "/login" {
		f.logins++
		code := f.loginCode
		login := loginReply{Policy: f.status.Policy, Token: f.token, SessionID: "session", ServerTime: f.status.ServerTime, DeadlineAt: f.status.DeadlineAt, MinVolumePercent: f.minVolume}
		f.mu.Unlock()
		if code != 0 {
			http.Error(w, "do not echo credentials", code)
			return
		}
		var input struct {
			Account string `json:"account_number"`
			PIN     string `json:"exam_pin"`
		}
		_ = json.NewDecoder(r.Body).Decode(&input)
		if input.Account != "00123" || input.PIN != f.pin {
			http.Error(w, "private login input rejected", 401)
			return
		}
		respond(w, login)
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+f.token {
		f.mu.Unlock()
		http.Error(w, "bad bearer", 401)
		return
	}
	switch r.URL.Path {
	case "/exam":
		code := f.examCode
		reply := examReply{Exam: Exam{ID: "exam", Title: "Sample", Navigation: f.navigation, Content: sampleContent, Tools: f.examTools}, CurrentExercise: f.status.CurrentExercise, Answers: maps.Clone(f.answers), Latest: f.saved}
		f.mu.Unlock()
		if code != 0 {
			http.Error(w, "exam failed", code)
			return
		}
		respond(w, reply)
	case "/session-status":
		code, status, hook := f.statusCode, f.status, f.onStatus
		f.mu.Unlock()
		if hook != nil {
			hook(r)
		}
		if code != 0 {
			http.Error(w, "status failed", code)
			return
		}
		respond(w, status)
	case "/heartbeat", "/event":
		clock, hook := f.status.ServerTime, f.onTelemetry
		f.mu.Unlock()
		var input struct {
			Type     string         `json:"event_type"`
			Detail   map[string]any `json:"detail"`
			Hostname string         `json:"hostname"`
			Volume   *int           `json:"volume_percent"`
			Muted    *bool          `json:"volume_muted"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "invalid telemetry", 400)
			return
		}
		record := telemetryRecord{path: r.URL.Path, kind: input.Type, at: time.Now(), detail: input.Detail, volume: input.Volume, muted: input.Muted}
		f.mu.Lock()
		f.telemetry = append(f.telemetry, record)
		id := int64(100 + len(f.telemetry))
		f.mu.Unlock()
		if hook != nil && hook(w, r, record) {
			return
		}
		if r.URL.Path == "/heartbeat" {
			respond(w, map[string]any{"server_time": clock})
		} else {
			respond(w, eventAck{ID: id, ServerTime: clock})
		}
	case "/screenshot":
		hook := f.onScreenshot
		clock := f.status.ServerTime
		f.mu.Unlock()
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			http.Error(w, "invalid screenshot", 400)
			return
		}
		defer r.MultipartForm.RemoveAll()
		file, header, err := r.FormFile("image")
		if err != nil {
			http.Error(w, "missing image", 400)
			return
		}
		defer file.Close()
		data, _ := io.ReadAll(file)
		event, _ := strconv.ParseInt(r.FormValue("event_log_id"), 10, 64)
		at, _ := time.Parse(time.RFC3339Nano, r.FormValue("client_captured_at"))
		index, _ := strconv.Atoi(r.FormValue("capture_index"))
		record := screenshotRecord{event: event, index: index, data: data, at: at, contentType: header.Header.Get("Content-Type")}
		f.mu.Lock()
		f.screenshots = append(f.screenshots, record)
		count := len(f.screenshots)
		f.mu.Unlock()
		if hook != nil && hook(w, r, record, count) {
			return
		}
		respond(w, map[string]any{"screenshot_id": event + 1000, "event_log_id": event, "captured_at": clock})
	case "/autosave", "/submit", "/advance":
		f.mu.Unlock()
		var input struct {
			Answers map[string]string `json:"answers"`
			From    *int              `json:"from_exercise"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "bad answers", 400)
			return
		}
		f.mu.Lock()
		record := writeRecord{r.URL.Path, maps.Clone(input.Answers)}
		f.writes = append(f.writes, record)
		count := len(f.writes)
		f.active++
		f.maxActive = max(f.maxActive, f.active)
		hook := f.onWrite
		f.mu.Unlock()
		defer func() { f.mu.Lock(); f.active--; f.mu.Unlock() }()
		if hook != nil && hook(w, r, record, count) {
			return
		}
		f.mu.Lock()
		if r.URL.Path == "/submit" {
			if f.final.VersionID == 0 {
				f.final = Final{Status: "submitted", SubmittedAt: f.status.ServerTime, VersionID: 17, Sequence: int64(count)}
				f.status.State = "submitted"
				f.status.SecondsRemaining = 0
			}
			final := f.final
			f.mu.Unlock()
			respond(w, final)
			return
		}
		if finalStatus(f.status.State) {
			f.mu.Unlock()
			http.Error(w, "locked", 403)
			return
		}
		if r.URL.Path == "/advance" {
			if f.navigation != "forward_only" || input.From == nil || *input.From != f.status.CurrentExercise {
				f.mu.Unlock()
				http.Error(w, "not on that exercise", 409)
				return
			}
			f.status.CurrentExercise++
		}
		saved := Saved{Sequence: int64(count), SavedAt: f.status.ServerTime}
		f.saved = &saved
		f.answers = maps.Clone(input.Answers)
		current := f.status.CurrentExercise
		f.mu.Unlock()
		if r.URL.Path == "/advance" {
			respond(w, advanceReply{CurrentExercise: current, Sequence: saved.Sequence, SavedAt: saved.SavedAt})
			return
		}
		respond(w, saved)
	default:
		f.mu.Unlock()
		http.NotFound(w, r)
	}
}
func newClient(t *testing.T, f *fixture, modify func(*Options)) *Service {
	t.Helper()
	opts := DefaultOptions()
	opts.Screenshots.Required = false
	opts.Server = f.server.URL
	opts.Debounce = 25 * time.Millisecond
	opts.Periodic = 100 * time.Millisecond
	opts.Poll = 20 * time.Millisecond
	opts.RequestTimeout = 500 * time.Millisecond
	opts.RetryMin = 10 * time.Millisecond
	opts.RetryMax = 40 * time.Millisecond
	if modify != nil {
		modify(&opts)
	}
	s, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.ServiceShutdown() })
	s.AcknowledgeRules()
	return s
}
func loginClient(t *testing.T, s *Service, f *fixture) View {
	t.Helper()
	view, err := s.Login(t.Context(), f.server.URL, " 00123 ", f.pin)
	if err != nil {
		t.Fatal(err)
	}
	return view
}
func waitView(t *testing.T, s *Service, condition func(View) bool) View {
	t.Helper()
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		v := s.Snapshot()
		if condition(v) {
			return v
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("condition not reached: %+v", s.Snapshot())
	return View{}
}
func update(t *testing.T, s *Service, text string, revision int64) {
	t.Helper()
	_, err := s.UpdateAnswers(map[string]string{"q1": text, "q2": "second"}, revision)
	if err != nil {
		t.Fatal(err)
	}
}
func TestLoginLoadAndPrivateCredentials(t *testing.T) {
	f := newFixture(t)
	s := newClient(t, f, nil)
	view := loginClient(t, s, f)
	if view.Phase != "exam" || view.Answers["q1"] != "restored" || view.Exam == nil || view.Status.SecondsRemaining != 40 {
		t.Fatalf("bad initial view: %+v", view)
	}
	data, _ := json.Marshal(view)
	if strings.Contains(string(data), f.pin) || strings.Contains(string(data), f.token) || strings.Contains(string(data), "session_token") {
		t.Fatal("credentials leaked to view")
	}
	if _, err := s.Login(t.Context(), f.server.URL, "00123", f.pin); err == nil {
		t.Fatal("second login must not discard current session")
	}
	snapshot := s.Snapshot()
	if snapshot.Exam != nil || snapshot.Answers != nil || snapshot.Observation <= view.Observation {
		t.Fatal("poll snapshots should be small and ordered")
	}
}
func TestInitializationRequiresBothReadsAndCanRetryWithoutPIN(t *testing.T) {
	for _, kind := range []string{"exam", "status"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			if kind == "exam" {
				f.examCode = 503
			} else {
				f.statusCode = 503
			}
			s := newClient(t, f, nil)
			if _, err := s.Login(t.Context(), f.server.URL, "00123", f.pin); err == nil {
				t.Fatal("load should fail")
			}
			if s.Snapshot().Phase != "loading" {
				t.Fatal("must retain started session for load retry")
			}
			if _, err := s.UpdateAnswers(map[string]string{"q1": "bad"}, 1); err == nil {
				t.Fatal("editing enabled before bootstrap")
			}
			f.mu.Lock()
			f.examCode, f.statusCode = 0, 0
			f.mu.Unlock()
			view, err := s.RetryLoad(t.Context())
			if err != nil || view.Phase != "exam" {
				t.Fatalf("retry: %+v %v", view, err)
			}
		})
	}
}
func TestLoginErrorsDistinctAndDoNotEchoServerBody(t *testing.T) {
	for _, code := range []int{401, 409, 503} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			f := newFixture(t)
			f.loginCode = code
			s := newClient(t, f, nil)
			_, err := s.Login(t.Context(), f.server.URL, "00123", f.pin)
			if failureCode(err) != code || strings.Contains(err.Error(), "do not echo") {
				t.Fatalf("unsafe/wrong error: %v", err)
			}
			if s.Snapshot().Phase != "login" {
				t.Fatal("failed login changed phase")
			}
		})
	}
}
func TestDebounceCoalescesAndIgnoresOlderIPCRevisions(t *testing.T) {
	f := newFixture(t)
	s := newClient(t, f, nil)
	loginClient(t, s, f)
	update(t, s, "old", 1)
	update(t, s, "new", 2)
	update(t, s, "stale delivery", 1)
	time.Sleep(8 * time.Millisecond)
	f.mu.Lock()
	before := len(f.writes)
	f.mu.Unlock()
	if before != 0 {
		t.Fatal("saved before debounce")
	}
	v := waitView(t, s, func(v View) bool { return v.AcknowledgedRevision == 2 })
	if v.SaveState != "saved" {
		t.Fatalf("no acknowledgment: %+v", v)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.writes) != 1 || f.writes[0].answers["q1"] != "new" || len(f.writes[0].answers) != 2 {
		t.Fatalf("writes: %+v", f.writes)
	}
}
func TestPeriodicBackstopSavesDuringContinuousTyping(t *testing.T) {
	f := newFixture(t)
	s := newClient(t, f, func(o *Options) { o.Debounce = time.Second; o.Periodic = 60 * time.Millisecond })
	loginClient(t, s, f)
	for i := int64(1); i <= 12; i++ {
		update(t, s, "typing", i)
		time.Sleep(10 * time.Millisecond)
	}
	v := waitView(t, s, func(v View) bool { return v.AcknowledgedRevision > 0 })
	if v.Saved == nil {
		t.Fatal("continuous typing starved backstop")
	}
}
func TestSubmitWaitsForInflightSaveAndUsesLatestVisibleSnapshot(t *testing.T) {
	f := newFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	f.onWrite = func(w http.ResponseWriter, r *http.Request, _ writeRecord, n int) bool {
		if n == 1 {
			close(entered)
			select {
			case <-release:
			case <-r.Context().Done():
			}
		}
		return false
	}
	s := newClient(t, f, nil)
	loginClient(t, s, f)
	update(t, s, "in-flight", 1)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("save not started")
	}
	update(t, s, "pending", 2)
	_, err := s.Submit(map[string]string{"q1": "visible, debounce not fired", "q2": "second"}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpdateAnswers(map[string]string{"q1": "too late"}, 4); err == nil {
		t.Fatal("edits accepted during pending submit")
	}
	close(release)
	v := waitView(t, s, func(v View) bool { return v.Final != nil && !v.Submitting })
	if v.Phase != "complete" {
		t.Fatal("submit not complete")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.maxActive != 1 || len(f.writes) != 2 || f.writes[1].path != "/submit" || f.writes[1].answers["q1"] != "visible, debounce not fired" {
		t.Fatalf("unordered writes: %+v", f.writes)
	}
}
func TestFailedOldSaveRetriesOnlyNewestSnapshot(t *testing.T) {
	f := newFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	f.onWrite = func(w http.ResponseWriter, r *http.Request, _ writeRecord, n int) bool {
		if n != 1 {
			return false
		}
		close(entered)
		select {
		case <-release:
		case <-r.Context().Done():
		}
		http.Error(w, "failed old save", 503)
		return true
	}
	s := newClient(t, f, nil)
	loginClient(t, s, f)
	update(t, s, "old", 1)
	<-entered
	update(t, s, "newer", 2)
	close(release)
	waitView(t, s, func(v View) bool { return v.AcknowledgedRevision == 2 })
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.writes) != 2 || f.writes[1].answers["q1"] != "newer" {
		t.Fatalf("replayed stale snapshot: %+v", f.writes)
	}
}
func TestLostSubmitResponseRetriesImmutablePayloadAfterTerminalPoll(t *testing.T) {
	f := newFixture(t)
	f.onWrite = func(w http.ResponseWriter, _ *http.Request, _ writeRecord, n int) bool {
		if n != 1 {
			return false
		}
		f.mu.Lock()
		f.final = Final{Status: "submitted", SubmittedAt: f.status.ServerTime, VersionID: 44, Sequence: 1}
		f.status.State = "submitted"
		f.status.SecondsRemaining = 0
		f.mu.Unlock()
		http.Error(w, "lost confirmation", 503)
		return true
	}
	s := newClient(t, f, func(o *Options) { o.RetryMin = 80 * time.Millisecond; o.RetryMax = 80 * time.Millisecond })
	loginClient(t, s, f)
	_, err := s.Submit(map[string]string{"q1": "final text", "q2": ""}, 1)
	if err != nil {
		t.Fatal(err)
	}
	waitView(t, s, func(v View) bool { return v.Phase == "complete" && v.Submitting })
	v := waitView(t, s, func(v View) bool { return v.Final != nil && !v.Submitting })
	if v.Final.VersionID != 44 {
		t.Fatal("lost selected identity")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.writes) != 2 || !maps.Equal(f.writes[0].answers, f.writes[1].answers) {
		t.Fatal("submit retry replaced captured snapshot")
	}
}
func TestStaleTimingAndGraceAreOnlyServerControlled(t *testing.T) {
	f := newFixture(t)
	s := newClient(t, f, nil)
	loginClient(t, s, f)
	f.mu.Lock()
	f.statusCode = 503
	f.mu.Unlock()
	v := waitView(t, s, func(v View) bool { return v.TimingStale })
	if v.Phase != "exam" || v.Status.SecondsRemaining != 40 {
		t.Fatal("client inferred expiry from local clock or failure")
	}
	f.mu.Lock()
	f.statusCode = 0
	f.status.State = "grace"
	f.status.SecondsRemaining = 0
	f.status.ServerTime = f.status.ServerTime.Add(time.Second)
	f.mu.Unlock()
	waitView(t, s, func(v View) bool { return !v.TimingStale && v.Status.State == "grace" })
	update(t, s, "during grace", 1)
	waitView(t, s, func(v View) bool { return v.AcknowledgedRevision == 1 })
	f.mu.Lock()
	f.status.State = "force_submitted"
	f.status.ServerTime = f.status.ServerTime.Add(time.Second)
	f.mu.Unlock()
	waitView(t, s, func(v View) bool { return v.Phase == "complete" })
	if _, err := s.UpdateAnswers(map[string]string{"q1": "after final"}, 2); err == nil {
		t.Fatal("force submitted still editable")
	}
}
func TestShutdownCancelsAndJoinsInflightWorkers(t *testing.T) {
	f := newFixture(t)
	entered, cancelled := make(chan struct{}), make(chan struct{})
	f.onWrite = func(_ http.ResponseWriter, r *http.Request, _ writeRecord, _ int) bool {
		close(entered)
		<-r.Context().Done()
		close(cancelled)
		return true
	}
	s := newClient(t, f, nil)
	loginClient(t, s, f)
	update(t, s, "pending", 1)
	<-entered
	done := make(chan struct{})
	go func() { _ = s.ServiceShutdown(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not join")
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("request not cancelled")
	}
}
func TestInvalidAnswerIdentityAndPermanentFailureRetainWork(t *testing.T) {
	f := newFixture(t)
	f.onWrite = func(w http.ResponseWriter, _ *http.Request, _ writeRecord, _ int) bool {
		http.Error(w, "never expose this body", 413)
		return true
	}
	s := newClient(t, f, nil)
	loginClient(t, s, f)
	if _, err := s.UpdateAnswers(map[string]string{"other": "bad"}, 1); err == nil {
		t.Fatal("unowned answer accepted")
	}
	update(t, s, "too large for server", 1)
	v := waitView(t, s, func(v View) bool { return v.SaveState == "failed" })
	if v.AcknowledgedRevision != 0 || v.Revision != 1 {
		t.Fatal("failure lost local revision or claimed saved")
	}
	time.Sleep(140 * time.Millisecond)
	f.mu.Lock()
	count := len(f.writes)
	f.mu.Unlock()
	if count != 1 {
		t.Fatal("permanent failure retried without edits")
	}
}
func TestRequestCancellation(t *testing.T) {
	f := newFixture(t)
	s := newClient(t, f, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.Login(ctx, f.server.URL, "00123", f.pin)
	if err == nil {
		t.Fatal("cancelled login succeeded")
	}
	if !errors.As(err, new(*apiFailure)) {
		t.Fatal("request error must be sanitized")
	}
}
