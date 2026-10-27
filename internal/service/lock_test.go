package service_test

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"exam-platform/internal/domain"
	"exam-platform/internal/service"
)

// lockSettings stores monitoring settings with the given lock times and publishes the exam again, so the exam freezes them.
// The exam's length is set too: a lock only matters while the exam is still running.
func lockSettings(t *testing.T, f *fixture, away, noContact time.Duration, durationSeconds int64) {
	t.Helper()
	policy := testPolicy()
	policy.FocusThreshold = 5 * time.Second
	policy.CaptureInterval = 5 * time.Second
	policy.LockAwayAfter, policy.LockNoContactAfter = away, noContact
	storeSettings(f.store, f.clock, policy)
	if _, err := f.raw.Exec(`UPDATE exams SET duration_seconds = ? WHERE exam_id = ?`, durationSeconds, testExamID); err != nil {
		t.Fatal(err)
	}
	f.publish(t, testExamID)
	var err error
	f.sessions, err = service.NewSessionsWithLimits(f.store, f.clock, service.DefaultAnswerLimits())
	if err != nil {
		t.Fatal(err)
	}
}

// keepInTouch heartbeats often enough that the server never sees a network gap.
func keepInTouch(t *testing.T, f *fixture, token string, until time.Duration) {
	t.Helper()
	for elapsed := 20 * time.Second; elapsed < until; elapsed += 20 * time.Second {
		at(t, f, token, elapsed)
	}
}

