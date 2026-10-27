package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"exam-platform/internal/domain"
	"exam-platform/internal/service"
	"exam-platform/internal/storage"
)

func heartbeat(t *testing.T, f *fixture, token string) {
	t.Helper()
	result, err := f.sessions.Heartbeat(t.Context(), token, "student-pc", "192.0.2.1", domain.Sound{})
	if err != nil || !result.ServerTime.Equal(f.clock.Now()) {
		t.Fatalf("heartbeat = %+v, %v", result, err)
	}
}
func presence(t *testing.T, f *fixture, sessionID string) domain.Presence {
	t.Helper()
	result, err := f.sessions.Presence(t.Context(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func clientEvent(t *testing.T, f *fixture, token, eventType string) service.EventResult {
	t.Helper()
	result, err := f.sessions.ClientEvent(t.Context(), token, service.EventInput{Type: eventType})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestPersistentPresenceStrictBoundariesAndHistory(t *testing.T) {
	f := newFixture(t, 0)
	login := f.login(t, "0012")
	initial := presence(t, f, login.SessionID)
	if initial.Connectivity != "no_heartbeat" || initial.LastSeen != nil || initial.HeartbeatAgeMillis != nil || initial.Overdue || initial.Focus.State != domain.FocusUnknown {
		t.Fatalf("initial presence = %+v", initial)
	}
	missing, err := f.sessions.StudentsWithoutSession(t.Context(), testExamID)
	if err != nil || len(missing) != 1 || missing[0].AccountNumber != "0034" {
		t.Fatal("unstarted roster students confused with sessions without heartbeats")
	}
	heartbeat(t, f, login.Token)
	baseline := f.count(t, `SELECT count(*) FROM event_log`)
	for _, elapsed := range []time.Duration{15 * time.Second, 30 * time.Second, 30*time.Second + time.Nanosecond} {
		f.clock.Set(epoch.Add(elapsed))
		snapshot := presence(t, f, login.SessionID)
		if snapshot.MissedIntervals != int64(elapsed/(15*time.Second)) || snapshot.Overdue != (elapsed > 30*time.Second) || snapshot.HeartbeatAgeMillis == nil {
			t.Fatalf("boundary = %+v", snapshot)
		}
	}
	for range 20 {
		presence(t, f, login.SessionID)
	}
	if f.count(t, `SELECT count(*) FROM event_log`) != baseline || f.storedSession(t, login.Token).NetworkGapSince != nil {
		t.Fatal("observational reads mutated gap history")
	}
	report, err := f.sessions.Reconcile(t.Context())
	if err != nil || report.NetworkGaps != 1 {
		t.Fatalf("scan = %+v, %v", report, err)
	}
	for range 10 {
		if report, err := f.sessions.Reconcile(t.Context()); err != nil || report.NetworkGaps != 0 {
			t.Fatal("duplicate gap scan")
		}
	}
	f.clock.Set(epoch.Add(45 * time.Second))
	heartbeat(t, f, login.Token)
	snapshot := presence(t, f, login.SessionID)
	if snapshot.Overdue || !snapshot.EverOverdue || snapshot.MissedIntervals != 0 || snapshot.Hostname != "student-pc" || snapshot.RemoteAddr != "192.0.2.1" || snapshot.Focus.State != domain.FocusUnknown {
		t.Fatalf("reconnected = %+v", snapshot)
	}
	for range 20 {
		heartbeat(t, f, login.Token)
	}
	if f.count(t, `SELECT count(*) FROM event_log WHERE event_type='network_connected'`) != 1 || f.count(t, `SELECT count(*) FROM event_log WHERE event_type='network_gap'`) != 1 || f.count(t, `SELECT count(*) FROM event_log WHERE event_type='network_reconnected'`) != 1 {
		t.Fatal("heartbeat rows inserted periodically or transitions duplicated")
	}
	var gap, overdue int64
	if err := f.raw.QueryRow(`SELECT json_extract(detail,'$.gap_duration_ms'), json_extract(detail,'$.overdue_duration_ms') FROM event_log WHERE event_type='network_reconnected'`).Scan(&gap, &overdue); err != nil {
		t.Fatal(err)
	}
	if gap != 45000 || overdue != 15000 {
		t.Fatal("gap timing not reconstructable")
	}
}

func TestHeartbeatCatchesGapWithoutScanAndResetsFocus(t *testing.T) {
	f := newFixture(t, 0)
	login := f.login(t, "0012")
	heartbeat(t, f, login.Token)
	clientEvent(t, f, login.Token, "focus_lost")
	f.clock.Set(epoch.Add(35 * time.Second))
	heartbeat(t, f, login.Token)
	if f.count(t, `SELECT count(*) FROM event_log WHERE event_type IN ('network_gap','network_reconnected')`) != 2 || presence(t, f, login.SessionID).Focus.State != domain.FocusUnknown {
		t.Fatal("late heartbeat lost gap/reconnection ambiguity")
	}
	clientEvent(t, f, login.Token, "focus_regained")
	var detail string
	if err := f.raw.QueryRow(`SELECT detail FROM event_log WHERE event_type='focus_regained'`).Scan(&detail); err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(detail), &data); err != nil {
		t.Fatal(err)
	}
	if data["unmatched"] != true || data["unfocused_duration_ms"] != nil {
		t.Fatal("reconnection fabricated an uninterrupted focus span")
	}
}

func TestFocusSpansDuplicatesAndClientClockSkew(t *testing.T) {
	f := newFixture(t, 0)
	login := f.login(t, "0012")
	heartbeat(t, f, login.Token)
	loss, err := f.sessions.ClientEvent(t.Context(), login.Token, service.EventInput{Type: "focus_lost", Detail: json.RawMessage(`{"timestamp":"2099-01-01T00:00:00Z","session_id":"other","unfocused_duration_ms":999999}`)})
	if err != nil || loss.EventLogID <= 0 {
		t.Fatal("loss event failed")
	}
	f.clock.Set(epoch.Add(10 * time.Second))
	duplicate := clientEvent(t, f, login.Token, "focus_lost")
	if !duplicate.Duplicate || duplicate.EventLogID != loss.EventLogID {
		t.Fatal("duplicate loss restarted an incident")
	}
	f.clock.Set(epoch.Add(15 * time.Second))
	heartbeat(t, f, login.Token)
	for _, elapsed := range []time.Duration{20 * time.Second, 20*time.Second + time.Nanosecond} {
		f.clock.Set(epoch.Add(elapsed))
		snapshot := presence(t, f, login.SessionID)
		if snapshot.Focus.State != domain.FocusUnfocused || snapshot.Focus.ThresholdExceeded != (elapsed > 20*time.Second) || snapshot.Focus.IncidentID == nil || *snapshot.Focus.IncidentID != loss.EventLogID || snapshot.Overdue {
			t.Fatalf("focus = %+v", snapshot.Focus)
		}
	}
	f.clock.Set(epoch.Add(25 * time.Second))
	regain, err := f.sessions.ClientEvent(t.Context(), login.Token, service.EventInput{Type: "focus_regained", Detail: json.RawMessage(`{"timestamp":"2001-01-01T00:00:00Z","unfocused_duration_ms":1}`)})
	if err != nil {
		t.Fatal(err)
	}
	if duplicate := clientEvent(t, f, login.Token, "focus_regained"); !duplicate.Duplicate || duplicate.EventLogID != regain.EventLogID {
		t.Fatal("duplicate regain inserted another row")
	}
	var duration int64
	var occurred string
	if err := f.raw.QueryRow(`SELECT json_extract(detail,'$.unfocused_duration_ms'), occurred_at FROM event_log WHERE id=?`, regain.EventLogID).Scan(&duration, &occurred); err != nil {
		t.Fatal(err)
	}
	serverTime, err := time.Parse(time.RFC3339Nano, occurred)
	if err != nil {
		t.Fatal(err)
	}
	if duration != 25000 || !serverTime.Equal(f.clock.Now()) || presence(t, f, login.SessionID).Focus.State != domain.FocusFocused {
		t.Fatal("client metadata controlled focus timing")
	}
	clientEvent(t, f, login.Token, "app_closed")
	if presence(t, f, login.SessionID).Focus.State != domain.FocusUnknown {
		t.Fatal("closure fabricated focused state")
	}
	if f.count(t, `SELECT count(*) FROM event_log WHERE event_type='focus_lost'`) != 1 || f.count(t, `SELECT count(*) FROM event_log WHERE event_type='focus_regained'`) != 1 {
		t.Fatal("duplicate focus rows")
	}
}

func TestUnknownFocusBeforeHeartbeatAndApplicationRestart(t *testing.T) {
	f := newFixture(t, 0)
	login := f.login(t, "0012")
	clientEvent(t, f, login.Token, "focus_regained")
	if presence(t, f, login.SessionID).Focus.State != domain.FocusUnknown {
		t.Fatal("unconfirmed connection was assumed focused")
	}
	heartbeat(t, f, login.Token)
	if presence(t, f, login.SessionID).Focus.State != domain.FocusUnknown {
		t.Fatal("first heartbeat assumed focus")
	}
	regain := clientEvent(t, f, login.Token, "focus_regained")
	if regain.Duplicate || presence(t, f, login.SessionID).Focus.State != domain.FocusFocused {
		t.Fatal("fresh regain not observed")
	}
	clientEvent(t, f, login.Token, "app_started")
	if presence(t, f, login.SessionID).Focus.State != domain.FocusUnknown {
		t.Fatal("application restart retained stale focus")
	}
	clientEvent(t, f, login.Token, "focus_lost")
	if _, err := f.sessions.Heartbeat(t.Context(), login.Token, "another-pc", "192.0.2.2", domain.Sound{}); err != nil {
		t.Fatal(err)
	}
	if presence(t, f, login.SessionID).Focus.State != domain.FocusUnknown || f.count(t, `SELECT count(*) FROM event_log WHERE event_type='connection_changed'`) != 1 {
		t.Fatal("connection metadata change retained stale focus")
	}
}

func TestPresenceRestartAndTerminalHistory(t *testing.T) {
	f := newFixture(t, 0)
	login := f.login(t, "0012")
	heartbeat(t, f, login.Token)
	clientEvent(t, f, login.Token, "focus_lost")
	f.clock.Set(epoch.Add(31 * time.Second))
	if _, err := f.sessions.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Close(); err != nil {
		t.Fatal(err)
	}
	var err error
	f.store, err = storage.Open(t.Context(), f.path)
	if err != nil {
		t.Fatal(err)
	}
	f.sessions = testSessions(f.store, f.clock)
	if report, err := f.sessions.Reconcile(t.Context()); err != nil || report.NetworkGaps != 0 {
		t.Fatal("restart duplicated an open gap")
	}
	snapshot := presence(t, f, login.SessionID)
	if !snapshot.Overdue || !snapshot.EverOverdue || snapshot.Focus.State != domain.FocusUnknown || snapshot.LastSeen == nil || !snapshot.LastSeen.Equal(epoch) {
		t.Fatal("restart lost presence/ambiguity")
	}
	heartbeat(t, f, login.Token)
	if _, err := f.sessions.Submit(t.Context(), login.Token, domain.Answers{}); err != nil {
		t.Fatal(err)
	}
	f.clock.Set(epoch.Add(time.Hour))
	before := f.count(t, `SELECT count(*) FROM event_log`)
	for range 10 {
		presence(t, f, login.SessionID)
		if _, err := f.sessions.Reconcile(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if f.count(t, `SELECT count(*) FROM event_log`) != before {
		t.Fatal("terminal reads/scans fabricated disconnections")
	}
	if snapshot := presence(t, f, login.SessionID); snapshot.Monitoring || snapshot.Overdue || snapshot.Connectivity != "terminal" || !snapshot.EverOverdue {
		t.Fatal("terminal connectivity not distinguished")
	}
	if _, err := f.sessions.Heartbeat(t.Context(), login.Token, "pc", "192.0.2.1", domain.Sound{}); !errors.Is(err, service.ErrSessionLocked) {
		t.Fatal("terminal heartbeat updated state")
	}
	if _, err := f.sessions.ClientEvent(t.Context(), login.Token, service.EventInput{Type: "app_closed"}); !errors.Is(err, service.ErrSessionLocked) {
		t.Fatal("terminal app-close exception invented")
	}
}

func TestHeartbeatScanConcurrencyProducesOneGapCycle(t *testing.T) {
	f := newFixture(t, 0)
	login := f.login(t, "0012")
	heartbeat(t, f, login.Token)
	f.clock.Set(epoch.Add(31 * time.Second))
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for range 32 {
		wg.Go(func() { _, err := f.sessions.Reconcile(t.Context()); errs <- err })
		wg.Go(func() {
			_, err := f.sessions.Heartbeat(t.Context(), login.Token, "student-pc", "192.0.2.1", domain.Sound{})
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if f.count(t, `SELECT count(*) FROM event_log WHERE event_type='network_gap'`) != 1 || f.count(t, `SELECT count(*) FROM event_log WHERE event_type='network_reconnected'`) != 1 {
		t.Fatal("race duplicated or lost transition")
	}
}

func TestScheduledPresenceScanWithoutDashboard(t *testing.T) {
	f := newFixture(t, 0)
	login := f.login(t, "0012")
	heartbeat(t, f, login.Token)
	f.clock.Set(epoch.Add(31 * time.Second))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	reports := make(chan service.ReconcileReport, 16)
	done := make(chan error, 1)
	go func() {
		done <- f.sessions.RunWorker(ctx, 5*time.Millisecond, func(r service.ReconcileReport) {
			select {
			case reports <- r:
			case <-ctx.Done():
			}
		})
	}()
	select {
	case r := <-reports:
		if r.NetworkGaps != 1 {
			t.Fatalf("initial presence scan = %+v", r)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no scheduled gap")
	}
	f.clock.Set(epoch.Add(32 * time.Second))
	heartbeat(t, f, login.Token)
	f.clock.Set(epoch.Add(63 * time.Second))
	deadline := time.After(3 * time.Second)
	for {
		select {
		case r := <-reports:
			if r.NetworkGaps == 1 {
				cancel()
				select {
				case err := <-done:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(time.Second):
					t.Fatal("worker did not stop")
				}
				if f.count(t, `SELECT count(*) FROM event_log WHERE event_type='network_gap'`) != 2 {
					t.Fatal("periodic scan not persisted")
				}
				return
			}
		case <-deadline:
			t.Fatal("periodic presence scan absent")
		}
	}
}

func TestPresencePolicyAndExpiredCatchup(t *testing.T) {
	f := newFixture(t, 0)
	policy := domain.Policy{HeartbeatInterval: 5 * time.Second, OverdueThreshold: 40 * time.Second, FocusThreshold: 10 * time.Second}
	policy.AlertInterval, policy.CaptureInterval = 2*time.Second, 10*time.Second
	policy.IncidentCaptures, policy.SessionCaptures = 30, 200
	policy.LockAwayAfter, policy.LockNoContactAfter = 0, 0
	// The exam is published again with these settings, so the sessions that follow follow them too.
	storeSettings(f.store, f.clock, policy)
	f.publish(t, testExamID)
	var err error
	f.sessions, err = service.NewSessionsWithLimits(f.store, f.clock, service.DefaultAnswerLimits())
	if err != nil {
		t.Fatal(err)
	}
	// A session takes its threshold at login, so log in under the policy being tested.
	login := f.login(t, "0012")
	heartbeat(t, f, login.Token)
	clientEvent(t, f, login.Token, "focus_lost")
	f.clock.Set(epoch.Add(20 * time.Second))
	if snapshot := presence(t, f, login.SessionID); snapshot.Overdue || !snapshot.Focus.ThresholdExceeded || snapshot.MissedIntervals != 4 {
		t.Fatal("focus/overdue thresholds conflated")
	}
	f.clock.Set(epoch.Add(40 * time.Second))
	if presence(t, f, login.SessionID).Overdue {
		t.Fatal("strict custom overdue boundary violated")
	}
	f.clock.Set(epoch.Add(100 * time.Second))
	if _, err := f.sessions.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if f.count(t, `SELECT count(*) FROM event_log WHERE event_type='network_gap'`) != 1 || f.storedSession(t, login.Token).Status != domain.Submitted {
		t.Fatal("restart after expiry omitted pre-terminal gap")
	}
	var observed string
	if err := f.raw.QueryRow(`SELECT json_extract(detail,'$.observed_until') FROM event_log WHERE event_type='network_gap'`).Scan(&observed); err != nil {
		t.Fatal(err)
	}
	until, err := time.Parse(time.RFC3339Nano, observed)
	if err != nil || !until.Equal(epoch.Add(90*time.Second)) {
		t.Fatal("gap extended beyond terminal boundary")
	}
}

// A finished session is no longer monitored: its heartbeat age and missed intervals are frozen at its end, however long
// the proctor keeps looking at it.
func TestHeartbeatCountersStopWhenTheSessionEnds(t *testing.T) {
	for _, end := range []string{"submit", "force", "expiry"} {
		t.Run(end, func(t *testing.T) {
			f := newFixture(t, 0)
			login := f.login(t, "0012")
			heartbeat(t, f, login.Token)
			switch end {
			case "submit":
				f.clock.Set(epoch.Add(20 * time.Second))
				if _, err := f.sessions.Submit(t.Context(), login.Token, domain.Answers{}); err != nil {
					t.Fatal(err)
				}
			case "force":
				f.clock.Set(epoch.Add(20 * time.Second))
				if _, err := f.sessions.ForceSubmit(t.Context(), login.SessionID, "proctor decision"); err != nil {
					t.Fatal(err)
				}
			case "expiry":
				// The fixture's exam lasts 60 s plus 30 s of grace; keep heartbeats until the deadline.
				for at := 15 * time.Second; at <= 60*time.Second; at += 15 * time.Second {
					f.clock.Set(epoch.Add(at))
					heartbeat(t, f, login.Token)
				}
			}
			want := f.storedSession(t, login.Token).GraceUntil
			if submitted := f.storedSession(t, login.Token).SubmittedAt; submitted != nil {
				want = *submitted
			}
			last := *presence(t, f, login.SessionID).LastSeen
			for _, later := range []time.Duration{time.Hour, 5 * time.Hour} {
				f.clock.Set(epoch.Add(later))
				snapshot := presence(t, f, login.SessionID)
				age := want.Sub(last)
				if snapshot.Monitoring || snapshot.HeartbeatAgeMillis == nil || *snapshot.HeartbeatAgeMillis != age.Milliseconds() || snapshot.MissedIntervals != int64(age/(15*time.Second)) {
					t.Fatalf("after %v: age %v, missed %d, want age %v", later, *snapshot.HeartbeatAgeMillis, snapshot.MissedIntervals, age)
				}
				list, err := f.sessions.ProctorSessions(t.Context(), testExamID)
				if err != nil || len(list.Sessions) != 1 || list.Sessions[0].Presence.MissedIntervals != snapshot.MissedIntervals || list.Sessions[0].NetworkWarning {
					t.Fatalf("roster after %v = %+v, %v", later, list.Sessions, err)
				}
			}
		})
	}
}
