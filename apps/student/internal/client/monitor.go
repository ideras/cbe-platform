package client

import (
	"context"
	"errors"
	"math"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// Policy is the server's: past FocusMillis a focus loss is sustained; the alert then repeats every AlertMillis and the
// screen is captured at once and every CaptureMillis after, at most IncidentCaptures times.
type Policy struct {
	HeartbeatMillis int64 `json:"heartbeat_interval_ms"`
	OverdueMillis   int64 `json:"overdue_threshold_ms"`
	FocusMillis     int64 `json:"focus_threshold_ms"`
	AlertMillis     int64 `json:"alert_interval_ms"`
	CaptureMillis   int64 `json:"capture_interval_ms"`
	// LockAwayAfterMillis is how long one absence may last before the exam locks; 0 means the lock is off.
	LockAwayAfterMillis int64 `json:"lock_away_after_ms"`
	// LockNoContactAfterMillis is how long the application may be out of contact before the exam locks; 0 means off.
	LockNoContactAfterMillis int64 `json:"lock_no_contact_after_ms"`
	IncidentCaptures         int   `json:"incident_captures"`
	SessionCaptures          int   `json:"session_captures"`
}

// valid checks the signals; alert and capture settings left out are filled in from the local defaults (withDefaults).
func (p Policy) valid() bool {
	const most = math.MaxInt64 / int64(time.Millisecond)
	return p.HeartbeatMillis > 0 && p.OverdueMillis >= p.HeartbeatMillis && p.FocusMillis > 0 && p.HeartbeatMillis <= most && p.FocusMillis <= most &&
		p.AlertMillis >= 0 && p.AlertMillis <= most && p.CaptureMillis >= 0 && p.CaptureMillis <= most && p.IncidentCaptures >= 0 && p.SessionCaptures >= 0 &&
		p.LockAwayAfterMillis >= 0 && p.LockAwayAfterMillis <= most && p.LockNoContactAfterMillis >= 0 && p.LockNoContactAfterMillis <= most
}

func (p Policy) withDefaults(o MonitorOptions) Policy {
	if p.AlertMillis == 0 {
		p.AlertMillis = max(1, o.AlertInterval.Milliseconds())
	}
	if p.CaptureMillis == 0 {
		p.CaptureMillis = max(1, o.CaptureInterval.Milliseconds())
	}
	if p.IncidentCaptures == 0 {
		p.IncidentCaptures = o.IncidentCaptures
	}
	if p.SessionCaptures < p.IncidentCaptures {
		p.SessionCaptures = p.IncidentCaptures
	}
	return p
}

type Incident struct {
	Sequence     uint64    `json:"local_incident"`
	LossEventID  int64     `json:"event_log_id"`
	ClientLostAt time.Time `json:"client_lost_at"`
	Alerted      bool      `json:"alerted"`
	// Beep counts the alerts of this focus loss; the first is 1.
	Beep   int  `json:"beep"`
	Hooked bool `json:"hooked"`
	// Captures counts the screenshots this focus loss has asked for.
	Captures int `json:"captures"`
}
type MonitoringView struct {
	Active         bool       `json:"active"`
	Policy         Policy     `json:"policy"`
	PolicySource   string     `json:"policy_source"`
	Focus          string     `json:"local_focus"`
	HeartbeatAt    *time.Time `json:"heartbeat_server_time,omitempty"`
	HeartbeatError string     `json:"heartbeat_error"`
	EventError     string     `json:"event_error"`
	AlertError     string     `json:"alert_error"`
	Dropped        uint64     `json:"dropped_events"`
	Queued         int        `json:"queued_events"`
	Incident       *Incident  `json:"incident,omitempty"`
	StoppedReason  string     `json:"stopped_reason"`
	// Locked says that the student application locked the exam itself; the server is told and locks it too.
	Locked     bool       `json:"locked"`
	LockReason string     `json:"lock_reason,omitempty"`
	LockSince  *time.Time `json:"lock_since,omitempty"`
	// ContactLost says that nothing has reached the server for as long as the policy calls a lost connection. While it holds,
	// NoContactMillis is how long it has lasted and NoContactLockInMillis how long is left before the application locks the
	// exam for it (nil when that lock is off). Neither is set once the exam is locked.
	ContactLost           bool   `json:"contact_lost"`
	NoContactMillis       int64  `json:"no_contact_ms"`
	NoContactLockInMillis *int64 `json:"no_contact_lock_in_ms,omitempty"`
	// NetworkLockAvailable says that the student may lock the exam now to reconnect to the exam network: the connection is
	// lost and they have requests left. NetworkLocksLeft is how many requests are left.
	NetworkLockAvailable bool `json:"network_lock_available"`
	NetworkLocksLeft     int  `json:"network_locks_left"`
	// Remote is what the application has found that says the computer is watched or controlled from elsewhere: a Remote Desktop
	// session, or remote-control software running. The instructor is told of it; the student is told that they are.
	Remote []RemoteFinding `json:"remote"`
}

// RemoteFinding is one thing found that says the computer is watched or controlled from elsewhere: Kind is "remote_session" or
// "remote_tool", Name the session or the program, and Detail more when there is, such as the computer a session comes from.
type RemoteFinding struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Detail string `json:"detail,omitempty"`
}

