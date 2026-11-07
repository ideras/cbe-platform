package client

import (
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// networkPolicy is a fast policy where a lost connection is noticed after 80 ms and the no-contact lock comes after
// noContact milliseconds (0 turns it off).
func networkPolicy(noContact int64) *Policy {
	return &Policy{HeartbeatMillis: 40, OverdueMillis: 80, FocusMillis: 40, AlertMillis: 30, CaptureMillis: 1000,
		LockAwayAfterMillis: 0, LockNoContactAfterMillis: noContact, IncidentCaptures: 30, SessionCaptures: 200}
}

// connection is the fake server's reachability: while it is down, the heartbeat and the events fail. The away_lock reports
// that get through are kept.
type connection struct {
	down      atomic.Bool
	delivered atomic.Value // the reason of the latest away_lock the server received
	age       atomic.Int64 // how long ago, in milliseconds, that report said the lock began
	// sever makes the server hang up on every request without answering, as it does when the network changes under the
	// application; down, by contrast, makes it answer with an error.
	sever atomic.Bool
}

func (c *connection) install(f *fixture) {
	f.onTelemetry = func(w http.ResponseWriter, _ *http.Request, record telemetryRecord) bool {
		if c.sever.Load() {
			if hijacker, ok := w.(http.Hijacker); ok {
				if conn, _, err := hijacker.Hijack(); err == nil {
					conn.Close()
				}
			}
			return true
		}
		if c.down.Load() {
			http.Error(w, "unreachable", 500)
			return true
		}
		if record.kind == "away_lock" {
			reason, _ := record.detail["reason"].(string)
			if age, ok := record.detail["lock_age_ms"].(float64); ok {
				c.age.Store(int64(age))
			}
			c.delivered.Store(reason)
			// The server answers with the record it made of the lock: always the same, as the real one does for a repeated report.
			f.mu.Lock()
			clock := f.status.ServerTime
			f.mu.Unlock()
			respond(w, eventAck{ID: lockRecordID, ServerTime: clock})
			return true
		}
		return false
	}
}

// lockRecordID is the record the fake server makes of the lock the student asked for.
const lockRecordID = 777

// The student is told how long is left before the exam locks, from the moment the connection counts as lost.
func TestTheStudentIsWarnedBeforeTheNoContactLock(t *testing.T) {
	f := newFixture(t)
	f.status.Policy = networkPolicy(1500)
	var link connection
	link.install(f)
	s := monitorClient(t, f, nil)
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.HeartbeatAt != nil })
	if v := s.Snapshot().Monitoring; v.ContactLost || v.NoContactLockInMillis != nil {
		t.Fatalf("a warning while the connection is fine: %+v", v)
	}
	link.down.Store(true)
	warned := waitMonitor(t, s, func(v MonitoringView) bool { return v.ContactLost })
	if warned.NoContactLockInMillis == nil || *warned.NoContactLockInMillis <= 0 || *warned.NoContactLockInMillis > 1500 {
		t.Fatalf("time left = %v", warned.NoContactLockInMillis)
	}
	// The time left only goes down, and the exam locks when it runs out.
	later := waitMonitor(t, s, func(v MonitoringView) bool {
		return v.NoContactLockInMillis != nil && *v.NoContactLockInMillis < *warned.NoContactLockInMillis
	})
	if later.NoContactMillis <= warned.NoContactMillis {
		t.Fatalf("the time without contact did not grow: %d then %d", warned.NoContactMillis, later.NoContactMillis)
	}
	locked := waitMonitor(t, s, func(v MonitoringView) bool { return v.Locked })
	if locked.LockReason != "no_contact" || locked.ContactLost || locked.NoContactLockInMillis != nil {
		t.Fatalf("after the lock = %+v", locked)
	}
}

// The warning has no countdown when the no-contact lock is off, and it ends when the connection comes back.
func TestTheWarningEndsWhenTheConnectionReturns(t *testing.T) {
	f := newFixture(t)
	f.status.Policy = networkPolicy(0)
	var link connection
	link.install(f)
	s := monitorClient(t, f, nil)
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.HeartbeatAt != nil })
	link.down.Store(true)
	warned := waitMonitor(t, s, func(v MonitoringView) bool { return v.ContactLost })
	if warned.NoContactLockInMillis != nil {
		t.Fatalf("a countdown to a lock that is off: %v", *warned.NoContactLockInMillis)
	}
	link.down.Store(false)
	waitMonitor(t, s, func(v MonitoringView) bool { return !v.ContactLost })
}

