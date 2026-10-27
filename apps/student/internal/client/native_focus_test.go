package client

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestNativeSamplingSerializesReadAndApplyDespiteConcurrentCallbacks(t *testing.T) {
	f := newFixture(t)
	s := monitorClient(t, f, nil)
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.HeartbeatAt != nil && v.Queued == 0 })
	entered, release, firstDone, secondDone := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var reads atomic.Int64
	go func() {
		ObserveNativeFocus(s, func() bool { reads.Add(1); close(entered); <-release; return false })
		close(firstDone)
	}()
	<-entered
	go func() { ObserveNativeFocus(s, func() bool { reads.Add(1); return true }); close(secondDone) }()
	time.Sleep(5 * time.Millisecond)
	if reads.Load() != 1 {
		t.Fatal("native state reads overlapped")
	}
	close(release)
	<-firstDone
	<-secondDone
	waitMonitor(t, s, func(v MonitoringView) bool { return v.Focus == "focused" && v.Queued == 0 })
	kinds := telemetryTypes(f)
	if len(kinds) < 4 || kinds[len(kinds)-2] != "focus_lost" || kinds[len(kinds)-1] != "focus_regained" {
		t.Fatalf("native observations applied out of order: %v", kinds)
	}
}