// MonitorOptions are the local defaults until the server's policy arrives, and the callbacks. OnAlert runs for every beep
// of a sustained focus loss; OnCapture asks for its index-th screenshot.
type MonitorOptions struct {
	Heartbeat, FocusThreshold, Tick, CloseTimeout time.Duration
	AlertInterval, CaptureInterval                time.Duration
	Attempts, QueueLimit, IncidentCaptures        int
	OnParticipation                               func()
	OnAlert                                       func(Incident)
	OnCapture                                     func(Incident, int)
	OnIncident                                    func(Incident)
	OnLossAcknowledged                            func(Incident)
	// OnLockCapture asks for the index-th screenshot of a lock the student asked for (at most limit of them), and OnLockRecorded
	// says which record the server made of that lock, once it has answered the report.
	OnLockCapture  func(lock uint64, index, limit int)
	OnLockRecorded func(lock uint64, event int64)
	// OnWindowRelaxed says when the window should be small enough for the student to reach the computer's network settings
	// (true) and when it should be full screen again (false). It is called on one goroutine, never with the monitor locked.
	OnWindowRelaxed func(relaxed bool)
	// OnLockEnded says that a lock the application was watching by screenshots has ended, so the capture manager can let go of
	// the screenshots that never got a record to be sent under.
	OnLockEnded func(lock uint64)
	// OnSessionAway says what took the student's session off screen when it left, and "" when it is back. It is called with the
	// monitor locked, so it must only record the value.
	OnSessionAway func(reason string)
	Sound         func() SoundReport
	// SoundCheck, when set, takes the place of Sound for a heartbeat: it asks for a fresh check of the sound and waits for it
	// a bounded time.
	SoundCheck func(context.Context) SoundReport
}

func DefaultMonitorOptions() MonitorOptions {
	return MonitorOptions{Heartbeat: 15 * time.Second, FocusThreshold: 5 * time.Second, AlertInterval: 2 * time.Second, CaptureInterval: 5 * time.Second, IncidentCaptures: 30,
		Tick: 100 * time.Millisecond, CloseTimeout: time.Second, Attempts: 3, QueueLimit: 128}
}
func (o MonitorOptions) validate() error {
	if o.Heartbeat <= 0 || o.FocusThreshold <= 0 || o.AlertInterval <= 0 || o.CaptureInterval <= 0 || o.IncidentCaptures < 1 || o.Tick <= 0 || o.CloseTimeout <= 0 || o.Attempts < 1 || o.QueueLimit < 1 {
		return errors.New("Monitoring intervals and queue/retry bounds must be positive.")
	}
	return nil
}

type clientEvent struct {
	// source says what the report comes from: the window ("native_window", the default) or the session ("session_inactive").
	// session is what took the session off screen, for a report of an absence it was part of.
	source   string
	session  string
	remote   []RemoteFinding // for a remote_changed event: everything found at that moment
	kind     string
	at       time.Time
	incident uint64
	// reason says why the application locked the exam, and lock which lock it is, for an away_lock event.
	reason string
	lock   uint64
	// since is when the lock began, for an away_lock event: the report may be made long after, with the server out of reach.
	since  time.Time
	closed chan struct{}
}
type eventAck struct {
	ID         int64     `json:"event_log_id"`
	ServerTime time.Time `json:"server_time"`
}
type monitor struct {
	nativeMu                                                   sync.Mutex
	mu                                                         sync.Mutex
	ctx                                                        context.Context
	cancel                                                     context.CancelFunc
	wg                                                         sync.WaitGroup
	closeOnce                                                  sync.Once
	http                                                       *http.Client
	options                                                    MonitorOptions
	retryMin, retryMax                                         time.Duration
	eventWake, heartbeatWake                                   chan struct{}
	pollNow                                                    chan struct{}
	base, token, hostname                                      string
	active, ready, closing, known, focused, resync, recovering bool
	// focused is whether the student is in the exam window: the window has the focus and the student's session is the one on
	// screen. windowKnown, windowFocused and sessionAway are the two things it is made of, as the native side reports them: the
	// window loses its focus when the student switches to another window, and the session stops being on screen when the
	// student switches to a text console or to another user, which does not change the focus of the window at all.
	windowKnown, windowFocused, sessionAway bool
	// awayReason is what took the session off screen, from the moment it left until the absence it began ends. The window may lose
	// its focus first, as it can when the student switches user, so the return names the reason too.
	awayReason string
	// remoteFound is what remote_changed last said, and remoteTools the program names the server asked to look for.
	remoteFound []RemoteFinding
	remoteTools []string
	// remoteLost is that a remote_changed report got no answer from the server, so what it holds may not be what the server has: it
	// is said again when the connection is back, even when it is that nothing is found.
	remoteLost bool
	// locked is the exam's lock as the application knows it. The application locks itself when its own measurement reaches a
	// limit; lockConfirmed says that a status from the server has reported the lock since, so a later status without one
	// means a proctor unlocked the exam. lockIncident is the absence the student was in when the exam locked (0 for none):
	// only its alarm keeps going while the exam is locked.
	locked, lockConfirmed, lockReportPending           bool
	lockReason                                         string
	lockSince, lockReportedAt, lastContact, unlockedAt time.Time
	lockIncident                                       uint64
	// networkLocksLeft is how many more times the student may lock the exam themselves to reconnect to the network, as the
	// server last said; a request the server has not recorded yet is already taken out of it.
	networkLocksLeft int
	// lockSeq counts the locks of this run; shotLock is the one the student asked for, whose screenshots the application takes,
	// and lockCaptures how many it has asked for.
	lockSeq, shotLock uint64
	// shotFrom is when the screenshots of that lock are counted from. bindSeq and bindEvent are a lock whose record the server
	// named in its status, which the capture manager has yet to be told.
	shotFrom  time.Time
	bindSeq   uint64
	bindEvent int64
	// boundSeq is the lock whose record the status named last, and endedSeq a watched lock that ended, which the capture
	// manager has yet to be told.
	boundSeq, endedSeq uint64
	lockCaptures       int
	// windowRelaxed is what OnWindowRelaxed was last told.
	windowRelaxed bool
	// awayFrom is when a lock the student asked for ended. The student left the window to change network, as the lock allowed, so
	// an absence they are still in counts from here: it is then that they are expected back.
	awayFrom                                              time.Time
	policy                                                Policy
	policySource                                          string
	queue                                                 []*clientEvent
	incident                                              *Incident
	sequence                                              uint64
	heartbeatAt                                           *time.Time
	heartbeatError, eventError, alertError, stoppedReason string
	dropped                                               uint64
}