// While the connection is lost the student may lock the exam to go and reconnect; the application tells the server as soon as it
// can, and the exam stays locked until a proctor lifts it.
func TestTheStudentCanLockTheExamWhileTheConnectionIsLost(t *testing.T) {
	f := newFixture(t)
	f.status.Policy = networkPolicy(60000)
	f.status.NetworkLocksLeft = 2
	var polls atomic.Int64
	f.onStatus = func(*http.Request) { polls.Add(1) }
	var link connection
	link.install(f)
	s := monitorClient(t, f, nil)
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.HeartbeatAt != nil })
	// With the connection fine there is no reason to lock the exam this way.
	if v := s.Snapshot().Monitoring; v.NetworkLockAvailable || v.NetworkLocksLeft != 2 {
		t.Fatalf("the option is offered with a working connection: %+v", v)
	}
	if _, err := s.RequestNetworkLock(); err == nil {
		t.Fatal("the exam was locked with a working connection")
	}
	link.down.Store(true)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.NetworkLockAvailable })
	view, err := s.RequestNetworkLock()
	if err != nil {
		t.Fatal(err)
	}
	if view.Locked == nil || view.Locked.Reason != "network" || view.Monitoring.NetworkLocksLeft != 1 {
		t.Fatalf("view after the request = %+v / %+v", view.Locked, view.Monitoring)
	}
	// The answers are kept, not sent, while the exam is locked.
	if _, err := s.UpdateAnswers(map[string]string{"q1": "kept"}, 1); err == nil {
		t.Fatal("an answer was accepted while the exam was locked")
	}
	// The connection comes back: the server is told, and the exam stays locked. The student has no way to unlock it.
	link.down.Store(false)
	waitUntil(t, func() bool { reason, _ := link.delivered.Load().(string); return reason == "network" })
	time.Sleep(150 * time.Millisecond)
	if s.Snapshot().Locked == nil {
		t.Fatal("the exam unlocked itself when the connection returned")
	}
	// The server records the lock and the application hears of it; then a proctor unlocks it, the only way out. The
	// application keeps a lock it made itself until the server has reported it, so the test lets two status polls go by.
	setServerLock(f, "network")
	seen := polls.Load()
	waitUntil(t, func() bool { return polls.Load() >= seen+2 })
	setServerLock(f, "")
	waitUntil(t, func() bool { return s.Snapshot().Locked == nil })
	if s.Snapshot().UnlockNote == "" {
		t.Fatal("the unlock was not reported to the student")
	}
}

// A lock the student asked for makes no noise when they leave the window to reconnect.
func TestTheAlarmStaysQuietWhileTheNetworkLockLasts(t *testing.T) {
	f := newFixture(t)
	f.status.Policy = networkPolicy(60000)
	f.status.NetworkLocksLeft = 1
	var link connection
	link.install(f)
	var alerts atomic.Int64
	s := monitorClient(t, f, func(o *Options) { o.Monitoring.OnAlert = func(Incident) { alerts.Add(1) } })
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.HeartbeatAt != nil })
	link.down.Store(true)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.NetworkLockAvailable })
	if _, err := s.RequestNetworkLock(); err != nil {
		t.Fatal(err)
	}
	// The student goes to the network settings and comes back.
	ReportNativeFocus(s, false)
	time.Sleep(300 * time.Millisecond)
	ReportNativeFocus(s, true)
	if alerts.Load() != 0 {
		t.Fatalf("the alarm sounded %d times during the network lock", alerts.Load())
	}
}

// The number of requests comes from the server. None left, or a server that does not say (an older one), means the option
// does not exist, and a request the server has not recorded yet is not given back by a status that does not count it.
func TestTheNetworkLockLimitHolds(t *testing.T) {
	f := newFixture(t)
	f.status.Policy = networkPolicy(60000)
	var link connection
	link.install(f)
	s := monitorClient(t, f, nil)
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.HeartbeatAt != nil })
	link.down.Store(true)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.ContactLost })
	if v := s.Snapshot().Monitoring; v.NetworkLockAvailable || v.NetworkLocksLeft != 0 {
		t.Fatalf("the option exists with no requests left: %+v", v)
	}
	if _, err := s.RequestNetworkLock(); err == nil {
		t.Fatal("the exam was locked with no requests left")
	}

	// A student with one request left uses it; the server's status still says one until it hears of the lock.
	g := newFixture(t)
	g.status.Policy = networkPolicy(60000)
	g.status.NetworkLocksLeft = 1
	var gLink connection
	gLink.install(g)
	r := monitorClient(t, g, nil)
	ReportNativeFocus(r, true)
	loginClient(t, r, g)
	waitMonitor(t, r, func(v MonitoringView) bool { return v.HeartbeatAt != nil })
	gLink.down.Store(true)
	waitMonitor(t, r, func(v MonitoringView) bool { return v.NetworkLockAvailable })
	if _, err := r.RequestNetworkLock(); err != nil {
		t.Fatal(err)
	}
	gLink.down.Store(false)
	time.Sleep(200 * time.Millisecond)
	if left := r.Snapshot().Monitoring.NetworkLocksLeft; left != 0 {
		t.Fatalf("the request was given back before the server recorded it: %d left", left)
	}
}

