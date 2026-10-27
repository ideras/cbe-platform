package client

import (
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPartialInitializationDoesNotDuplicateMonitoringOrParticipation(t *testing.T) {
	f := newFixture(t)
	f.examCode = 503
	var participated atomic.Int64
	s := monitorClient(t, f, func(o *Options) { o.Monitoring.OnParticipation = func() { participated.Add(1) } })
	if _, err := s.Login(t.Context(), f.server.URL, "00123", f.pin); err == nil {
		t.Fatal("expected partial initialization")
	}
	waitMonitor(t, s, func(v MonitoringView) bool { return v.HeartbeatAt != nil && v.Queued == 0 })
	f.mu.Lock()
	f.examCode = 0
	f.mu.Unlock()
	if _, err := s.RetryLoad(t.Context()); err != nil {
		t.Fatal(err)
	}
	if participated.Load() != 1 {
		t.Fatal("load retry repeated participation side effects")
	}
	kinds := telemetryTypes(f)
	starts := 0
	for _, kind := range kinds {
		if kind == "app_started" {
			starts++
		}
	}
	if starts != 1 {
		t.Fatalf("duplicate lifecycle loop: %v", kinds)
	}
}
func TestClosingDrainIsBoundedAndCancelsBlockedEvent(t *testing.T) {
	f := newFixture(t)
	entered := make(chan struct{})
	var once sync.Once
	f.onTelemetry = func(_ http.ResponseWriter, r *http.Request, record telemetryRecord) bool {
		if record.path != "/event" {
			return false
		}
		once.Do(func() { close(entered) })
		<-r.Context().Done()
		return true
	}
	s := monitorClient(t, f, nil)
	loginClient(t, s, f)
	<-entered
	done := make(chan struct{})
	go func() { CloseMonitoring(s); close(done) }()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("closing waited indefinitely behind event traffic")
	}
	if s.Snapshot().Monitoring.Active {
		t.Fatal("closed monitor still active")
	}
}
