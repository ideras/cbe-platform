package client

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The student application locks the exam itself when its own measurement of the absence reaches the limit, even offline.
func TestTheApplicationLocksItselfAfterAnAbsence(t *testing.T) {
	f := newFixture(t)
	f.status.Policy = &Policy{HeartbeatMillis: 40, OverdueMillis: 80, FocusMillis: 40, AlertMillis: 30, CaptureMillis: 1000,
		LockAwayAfterMillis: 200, LockNoContactAfterMillis: 0, IncidentCaptures: 30, SessionCaptures: 200}
	s := monitorClient(t, f, nil)
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	// The student leaves the exam window and stays away.
	ReportNativeFocus(s, false)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.Locked })
	if s.Snapshot().Monitoring.LockReason != "away" {
		t.Fatalf("lock reason = %q", s.Snapshot().Monitoring.LockReason)
	}
	// The server is told about the lock.
	waitUntil(t, func() bool { return containsEvent(telemetryTypes(f), "away_lock") })
}

// The alarm of the absence that caused the lock keeps going until the student comes back to the window.
func TestTheAlarmContinuesUntilTheStudentReturns(t *testing.T) {
	f := newFixture(t)
	f.status.Policy = &Policy{HeartbeatMillis: 40, OverdueMillis: 80, FocusMillis: 40, AlertMillis: 30, CaptureMillis: 1000,
		LockAwayAfterMillis: 150, LockNoContactAfterMillis: 0, IncidentCaptures: 30, SessionCaptures: 200}
	var alerts atomic.Int64
	s := monitorClient(t, f, func(o *Options) { o.Monitoring.OnAlert = func(Incident) { alerts.Add(1) } })
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	ReportNativeFocus(s, false)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.Locked })
	before := alerts.Load()
	time.Sleep(200 * time.Millisecond)
	if alerts.Load() <= before {
		t.Fatal("the alarm stopped when the exam was locked")
	}
	// The student comes back: the alarm stops.
	ReportNativeFocus(s, true)
	time.Sleep(120 * time.Millisecond)
	quiet := alerts.Load()
	time.Sleep(200 * time.Millisecond)
	if alerts.Load() != quiet {
		t.Fatal("the alarm kept going after the student returned")
	}
}

// The application locks itself when it cannot reach the server for long enough.
func TestTheApplicationLocksItselfWithoutContact(t *testing.T) {
	f := newFixture(t)
	f.status.Policy = &Policy{HeartbeatMillis: 40, OverdueMillis: 80, FocusMillis: 40, AlertMillis: 30, CaptureMillis: 1000,
		LockAwayAfterMillis: 0, LockNoContactAfterMillis: 200, IncidentCaptures: 30, SessionCaptures: 200}
	// Nothing reaches the server any more: the heartbeat and the events fail.
	f.onTelemetry = func(w http.ResponseWriter, _ *http.Request, _ telemetryRecord) bool {
		http.Error(w, "server down", 500)
		return true
	}
	s := monitorClient(t, f, nil)
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.Locked })
	if s.Snapshot().Monitoring.LockReason != "no_contact" {
		t.Fatalf("lock reason = %q", s.Snapshot().Monitoring.LockReason)
	}
}