// withLockShots is a fast policy whose lock screenshots come every 30 ms, at most four of them.
func withLockShots(noContact int64) *Policy {
	p := networkPolicy(noContact)
	p.CaptureMillis, p.IncidentCaptures = 30, 4
	return p
}

// sentShots copies what the fake server has received as screenshots.
func sentShots(f *fixture) []screenshotRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]screenshotRecord(nil), f.screenshots...)
}

// While a lock the student asked for lasts, the application takes screenshots and holds them; once the server has recorded
// the lock they are sent under its record, and the application keeps nothing.
func TestTheScreenshotsOfANetworkLockAreHeldUntilTheServerRecordsIt(t *testing.T) {
	f := newFixture(t)
	f.status.Policy = withLockShots(60000)
	f.status.NetworkLocksLeft = 1
	var link connection
	link.install(f)
	s, shots := captureClient(t, f, nil)
	link.down.Store(true)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.NetworkLockAvailable })
	if _, err := s.RequestNetworkLock(); err != nil {
		t.Fatal(err)
	}
	// The server is out of reach: the screenshots are taken and held, and nothing is sent.
	waitUntil(t, func() bool { return shots.Load() >= 2 && s.Snapshot().Capture.Pending >= 2 })
	if sent := sentShots(f); len(sent) != 0 {
		t.Fatalf("%d screenshots were sent before the server recorded the lock", len(sent))
	}
	// The connection returns and the server records the lock: the screenshots go under that record.
	link.down.Store(false)
	waitUntil(t, func() bool { return len(sentShots(f)) >= 3 && s.Snapshot().Capture.Pending == 0 })
	seen := map[int]bool{}
	for _, shot := range sentShots(f) {
		if shot.event != lockRecordID || shot.contentType != "image/jpeg" || shot.index < 0 || shot.index >= 4 || seen[shot.index] {
			t.Fatalf("screenshot sent = %+v", shot)
		}
		seen[shot.index] = true
	}
	// No more than the policy's count for one lock, however long the lock lasts.
	time.Sleep(200 * time.Millisecond)
	if sent := len(sentShots(f)); sent > 4 || shots.Load() > 4 {
		t.Fatalf("%d screenshots sent, %d taken: more than the policy allows for one lock", sent, shots.Load())
	}
}

// The locks the student did not ask for are not watched by screenshots of their own.
func TestOtherLocksAreNotWatchedByLockScreenshots(t *testing.T) {
	f := newFixture(t)
	f.status.Policy = withLockShots(60000)
	s, shots := captureClient(t, f, nil)
	// The server locks the exam for an absence.
	setServerLock(f, "away")
	waitMonitor(t, s, func(v MonitoringView) bool { return v.Locked && v.LockReason == "away" })
	time.Sleep(200 * time.Millisecond)
	if shots.Load() != 0 || s.Snapshot().Capture.Pending != 0 {
		t.Fatalf("%d screenshots taken, %d pending, for a lock that is not for the network", shots.Load(), s.Snapshot().Capture.Pending)
	}
}

// The lock for lack of contact is watched in the same way: the screenshots are taken and held while the server cannot be
// reached, and go under the record of the lock once it has answered the report.
func TestTheScreenshotsOfALockForLackOfContactAreHeldUntilTheServerRecordsIt(t *testing.T) {
	f := newFixture(t)
	f.status.Policy = withLockShots(150)
	var link connection
	link.install(f)
	s, shots := captureClient(t, f, nil)
	link.down.Store(true)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.Locked && v.LockReason == "no_contact" })
	waitUntil(t, func() bool { return shots.Load() >= 2 && s.Snapshot().Capture.Pending >= 2 })
	if sent := sentShots(f); len(sent) != 0 {
		t.Fatalf("%d screenshots were sent before the server recorded the lock", len(sent))
	}
	link.down.Store(false)
	waitUntil(t, func() bool { return len(sentShots(f)) >= 3 && s.Snapshot().Capture.Pending == 0 })
	for _, shot := range sentShots(f) {
		if shot.event != lockRecordID || shot.index < 0 || shot.index >= 4 {
			t.Fatalf("screenshot sent = %+v", shot)
		}
	}
}

// A lock the application did not make, because the server made it or because the application was started during it, is
// watched from the moment the application learns of it, under the record the server names.
func TestALockTheServerNamesIsWatchedFromTheMomentTheApplicationLearnsOfIt(t *testing.T) {
	f := newFixture(t)
	f.status.Policy = withLockShots(60000)
	f.status.Locked = &Lock{Since: time.Now().Add(-time.Hour), Reason: "no_contact", EventID: 555}
	s, _ := captureClient(t, f, nil)
	waitUntil(t, func() bool { return len(sentShots(f)) >= 2 })
	seen := map[int]bool{}
	for _, shot := range sentShots(f) {
		// However long ago the lock began, the screenshots are numbered from the first, so the server's count for one lock holds.
		if shot.event != 555 || shot.index < 0 || shot.index >= 4 || seen[shot.index] {
			t.Fatalf("screenshot sent = %+v", shot)
		}
		seen[shot.index] = true
	}
	if s.Snapshot().Locked == nil {
		t.Fatal("the exam unlocked itself")
	}
}

