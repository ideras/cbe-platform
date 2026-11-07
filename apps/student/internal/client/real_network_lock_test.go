package client

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

// The whole flow against the built server: the student loses the connection, locks the exam to change network, and the
// screenshots taken meanwhile are held. Once the connection is back the server has the lock, with who asked for it and which
// request it was, and the screenshots under its record. Nothing the student does lifts it: only the proctor's unlock does.
func TestRealServerNetworkLockHoldsAndStoresTheScreenshots(t *testing.T) {
	server := setupRealServer(t)
	exam := server.create(t, 600, 10)
	policy := fastPolicyWith(100)
	policy.LockNoContact = 60000 // the no-contact lock does not get in the way of the test
	policy.IncidentCaptures = 5
	stop := server.start(t, exam, policy)
	defer stop()

	// The student reaches the server through a connection the test can cut.
	target, err := url.Parse(server.base)
	if err != nil {
		t.Fatal(err)
	}
	var down atomic.Bool
	reverse := httputil.NewSingleHostReverseProxy(target)
	reverse.ErrorLog = log.New(io.Discard, "", 0)
	link := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if down.Load() {
			http.Error(w, "unreachable", http.StatusBadGateway)
			return
		}
		reverse.ServeHTTP(w, r)
	}))
	t.Cleanup(link.Close)

	picture := syntheticPNG(t)
	var taken atomic.Int64
	s := realClientWith(t, link.URL, func(o *Options) {
		o.Monitoring.Tick = 5 * time.Millisecond
		o.Screenshots.Timeout = 200 * time.Millisecond
		o.Screenshots.Capture = func(context.Context) ([]byte, error) { taken.Add(1); return bytes.Clone(picture), nil }
		o.Screenshots.Permission = func(bool) CapturePermission { return CapturePermission{Supported: true, Granted: true} }
	})
	ReportNativeFocus(s, true)
	if _, err := s.Login(t.Context(), link.URL, "00123", "synthetic PIN"); err != nil {
		t.Fatal(err)
	}
	waitMonitor(t, s, func(v MonitoringView) bool { return v.HeartbeatAt != nil })
	if v := s.Snapshot().Monitoring; v.NetworkLocksLeft != 2 {
		t.Fatalf("the login did not say how many network locks are left: %+v", v)
	}

	// The connection is lost, the student locks the exam, and the screenshots are held.
	down.Store(true)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.NetworkLockAvailable })
	if _, err := s.RequestNetworkLock(); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, func() bool { return s.Snapshot().Capture.Pending >= 2 })

	// The connection returns: the lock reaches the server, and the screenshots go under its record.
	down.Store(false)
	waitUntil(t, func() bool { return taken.Load() >= 3 && s.Snapshot().Capture.Pending == 0 })
	if s.Snapshot().Locked == nil {
		t.Fatal("the exam unlocked itself when the connection returned")
	}

	var list struct {
		Sessions []struct {
			Account string `json:"account_number"`
			ID      string `json:"session_id"`
		} `json:"sessions"`
	}
	server.admin(t, "GET", "/admin/sessions?exam_id="+exam, nil, &list, 200)
	if len(list.Sessions) != 1 || list.Sessions[0].Account != "00123" {
		t.Fatalf("sessions = %+v", list.Sessions)
	}
	id := list.Sessions[0].ID
	type lockHistory struct {
		Items []struct {
			At    time.Time `json:"occurred_at"`
			Event *struct {
				Type        string          `json:"event_type"`
				Detail      json.RawMessage `json:"detail"`
				Screenshots []struct {
					Event int64 `json:"event_log_id"`
				} `json:"screenshots"`
			} `json:"event"`
		} `json:"items"`
	}
	var history lockHistory
	server.admin(t, "GET", "/admin/session/"+id+"/timeline", nil, &history, 200)
	locks := 0
	for _, item := range history.Items {
		if item.Event == nil || item.Event.Type != "locked" {
			continue
		}
		locks++
		var detail struct {
			Reason  string `json:"reason"`
			Source  string `json:"source"`
			Request int    `json:"request"`
			Limit   int    `json:"limit"`
			// Since is when the student asked for the lock, which the application said when the connection returned.
			Since time.Time `json:"since"`
		}
		if err := json.Unmarshal(item.Event.Detail, &detail); err != nil {
			t.Fatal(err)
		}
		if detail.Reason != "network" || detail.Source != "client" || detail.Request != 1 || detail.Limit != 2 {
			t.Fatalf("lock detail = %+v", detail)
		}
		// The server heard of the lock when the connection returned, some time after the student asked for it.
		if gap := item.At.Sub(detail.Since); detail.Since.IsZero() || gap < 100*time.Millisecond || gap > time.Minute {
			t.Fatalf("the lock began %v before the server recorded it (since %v, recorded %v)", gap, detail.Since, item.At)
		}
		if len(item.Event.Screenshots) < 2 {
			t.Fatalf("the lock's screenshots on its row of the history = %d", len(item.Event.Screenshots))
		}
	}
	if locks != 1 {
		t.Fatalf("locks in the history = %d", locks)
	}

	// The status reports one request left, and a proctor's unlock is the only way out.
	waitUntil(t, func() bool { return s.Snapshot().Monitoring.NetworkLocksLeft == 1 })
	server.admin(t, "POST", "/admin/session/"+id+"/unlock", map[string]any{"reason": "back on the exam network"}, nil, 200)
	waitUntil(t, func() bool { return s.Snapshot().Locked == nil })
	if s.Snapshot().UnlockNote == "" {
		t.Fatal("the unlock was not reported to the student")
	}
}