// A refusal because the exam is locked never loses the answers: they are sent after the unlock.
func TestALockedExamKeepsTheAnswersUntilTheUnlock(t *testing.T) {
	f := newFixture(t)
	f.status.Policy = &Policy{HeartbeatMillis: 40, OverdueMillis: 80, FocusMillis: 40, AlertMillis: 30, CaptureMillis: 1000,
		LockAwayAfterMillis: 0, LockNoContactAfterMillis: 0, IncidentCaptures: 30, SessionCaptures: 200}
	// The server refuses the writes because the exam is locked.
	f.onWrite = func(w http.ResponseWriter, _ *http.Request, _ writeRecord, _ int) bool {
		http.Error(w, "the exam is locked", 423)
		return true
	}
	s := monitorClient(t, f, nil)
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	if _, err := s.UpdateAnswers(map[string]string{"q1": "kept", "q2": "also kept"}, 1); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, func() bool { return s.Snapshot().Locked != nil })
	if s.Snapshot().Revision != 1 {
		t.Fatalf("the local revision was lost: %+v", s.Snapshot().Revision)
	}
	// The proctor unlocks the exam: the answers are sent and the exam comes back.
	f.onWrite = nil
	f.mu.Lock()
	f.status.Locked = nil
	f.mu.Unlock()
	waitUntil(t, func() bool { return s.Snapshot().SaveState == "saved" })
	// The answers that were refused are sent after the unlock.
	f.mu.Lock()
	writes := len(f.writes)
	last := f.writes[writes-1]
	f.mu.Unlock()
	if last.answers["q1"] != "kept" || last.answers["q2"] != "also kept" {
		t.Fatalf("the answers were not sent after the unlock: %+v", last.answers)
	}
	if s.Snapshot().Locked != nil {
		t.Fatal("the exam is still locked")
	}
	if s.Snapshot().UnlockNote == "" {
		t.Fatal("the unlock was not reported to the student")
	}
}

// lockPolicy is a fast policy with the given lock times, in milliseconds (0 turns a lock off).
func lockPolicy(away, noContact int64) *Policy {
	return &Policy{HeartbeatMillis: 40, OverdueMillis: 80, FocusMillis: 40, AlertMillis: 30, CaptureMillis: 50,
		LockAwayAfterMillis: away, LockNoContactAfterMillis: noContact, IncidentCaptures: 30, SessionCaptures: 200}
}

// setServerLock makes the fake server's status report a lock, or none.
func setServerLock(f *fixture, reason string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if reason == "" {
		f.status.Locked = nil
		return
	}
	f.status.Locked = &Lock{Since: time.Now(), Reason: reason}
}

// stays checks that the condition holds for the whole period.
func stays(t *testing.T, s *Service, period time.Duration, check func(View) bool, problem string) {
	t.Helper()
	until := time.Now().Add(period)
	for time.Now().Before(until) {
		if !check(s.Snapshot()) {
			t.Fatalf("%s: %+v", problem, s.Snapshot())
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// The no-contact clock starts at the login: a student who waited on the login page longer than the limit is not locked
// the moment they log in.
func TestALateLoginIsNotLockedAtOnce(t *testing.T) {
	f := newFixture(t)
	f.status.Policy = lockPolicy(0, 150)
	s := monitorClient(t, f, nil)
	ReportNativeFocus(s, true)
	time.Sleep(200 * time.Millisecond)
	loginClient(t, s, f)
	stays(t, s, 100*time.Millisecond, func(v View) bool { return !v.Monitoring.Locked && v.Locked == nil }, "locked right after the login")
	if containsEvent(telemetryTypes(f), "away_lock") {
		t.Fatal("a lock was reported right after the login")
	}
}

// The application's own lock hides the exam and refuses the answers at once, before the server knows about it.
func TestTheApplicationsOwnLockHidesTheExam(t *testing.T) {
	f := newFixture(t)
	f.status.Policy = lockPolicy(0, 150)
	// The heartbeat and the events cannot reach the server any more.
	f.onTelemetry = func(w http.ResponseWriter, _ *http.Request, _ telemetryRecord) bool {
		http.Error(w, "server down", 500)
		return true
	}
	s := monitorClient(t, f, nil)
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	view := waitView(t, s, func(v View) bool { return v.Locked != nil })
	if view.Locked.Reason != "no_contact" {
		t.Fatalf("lock = %+v", view.Locked)
	}
	if _, err := s.UpdateAnswers(map[string]string{"q1": "typed while locked", "q2": ""}, 1); err == nil || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("an answer was taken while the exam was locked: %v", err)
	}
}

// After a network outage the server has locked the session before the application's own report gets through, and the
// report may never get through. The lock the server's status reports is the one a proctor lifts: the application leaves
// it, and the next absence is captured again.
func TestTheServersLockConfirmsTheApplicationsOwnAndItsUnlockEndsIt(t *testing.T) {
	f := newFixture(t)
	f.status.Policy = lockPolicy(0, 150)
	var offline atomic.Bool
	offline.Store(true)
	f.onTelemetry = func(w http.ResponseWriter, _ *http.Request, record telemetryRecord) bool {
		if offline.Load() || record.kind == "away_lock" {
			http.Error(w, "unreachable", 500)
			return true
		}
		return false
	}
	s := monitorClient(t, f, nil)
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.Locked })
	// The network comes back; the server had locked the session on its own.
	offline.Store(false)
	setServerLock(f, "no_contact")
	waitView(t, s, func(v View) bool { return v.Locked != nil && v.Phase == "locked" })
	// The proctor unlocks.
	setServerLock(f, "")
	view := waitView(t, s, func(v View) bool { return !v.Monitoring.Locked && v.Locked == nil && v.Phase == "exam" })
	if view.UnlockNote == "" {
		t.Fatal("the unlock was not reported to the student")
	}
	// The next absence is captured again.
	ReportNativeFocus(s, false)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.Incident != nil && v.Incident.Captures > 0 })
}