func newMonitor(ctx context.Context, h *http.Client, options MonitorOptions, retryMin, retryMax time.Duration, pollNow chan struct{}) *monitor {
	ctx, cancel := context.WithCancel(ctx)
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown-host"
	}
	hostname = strings.TrimSpace(hostname)
	if hostname == "" || strings.ContainsFunc(hostname, unicode.IsControl) {
		hostname = "unknown-host"
	}
	for len(hostname) > 255 {
		_, size := utf8.DecodeLastRuneInString(hostname)
		hostname = hostname[:len(hostname)-size]
	}
	m := &monitor{ctx: ctx, cancel: cancel, http: h, options: options, retryMin: retryMin, retryMax: retryMax, eventWake: make(chan struct{}, 1), heartbeatWake: make(chan struct{}, 1), pollNow: pollNow, hostname: hostname, lastContact: time.Now(), policy: Policy{HeartbeatMillis: max(1, options.Heartbeat.Milliseconds()), OverdueMillis: max(1, options.Heartbeat.Milliseconds()*2), FocusMillis: max(1, options.FocusThreshold.Milliseconds()),
		AlertMillis: max(1, options.AlertInterval.Milliseconds()), CaptureMillis: max(1, options.CaptureInterval.Milliseconds()), IncidentCaptures: options.IncidentCaptures, SessionCaptures: options.IncidentCaptures}, policySource: "local defaults (server policy unavailable)"}
	m.wg.Add(3)
	go m.heartbeatLoop()
	go m.eventLoop()
	go m.incidentLoop()
	return m
}
func (m *monitor) setPolicyLocked(policy *Policy) {
	if policy != nil && policy.valid() {
		next := policy.withDefaults(m.options)
		changed := m.policy != next
		m.policy = next
		m.policySource = "server"
		if changed {
			signal(m.heartbeatWake)
		}
	}
}
func (m *monitor) setPolicy(policy *Policy) { m.mu.Lock(); m.setPolicyLocked(policy); m.mu.Unlock() }
func (m *monitor) start(base, token string, policy *Policy) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active || m.token != "" || m.closing {
		return
	}
	m.base, m.token, m.active = base, token, true
	// The window is full screen from the login on (OnParticipation); a lock that asks otherwise says so afterwards.
	m.windowRelaxed = false
	// The login reached the server: the no-contact clock starts now, however long the application waited before it.
	m.lastContact = time.Now()
	if policy != nil && policy.valid() {
		m.policy = policy.withDefaults(m.options)
		m.policySource = "server"
	}
	m.enqueueLocked(&clientEvent{kind: "app_started", at: time.Now()})
	if m.known {
		m.baselineLocked(time.Now())
	}
	m.remoteBaselineLocked()
	signal(m.heartbeatWake)
}

// enqueueLocked queues an event for the server; false means the queue was full and the event was lost.
func (m *monitor) enqueueLocked(event *clientEvent) bool {
	if len(m.queue) >= m.options.QueueLimit {
		m.dropped++
		m.eventError = "Focus-event queue full; some reports were lost. Monitoring is incomplete."
		m.resync = true
		if event.closed != nil {
			close(event.closed)
		}
		return false
	}
	m.queue = append(m.queue, event)
	signal(m.eventWake)
	return true
}
func (m *monitor) baselineLocked(now time.Time) {
	kind := "focus_regained"
	var seq uint64
	if !m.focused {
		kind = "focus_lost"
		if m.incident == nil {
			m.sequence++
			m.incident = &Incident{Sequence: m.sequence, ClientLostAt: now}
		}
		seq = m.incident.Sequence
	}
	m.enqueueLocked(&clientEvent{kind: kind, at: now, incident: seq, source: m.focusSource(), session: m.awayReason})
}
func (m *monitor) focus(focused bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.windowKnown, m.windowFocused = true, focused
	m.applyFocusLocked("native_window")
}