// An older server does not name the record: there is nowhere to send the screenshots, so none are taken and held for good.
func TestALockWithoutARecordIsNotWatched(t *testing.T) {
	f := newFixture(t)
	f.status.Policy = withLockShots(60000)
	f.status.Locked = &Lock{Since: time.Now(), Reason: "no_contact"}
	s, shots := captureClient(t, f, nil)
	time.Sleep(200 * time.Millisecond)
	if shots.Load() != 0 || s.Snapshot().Capture.Pending != 0 {
		t.Fatalf("%d screenshots taken, %d pending, for a lock with no record", shots.Load(), s.Snapshot().Capture.Pending)
	}
}

// A screenshot the server cannot be asked about stays held and is tried again; one the server refuses is released without
// failing anything.
func TestAHeldScreenshotIsRetriedAndARefusedOneIsReleased(t *testing.T) {
	f := newFixture(t)
	f.status.Policy = withLockShots(60000)
	f.status.NetworkLocksLeft = 1
	var attempts atomic.Int64
	f.onScreenshot = func(w http.ResponseWriter, _ *http.Request, record screenshotRecord, _ int) bool {
		switch {
		case record.index == 0 && attempts.Add(1) <= 3:
			http.Error(w, "busy", 503) // the first screenshot is refused for now, for a whole round of attempts
			return true
		case record.index == 1:
			http.Error(w, "not wanted", 403) // the server does not want the second one at all
			return true
		}
		return false
	}
	var link connection
	link.install(f)
	s, _ := captureClient(t, f, nil)
	link.down.Store(true)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.NetworkLockAvailable })
	if _, err := s.RequestNetworkLock(); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, func() bool { return s.Snapshot().Capture.Pending >= 3 })
	link.down.Store(false)
	// Everything is released in the end: the first after its retries, the second at once, the rest as sent.
	waitUntil(t, func() bool { return s.Snapshot().Capture.Pending == 0 })
	var first bool
	for _, shot := range sentShots(f) {
		first = first || (shot.index == 0 && shot.event == lockRecordID)
	}
	if attempts.Load() < 4 || !first {
		t.Fatalf("the first screenshot was not retried until it was accepted: %d attempts", attempts.Load())
	}
	if v := s.Snapshot().Capture; v.Failures != 0 || v.Blocked {
		t.Fatalf("a refused screenshot failed the capture: %+v", v)
	}
}

// windowLog is what the application told the native window to do: true for a small window, false for full screen again.
type windowLog struct {
	mu    sync.Mutex
	calls []bool
}

func (w *windowLog) record(relaxed bool) {
	w.mu.Lock()
	w.calls = append(w.calls, relaxed)
	w.mu.Unlock()
}
func (w *windowLog) all() []bool { w.mu.Lock(); defer w.mu.Unlock(); return slices.Clone(w.calls) }
func (w *windowLog) wait(t *testing.T, want ...bool) {
	t.Helper()
	waitUntil(t, func() bool { return slices.Equal(w.all(), want) })
}

// The window is a small one while the student reconnects, so the computer's network settings are within reach, and full
// screen again once a proctor has unlocked the exam. The student has no part in either.
func TestTheWindowIsSmallWhileTheNetworkLockLasts(t *testing.T) {
	f := newFixture(t)
	f.status.Policy = networkPolicy(60000)
	f.status.NetworkLocksLeft = 2
	var polls atomic.Int64
	f.onStatus = func(*http.Request) { polls.Add(1) }
	var link connection
	link.install(f)
	var window windowLog
	s := monitorClient(t, f, func(o *Options) { o.Monitoring.OnWindowRelaxed = window.record })
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.HeartbeatAt != nil })
	link.down.Store(true)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.NetworkLockAvailable })
	if calls := window.all(); len(calls) != 0 {
		t.Fatalf("the window changed before the lock: %v", calls)
	}
	if _, err := s.RequestNetworkLock(); err != nil {
		t.Fatal(err)
	}
	window.wait(t, true)
	// The connection returns and the server records the lock: the window stays small until a proctor unlocks the exam.
	link.down.Store(false)
	waitUntil(t, func() bool { reason, _ := link.delivered.Load().(string); return reason == "network" })
	setServerLock(f, "network")
	seen := polls.Load()
	waitUntil(t, func() bool { return polls.Load() >= seen+2 })
	time.Sleep(100 * time.Millisecond)
	window.wait(t, true)
	setServerLock(f, "")
	window.wait(t, true, false)
	time.Sleep(100 * time.Millisecond)
	window.wait(t, true, false)
}