// A second lock of the application stays until the server reports it: a status read before the server heard of it does
// not unlock the exam.
func TestASecondLockWaitsForTheServer(t *testing.T) {
	f := newFixture(t)
	f.status.Policy = lockPolicy(200, 0)
	var reports atomic.Bool
	reports.Store(true)
	f.onTelemetry = func(w http.ResponseWriter, _ *http.Request, record telemetryRecord) bool {
		if record.kind == "away_lock" && !reports.Load() {
			http.Error(w, "unreachable", 500)
			return true
		}
		return false
	}
	s := monitorClient(t, f, nil)
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	// The first lock is reported, confirmed and lifted.
	ReportNativeFocus(s, false)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.Locked })
	setServerLock(f, "away")
	waitView(t, s, func(v View) bool { return v.Phase == "locked" })
	ReportNativeFocus(s, true)
	setServerLock(f, "")
	waitView(t, s, func(v View) bool { return !v.Monitoring.Locked && v.Phase == "exam" })
	// The second lock's report does not get through; the server's status keeps saying the exam is open.
	reports.Store(false)
	ReportNativeFocus(s, false)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.Locked })
	stays(t, s, 150*time.Millisecond, func(v View) bool { return v.Monitoring.Locked && v.Locked != nil }, "the second lock ended before the server reported it")
	// The report is sent again until it gets through.
	reports.Store(true)
	waitUntil(t, func() bool {
		count := 0
		for _, kind := range telemetryTypes(f) {
			if kind == "away_lock" {
				count++
			}
		}
		return count >= 3
	})
}

// After an unlock the absence counts from zero: a student still away is locked again only once the away time has passed
// since the unlock.
func TestAfterAnUnlockTheAbsenceCountsFromTheUnlock(t *testing.T) {
	f := newFixture(t)
	f.status.Policy = lockPolicy(250, 0)
	s := monitorClient(t, f, nil)
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	ReportNativeFocus(s, false)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.Locked })
	setServerLock(f, "away")
	waitView(t, s, func(v View) bool { return v.Phase == "locked" })
	// The proctor unlocks while the student is still away.
	setServerLock(f, "")
	waitMonitor(t, s, func(v MonitoringView) bool { return !v.Locked })
	stays(t, s, 120*time.Millisecond, func(v View) bool { return !v.Monitoring.Locked }, "locked again at once after the unlock")
	waitMonitor(t, s, func(v MonitoringView) bool { return v.Locked })
}