// session takes whether the student's session is the one on screen, and when it is not, what took it off. A student who
// switches to a text console (Ctrl+Alt+F3) or to another user leaves the exam as much as one who switches to another window, but
// the window may keep its focus, so this is told apart from it.
func (m *monitor) session(active bool, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessionAway == !active {
		return
	}
	m.sessionAway = !active
	source := "session_active"
	if m.sessionAway {
		source = "session_inactive"
		m.awayReason = reason
	} else {
		reason = ""
	}
	if m.options.OnSessionAway != nil {
		m.options.OnSessionAway(reason)
	}
	m.applyFocusLocked(source)
}

// remote takes everything the watch has found that shows the computer is watched or controlled from elsewhere, and tells the
// server when that is not what it was told last.
func (m *monitor) remote(findings []RemoteFinding) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if slices.Equal(m.remoteFound, findings) {
		return
	}
	m.remoteFound = slices.Clone(findings)
	if m.active && !m.closing {
		m.enqueueLocked(&clientEvent{kind: "remote_changed", at: time.Now(), source: "remote_watch", remote: slices.Clone(findings)})
	}
}

// remoteBaselineLocked tells the server what has been found, as at the login or after contact was lost: what it was told before may
// not have reached it. Nothing is said when nothing has been found, as that is what it assumes, unless a report that said so (that
// something had gone) got no answer: the server would go on showing what it had been told before.
func (m *monitor) remoteBaselineLocked() {
	if m.active && !m.closing && (len(m.remoteFound) > 0 || m.remoteLost) {
		m.remoteLost = false
		m.enqueueLocked(&clientEvent{kind: "remote_changed", at: time.Now(), source: "remote_watch", remote: slices.Clone(m.remoteFound)})
	}
}
func (m *monitor) setRemoteTools(tools []string) {
	m.mu.Lock()
	m.remoteTools = slices.Clone(tools)
	m.mu.Unlock()
}
func (m *monitor) remoteToolList() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.remoteTools)
}

// focusSource is what the focus being lost is down to now.
func (m *monitor) focusSource() string {
	if m.sessionAway {
		return "session_inactive"
	}
	return "native_window"
}

// applyFocusLocked works out whether the student is in the exam window and, when that changed, reports it.
func (m *monitor) applyFocusLocked(source string) {
	if !m.windowKnown {
		return
	}
	focused := m.windowFocused && !m.sessionAway
	if m.known && m.focused == focused {
		return
	}
	m.known, m.focused = true, focused
	// The reason the session left belongs to the absence it began: it goes with the return, whether or not that is reported.
	reason := m.awayReason
	if focused {
		m.awayReason = ""
	}
	if !m.active || m.closing {
		return
	}
	now := time.Now()
	kind := "focus_regained"
	var seq uint64
	if !focused {
		kind = "focus_lost"
		m.sequence++
		m.incident = &Incident{Sequence: m.sequence, ClientLostAt: now}
		seq = m.sequence
	}
	m.enqueueLocked(&clientEvent{kind: kind, at: now, incident: seq, source: source, session: reason})
}
func (m *monitor) stopLocked(reason string) {
	m.cancel()
	m.active = false
	m.stoppedReason = reason
	m.queue = nil
	m.resync = false
	signal(m.eventWake)
	signal(m.heartbeatWake)
}
func (m *monitor) stop(reason string) { m.mu.Lock(); m.stopLocked(reason); m.mu.Unlock() }
func (m *monitor) view() MonitoringView {
	m.mu.Lock()
	defer m.mu.Unlock()
	v := MonitoringView{Active: m.active, Policy: m.policy, PolicySource: m.policySource, Focus: "unknown", HeartbeatError: m.heartbeatError, EventError: m.eventError, AlertError: m.alertError, Dropped: m.dropped, Queued: len(m.queue), StoppedReason: m.stoppedReason}
	if m.known {
		if m.focused {
			v.Focus = "focused"
		} else {
			v.Focus = "unfocused"
		}
	}
	if m.heartbeatAt != nil {
		copy := *m.heartbeatAt
		v.HeartbeatAt = &copy
	}
	if m.incident != nil {
		copy := *m.incident
		v.Incident = &copy
	}
	v.Locked, v.LockReason = m.locked, m.lockReason
	if !m.lockSince.IsZero() {
		since := m.lockSince
		v.LockSince = &since
	}
	v.NetworkLocksLeft = m.networkLocksLeft
	v.Remote = append([]RemoteFinding(nil), m.remoteFound...)
	if now := time.Now(); !m.locked && m.contactLostLocked(now) {
		v.ContactLost = true
		v.NoContactMillis = now.Sub(m.lastContact).Milliseconds()
		if limit := m.policy.LockNoContactAfterMillis; limit > 0 {
			left := max(0, limit-v.NoContactMillis)
			v.NoContactLockInMillis = &left
		}
		v.NetworkLockAvailable = m.networkLocksLeft > 0
	}
	return v
}