// Only the lock the student asked for gives the window up: the others stay full screen, since nothing watches the screen
// during them as it does during this one.
func TestOtherLocksKeepTheWindowFullScreen(t *testing.T) {
	f := newFixture(t)
	f.status.Policy = networkPolicy(60000)
	var window windowLog
	s := monitorClient(t, f, func(o *Options) { o.Monitoring.OnWindowRelaxed = window.record })
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	// The server locks the exam for an absence.
	setServerLock(f, "away")
	waitMonitor(t, s, func(v MonitoringView) bool { return v.Locked && v.LockReason == "away" })
	s.pollOnce()
	time.Sleep(100 * time.Millisecond)
	if calls := window.all(); len(calls) != 0 {
		t.Fatalf("the window changed for a lock that is not for the network: %v", calls)
	}
}

// The lock for lack of contact is a lock for the network too: the student needs the computer's network settings during it,
// whether the application made it or the server did.
func TestALockForLackOfContactHasTheWindowSmall(t *testing.T) {
	f := newFixture(t)
	f.status.Policy = networkPolicy(150)
	var polls atomic.Int64
	f.onStatus = func(*http.Request) { polls.Add(1) }
	var link connection
	link.install(f)
	var window windowLog
	s := monitorClient(t, f, func(o *Options) { o.Monitoring.OnWindowRelaxed = window.record })
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.HeartbeatAt != nil })
	link.down.Store(true)
	// The application locks itself for lack of contact, and the window stops being full screen.
	waitMonitor(t, s, func(v MonitoringView) bool { return v.Locked && v.LockReason == "no_contact" })
	window.wait(t, true)
	// The connection returns and the server records the lock; a proctor unlocks it, and the window is full screen again.
	link.down.Store(false)
	waitUntil(t, func() bool { reason, _ := link.delivered.Load().(string); return reason == "no_contact" })
	setServerLock(f, "no_contact")
	seen := polls.Load()
	waitUntil(t, func() bool { return polls.Load() >= seen+2 })
	window.wait(t, true)
	setServerLock(f, "")
	window.wait(t, true, false)
}

// A student who closes and reopens the application during the lock finds it locked, and the window small again.
func TestAResumedNetworkLockHasTheWindowSmall(t *testing.T) {
	f := newFixture(t)
	f.status.Policy = networkPolicy(60000)
	setServerLock(f, "network")
	var window windowLog
	s := monitorClient(t, f, func(o *Options) { o.Monitoring.OnWindowRelaxed = window.record })
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	window.wait(t, true)
}

// The end of the exam leaves the window as the completion left it, even when the exam was locked for the network.
func TestAFinishedExamIsNotMadeFullScreenAgain(t *testing.T) {
	f := newFixture(t)
	f.status.Policy = networkPolicy(60000)
	f.status.NetworkLocksLeft = 1
	var polls atomic.Int64
	f.onStatus = func(*http.Request) { polls.Add(1) }
	var link connection
	link.install(f)
	var window windowLog
	s := monitorClient(t, f, func(o *Options) { o.Monitoring.OnWindowRelaxed = window.record })
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.HeartbeatAt != nil })
	link.down.Store(true)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.NetworkLockAvailable })
	if _, err := s.RequestNetworkLock(); err != nil {
		t.Fatal(err)
	}
	window.wait(t, true)
	// The server records the lock and the application hears of it; then the exam ends while it is locked.
	link.down.Store(false)
	setServerLock(f, "network")
	seen := polls.Load()
	waitUntil(t, func() bool { return polls.Load() >= seen+2 })
	f.mu.Lock()
	f.status.State, f.status.SecondsRemaining, f.status.Locked = "force_submitted", 0, nil
	f.mu.Unlock()
	waitView(t, s, func(v View) bool { return v.Phase == "complete" })
	time.Sleep(150 * time.Millisecond)
	window.wait(t, true)
}

// The report of a lock made while the server was out of reach says how long ago it was made, so the server can put the
// lock where it belongs in the history instead of where it heard of it.
func TestTheReportSaysHowLongAgoTheLockBegan(t *testing.T) {
	f := newFixture(t)
	f.status.Policy = networkPolicy(60000)
	f.status.NetworkLocksLeft = 2
	var link connection
	link.install(f)
	s := monitorClient(t, f, nil)
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.HeartbeatAt != nil })
	link.down.Store(true)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.NetworkLockAvailable })
	if _, err := s.RequestNetworkLock(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(400 * time.Millisecond)
	link.down.Store(false)
	waitUntil(t, func() bool { reason, _ := link.delivered.Load().(string); return reason == "network" })
	if age := link.age.Load(); age < 350 || age > 3000 {
		t.Fatalf("the report said the lock began %d ms ago, after the student waited 400 ms", age)
	}
}