// While the exam is locked, a new absence starts neither the alarm nor the screenshots, whoever locked the exam.
func TestALaterAbsenceIsSilentWhileTheServerHasTheExamLocked(t *testing.T) {
	f := newFixture(t)
	f.status.Policy = lockPolicy(0, 0)
	var alerts atomic.Int64
	s := monitorClient(t, f, func(o *Options) { o.Monitoring.OnAlert = func(Incident) { alerts.Add(1) } })
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	// The server locks the exam while the student is at the window.
	setServerLock(f, "away")
	waitMonitor(t, s, func(v MonitoringView) bool { return v.Locked })
	ReportNativeFocus(s, false)
	time.Sleep(200 * time.Millisecond)
	if alerts.Load() != 0 {
		t.Fatalf("the alarm sounded %d times for an absence after the lock", alerts.Load())
	}
	if incident := s.Snapshot().Monitoring.Incident; incident != nil && incident.Captures != 0 {
		t.Fatalf("an absence after the lock was captured: %+v", incident)
	}
}

// When the time runs out while the exam is locked, the exam ends: the student sees the end, not an unlock.
func TestTheEndOfTheExamEndsTheLock(t *testing.T) {
	f := newFixture(t)
	f.status.Policy = lockPolicy(0, 0)
	s := monitorClient(t, f, nil)
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	setServerLock(f, "away")
	waitView(t, s, func(v View) bool { return v.Phase == "locked" })
	f.mu.Lock()
	f.status.Locked, f.status.State, f.status.SecondsRemaining = nil, "submitted", 0
	f.mu.Unlock()
	view := waitView(t, s, func(v View) bool { return v.Phase == "complete" })
	if view.Locked != nil || view.UnlockNote != "" {
		t.Fatalf("the end of the exam was taken for an unlock: %+v", view)
	}
}

// An application that started on the lock screen never had the questions: after the unlock it loads them, retrying until
// the server sends them.
func TestAnExamFirstSeenLockedIsLoadedAfterTheUnlock(t *testing.T) {
	f := newFixture(t)
	f.status.Policy = lockPolicy(0, 0)
	f.examCode = 423
	f.status.Locked = &Lock{Since: time.Now(), Reason: "no_contact"}
	s := monitorClient(t, f, nil)
	ReportNativeFocus(s, true)
	view := loginClient(t, s, f)
	if view.Phase != "locked" || view.Exam != nil || view.Locked == nil || view.Locked.Reason != "no_contact" {
		t.Fatalf("login on a locked exam = %+v", view)
	}
	// The proctor unlocks, but the first attempts to read the exam fail.
	f.mu.Lock()
	f.examCode = 500
	f.status.Locked = nil
	f.mu.Unlock()
	waitView(t, s, func(v View) bool { return v.Phase == "loading" && v.UnlockNote != "" })
	f.mu.Lock()
	f.examCode = 0
	f.mu.Unlock()
	waitView(t, s, func(v View) bool { return v.Phase == "exam" && v.Exam != nil })
}

// The note of an unlock is over once the exam is locked again.
func TestTheUnlockNoteEndsAtTheNextLock(t *testing.T) {
	f := newFixture(t)
	f.status.Policy = lockPolicy(0, 0)
	s := monitorClient(t, f, nil)
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	setServerLock(f, "away")
	waitView(t, s, func(v View) bool { return v.Phase == "locked" })
	setServerLock(f, "")
	waitView(t, s, func(v View) bool { return v.Phase == "exam" && v.UnlockNote != "" })
	setServerLock(f, "no_contact")
	view := waitView(t, s, func(v View) bool { return v.Phase == "locked" })
	if view.UnlockNote != "" || view.AddedSeconds != 0 {
		t.Fatalf("the lock screen still shows the last unlock: %+v", view)
	}
}

// containsEvent says whether the telemetry holds an event of that kind.
func containsEvent(kinds []string, kind string) bool {
	for _, name := range kinds {
		if name == kind {
			return true
		}
	}
	return false
}

// waitUntil waits until the check says yes.
func waitUntil(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the condition was not met in time")
}