// contactLostLocked says that nothing has reached the server for as long as the policy calls a lost connection.
func (m *monitor) contactLostLocked(now time.Time) bool {
	return m.active && !m.closing && !m.lastContact.IsZero() && now.Sub(m.lastContact) >= time.Duration(m.policy.OverdueMillis)*time.Millisecond
}

// networkLockReason is the reason of a lock the student asked for, and noContactLockReason the one for lack of contact.
const (
	networkLockReason   = "network"
	noContactLockReason = "no_contact"
)

// lockForNetwork says whether a lock is one of the two that come from the network: the student asked for it to change network,
// or the app could not reach the server for too long. The student needs the computer's network settings during either, so the
// window stops being full screen, and the application watches the screen by screenshots meanwhile instead of by the alarm.
func lockForNetwork(reason string) bool {
	return reason == networkLockReason || reason == noContactLockReason
}

// setNetworkLocksLeft takes how many requests the server says the student has left. A request the server has not recorded
// yet is already counted here, and its answer must not give it back.
func (m *monitor) setNetworkLocksLeft(left int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.locked && m.lockReason == networkLockReason && !m.lockConfirmed {
		return
	}
	m.networkLocksLeft = max(0, left)
}

// requestNetworkLock locks the exam at the student's request, so they can go and reconnect to the exam network without the
// alarm. It is allowed only while the connection is lost and the student has requests left; the server is told as soon as
// it can be reached, and only a proctor unlocks the exam.
func (m *monitor) requestNetworkLock() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	switch {
	case !m.active || m.closing:
		return errors.New("The exam is not being monitored right now.")
	case m.locked:
		return errors.New("Your exam is locked already. Call your instructor.")
	case m.networkLocksLeft <= 0:
		return errors.New("You cannot lock the exam to change network any more. Call your instructor.")
	case !m.contactLostLocked(now):
		return errors.New("The exam can be locked this way only while the app cannot reach the server.")
	}
	m.networkLocksLeft--
	m.lockLocked(networkLockReason, now)
	m.watchLockLocked(now)
	return nil
}

// watchLockLocked has the application take screenshots while the current lock lasts, the first at from and the next every
// capture interval, and hold them until the server has recorded the lock.
func (m *monitor) watchLockLocked(from time.Time) { m.shotLock, m.shotFrom = m.lockSeq, from }

// isLocked says whether the exam is locked, by the application itself or by the server.
func (m *monitor) isLocked() bool { m.mu.Lock(); defer m.mu.Unlock(); return m.locked }

// lockLocked locks the exam. The alarm of the absence the student is in, if any, keeps going until the student returns, unless the
// lock is one for the network: that lock lets the student use the computer to reconnect, and the alarm is quiet during it.
func (m *monitor) lockLocked(reason string, since time.Time) {
	m.lockSeq++
	m.lockCaptures = 0
	m.locked, m.lockReason, m.lockSince, m.lockIncident = true, reason, since, 0
	if m.known && !m.focused && m.incident != nil && !lockForNetwork(reason) {
		m.lockIncident = m.incident.Sequence
	}
}