// The lock for lack of contact, against the built server: the connection is cut and not restored for longer than the policy
// allows. The application locks the exam, and so does the server, whichever is first. The screenshots taken meanwhile are held
// and, once the connection is back, are stored under the record of the lock and show on its row of the history.
func TestRealServerLockForLackOfContactStoresTheScreenshots(t *testing.T) {
	server := setupRealServer(t)
	exam := server.create(t, 600, 10)
	policy := fastPolicyWith(100)
	policy.LockNoContact = 700
	policy.IncidentCaptures = 5
	stop := server.start(t, exam, policy)
	defer stop()

	target, err := url.Parse(server.base)
	if err != nil {
		t.Fatal(err)
	}
	var down atomic.Bool
	reverse := httputil.NewSingleHostReverseProxy(target)
	reverse.ErrorLog = log.New(io.Discard, "", 0)
	link := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if down.Load() {
			http.Error(w, "unreachable", http.StatusBadGateway)
			return
		}
		reverse.ServeHTTP(w, r)
	}))
	t.Cleanup(link.Close)

	picture := syntheticPNG(t)
	var taken atomic.Int64
	s := realClientWith(t, link.URL, func(o *Options) {
		o.Monitoring.Tick = 5 * time.Millisecond
		o.Screenshots.Timeout = 200 * time.Millisecond
		o.Screenshots.Capture = func(context.Context) ([]byte, error) { taken.Add(1); return bytes.Clone(picture), nil }
		o.Screenshots.Permission = func(bool) CapturePermission { return CapturePermission{Supported: true, Granted: true} }
	})
	ReportNativeFocus(s, true)
	if _, err := s.Login(t.Context(), link.URL, "00123", "synthetic PIN"); err != nil {
		t.Fatal(err)
	}
	waitMonitor(t, s, func(v MonitoringView) bool { return v.HeartbeatAt != nil })

	// Nothing reaches the server for longer than the policy allows: the exam locks, and the screenshots are held.
	down.Store(true)
	waitUntil(t, func() bool { return s.Snapshot().Locked != nil })
	if lock := s.Snapshot().Locked; lock.Reason != "no_contact" {
		t.Fatalf("lock = %+v", lock)
	}
	waitUntil(t, func() bool { return s.Snapshot().Capture.Pending >= 2 })

	// The connection returns: the lock is on the server's record, whoever made it, and the screenshots go under it.
	down.Store(false)
	waitUntil(t, func() bool { return taken.Load() >= 3 && s.Snapshot().Capture.Pending == 0 })
	if s.Snapshot().Locked == nil {
		t.Fatal("the exam unlocked itself when the connection returned")
	}

	var list struct {
		Sessions []struct {
			ID string `json:"session_id"`
		} `json:"sessions"`
	}
	server.admin(t, "GET", "/admin/sessions?exam_id="+exam, nil, &list, 200)
	if len(list.Sessions) != 1 {
		t.Fatalf("sessions = %+v", list.Sessions)
	}
	id := list.Sessions[0].ID
	var history struct {
		Items []struct {
			Event *struct {
				Type        string          `json:"event_type"`
				Detail      json.RawMessage `json:"detail"`
				Screenshots []struct {
					Event int64 `json:"event_log_id"`
				} `json:"screenshots"`
			} `json:"event"`
		} `json:"items"`
	}
	server.admin(t, "GET", "/admin/session/"+id+"/timeline", nil, &history, 200)
	locks := 0
	for _, item := range history.Items {
		if item.Event == nil || item.Event.Type != "locked" {
			continue
		}
		locks++
		var detail struct {
			Reason string `json:"reason"`
		}
		if err := json.Unmarshal(item.Event.Detail, &detail); err != nil {
			t.Fatal(err)
		}
		if detail.Reason != "no_contact" {
			t.Fatalf("lock detail = %s", item.Event.Detail)
		}
		if len(item.Event.Screenshots) < 2 {
			t.Fatalf("the lock's screenshots on its row of the history = %d", len(item.Event.Screenshots))
		}
	}
	if locks != 1 {
		t.Fatalf("locks in the history = %d", locks)
	}
	server.admin(t, "POST", "/admin/session/"+id+"/unlock", map[string]any{"reason": "back on the exam network"}, nil, 200)
	waitUntil(t, func() bool { return s.Snapshot().Locked == nil })
}
