package client

import (
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestRejectedOldSaveDoesNotBlockCorrectedPendingSnapshot(t *testing.T) {
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
		http.Error(w, "old snapshot too large", 413)
		return true
	}
	s := newClient(t, f, nil)
	loginClient(t, s, f)
	update(t, s, "old large answer", 1)
	<-entered
	update(t, s, "corrected answer", 2)
	close(release)
	waitView(t, s, func(v View) bool { return v.AcknowledgedRevision == 2 })
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.writes) != 2 || f.writes[1].answers["q1"] != "corrected answer" {
		t.Fatal("old rejection blocked newer corrected work")
	}
}
func TestSlowStatusCannotBlockWritesOrReviveCompletedState(t *testing.T) {
	f := newFixture(t)
	s := newClient(t, f, nil)
	loginClient(t, s, f)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	f.mu.Lock()
	f.onStatus = func(r *http.Request) {
		once.Do(func() { close(entered) })
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}
	f.mu.Unlock()
	signal(s.pollNow)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("poll did not start")
	}
	update(t, s, "not blocked by status", 1)
	waitView(t, s, func(v View) bool { return v.AcknowledgedRevision == 1 })
	_, err := s.Submit(map[string]string{"q1": "latest", "q2": "second"}, 2)
	if err != nil {
		t.Fatal(err)
	}
	waitView(t, s, func(v View) bool { return v.Final != nil })
	close(release)
	time.Sleep(40 * time.Millisecond)
	v := s.Snapshot()
	if v.Phase != "complete" || v.Final == nil {
		t.Fatal("old active status revived completed client")
	}
}
func TestBackwardServerClockRemainsAuthoritative(t *testing.T) {
	f := newFixture(t)
	s := newClient(t, f, nil)
	initial := loginClient(t, s, f)
	f.mu.Lock()
	f.status.ServerTime = f.status.ServerTime.Add(-time.Minute)
	f.status.SecondsRemaining = 75
	f.mu.Unlock()
	v := waitView(t, s, func(v View) bool { return v.Status.SecondsRemaining == 75 })
	if !v.Status.ServerTime.Before(initial.Status.ServerTime) {
		t.Fatal("ignored authoritative backwards clock")
	}
}
