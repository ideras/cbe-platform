package dashboard

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSlowPollDoesNotBlockSnapshotAndShutdownCancels(t *testing.T) {
	var slow atomic.Bool
	entered := make(chan struct{}, 1)
	s, _ := start(t, func(w http.ResponseWriter, r *http.Request) {
		if slow.Load() {
			entered <- struct{}{}
			<-r.Context().Done()
			return
		}
		reply(w, roster())
	})
	slow.Store(true)
	s.Refresh()
	<-entered
	begin := time.Now()
	v := s.Snapshot()
	if time.Since(begin) > 100*time.Millisecond || !v.Loaded {
		t.Fatal("poll blocked local snapshot")
	}
	begin = time.Now()
	s.ServiceShutdown()
	if time.Since(begin) > 500*time.Millisecond {
		t.Fatal("shutdown failed to cancel network work")
	}
}
func TestBoundedMalformedResponsesRetainObservations(t *testing.T) {
	var mode atomic.Int32
	s, _ := start(t, func(w http.ResponseWriter, r *http.Request) {
		switch mode.Load() {
		case 1:
			w.Write([]byte("invalid " + secret))
		case 2:
			w.Write(bytes.Repeat([]byte("x"), (16<<20)+1))
		case 3:
			http.Error(w, secret, 401)
		default:
			reply(w, roster())
		}
	})
	for _, value := range []int32{1, 2, 3} {
		mode.Store(value)
		s.Refresh()
		wait(t, func() bool { return s.Snapshot().Stale })
		v := s.Snapshot()
		if !v.Loaded || len(v.Roster.Sessions) != 1 || strings.Contains(v.Error, secret) {
			t.Fatal("lost observations or leaked raw body")
		}
		mode.Store(0)
		s.Refresh()
		wait(t, func() bool { return !s.Snapshot().Stale })
	}
}
func TestPollStartedBeforeControlCannotPublishAfterItsAcknowledgment(t *testing.T) {
	entered := make(chan int, 2)
	oldDone := make(chan struct{})
	freshDone := make(chan struct{})
	var gets atomic.Int32
	var forced atomic.Bool
	s, _ := start(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			forced.Store(true)
			reply(w, map[string]any{"status": "force_submitted", "submitted_at": at, "final_version_id": 12, "version_seq": 3})
			return
		}
		count := gets.Add(1)
		rr := roster()
		if count == 2 {
			entered <- 2
			select {
			case <-oldDone:
			case <-r.Context().Done():
				return
			}
		} else if count == 3 {
			entered <- 3
			select {
			case <-freshDone:
			case <-r.Context().Done():
				return
			}
		}
		if count > 2 && forced.Load() {
			rr.Sessions[0].Status = "force_submitted"
			rr.Sessions[0].Final = &FinalSummary{VersionID: 12, Sequence: 3}
		}
		reply(w, rr)
	})
	defer close(freshDone)
	s.Refresh()
	if <-entered != 2 {
		t.Fatal("missing held poll")
	}
	if _, err := s.ForceSubmit(sessionID, "reason", true); err != nil {
		t.Fatal(err)
	}
	close(oldDone)
	if <-entered != 3 {
		t.Fatal("missing refresh")
	}
	v := s.Snapshot()
	if !v.Stale {
		t.Fatal("old pre-control poll falsely marked observations current")
	}
}
func TestRuntimeCredentialReuseIsHostScoped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+secret {
			t.Error("missing credential")
		}
		if serveAdminState(w, r) {
			return
		}
		rr := roster()
		rr.ExamID = r.URL.Query().Get("exam_id")
		rr.Sessions[0].ExamID = rr.ExamID
		reply(w, rr)
	}))
	defer server.Close()
	opts := DefaultOptions()
	opts.Server = server.URL
	opts.ExamID = examID
	opts.Choose = func(FileDialog) (string, error) { return writeToken(t, secret, 0600), nil }
	s, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer s.ServiceShutdown()
	if _, err := s.ChooseCredential(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Connect(server.URL); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool { return s.Snapshot().Loaded })
	if _, err := s.Connect(server.URL); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool { return s.Snapshot().Loaded })
	if _, err := s.Connect("http://different-host.invalid"); err == nil {
		t.Fatal("runtime credential silently moved to another host")
	}
}