// setServerLock tells the monitor what the server's status says about the lock. A lock the status reports is confirmed;
// a confirmed lock ends when a later status reports none, which only a proctor's unlock does. A lock the application made
// itself stays until the server has reported it, whatever the status said before.
func (m *monitor) setServerLock(lock *Lock) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if lock != nil {
		if !m.locked {
			m.lockLocked(lock.Reason, lock.Since)
			if lockForNetwork(lock.Reason) && lock.EventID > 0 {
				// The application did not make this lock: the server did, or the application was started during it. The server
				// names its record, so the screenshots of the lock go under it, taken from now on.
				m.watchLockLocked(time.Now())
			}
		}
		if lock.EventID > 0 && m.shotLock == m.lockSeq && m.boundSeq != m.lockSeq {
			// The server names the record of the lock the screenshots are being taken for. When the application made the lock itself
			// and the server made it too, this is the only way to learn it: the report of the lock is not sent once the status has
			// confirmed it, so there is no acknowledgment to take the record from.
			m.boundSeq = m.lockSeq
			m.bindSeq, m.bindEvent = m.lockSeq, lock.EventID
		}
		m.lockConfirmed = true
		return
	}
	if m.locked && m.lockConfirmed {
		if lockForNetwork(m.lockReason) {
			m.awayFrom = time.Now()
			if m.incident != nil {
				// An absence that was under way when the lock came counts from the unlock: its beeps and screenshots start over.
				m.incident.Beep, m.incident.Captures = 0, 0
			}
		}
		if m.shotLock == m.lockSeq {
			m.endedSeq = m.lockSeq
		}
		m.locked, m.lockConfirmed, m.lockReason, m.lockSince, m.lockIncident = false, false, "", time.Time{}, 0
		m.lockReportedAt, m.lockReportPending = time.Time{}, false
		// The next absence counts from the unlock, even when the student has not come back to the window yet.
		m.unlockedAt = time.Now()
	}
}
func (m *monitor) heartbeatLoop() {
	defer m.wg.Done()
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	var backoff time.Duration
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-m.heartbeatWake:
		case <-timer.C:
		}
		m.mu.Lock()
		active := m.active && !m.closing
		base, token := m.base, m.token
		interval := time.Duration(m.policy.HeartbeatMillis) * time.Millisecond
		m.mu.Unlock()
		if !active {
			timer.Reset(time.Hour)
			continue
		}
		var reply struct {
			ServerTime time.Time `json:"server_time"`
		}
		var sound SoundReport
		if m.options.SoundCheck != nil {
			sound = m.options.SoundCheck(m.ctx)
		} else if m.options.Sound != nil {
			sound = m.options.Sound()
		}
		err := requestJSON(m.ctx, m.http, base, token, http.MethodPost, "/heartbeat", struct {
			Hostname string `json:"hostname"`
			SoundReport
		}{m.hostname, sound}, &reply)
		if err == nil && reply.ServerTime.IsZero() {
			err = &apiFailure{message: "Heartbeat acknowledgment is invalid."}
		}
		m.mu.Lock()
		if !m.active || m.closing {
			m.mu.Unlock()
			continue
		}
		if err != nil {
			m.heartbeatError = "Heartbeat reporting unavailable; presence cannot be confirmed."
			m.recovering = true
			if failureCode(err) == 401 || failureCode(err) == 403 {
				m.stopLocked("Telemetry authorization ended; waiting for authoritative session status.")
				signal(m.pollNow)
			}
			backoff = min(max(backoff*2, m.retryMin), m.retryMax)
			if retryable(err) {
				interval = min(interval, backoff)
			}
		} else {
			m.heartbeatAt = &reply.ServerTime
			m.lastContact = time.Now()
			m.heartbeatError = ""
			backoff = 0
			if m.ready && m.recovering {
				if m.known {
					m.baselineLocked(time.Now())
				}
				m.remoteBaselineLocked()
			}
			m.ready = true
			m.recovering = false
			signal(m.eventWake)
		}
		m.mu.Unlock()
		timer.Reset(interval)
	}
}
func (m *monitor) eventLoop() {
	defer m.wg.Done()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-m.eventWake:
		}
		for {
			m.mu.Lock()
			if !m.active || !m.ready || len(m.queue) == 0 {
				m.mu.Unlock()
				break
			}
			event := m.queue[0]
			if event.kind == "away_lock" && (!m.locked || m.lockConfirmed) {
				// The server reported the lock, or a proctor lifted it already: the report would only lock the exam again.
				m.queue = m.queue[1:]
				m.lockReportPending = false
				m.mu.Unlock()
				continue
			}
			base, token := m.base, m.token
			m.mu.Unlock()
			var reply eventAck
			var err error
			backoff := m.retryMin
			for attempt := 0; attempt < m.options.Attempts; attempt++ {
				if m.ctx.Err() != nil {
					return
				}
				reply = eventAck{}
				source := event.source
				if source == "" {
					source = "native_window"
				}
				detail := map[string]any{"client_time": event.at.UTC().Format(time.RFC3339Nano), "local_incident": event.incident, "source": source}
				if event.session != "" {
					detail["session_reason"] = event.session
				}
				if event.kind == "remote_changed" {
					detail["source"] = "remote_watch"
					detail["active"] = append([]RemoteFinding{}, event.remote...)
				}
				if event.kind == "away_lock" && event.reason != "" {
					detail["reason"] = event.reason
					if !event.since.IsZero() {
						// The age is measured here, so the student's clock being off the server's does not matter.
						detail["lock_age_ms"] = time.Since(event.since).Milliseconds()
					}
				}
				err = requestJSON(m.ctx, m.http, base, token, http.MethodPost, "/event", struct {
					Type   string         `json:"event_type"`
					Detail map[string]any `json:"detail"`
				}{event.kind, detail}, &reply)
				if err == nil && (reply.ID <= 0 || reply.ServerTime.IsZero()) {
					err = &apiFailure{message: "Event acknowledgment is invalid."}
				}
				if err == nil || !retryable(err) || attempt+1 == m.options.Attempts {
					break
				}
				m.mu.Lock()
				m.eventError = "Event reporting delayed; delivery is not yet confirmed."
				active := m.active
				m.mu.Unlock()
				if !active {
					break
				}
				timer := time.NewTimer(backoff)
				select {
				case <-m.ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
				backoff = min(backoff*2, m.retryMax)
			}
			m.mu.Lock()
			if event.closed != nil {
				close(event.closed)
				event.closed = nil
			}
			if !m.active {
				m.mu.Unlock()
				break
			}
			if len(m.queue) > 0 && m.queue[0] == event {
				m.queue = m.queue[1:]
			}
			if event.kind == "away_lock" {
				m.lockReportPending = false
			}
			if err != nil && unreachable(err) && (event.kind == "focus_lost" || event.kind == "focus_regained" || event.kind == "remote_changed" || event.kind == "away_lock") {
				// The server could not be reached, as it cannot while the student changes network. The server has the gap on its
				// record, and the first heartbeat that gets through sends the current focus state and what the watch found again, so
				// the history is not left with a wrong state: there is nothing lost to warn the student about. A lock report is sent
				// again every heartbeat interval until the server has it.
				m.recovering = true
				if event.kind == "remote_changed" {
					m.remoteLost = true
				}
			} else if err != nil {
				m.dropped++
				m.eventError = "An event could not be confirmed after bounded retries; monitoring history may be incomplete."
				if failureCode(err) == 401 || failureCode(err) == 403 {
					m.stopLocked("Telemetry authorization ended; waiting for authoritative session status.")
					signal(m.pollNow)
				}
			} else {
				m.lastContact = time.Now()
				if event.kind == "remote_changed" {
					m.remoteLost = false
				}
				if m.dropped == 0 {
					m.eventError = ""
				}
				if event.kind == "focus_lost" && m.incident != nil && event.incident == m.incident.Sequence {
					m.incident.LossEventID = reply.ID
				}
			}
			if len(m.queue) == 0 && m.resync && m.known && !m.closing && m.active {
				m.resync = false
				m.baselineLocked(time.Now())
			}
			m.mu.Unlock()
			if err == nil && event.kind == "focus_lost" && m.options.OnLossAcknowledged != nil {
				m.options.OnLossAcknowledged(Incident{Sequence: event.incident, LossEventID: reply.ID, ClientLostAt: event.at})
			}
			if err == nil && event.kind == "away_lock" && m.options.OnLockRecorded != nil {
				// The acknowledgment of the report is the record of the lock, which the screenshots of the lock are sent under.
				m.options.OnLockRecorded(event.lock, reply.ID)
			}
		}
	}
}
func (m *monitor) incidentLoop() {
	defer m.wg.Done()
	ticker := time.NewTicker(m.options.Tick)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
		}
		var alert, hook *Incident
		capture, lockShot, lockShotLimit := -1, -1, 0
		var lockShotSeq uint64
		m.mu.Lock()
		if m.active && !m.closing {
			now := time.Now()
			// The student application measures the absence itself, so it can lock the exam even while offline. An absence counts
			// from the latest unlock, if a proctor unlocked the exam while the student was still away.
			if !m.locked {
				reason := ""
				switch {
				case m.known && !m.focused && m.incident != nil && m.policy.LockAwayAfterMillis > 0 &&
					now.Sub(latest(m.incident.ClientLostAt, m.unlockedAt)) >= time.Duration(m.policy.LockAwayAfterMillis)*time.Millisecond:
					reason = "away"
				case m.policy.LockNoContactAfterMillis > 0 && !m.lastContact.IsZero() &&
					now.Sub(m.lastContact) >= time.Duration(m.policy.LockNoContactAfterMillis)*time.Millisecond:
					reason = "no_contact"
				}
				if reason != "" {
					m.lockLocked(reason, now)
					if reason == noContactLockReason {
						m.watchLockLocked(now)
					}
				}
			}
			// The server is told about a lock it has not reported yet, again every heartbeat interval until it does: a report
			// lost on the way would otherwise leave the exam locked here and open on the proctor's screen.
			if m.locked && !m.lockConfirmed && !m.lockReportPending &&
				(m.lockReportedAt.IsZero() || now.Sub(m.lockReportedAt) >= time.Duration(m.policy.HeartbeatMillis)*time.Millisecond) {
				m.lockReportedAt = now
				m.lockReportPending = m.enqueueLocked(&clientEvent{kind: "away_lock", at: now, reason: m.lockReason, lock: m.lockSeq, since: m.lockSince})
			}
			// A lock the student asked for is watched by screenshots, the first at once and then every capture interval. The
			// application holds them until the server has recorded the lock, since the student asked for it while the server was
			// out of reach.
			if m.locked && m.shotLock == m.lockSeq && m.policy.CaptureMillis > 0 {
				due := int(now.Sub(m.shotFrom)/(time.Duration(m.policy.CaptureMillis)*time.Millisecond)) + 1
				if m.lockCaptures < min(due, m.policy.IncidentCaptures) {
					lockShot, lockShotSeq, lockShotLimit = due-1, m.lockSeq, m.policy.IncidentCaptures
					m.lockCaptures = due
				}
			}
			// The alarm of the absence that caused the lock keeps going until the student returns to the window; a later
			// absence starts neither the alarm nor the screenshots.
			if m.known && !m.focused && m.incident != nil && (!m.locked || m.lockIncident != 0 && m.lockIncident == m.incident.Sequence) {
				// The alert repeats every AlertMillis and the screen is captured every CaptureMillis, both counted from the
				// threshold, until the student returns: focus regained replaces the incident.
				away := now.Sub(latest(m.incident.ClientLostAt, m.awayFrom))
				threshold := time.Duration(m.policy.FocusMillis) * time.Millisecond
				if away > threshold {
					sustained := away - threshold
					if beeps := int(sustained/(time.Duration(m.policy.AlertMillis)*time.Millisecond)) + 1; beeps > m.incident.Beep {
						m.incident.Alerted, m.incident.Beep = true, beeps
						copy := *m.incident
						alert = &copy
					}
					if !m.locked {
						due := int(sustained/(time.Duration(m.policy.CaptureMillis)*time.Millisecond)) + 1
						if m.incident.Captures < min(due, m.policy.IncidentCaptures) {
							// A capture missed while the computer was busy is not made up later: only the latest due one is asked for.
							capture = due - 1
							m.incident.Captures = due
						}
					}
					if m.incident.Alerted && !m.incident.Hooked && m.incident.LossEventID > 0 {
						m.incident.Hooked = true
						copy := *m.incident
						hook = &copy
					}
				}
			}
		}
		// The window is small while the student reconnects after asking for it, and full screen at any other time.
		var bindSeq, endedSeq uint64
		var bindEvent int64
		if m.bindEvent > 0 {
			bindSeq, bindEvent, m.bindSeq, m.bindEvent = m.bindSeq, m.bindEvent, 0, 0
		}
		endedSeq, m.endedSeq = m.endedSeq, 0
		relaxed := m.locked && lockForNetwork(m.lockReason) && !m.closing
		windowChanged := relaxed != m.windowRelaxed
		m.windowRelaxed = relaxed
		var captured Incident
		if capture >= 0 && m.incident != nil {
			captured = *m.incident
		}
		m.mu.Unlock()
		if bindEvent > 0 && m.options.OnLockRecorded != nil {
			m.options.OnLockRecorded(bindSeq, bindEvent)
		}
		if endedSeq > 0 && m.options.OnLockEnded != nil {
			m.options.OnLockEnded(endedSeq)
		}
		if windowChanged && m.options.OnWindowRelaxed != nil {
			m.options.OnWindowRelaxed(relaxed)
		}
		if alert != nil && m.options.OnAlert != nil {
			m.options.OnAlert(*alert)
		}
		if capture >= 0 && m.options.OnCapture != nil {
			m.options.OnCapture(captured, capture)
		}
		if lockShot >= 0 && m.options.OnLockCapture != nil {
			m.options.OnLockCapture(lockShotSeq, lockShot, lockShotLimit)
		}
		if hook != nil && m.options.OnIncident != nil {
			m.options.OnIncident(*hook)
		}
	}
}
func (m *monitor) close() {
	m.closeOnce.Do(func() {
		done := make(chan struct{})
		m.mu.Lock()
		m.closing = true
		if m.active && m.ready {
			m.enqueueLocked(&clientEvent{kind: "app_closed", at: time.Now(), closed: done})
		} else {
			close(done)
		}
		m.mu.Unlock()
		timer := time.NewTimer(m.options.CloseTimeout)
		select {
		case <-done:
		case <-m.ctx.Done():
		case <-timer.C:
		}
		timer.Stop()
		m.cancel()
		m.wg.Wait()
		m.mu.Lock()
		m.active = false
		if m.stoppedReason == "" {
			m.stoppedReason = "Application closing; telemetry stopped."
		}
		m.mu.Unlock()
	})
}

