package client

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func monitorClient(t *testing.T, f *fixture, modify func(*Options)) *Service {
	t.Helper()
	return newClient(t, f, func(o *Options) {
		o.Monitoring.Heartbeat = 40 * time.Millisecond
		o.Monitoring.FocusThreshold = 55 * time.Millisecond
		o.Monitoring.Tick = 2 * time.Millisecond
		o.Monitoring.CloseTimeout = 50 * time.Millisecond
		if modify != nil {
			modify(o)
		}
	})
}
func telemetryTypes(f *fixture) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var kinds []string
	for _, r := range f.telemetry {
		if r.path == "/event" {
			kinds = append(kinds, r.kind)
		}
	}
	return kinds
}
func waitMonitor(t *testing.T, s *Service, check func(MonitoringView) bool) MonitoringView {
	t.Helper()
	return waitView(t, s, func(v View) bool { return check(v.Monitoring) }).Monitoring
}
func TestDisclosureRequiredBeforeAdmission(t *testing.T) {
	f := newFixture(t)
	o := DefaultOptions()
	o.Screenshots.Required = false
	o.Server = f.server.URL
	s, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	defer s.ServiceShutdown()
	if _, err = s.Login(t.Context(), f.server.URL, "00123", f.pin); err == nil {
		t.Fatal("admitted without disclosure")
	}
	if s.Snapshot().Phase != "login" {
		t.Fatal("started session before acknowledgement")
	}
}
func TestImmediateHeartbeatConfiguredCadenceAndSingleWorkers(t *testing.T) {
	f := newFixture(t)
	f.status.Policy = &Policy{HeartbeatMillis: 80, OverdueMillis: 160, FocusMillis: 500}
	s := monitorClient(t, f, nil)
	ReportNativeFocus(s, true)
	before := time.Now()
	loginClient(t, s, f)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.HeartbeatAt != nil && v.Queued == 0 })
	f.mu.Lock()
	first := f.telemetry[0]
	f.mu.Unlock()
	if first.path != "/heartbeat" || first.at.Sub(before) > 100*time.Millisecond {
		t.Fatal("first heartbeat was not immediate")
	}
	if _, err := s.Login(t.Context(), f.server.URL, "00123", f.pin); err == nil {
		t.Fatal("duplicate login opened extra loops")
	}
	time.Sleep(185 * time.Millisecond)
	f.mu.Lock()
	var beats []time.Time
	for _, r := range f.telemetry {
		if r.path == "/heartbeat" {
			beats = append(beats, r.at)
		}
	}
	f.mu.Unlock()
	if len(beats) < 3 || len(beats) > 4 {
		t.Fatalf("unexpected cadence/duplicated loops: %d", len(beats))
	}
	for i := 1; i < len(beats); i++ {
		if beats[i].Sub(beats[i-1]) < 60*time.Millisecond {
			t.Fatal("status polls multiplied heartbeat sends")
		}
	}
	if got := s.Snapshot().Monitoring; got.PolicySource != "server" || got.Policy.FocusMillis != 500 {
		t.Fatal("ignored configured policy")
	}
	kinds := telemetryTypes(f)
	if len(kinds) < 2 || kinds[0] != "app_started" || kinds[1] != "focus_regained" {
		t.Fatalf("startup ordering: %v", kinds)
	}
}
func TestShortBlurAndDuplicateNativeEventsCancelAlertAndHook(t *testing.T) {
	f := newFixture(t)
	var alerts, hooks atomic.Int64
	s := monitorClient(t, f, func(o *Options) {
		o.Monitoring.OnAlert = func(Incident) { alerts.Add(1) }
		o.Monitoring.OnIncident = func(Incident) { hooks.Add(1) }
	})
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.Queued == 0 && v.HeartbeatAt != nil })
	ReportNativeFocus(s, false)
	ReportNativeFocus(s, false)
	time.Sleep(8 * time.Millisecond)
	ReportNativeFocus(s, true)
	ReportNativeFocus(s, true)
	time.Sleep(80 * time.Millisecond)
	if alerts.Load() != 0 || hooks.Load() != 0 {
		t.Fatal("short blur alerted or hooked")
	}
	kinds := telemetryTypes(f)
	losses, regains := 0, 0
	for _, kind := range kinds {
		if kind == "focus_lost" {
			losses++
		}
		if kind == "focus_regained" {
			regains++
		}
	}
	if losses != 1 || regains != 2 {
		t.Fatalf("duplicate native callbacks produced duplicates: %v", kinds)
	}
}
func TestSustainedBlurOneAlertAndAcknowledgedHookPerIncident(t *testing.T) {
	f := newFixture(t)
	var alerts, hooks atomic.Int64
	var mu sync.Mutex
	var refs []Incident
	s := monitorClient(t, f, func(o *Options) {
		o.Monitoring.OnAlert = func(Incident) { alerts.Add(1) }
		o.Monitoring.OnIncident = func(i Incident) { hooks.Add(1); mu.Lock(); refs = append(refs, i); mu.Unlock() }
	})
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.HeartbeatAt != nil && v.Queued == 0 })
	for n := int64(1); n <= 2; n++ {
		ReportNativeFocus(s, false)
		ReportNativeFocus(s, false)
		waitMonitor(t, s, func(v MonitoringView) bool { return hooks.Load() == n })
		time.Sleep(20 * time.Millisecond)
		if alerts.Load() != n || hooks.Load() != n {
			t.Fatal("repeated threshold action")
		}
		ReportNativeFocus(s, true)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(refs) != 2 || refs[0].LossEventID <= 0 || refs[1].LossEventID <= refs[0].LossEventID || refs[1].Sequence <= refs[0].Sequence {
		t.Fatalf("invalid incident references: %+v", refs)
	}
}
func TestLateLossAcknowledgmentCannotHookReturnedFocus(t *testing.T) {
	f := newFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	f.onTelemetry = func(_ http.ResponseWriter, r *http.Request, record telemetryRecord) bool {
		if record.kind == "focus_lost" {
			close(entered)
			select {
			case <-release:
			case <-r.Context().Done():
			}
		}
		return false
	}
	var alerts, hooks atomic.Int64
	s := monitorClient(t, f, func(o *Options) {
		o.Monitoring.OnAlert = func(Incident) { alerts.Add(1) }
		o.Monitoring.OnIncident = func(Incident) { hooks.Add(1) }
	})
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.HeartbeatAt != nil && v.Queued == 0 })
	ReportNativeFocus(s, false)
	<-entered
	waitMonitor(t, s, func(v MonitoringView) bool { return alerts.Load() == 1 })
	ReportNativeFocus(s, true)
	close(release)
	time.Sleep(25 * time.Millisecond)
	if hooks.Load() != 0 {
		t.Fatal("late loss ID invoked cancelled capture hook")
	}
}
func TestSlowAndFailedTelemetryNeverBlocksAnswerWrites(t *testing.T) {
	f := newFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	f.onTelemetry = func(w http.ResponseWriter, r *http.Request, _ telemetryRecord) bool {
		once.Do(func() { close(entered) })
		select {
		case <-release:
		case <-r.Context().Done():
		}
		http.Error(w, "private backend failure", 503)
		return true
	}
	s := monitorClient(t, f, nil)
	loginClient(t, s, f)
	<-entered
	update(t, s, "independent autosave", 1)
	waitView(t, s, func(v View) bool { return v.AcknowledgedRevision == 1 })
	close(release)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.HeartbeatError != "" })
	if s.Snapshot().Phase != "exam" {
		t.Fatal("monitoring failure finalized or locked answers")
	}
}
func TestEventRetryOrderBoundedAttemptsAndDiagnosticTime(t *testing.T) {
	f := newFixture(t)
	f.onTelemetry = func(w http.ResponseWriter, _ *http.Request, record telemetryRecord) bool {
		if record.kind == "focus_lost" {
			http.Error(w, "server failure", 503)
			return true
		}
		return false
	}
	s := monitorClient(t, f, nil)
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.HeartbeatAt != nil && v.Queued == 0 })
	ReportNativeFocus(s, false)
	ReportNativeFocus(s, true)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.Dropped == 1 && v.Queued == 0 })
	kinds := telemetryTypes(f)
	losses := 0
	for _, kind := range kinds {
		if kind == "focus_lost" {
			losses++
		}
	}
	if losses != 3 || kinds[len(kinds)-1] != "focus_regained" {
		t.Fatalf("unbounded or reordered retries: %v", kinds)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.telemetry {
		if r.kind == "focus_lost" {
			if r.detail["client_time"] == nil || r.detail["client_time"] == f.status.ServerTime.Format(time.RFC3339Nano) {
				t.Fatal("client diagnostic time confused with server time")
			}
		}
	}
}
func TestTerminalAuthorizationStopsTelemetryWithoutLocalFinalization(t *testing.T) {
	f := newFixture(t)
	f.onTelemetry = func(w http.ResponseWriter, _ *http.Request, _ telemetryRecord) bool {
		http.Error(w, "locked", 403)
		return true
	}
	s := monitorClient(t, f, nil)
	loginClient(t, s, f)
	waitMonitor(t, s, func(v MonitoringView) bool { return !v.Active && v.StoppedReason != "" })
	f.mu.Lock()
	count := len(f.telemetry)
	f.mu.Unlock()
	time.Sleep(100 * time.Millisecond)
	f.mu.Lock()
	after := len(f.telemetry)
	f.mu.Unlock()
	if count != after {
		t.Fatal("telemetry continued after 403")
	}
	if s.Snapshot().Phase != "exam" {
		t.Fatal("telemetry 403 inferred finalization locally")
	}
}
func TestClosureIsOrderedBestEffortAndTerminalSendsNoClosure(t *testing.T) {
	f := newFixture(t)
	s := monitorClient(t, f, nil)
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.HeartbeatAt != nil && v.Queued == 0 })
	CloseMonitoring(s)
	kinds := telemetryTypes(f)
	if kinds[len(kinds)-1] != "app_closed" {
		t.Fatalf("closure order: %v", kinds)
	}
	data, _ := json.Marshal(s.Snapshot().Monitoring)
	if strings.Contains(string(data), f.token) || strings.Contains(string(data), f.pin) {
		t.Fatal("monitor view leaked credential")
	}
	f2 := newFixture(t)
	s2 := monitorClient(t, f2, nil)
	loginClient(t, s2, f2)
	_, err := s2.Submit(map[string]string{"q1": "done"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	waitView(t, s2, func(v View) bool { return v.Final != nil })
	CloseMonitoring(s2)
	for _, kind := range telemetryTypes(f2) {
		if kind == "app_closed" {
			t.Fatal("terminal closure attempted")
		}
	}
}
func TestQueueOverflowIsBoundedAndVisible(t *testing.T) {
	f := newFixture(t)
	block, entered := make(chan struct{}), make(chan struct{})
	var once sync.Once
	f.onTelemetry = func(_ http.ResponseWriter, r *http.Request, record telemetryRecord) bool {
		if record.path == "/event" {
			once.Do(func() { close(entered) })
			select {
			case <-block:
			case <-r.Context().Done():
			}
		}
		return false
	}
	s := monitorClient(t, f, func(o *Options) { o.Monitoring.QueueLimit = 3 })
	loginClient(t, s, f)
	<-entered
	for i := 0; i < 20; i++ {
		ReportNativeFocus(s, i%2 == 0)
	}
	v := s.Snapshot().Monitoring
	if v.Queued > 3 || v.Dropped == 0 || v.EventError == "" {
		t.Fatal("queue overflow not bounded/visible")
	}
	close(block)
}