// The student leaves the window to change network, as the lock allows. When a proctor unlocks the exam and the student is
// still out of the window, the absence counts from the unlock: it is then that they are expected back, and the alarm must not
// start at once for time that was spent in the lock.
func TestAnAbsenceAfterANetworkLockCountsFromTheUnlock(t *testing.T) {
	for _, reason := range []string{"network", "no_contact"} {
		t.Run(reason, func(t *testing.T) { absenceAfterTheUnlock(t, reason) })
	}
}

// absenceAfterTheUnlock runs that for a lock the student asked for and for the one for lack of contact.
func absenceAfterTheUnlock(t *testing.T, reason string) {
	f := newFixture(t)
	policy := networkPolicy(60000)
	if reason == "no_contact" {
		policy = networkPolicy(200)
	}
	policy.FocusMillis = 400
	f.status.Policy = policy
	f.status.NetworkLocksLeft = 2
	var polls atomic.Int64
	f.onStatus = func(*http.Request) { polls.Add(1) }
	var link connection
	link.install(f)
	var alerts atomic.Int64
	s := monitorClient(t, f, func(o *Options) { o.Monitoring.OnAlert = func(Incident) { alerts.Add(1) } })
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.HeartbeatAt != nil })
	link.down.Store(true)
	if reason == "network" {
		waitMonitor(t, s, func(v MonitoringView) bool { return v.NetworkLockAvailable })
		if _, err := s.RequestNetworkLock(); err != nil {
			t.Fatal(err)
		}
	} else {
		waitMonitor(t, s, func(v MonitoringView) bool { return v.Locked && v.LockReason == reason })
	}
	// The student goes to the network settings and stays there longer than the focus threshold.
	ReportNativeFocus(s, false)
	time.Sleep(700 * time.Millisecond)
	if alerts.Load() != 0 {
		t.Fatalf("the alarm sounded %d times during the lock", alerts.Load())
	}
	link.down.Store(false)
	waitUntil(t, func() bool { delivered, _ := link.delivered.Load().(string); return delivered == reason })
	setServerLock(f, reason)
	seen := polls.Load()
	waitUntil(t, func() bool { return polls.Load() >= seen+2 })
	// A proctor unlocks the exam while the student is still out of the window.
	setServerLock(f, "")
	waitUntil(t, func() bool { return s.Snapshot().Locked == nil })
	unlocked := time.Now()
	waitUntil(t, func() bool { return alerts.Load() > 0 })
	if after := time.Since(unlocked); after < 300*time.Millisecond {
		t.Fatalf("the alarm started %v after the unlock, for an absence that began during the lock", after)
	}
	// The student comes back: it stops. A later absence counts from when it began, as always.
	ReportNativeFocus(s, true)
	ReportNativeFocus(s, false)
	before := alerts.Load()
	waitUntil(t, func() bool { return alerts.Load() > before })
}

// While the server cannot be reached, the focus reports are given up after their retries. That is no loss to warn the student
// about: the gap is on the server's record and the current state is sent again as soon as the connection is back.
func TestFocusReportsLostToAnUnreachableServerAreNotAWarning(t *testing.T) {
	f := newFixture(t)
	f.status.Policy = networkPolicy(60000)
	var link connection
	link.install(f)
	s := monitorClient(t, f, nil)
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.HeartbeatAt != nil && v.Queued == 0 })
	link.sever.Store(true)
	ReportNativeFocus(s, false) // the student goes to the network settings
	waitMonitor(t, s, func(v MonitoringView) bool { return v.Queued == 0 && v.EventError != "" })
	if v := s.Snapshot().Monitoring; v.Dropped != 0 || strings.Contains(v.EventError, "bounded retries") {
		t.Fatalf("reports lost to an unreachable server are called lost history: %+v", v)
	}
	// The connection is back: the server is told where the student is now, and no warning is left.
	f.mu.Lock()
	known := len(f.telemetry)
	f.mu.Unlock()
	link.sever.Store(false)
	waitUntil(t, func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		for _, record := range f.telemetry[known:] {
			if record.path == "/event" && record.kind == "focus_lost" {
				return true
			}
		}
		return false
	})
	v := waitMonitor(t, s, func(v MonitoringView) bool { return v.EventError == "" })
	if v.Dropped != 0 {
		t.Fatalf("dropped = %d", v.Dropped)
	}
}

