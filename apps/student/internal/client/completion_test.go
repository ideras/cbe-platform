package client

import (
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestCompletionReleasesOnceAndAllowsExplicitClose(t *testing.T) {
	f := newFixture(t)
	var released, closed atomic.Int32
	var s *Service
	s = newClient(t, f, func(o *Options) {
		o.OnCompletion = func() { s.Snapshot(); released.Add(1) }
		o.OnClose = func() { s.Snapshot(); closed.Add(1) }
	})
	loginClient(t, s, f)
	if err := s.CloseCompletedWindow(); err == nil || closed.Load() != 0 {
		t.Fatal("completion close allowed while active")
	}
	if _, err := s.Submit(map[string]string{"q1": "final", "q2": ""}, 0); err != nil {
		t.Fatal(err)
	}
	waitView(t, s, func(v View) bool { return v.Phase == "complete" && released.Load() == 1 })
	s.pollOnce()
	s.pollOnce()
	if released.Load() != 1 {
		t.Fatal("completion action repeated")
	}
	if err := s.CloseCompletedWindow(); err != nil || closed.Load() != 1 {
		t.Fatal(err)
	}
	if err := s.CloseCompletedWindow(); err != nil || closed.Load() != 1 {
		t.Fatal("repeated native close", err)
	}
}

func TestAuthoritativeTerminalStatusReleasesIncludingPartialLoad(t *testing.T) {
	for _, state := range []string{"submitted", "force_submitted"} {
		t.Run(state, func(t *testing.T) {
			f := newFixture(t)
			var released atomic.Int32
			s := newClient(t, f, func(o *Options) { o.OnCompletion = func() { released.Add(1) } })
			loginClient(t, s, f)
			f.mu.Lock()
			f.status.State = state
			f.status.SecondsRemaining = 0
			f.mu.Unlock()
			waitView(t, s, func(v View) bool { return v.Phase == "complete" && released.Load() == 1 })
			s.pollOnce()
			if released.Load() != 1 {
				t.Fatal("repeat release")
			}
		})
	}
	f := newFixture(t)
	f.status.State = "submitted"
	f.status.SecondsRemaining = 0
	f.examCode = 403
	var released atomic.Int32
	s := newClient(t, f, func(o *Options) { o.OnCompletion = func() { released.Add(1) } })
	v := loginClient(t, s, f)
	if v.Phase != "complete" || released.Load() != 1 {
		t.Fatal("terminal partial initialization retained deterrents")
	}
}

func TestUnconfirmedSubmitAndAuthorizationErrorsDoNotReleaseWindow(t *testing.T) {
	f := newFixture(t)
	var released atomic.Int32
	s := newClient(t, f, func(o *Options) { o.OnCompletion = func() { released.Add(1) } })
	loginClient(t, s, f)
	f.mu.Lock()
	f.statusCode = 403
	f.onWrite = func(w http.ResponseWriter, r *http.Request, _ writeRecord, _ int) bool {
		http.Error(w, "unconfirmed", 503)
		return true
	}
	f.mu.Unlock()
	if _, err := s.Submit(map[string]string{"q1": "pending", "q2": ""}, 0); err != nil {
		t.Fatal(err)
	}
	waitView(t, s, func(v View) bool { return v.SubmitError != "" && v.TimingStale })
	time.Sleep(30 * time.Millisecond)
	if released.Load() != 0 {
		t.Fatal("unconfirmed state released native deterrents")
	}
	if err := s.CloseCompletedWindow(); err == nil {
		t.Fatal("unconfirmed close allowed")
	}
}