// The worker locks a student who stayed away from the exam window for too long.
func TestTheWorkerLocksAStudentWhoStayedAway(t *testing.T) {
	f := newFixture(t, 0)
	lockSettings(t, f, 5*time.Minute, 0, 3600)
	login := f.login(t, "0012")
	heartbeat(t, f, login.Token)
	clientEvent(t, f, login.Token, "focus_lost")
	// The student keeps in touch: only the focus is lost.
	keepInTouch(t, f, login.Token, 4*time.Minute)
	if _, err := f.sessions.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if f.storedSession(t, login.Token).LockedAt != nil {
		t.Fatal("the session was locked before the away lock time")
	}
	keepInTouch(t, f, login.Token, 5*time.Minute+time.Second)
	report, err := f.sessions.Reconcile(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if report.Locks != 1 {
		t.Fatalf("locks = %d", report.Locks)
	}
	session := f.storedSession(t, login.Token)
	if session.LockedAt == nil || session.LockReason != service.LockReasonAway {
		t.Fatalf("session = %+v", session)
	}
	if f.count(t, `SELECT count(*) FROM event_log WHERE event_type='locked'`) != 1 {
		t.Fatal("the lock was not recorded")
	}
	// A locked session is never locked again.
	f.clock.Set(epoch.Add(6 * time.Minute))
	if _, err := f.sessions.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if f.count(t, `SELECT count(*) FROM event_log WHERE event_type='locked'`) != 1 {
		t.Fatal("the session was locked twice")
	}
}

// The server cannot measure an absence across a network gap, so the away lock does not fire there: the no-contact rule
// covers it instead.
func TestANetworkGapStopsTheAwayLock(t *testing.T) {
	f := newFixture(t, 0)
	lockSettings(t, f, 5*time.Minute, 0, 3600)
	login := f.login(t, "0012")
	heartbeat(t, f, login.Token)
	clientEvent(t, f, login.Token, "focus_lost")
	// The heartbeat stops: the server sees a network gap.
	f.clock.Set(epoch.Add(6 * time.Minute))
	if _, err := f.sessions.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	session := f.storedSession(t, login.Token)
	if session.LockedAt != nil {
		t.Fatal("the away lock fired across a network gap")
	}
	if session.NetworkGapSince == nil {
		t.Fatal("the network gap was not recorded")
	}
}

// Nothing reaching the server for long enough locks the exam, even when the student is at the window.
func TestTheWorkerLocksAStudentTheServerCannotReach(t *testing.T) {
	f := newFixture(t, 0)
	lockSettings(t, f, 0, 5*time.Minute, 3600)
	login := f.login(t, "0012")
	heartbeat(t, f, login.Token)
	f.clock.Set(epoch.Add(4 * time.Minute))
	if _, err := f.sessions.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if f.storedSession(t, login.Token).LockedAt != nil {
		t.Fatal("the session was locked before the no-contact time")
	}
	f.clock.Set(epoch.Add(5*time.Minute + time.Second))
	report, err := f.sessions.Reconcile(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if report.Locks != 1 {
		t.Fatalf("locks = %d", report.Locks)
	}
	session := f.storedSession(t, login.Token)
	if session.LockedAt == nil || session.LockReason != service.LockReasonNoContact {
		t.Fatalf("session = %+v", session)
	}
}

// The student application may lock itself and tell the server; the server locks the session too.
func TestTheClientAwayLockLocksTheSession(t *testing.T) {
	f := newFixture(t, 0)
	lockSettings(t, f, 5*time.Minute, 0, 3600)
	login := f.login(t, "0012")
	heartbeat(t, f, login.Token)
	clientEvent(t, f, login.Token, "focus_lost")
	clientEvent(t, f, login.Token, "away_lock")
	session := f.storedSession(t, login.Token)
	if session.LockedAt == nil || session.LockReason != service.LockReasonAway {
		t.Fatalf("session = %+v", session)
	}
	// The lock is recorded once, with its source.
	if f.count(t, `SELECT count(*) FROM event_log WHERE event_type='locked'`) != 1 {
		t.Fatal("the lock was not recorded")
	}
	var source string
	if err := f.raw.QueryRow(`SELECT json_extract(detail,'$.source') FROM event_log WHERE event_type='locked'`).Scan(&source); err != nil || source != "client" {
		t.Fatalf("source = %q, %v", source, err)
	}
}

// Only a proctor unlocks, and the time given back is the proctor's explicit choice.
func TestUnlockingWithAndWithoutTimeBack(t *testing.T) {
	f := newFixture(t, 0)
	lockSettings(t, f, 5*time.Minute, 0, 3600)
	login := f.login(t, "0012")
	heartbeat(t, f, login.Token)
	clientEvent(t, f, login.Token, "focus_lost")
	clientEvent(t, f, login.Token, "away_lock")
	locked := f.storedSession(t, login.Token)
	f.clock.Set(epoch.Add(7 * time.Minute))
	// Without time back the deadline does not move.
	result, err := f.sessions.Unlock(t.Context(), login.SessionID, "student came back", false, locked.LockedAt)
	if err != nil {
		t.Fatal(err)
	}
	if result.SecondsAdded != 0 || !result.DeadlineAt.Equal(locked.DeadlineAt) {
		t.Fatalf("unlock = %+v", result)
	}
	session := f.storedSession(t, login.Token)
	if session.LockedAt != nil {
		t.Fatal("the session is still locked")
	}
	if f.count(t, `SELECT count(*) FROM event_log WHERE event_type='unlocked'`) != 1 {
		t.Fatal("the unlock was not recorded")
	}
	// The student is locked again and unlocked with the time back.
	clientEvent(t, f, login.Token, "away_lock")
	again := f.storedSession(t, login.Token)
	f.clock.Set(epoch.Add(9 * time.Minute))
	withTime, err := f.sessions.Unlock(t.Context(), login.SessionID, "network outage", true, again.LockedAt)
	if err != nil {
		t.Fatal(err)
	}
	if withTime.SecondsAdded != 120 {
		t.Fatalf("seconds added = %d", withTime.SecondsAdded)
	}
	if !withTime.DeadlineAt.Equal(locked.DeadlineAt.Add(2 * time.Minute)) {
		t.Fatalf("deadline = %v, want %v", withTime.DeadlineAt, locked.DeadlineAt.Add(2*time.Minute))
	}
}

// A session that ran out of time while it was locked is over: it cannot be unlocked or extended.
func TestAFinishedSessionCannotBeUnlocked(t *testing.T) {
	f := newFixture(t, 0)
	lockSettings(t, f, 5*time.Minute, 0, 60)
	login := f.login(t, "0012")
	heartbeat(t, f, login.Token)
	clientEvent(t, f, login.Token, "away_lock")
	// The time runs out while the exam is locked.
	f.clock.Set(epoch.Add(2 * time.Minute))
	if _, err := f.sessions.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	session := f.storedSession(t, login.Token)
	if session.Status != domain.Submitted {
		t.Fatalf("the session was not submitted: %+v", session)
	}
	if _, err := f.sessions.Unlock(t.Context(), login.SessionID, "too late", true, session.LockedAt); !errors.Is(err, service.ErrNotLocked) {
		t.Fatalf("unlock of a finished session = %v", err)
	}
}

// When the time runs out while the exam is locked, the session is submitted as usual and its lock ends with it: the
// proctor no longer sees it as locked, and the student gets the final answer instead of a refusal.
func TestTimeRunningOutWhileLockedEndsTheLock(t *testing.T) {
	f := newFixture(t, 0)
	lockSettings(t, f, 5*time.Minute, 0, 60)
	login := f.login(t, "0012")
	heartbeat(t, f, login.Token)
	clientEvent(t, f, login.Token, "away_lock")
	if catalog := examEntry(t, f); catalog.LockedSessions != 1 {
		t.Fatalf("locked sessions while locked = %d", catalog.LockedSessions)
	}
	f.clock.Set(epoch.Add(2 * time.Minute))
	if _, err := f.sessions.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	session := f.storedSession(t, login.Token)
	if session.Status != domain.Submitted || session.LockedAt != nil || session.LockReason != "" {
		t.Fatalf("the finished session is still locked: %+v", session)
	}
	list, err := f.sessions.ProctorSessions(t.Context(), testExamID)
	if err != nil || len(list.Sessions) != 1 || list.Sessions[0].Locked != nil || len(list.Sessions[0].Locks) != 1 {
		t.Fatalf("the proctor sees %+v, %v", list.Sessions, err)
	}
	if catalog := examEntry(t, f); catalog.LockedSessions != 0 {
		t.Fatalf("locked sessions after the end = %d", catalog.LockedSessions)
	}
	principal, err := f.sessions.Authenticate(t.Context(), login.Token)
	if err != nil || principal.Locked != nil {
		t.Fatalf("the status still reports a lock: %+v, %v", principal.Locked, err)
	}
	// A submission that was waiting when the exam locked now gets the final answer, as after any other end.
	final, err := f.sessions.Submit(t.Context(), login.Token, domain.Answers{})
	if err != nil || final.Status != domain.Submitted {
		t.Fatalf("submit after the end = %+v, %v", final, err)
	}
}

// examEntry is the test exam as the proctor's exam list shows it.
func examEntry(t *testing.T, f *fixture) service.ExamEntry {
	t.Helper()
	catalog, err := service.NewPublisher(f.store, f.clock).Catalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range catalog.Exams {
		if entry.ID == testExamID {
			return entry
		}
	}
	t.Fatal("the test exam is not listed")
	return service.ExamEntry{}
}

// The server locked the session before the student application could tell it about its own lock (after a network outage,
// say). The application's report is acknowledged with the server's lock, so the application knows the server has it, and
// the lock is recorded once.
func TestAnAwayLockAfterTheServersOwnIsAcknowledgedWithIt(t *testing.T) {
	f := newFixture(t, 0)
	lockSettings(t, f, 0, 5*time.Minute, 3600)
	login := f.login(t, "0012")
	heartbeat(t, f, login.Token)
	f.clock.Set(epoch.Add(5*time.Minute + time.Second))
	if report, err := f.sessions.Reconcile(t.Context()); err != nil || report.Locks != 1 {
		t.Fatalf("reconcile = %+v, %v", report, err)
	}
	var lockID int64
	if err := f.raw.QueryRow(`SELECT id FROM event_log WHERE event_type='locked'`).Scan(&lockID); err != nil {
		t.Fatal(err)
	}
	f.clock.Set(epoch.Add(6 * time.Minute))
	reply, err := f.sessions.ClientEvent(t.Context(), login.Token, service.EventInput{Type: "away_lock", Detail: []byte(`{"reason":"no_contact"}`)})
	if err != nil || reply.EventLogID != lockID {
		t.Fatalf("away_lock = %+v, %v; want the lock %d", reply, err, lockID)
	}
	if f.count(t, `SELECT count(*) FROM event_log WHERE event_type IN ('locked', 'away_lock')`) != 1 {
		t.Fatal("one lock was recorded more than once")
	}
}

// After an unlock the absence counts from zero: a student still away is locked again only once the away time has passed
// since the unlock.
func TestAfterAnUnlockTheAbsenceCountsFromTheUnlock(t *testing.T) {
	f := newFixture(t, 0)
	lockSettings(t, f, 5*time.Minute, 0, 3600)
	login := f.login(t, "0012")
	heartbeat(t, f, login.Token)
	clientEvent(t, f, login.Token, "focus_lost")
	stayInTouch := func(from, until time.Duration) {
		for elapsed := from; elapsed <= until; elapsed += 20 * time.Second {
			at(t, f, login.Token, elapsed)
		}
	}
	stayInTouch(20*time.Second, 5*time.Minute+time.Second)
	if report, err := f.sessions.Reconcile(t.Context()); err != nil || report.Locks != 1 {
		t.Fatalf("first lock = %+v, %v", report, err)
	}
	stayInTouch(5*time.Minute+21*time.Second, 6*time.Minute)
	// The proctor unlocks 5:41 into the exam.
	if _, err := f.sessions.Unlock(t.Context(), login.SessionID, "talked to the student", false, nil); err != nil {
		t.Fatal(err)
	}
	// The student has not come back to the window yet; the absence started over at the unlock.
	stayInTouch(6*time.Minute+time.Second, 10*time.Minute+40*time.Second)
	if report, err := f.sessions.Reconcile(t.Context()); err != nil || report.Locks != 0 {
		t.Fatalf("locked again before the away time after the unlock = %+v, %v", report, err)
	}
	stayInTouch(10*time.Minute+42*time.Second, 10*time.Minute+42*time.Second)
	if report, err := f.sessions.Reconcile(t.Context()); err != nil || report.Locks != 1 {
		t.Fatalf("not locked again after the away time = %+v, %v", report, err)
	}
}

// The screenshots stop at the lock. The pending uploads of the absence the student was in when the exam locked are still
// accepted, even after the student came back, but only the captures that were due before the lock; a later absence gets
// none.
func TestScreenshotsStopAtTheLock(t *testing.T) {
	f := newFixture(t, 0)
	lockSettings(t, f, 5*time.Minute, 0, 3600)
	login := f.login(t, "0012")
	shots, _ := imageService(t, f)
	loss := eligibleLoss(t, f, login.Token)
	upload := func(event int64, index int) error {
		_, err := shots.Upload(t.Context(), login.Token, event, index, nil, bytes.NewReader(imagePNG(t)))
		return err
	}
	// The application locks the exam 40 seconds into the absence, and the student comes back. The heartbeat keeps going.
	at(t, f, login.Token, 20*time.Second)
	at(t, f, login.Token, 40*time.Second)
	clientEvent(t, f, login.Token, "away_lock")
	at(t, f, login.Token, 41*time.Second)
	clientEvent(t, f, login.Token, "focus_regained")
	at(t, f, login.Token, 45*time.Second)
	// Capture 7 was due 35 seconds in (5 s threshold + 6 intervals of 5 s): it was taken before the lock.
	if err := upload(loss, 7); err != nil {
		t.Fatalf("a pending capture of the locking absence = %v", err)
	}
	// Capture 8 was due exactly at the lock, 40 seconds in: it was not taken.
	if err := upload(loss, 8); !errors.Is(err, service.ErrScreenshotIneligible) {
		t.Fatalf("a capture due at the lock = %v", err)
	}
	// A later absence, while the exam is still locked, gets no screenshots at all.
	later := clientEvent(t, f, login.Token, "focus_lost").EventLogID
	at(t, f, login.Token, 60*time.Second)
	if err := upload(later, 0); !errors.Is(err, service.ErrScreenshotIneligible) {
		t.Fatalf("a capture of a later absence = %v", err)
	}
}

// A session in grace goes back to active when the proctor gives the time back.
func TestAnUnlockedSessionInGraceGoesBackToActive(t *testing.T) {
	f := newFixture(t, 0)
	lockSettings(t, f, 5*time.Minute, 0, 60)
	login := f.login(t, "0012")
	heartbeat(t, f, login.Token)
	clientEvent(t, f, login.Token, "away_lock")
	// The exam time runs out: the session is in grace.
	f.clock.Set(epoch.Add(61 * time.Second))
	if _, err := f.sessions.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	session := f.storedSession(t, login.Token)
	if session.Status != domain.Grace {
		t.Fatalf("the session is not in grace: %+v", session)
	}
	f.clock.Set(epoch.Add(70 * time.Second))
	result, err := f.sessions.Unlock(t.Context(), login.SessionID, "came back", true, session.LockedAt)
	if err != nil {
		t.Fatal(err)
	}
	if !result.DeadlineAt.After(f.clock.Now()) {
		t.Fatalf("the new deadline is in the past: %v", result.DeadlineAt)
	}
	after := f.storedSession(t, login.Token)
	if after.Status != domain.Active {
		t.Fatalf("the session did not go back to active: %+v", after)
	}
}

// The exam is locked while the student is away: the questions and the answers are refused, and the status still answers.
func TestALockedSessionRefusesTheExamAndTheAnswers(t *testing.T) {
	f := newFixture(t, 0)
	lockSettings(t, f, 5*time.Minute, 0, 3600)
	login := f.login(t, "0012")
	heartbeat(t, f, login.Token)
	clientEvent(t, f, login.Token, "away_lock")
	if _, err := f.sessions.GetExam(t.Context(), login.Token); !errors.Is(err, service.ErrSessionOnHold) {
		t.Fatalf("the exam was served while locked: %v", err)
	}
	if _, err := f.sessions.Autosave(t.Context(), login.Token, domain.Answers{"1": "text"}); !errors.Is(err, service.ErrSessionOnHold) {
		t.Fatalf("an answer was saved while locked: %v", err)
	}
	if _, err := f.sessions.Submit(t.Context(), login.Token, domain.Answers{"1": "text"}); !errors.Is(err, service.ErrSessionOnHold) {
		t.Fatalf("the exam was submitted while locked: %v", err)
	}
	if _, err := f.sessions.Advance(t.Context(), login.Token, 0, domain.Answers{"1": "text"}); !errors.Is(err, service.ErrSessionOnHold) {
		t.Fatalf("the student moved on while locked: %v", err)
	}
	// The status, the heartbeat and the events still work.
	if _, err := f.sessions.Presence(t.Context(), login.SessionID); err != nil {
		t.Fatal(err)
	}
	heartbeat(t, f, login.Token)
	clientEvent(t, f, login.Token, "focus_regained")
	// A login resumes the locked session, so the application can show the lock screen.
	resumed, err := f.sessions.Login(t.Context(), "0012", "exam-secret")
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Locked == nil || resumed.Locked.Reason != service.LockReasonAway {
		t.Fatalf("the login did not report the lock: %+v", resumed.Locked)
	}
}