// A server that answers with an error is a different matter: the report is lost, and the student and the proctor are told.
func TestFocusReportsRefusedByTheServerAreStillCountedAsLost(t *testing.T) {
	f := newFixture(t)
	f.status.Policy = networkPolicy(60000)
	var link connection
	link.install(f)
	s := monitorClient(t, f, nil)
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.HeartbeatAt != nil && v.Queued == 0 })
	link.down.Store(true) // answers 500
	ReportNativeFocus(s, false)
	waitMonitor(t, s, func(v MonitoringView) bool {
		return v.Dropped >= 1 && v.Queued == 0 && strings.Contains(v.EventError, "bounded retries")
	})
}

// The server locks the exam for lack of contact while the application is already back in touch, and the application learns of
// it from the status in the middle of the exam: the screenshots it takes from then on go under the record the status names.
func TestALockTheServerMakesDuringTheExamIsWatchedUnderItsRecord(t *testing.T) {
	f := newFixture(t)
	f.status.Policy = withLockShots(60000)
	s, shots := captureClient(t, f, nil)
	if s.Snapshot().Locked != nil {
		t.Fatal("locked before the server locked the exam")
	}
	f.mu.Lock()
	f.status.Locked = &Lock{Since: time.Now(), Reason: "no_contact", EventID: 556}
	f.mu.Unlock()
	waitUntil(t, func() bool { return len(sentShots(f)) >= 2 && s.Snapshot().Capture.Pending == 0 })
	for _, shot := range sentShots(f) {
		if shot.event != 556 {
			t.Fatalf("screenshot sent = %+v", shot)
		}
	}
	if shots.Load() < 2 {
		t.Fatalf("%d screenshots taken", shots.Load())
	}
}

// The application locks the exam itself for lack of contact, and the server made the same lock: the status, which gets through
// before the report of the lock does, names the record. The report is not sent then, so the status is the only way the
// application learns which record its screenshots belong under; without it they would be held for good.
func TestAnOwnLockForLackOfContactTheServerConfirmsIsBoundToItsRecord(t *testing.T) {
	f := newFixture(t)
	f.status.Policy = withLockShots(150)
	var link connection
	link.install(f)
	s, shots := captureClient(t, f, nil)
	link.down.Store(true)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.Locked && v.LockReason == "no_contact" })
	waitUntil(t, func() bool { return shots.Load() >= 2 && s.Snapshot().Capture.Pending >= 2 })
	// The status gets through while the heartbeat and the events do not, and says that the server locked the exam too.
	f.mu.Lock()
	f.status.Locked = &Lock{Since: time.Now(), Reason: "no_contact", EventID: 778}
	f.mu.Unlock()
	waitMonitor(t, s, func(v MonitoringView) bool { return v.Locked })
	time.Sleep(200 * time.Millisecond)
	link.down.Store(false)
	waitUntil(t, func() bool { return len(sentShots(f)) >= 2 && s.Snapshot().Capture.Pending == 0 })
	for _, shot := range sentShots(f) {
		if shot.event != 778 {
			t.Fatalf("screenshot sent = %+v", shot)
		}
	}
}

// An older server does not name the record of a lock, so the screenshots of a lock the application made and the server confirmed
// can never be sent. They are held while the lock lasts, in case it comes, and let go a while after it ends instead of being
// shown as pending for the rest of the exam.
func TestScreenshotsWithoutARecordAreLetGoAfterTheLockEnds(t *testing.T) {
	f := newFixture(t)
	f.status.Policy = withLockShots(150)
	var polls atomic.Int64
	f.onStatus = func(*http.Request) { polls.Add(1) }
	var link connection
	link.install(f)
	s, shots := captureClient(t, f, func(o *Options) { o.Screenshots.UnboundGrace = 150 * time.Millisecond })
	link.down.Store(true)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.Locked && v.LockReason == "no_contact" })
	waitUntil(t, func() bool { return shots.Load() >= 2 && s.Snapshot().Capture.Pending >= 2 })
	setServerLock(f, "no_contact") // confirmed, with no record named
	seen := polls.Load()
	waitUntil(t, func() bool { return polls.Load() >= seen+2 })
	time.Sleep(300 * time.Millisecond)
	if s.Snapshot().Capture.Pending < 2 {
		t.Fatalf("the screenshots were let go while the lock lasted: %d pending", s.Snapshot().Capture.Pending)
	}
	// The connection is back and the server has heard from the application, as it must have for a proctor to unlock the exam.
	link.down.Store(false)
	time.Sleep(200 * time.Millisecond)
	setServerLock(f, "")
	waitUntil(t, func() bool { return s.Snapshot().Locked == nil })
	waitUntil(t, func() bool { return s.Snapshot().Capture.Pending == 0 })
	if sent := sentShots(f); len(sent) != 0 {
		t.Fatalf("%d screenshots were sent with no record to send them under", len(sent))
	}
}