// V3 callbacks run concurrently; serialize native reads instead of trusting late event labels.
func ObserveNativeFocus(s *Service, read func() bool) {
	s.monitor.nativeMu.Lock()
	defer s.monitor.nativeMu.Unlock()
	s.monitor.focus(read())
}

// Package functions keep native lifecycle entry points out of frontend bindings.
func ReportNativeFocus(s *Service, focused bool) { s.monitor.focus(focused) }

// ReportRemote says everything that has been found that shows the computer is watched or controlled from elsewhere, each time that
// changes; none when it is all gone. The instructor is told, once the exam is running.
func ReportRemote(s *Service, findings []RemoteFinding) { s.monitor.remote(findings) }

// RemoteTools are the program names to look for, as the server gave them at the login; none before it.
func RemoteTools(s *Service) []string { return s.monitor.remoteToolList() }

// ReportSessionActive says whether the student's session is the one on screen, as opposed to a text console or another user's, and
// what took it off screen when it is not.
func ReportSessionActive(s *Service, active bool, reason string) { s.monitor.session(active, reason) }
func CloseMonitoring(s *Service)                                 { s.monitor.close() }
func (s *Service) AcknowledgeRules()                             { s.mu.Lock(); s.rulesAccepted = true; s.mu.Unlock() }
func (s *Service) ReportAlertFailure(sequence uint64) {
	s.monitor.mu.Lock()
	defer s.monitor.mu.Unlock()
	if s.monitor.incident != nil && s.monitor.incident.Sequence == sequence {
		s.monitor.alertError = "The audible alert could not play. Check sound output/permissions; monitoring continues."
	}
}

// latest is the later of two times.
func latest(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}
