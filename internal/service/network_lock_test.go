package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"exam-platform/internal/domain"
	"exam-platform/internal/service"
)

// networkLock is the report of a student who locked the exam themselves because the server was out of reach: the
// application sends it as soon as the connection is back.
func networkLock(t *testing.T, f *fixture, token string) service.EventResult {
	t.Helper()
	result, err := f.sessions.ClientEvent(t.Context(), token, service.EventInput{Type: "away_lock", Detail: json.RawMessage(`{"reason":"network"}`)})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// lockRecord is what the server records about a lock.
type lockRecord struct {
	Reason  string `json:"reason"`
	Source  string `json:"source"`
	Request int    `json:"request"`
	Limit   int    `json:"limit"`
}

// lockRecords reads the detail of every lock of the exam, oldest first.
func lockRecords(t *testing.T, f *fixture) []lockRecord {
	t.Helper()
	rows, err := f.raw.Query(`SELECT detail FROM event_log WHERE event_type = 'locked' ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var records []lockRecord
	for rows.Next() {
		var detail string
		if err := rows.Scan(&detail); err != nil {
			t.Fatal(err)
		}
		var record lockRecord
		if err := json.Unmarshal([]byte(detail), &record); err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	return records
}

// A student who cannot reach the server may lock the exam themselves; the server locks the session, records who asked and
// which of the allowed locks it is, and tells the application how many are left.
func TestAStudentRequestedNetworkLockLocksTheSession(t *testing.T) {
	f := newFixture(t, 0)
	lockSettings(t, f, 5*time.Minute, 5*time.Minute, 3600)
	login := f.login(t, "0012")
	heartbeat(t, f, login.Token)
	if login.NetworkLocksLeft != domain.NetworkLockLimit {
		t.Fatalf("locks left at login = %d", login.NetworkLocksLeft)
	}
	networkLock(t, f, login.Token)
	session := f.storedSession(t, login.Token)
	if session.LockedAt == nil || session.LockReason != service.LockReasonNetwork || session.NetworkLocks != 1 {
		t.Fatalf("session = %+v", session)
	}
	records := lockRecords(t, f)
	if len(records) != 1 || records[0].Reason != "network" || records[0].Source != "client" || records[0].Request != 1 || records[0].Limit != domain.NetworkLockLimit {
		t.Fatalf("lock records = %+v", records)
	}
	// The status says why, and how many requests are left.
	principal, err := f.sessions.Authenticate(t.Context(), login.Token)
	if err != nil {
		t.Fatal(err)
	}
	if principal.Locked == nil || principal.Locked.Reason != service.LockReasonNetwork || principal.NetworkLocksLeft != domain.NetworkLockLimit-1 {
		t.Fatalf("principal = %+v", principal)
	}
	// A login after a restart reports both, so the limit does not depend on the application that asked.
	resumed := f.resume(t, "0012", login.SessionID)
	if resumed.Locked == nil || resumed.Locked.Reason != service.LockReasonNetwork || resumed.NetworkLocksLeft != domain.NetworkLockLimit-1 {
		t.Fatalf("resumed login = %+v", resumed)
	}
}

// Only a proctor lifts the lock, and a student past the limit is recorded as such: the lock still happens, because the
// application is locked already, but the proctor sees that it is more than the allowed number.
func TestOnlyAProctorLiftsANetworkLockAndTheLimitIsRecorded(t *testing.T) {
	f := newFixture(t, 0)
	lockSettings(t, f, 5*time.Minute, 5*time.Minute, 3600)
	login := f.login(t, "0012")
	heartbeat(t, f, login.Token)
	for i := 1; i <= domain.NetworkLockLimit+1; i++ {
		networkLock(t, f, login.Token)
		// Neither a heartbeat nor an event of the student lifts it.
		heartbeat(t, f, login.Token)
		clientEvent(t, f, login.Token, "focus_regained")
		if f.storedSession(t, login.Token).LockedAt == nil {
			t.Fatalf("request %d: the exam was unlocked without a proctor", i)
		}
		if _, err := f.sessions.Unlock(t.Context(), login.SessionID, "back on the exam network", false, nil); err != nil {
			t.Fatal(err)
		}
	}
	records := lockRecords(t, f)
	if len(records) != domain.NetworkLockLimit+1 {
		t.Fatalf("lock records = %+v", records)
	}
	last := records[len(records)-1]
	if last.Request != domain.NetworkLockLimit+1 || last.Limit != domain.NetworkLockLimit {
		t.Fatalf("the request past the limit = %+v", last)
	}
	principal, err := f.sessions.Authenticate(t.Context(), login.Token)
	if err != nil {
		t.Fatal(err)
	}
	if principal.NetworkLocksLeft != 0 {
		t.Fatalf("locks left past the limit = %d", principal.NetworkLocksLeft)
	}
}

// The locks the application or the server make for other reasons never count against the student's requests.
func TestOtherLocksDoNotCountAsNetworkLocks(t *testing.T) {
	f := newFixture(t, 0)
	lockSettings(t, f, 5*time.Minute, 5*time.Minute, 3600)
	login := f.login(t, "0012")
	heartbeat(t, f, login.Token)
	clientEvent(t, f, login.Token, "away_lock")
	if session := f.storedSession(t, login.Token); session.LockReason != service.LockReasonAway || session.NetworkLocks != 0 {
		t.Fatalf("session = %+v", session)
	}
	if _, err := f.sessions.Unlock(t.Context(), login.SessionID, "came back", false, nil); err != nil {
		t.Fatal(err)
	}
	// The server's own no-contact lock is not a request either.
	f.clock.Set(epoch.Add(6 * time.Minute))
	if _, err := f.sessions.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if session := f.storedSession(t, login.Token); session.LockReason != service.LockReasonNoContact || session.NetworkLocks != 0 {
		t.Fatalf("session = %+v", session)
	}
	principal, err := f.sessions.Authenticate(t.Context(), login.Token)
	if err != nil {
		t.Fatal(err)
	}
	if principal.NetworkLocksLeft != domain.NetworkLockLimit {
		t.Fatalf("locks left = %d", principal.NetworkLocksLeft)
	}
}

// The rules the student reads before login say how many times the exam may be locked this way.
func TestTheRulesSayHowManyNetworkLocksAreAllowed(t *testing.T) {
	f := newFixture(t, 0)
	lockSettings(t, f, 5*time.Minute, 5*time.Minute, 3600)
	rules, err := f.sessions.Rules(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if rules.NetworkLockLimit != domain.NetworkLockLimit {
		t.Fatalf("rules = %+v", rules)
	}
}

// The screenshots the application takes while a lock the student asked for lasts are stored under the record of that lock,
// even though they were taken before the server heard of it, and show in the history on its row.
func TestScreenshotsOfANetworkLockAreStoredUnderItsRecord(t *testing.T) {
	f := newFixture(t, 0)
	lockSettings(t, f, 5*time.Minute, 5*time.Minute, 3600)
	login := f.login(t, "0012")
	other := f.login(t, "0034")
	shots, _ := imageService(t, f)
	heartbeat(t, f, login.Token)
	lock := networkLock(t, f, login.Token).EventLogID
	upload := func(token string, event int64, index int) error {
		_, err := shots.Upload(t.Context(), token, event, index, nil, bytes.NewReader(imagePNG(t)))
		return err
	}
	for index := 0; index < 3; index++ {
		if err := upload(login.Token, lock, index); err != nil {
			t.Fatalf("capture %d of the network lock = %v", index, err)
		}
	}
	// A retry of the same capture is not stored twice.
	if err := upload(login.Token, lock, 1); err != nil {
		t.Fatalf("a retry = %v", err)
	}
	if n := f.count(t, `SELECT count(*) FROM screenshots WHERE event_log_id = ?`, lock); n != 3 {
		t.Fatalf("stored screenshots = %d", n)
	}
	// The history shows them on the row of the lock.
	page, err := f.sessions.ProctorTimeline(t.Context(), login.SessionID, 100, "")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range page.Items {
		if item.Event != nil && item.Event.ID == lock {
			found = len(item.Event.Screenshots) == 3
		}
	}
	if !found {
		t.Fatal("the lock's screenshots are not on its row of the history")
	}
	// Another student's token cannot add to this lock.
	if err := upload(other.Token, lock, 0); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("another student's upload under the lock = %v", err)
	}
	// The per-lock count of the policy holds: the screenshots of one absence and of one lock share it.
	limit := f.storedSession(t, login.Token).Policy.IncidentCaptures
	if err := upload(login.Token, lock, limit); !errors.Is(err, service.ErrScreenshotIneligible) {
		t.Fatalf("a capture past the per-lock limit = %v", err)
	}
}

// Only the lock a student asked for has screenshots: the other locks stay as they were, and so do the absences.
func TestOtherLocksHaveNoScreenshotsOfTheirOwn(t *testing.T) {
	f := newFixture(t, 0)
	lockSettings(t, f, 5*time.Minute, 5*time.Minute, 3600)
	login := f.login(t, "0012")
	shots, _ := imageService(t, f)
	heartbeat(t, f, login.Token)
	awayLock := clientEvent(t, f, login.Token, "away_lock").EventLogID
	if _, err := shots.Upload(t.Context(), login.Token, awayLock, 0, nil, bytes.NewReader(imagePNG(t))); !errors.Is(err, service.ErrScreenshotIneligible) {
		t.Fatalf("a screenshot under an away lock = %v", err)
	}
	// An event that is neither a lock nor a focus loss has none either.
	regained := clientEvent(t, f, login.Token, "focus_regained").EventLogID
	if _, err := shots.Upload(t.Context(), login.Token, regained, 0, nil, bytes.NewReader(imagePNG(t))); !errors.Is(err, service.ErrScreenshotIneligible) {
		t.Fatalf("a screenshot under a focus_regained event = %v", err)
	}
}

// The screenshots of a lock can still arrive after a proctor unlocked the exam, as long as the exam is running.
func TestTheScreenshotsOfANetworkLockMayArriveAfterTheUnlock(t *testing.T) {
	f := newFixture(t, 0)
	lockSettings(t, f, 5*time.Minute, 5*time.Minute, 3600)
	login := f.login(t, "0012")
	shots, _ := imageService(t, f)
	heartbeat(t, f, login.Token)
	lock := networkLock(t, f, login.Token).EventLogID
	if _, err := f.sessions.Unlock(t.Context(), login.SessionID, "connected", false, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := shots.Upload(t.Context(), login.Token, lock, 0, nil, bytes.NewReader(imagePNG(t))); err != nil {
		t.Fatalf("an upload after the unlock = %v", err)
	}
}

// reportLock sends the away_lock report of a student whose application locked the exam with the given detail.
func reportLock(t *testing.T, f *fixture, token, detail string) {
	t.Helper()
	if _, err := f.sessions.ClientEvent(t.Context(), token, service.EventInput{Type: "away_lock", Detail: json.RawMessage(detail)}); err != nil {
		t.Fatal(err)
	}
}

// lockTimes are when the server says the latest lock began (the session's locked_at and the since of its record) and when
// it made the record.
func lockTimes(t *testing.T, f *fixture, token string) (lockedAt, since, recorded time.Time) {
	t.Helper()
	session := f.storedSession(t, token)
	if session.LockedAt == nil {
		t.Fatal("the session is not locked")
	}
	var detail, occurred string
	if err := f.raw.QueryRow(`SELECT detail, occurred_at FROM event_log WHERE event_type = 'locked' ORDER BY id DESC LIMIT 1`).Scan(&detail, &occurred); err != nil {
		t.Fatal(err)
	}
	var record struct {
		Since time.Time `json:"since"`
	}
	if err := json.Unmarshal([]byte(detail), &record); err != nil {
		t.Fatal(err)
	}
	recorded, err := time.Parse(time.RFC3339Nano, occurred)
	if err != nil {
		t.Fatal(err)
	}
	return *session.LockedAt, record.Since, recorded
}

// The student asks for the lock while the server is out of reach, and the report arrives when the connection is back. The
// lock began when the student asked for it, which is what the history shows and what a proctor's give-back time counts from;
// the record of it is still made when the server heard of it.
func TestANetworkLockBeginsWhenTheStudentAskedForIt(t *testing.T) {
	for name, heartbeatFirst := range map[string]bool{"the lock reaches the server first": false, "the heartbeat reaches the server first": true} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, 0)
			lockSettings(t, f, 5*time.Minute, 5*time.Minute, 3600)
			login := f.login(t, "0012")
			heartbeat(t, f, login.Token)
			// Contact is lost for 90 s; the student asked for the lock 40 s before the connection came back.
			f.clock.Set(epoch.Add(90 * time.Second))
			if heartbeatFirst {
				heartbeat(t, f, login.Token)
			}
			reportLock(t, f, login.Token, `{"reason":"network","lock_age_ms":40000}`)
			lockedAt, since, recorded := lockTimes(t, f, login.Token)
			want := epoch.Add(50 * time.Second)
			if !lockedAt.Equal(want) || !since.Equal(want) {
				t.Fatalf("the lock began at %v (record: %v), want %v", lockedAt, since, want)
			}
			if !recorded.Equal(epoch.Add(90 * time.Second)) {
				t.Fatalf("the record was made at %v", recorded)
			}
			// A proctor who gives the time back gives all of it, the part spent offline included.
			f.clock.Set(epoch.Add(100 * time.Second))
			result, err := f.sessions.Unlock(t.Context(), login.SessionID, "back on the network", true, nil)
			if err != nil {
				t.Fatal(err)
			}
			if result.LockedMillis != 50000 || result.SecondsAdded != 50 {
				t.Fatalf("unlock = %+v", result)
			}
		})
	}
}

// The server believes the application no further than what it knows: a lock cannot have begun before the network gap the
// student was in, and without a gap nothing says the student was out of reach.
func TestTheStartOfANetworkLockIsBoundedByWhatTheServerKnows(t *testing.T) {
	cases := []struct {
		name   string
		detail string
		at     time.Duration
		want   time.Duration
	}{
		// The gap began 30 s after the last contact (the policy's overdue time), whatever age the application claims.
		{"an age that reaches back before the gap", `{"reason":"network","lock_age_ms":3600000}`, 90 * time.Second, 30 * time.Second},
		{"an age that reaches back before the exam", `{"reason":"network","lock_age_ms":9999999999}`, 90 * time.Second, 30 * time.Second},
		// No gap yet: only 5 s since the last contact.
		{"no gap", `{"reason":"network","lock_age_ms":60000}`, 5 * time.Second, 5 * time.Second},
		{"an age of nothing", `{"reason":"network","lock_age_ms":0}`, 90 * time.Second, 90 * time.Second},
		{"a negative age", `{"reason":"network","lock_age_ms":-5000}`, 90 * time.Second, 90 * time.Second},
		{"a report that says nothing of the age", `{"reason":"network"}`, 90 * time.Second, 90 * time.Second},
		// The application makes the lock for lack of contact in the same way, so it has a time of its own too; the lock for an
		// absence is made where the server can see it, and has none.
		{"the lock for lack of contact", `{"reason":"no_contact","lock_age_ms":40000}`, 90 * time.Second, 50 * time.Second},
		{"the lock for an absence", `{"reason":"away","lock_age_ms":40000}`, 90 * time.Second, 90 * time.Second},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t, 0)
			lockSettings(t, f, 5*time.Minute, 5*time.Minute, 3600)
			login := f.login(t, "0012")
			heartbeat(t, f, login.Token)
			f.clock.Set(epoch.Add(c.at))
			reportLock(t, f, login.Token, c.detail)
			lockedAt, since, _ := lockTimes(t, f, login.Token)
			want := epoch.Add(c.want)
			if !lockedAt.Equal(want) || !since.Equal(want) {
				t.Fatalf("the lock began at %v (record: %v), want %v", lockedAt, since, want)
			}
		})
	}
}

// lockRecordOf is the record of the latest lock of the exam.
func lockRecordOf(t *testing.T, f *fixture) int64 {
	t.Helper()
	var id int64
	if err := f.raw.QueryRow(`SELECT id FROM event_log WHERE event_type = 'locked' ORDER BY id DESC LIMIT 1`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// The lock for lack of contact gives the student the run of the computer to reconnect, as the one they asked for does, so it
// is watched by screenshots in the same way, whether the application made it or the server did first.
func TestScreenshotsOfALockForLackOfContactAreStoredUnderItsRecord(t *testing.T) {
	upload := func(t *testing.T, f *fixture, token string, event int64, index int) error {
		shots, _ := imageService(t, f)
		_, err := shots.Upload(t.Context(), token, event, index, nil, bytes.NewReader(imagePNG(t)))
		return err
	}
	t.Run("the application made the lock", func(t *testing.T) {
		f := newFixture(t, 0)
		lockSettings(t, f, 5*time.Minute, 5*time.Minute, 3600)
		login := f.login(t, "0012")
		heartbeat(t, f, login.Token)
		f.clock.Set(epoch.Add(90 * time.Second))
		reportLock(t, f, login.Token, `{"reason":"no_contact","lock_age_ms":10000}`)
		for index := 0; index < 3; index++ {
			if err := upload(t, f, login.Token, lockRecordOf(t, f), index); err != nil {
				t.Fatalf("capture %d = %v", index, err)
			}
		}
	})
	t.Run("the server made the lock first", func(t *testing.T) {
		f := newFixture(t, 0)
		lockSettings(t, f, 0, 5*time.Minute, 3600)
		login := f.login(t, "0012")
		heartbeat(t, f, login.Token)
		f.clock.Set(epoch.Add(5*time.Minute + 2*time.Second))
		if report, err := f.sessions.Reconcile(t.Context()); err != nil || report.Locks != 1 {
			t.Fatalf("reconcile = %+v, %v", report, err)
		}
		// The application, back in touch, reports its own lock and is answered with the server's record of it.
		reportLock(t, f, login.Token, `{"reason":"no_contact","lock_age_ms":10000}`)
		if err := upload(t, f, login.Token, lockRecordOf(t, f), 0); err != nil {
			t.Fatalf("a screenshot under the server's lock = %v", err)
		}
	})
}

// The status and the login say which record the lock has, so that an application that learns of a lock it did not make, or
// that was restarted during it, can still send the screenshots of the lock under it. The proctor's view has no use for it.
func TestTheStatusSaysWhichRecordTheLockHas(t *testing.T) {
	f := newFixture(t, 0)
	lockSettings(t, f, 0, 5*time.Minute, 3600)
	login := f.login(t, "0012")
	heartbeat(t, f, login.Token)
	principal, err := f.sessions.Authenticate(t.Context(), login.Token)
	if err != nil || principal.Locked != nil {
		t.Fatalf("an open exam = %+v, %v", principal.Locked, err)
	}
	f.clock.Set(epoch.Add(5*time.Minute + 2*time.Second))
	if report, err := f.sessions.Reconcile(t.Context()); err != nil || report.Locks != 1 {
		t.Fatalf("reconcile = %+v, %v", report, err)
	}
	record := lockRecordOf(t, f)
	principal, err = f.sessions.Authenticate(t.Context(), login.Token)
	if err != nil {
		t.Fatal(err)
	}
	if principal.Locked == nil || principal.Locked.Reason != service.LockReasonNoContact || principal.Locked.EventID != record {
		t.Fatalf("status = %+v, want record %d", principal.Locked, record)
	}
	resumed := f.resume(t, "0012", login.SessionID)
	if resumed.Locked == nil || resumed.Locked.EventID != record {
		t.Fatalf("resumed login = %+v, want record %d", resumed.Locked, record)
	}
}

// The application makes the lock while the server is out of reach and reports it as soon as contact is back, so the gap the lock
// belongs to has just closed, or is still open. A gap that closed long ago says nothing of a lock reported now: the age the
// application claims is not taken back to it, or a student who changed the application could have a lock begin at the start of an
// old gap, and be given back the time since.
func TestTheStartOfANetworkLockIsNotTakenFromAGapThatClosedLongAgo(t *testing.T) {
	cases := []struct {
		name   string
		closed time.Duration // how long before the report the gap closed
		want   time.Duration // when the lock is said to have begun, from the start of the exam
	}{
		{"a gap that closed a minute ago", time.Minute, 30 * time.Second},
		{"a gap that closed ten minutes ago", 10 * time.Minute, 90*time.Second + 10*time.Minute},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t, 0)
			lockSettings(t, f, 5*time.Minute, 5*time.Minute, 3600)
			login := f.login(t, "0012")
			heartbeat(t, f, login.Token)
			// Contact is lost from 30 s to 90 s: the one gap of the exam.
			f.clock.Set(epoch.Add(90 * time.Second))
			heartbeat(t, f, login.Token)
			// Contact stays good, with a heartbeat well inside the overdue time, until the report.
			reportAt := epoch.Add(90*time.Second + c.closed)
			for at := epoch.Add(110 * time.Second); at.Before(reportAt); at = at.Add(20 * time.Second) {
				f.clock.Set(at)
				heartbeat(t, f, login.Token)
			}
			f.clock.Set(reportAt)
			reportLock(t, f, login.Token, `{"reason":"network","lock_age_ms":3600000}`)
			lockedAt, since, _ := lockTimes(t, f, login.Token)
			want := epoch.Add(c.want)
			if !lockedAt.Equal(want) || !since.Equal(want) {
				t.Fatalf("the lock began at %v (record: %v), want %v", lockedAt, since, want)
			}
		})
	}
}

// lockReads counts how often the record of a lock is looked up.
type lockReads struct {
	service.SessionTransaction
	reads *atomic.Int64
}

func (tx lockReads) LatestLockEvent(ctx context.Context, id string) (domain.Event, error) {
	tx.reads.Add(1)
	return tx.SessionTransaction.LatestLockEvent(ctx, id)
}

// Only what a student is told names the record of the lock; the worker's tick over every session does not look it up.
func TestTheRecordOfALockIsLookedUpOnlyForWhatTheStudentIsTold(t *testing.T) {
	f := newFixture(t, 0)
	lockSettings(t, f, 5*time.Minute, 5*time.Minute, 3600)
	login := f.login(t, "0012")
	heartbeat(t, f, login.Token)
	networkLock(t, f, login.Token)
	var reads atomic.Int64
	counted := testSessions(wrappedRepository{f.store, func(tx service.SessionTransaction) service.SessionTransaction {
		return lockReads{tx, &reads}
	}}, f.clock)
	if _, err := counted.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if n := reads.Load(); n != 0 {
		t.Fatalf("the worker looked up the record of a lock %d times", n)
	}
	principal, err := counted.Authenticate(t.Context(), login.Token)
	if err != nil {
		t.Fatal(err)
	}
	if principal.Locked == nil || principal.Locked.EventID <= 0 {
		t.Fatalf("what the student is told does not name the record: %+v", principal.Locked)
	}
}