// The end of the exam lets go of them at once: there is no lock left to learn a record from, and the student is told to keep
// the window open until what is pending has been uploaded.
func TestScreenshotsWithoutARecordAreLetGoWhenTheExamEnds(t *testing.T) {
	f := newFixture(t)
	f.status.Policy = withLockShots(150)
	var link connection
	link.install(f)
	s, shots := captureClient(t, f, func(o *Options) { o.Screenshots.UnboundGrace = time.Hour })
	link.down.Store(true)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.Locked && v.LockReason == "no_contact" })
	waitUntil(t, func() bool { return shots.Load() >= 2 && s.Snapshot().Capture.Pending >= 2 })
	setServerLock(f, "no_contact")
	waitMonitor(t, s, func(v MonitoringView) bool { return v.Locked })
	f.mu.Lock()
	f.status.State, f.status.SecondsRemaining, f.status.Locked = "submitted", 0, nil
	f.mu.Unlock()
	waitView(t, s, func(v View) bool { return v.Phase == "complete" })
	waitUntil(t, func() bool { return s.Snapshot().Capture.Pending == 0 })
}

// The report of the lock the student asked for is sent while the server cannot be reached, and is sent again until it gets
// through: a try that got no answer is no loss, and must not leave "history may be incomplete" on the exam for the rest of it.
func TestTheNetworkLockReportLostToAnUnreachableServerIsNotAWarning(t *testing.T) {
	f := newFixture(t)
	f.status.Policy = networkPolicy(60000)
	f.status.NetworkLocksLeft = 2
	var link connection
	link.install(f)
	var attempts atomic.Int64
	inner := f.onTelemetry
	f.onTelemetry = func(w http.ResponseWriter, r *http.Request, record telemetryRecord) bool {
		if record.kind == "away_lock" {
			attempts.Add(1)
		}
		return inner(w, r, record)
	}
	s := monitorClient(t, f, nil)
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.HeartbeatAt != nil && v.Queued == 0 })
	link.sever.Store(true)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.NetworkLockAvailable })
	if _, err := s.RequestNetworkLock(); err != nil {
		t.Fatal(err)
	}
	// More than two rounds of retries have gone by without an answer.
	waitUntil(t, func() bool { return attempts.Load() >= 7 })
	if v := s.Snapshot().Monitoring; v.Dropped != 0 || strings.Contains(v.EventError, "bounded retries") {
		t.Fatalf("a report that got no answer is called lost history: %+v", v)
	}
	// The connection is back, the report gets through, and no warning is left.
	link.sever.Store(false)
	waitUntil(t, func() bool { reason, _ := link.delivered.Load().(string); return reason == "network" })
	v := waitMonitor(t, s, func(v MonitoringView) bool { return v.EventError == "" })
	if v.Dropped != 0 {
		t.Fatalf("dropped = %d", v.Dropped)
	}
}

// The alarm of an absence that was under way when the exam locked for the network stops with the lock, as the lock is what lets
// the student use the computer to reconnect; when a proctor unlocks the exam and the student is still away, it counts from the
// unlock, not from the beeps it had reached.
func TestTheAlarmStopsWhenANoContactLockFindsTheStudentAwayAndCountsFromTheUnlock(t *testing.T) {
	f := newFixture(t)
	policy := networkPolicy(300)
	policy.FocusMillis = 40
	f.status.Policy = policy
	var polls atomic.Int64
	f.onStatus = func(*http.Request) { polls.Add(1) }
	var link connection
	link.install(f)
	var alerts atomic.Int64
	s := monitorClient(t, f, func(o *Options) { o.Monitoring.OnAlert = func(Incident) { alerts.Add(1) } })
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.HeartbeatAt != nil })
	// The student leaves the window with the connection fine: this is an absence like any other, and the alarm sounds.
	ReportNativeFocus(s, false)
	waitUntil(t, func() bool { return alerts.Load() >= 10 })
	link.down.Store(true)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.Locked && v.LockReason == "no_contact" })
	time.Sleep(60 * time.Millisecond)
	quiet := alerts.Load()
	time.Sleep(300 * time.Millisecond)
	if alerts.Load() != quiet {
		t.Fatalf("the alarm went on %d times after the exam locked for the network", alerts.Load()-quiet)
	}
	// The connection is back and the server has the lock; a proctor unlocks it while the student is still away.
	link.down.Store(false)
	waitUntil(t, func() bool { reason, _ := link.delivered.Load().(string); return reason == "no_contact" })
	setServerLock(f, "no_contact")
	seen := polls.Load()
	waitUntil(t, func() bool { return polls.Load() >= seen+2 })
	setServerLock(f, "")
	waitUntil(t, func() bool { return s.Snapshot().Locked == nil })
	unlocked := time.Now()
	waitUntil(t, func() bool { return alerts.Load() > quiet })
	if after := time.Since(unlocked); after > 300*time.Millisecond {
		t.Fatalf("the alarm started %v after the unlock: it had to reach the beeps of the earlier absence first", after)
	}
}
