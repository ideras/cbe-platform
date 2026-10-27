package client

import (
	"context"
	"errors"
	"math"
	"net/http"
	"os"
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
	Sound                                         func() SoundReport
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
	kind     string
	at       time.Time
	incident uint64
	// reason says why the application locked the exam, for an away_lock event.
	reason string
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
	// locked is the exam's lock as the application knows it. The application locks itself when its own measurement reaches a
	// limit; lockConfirmed says that a status from the server has reported the lock since, so a later status without one
	// means a proctor unlocked the exam. lockIncident is the absence the student was in when the exam locked (0 for none):
	// only its alarm keeps going while the exam is locked.
	locked, lockConfirmed, lockReportPending              bool
	lockReason                                            string
	lockSince, lockReportedAt, lastContact, unlockedAt    time.Time
	lockIncident                                          uint64
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
	m.enqueueLocked(&clientEvent{kind: kind, at: now, incident: seq})
}
func (m *monitor) focus(focused bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.known && m.focused == focused {
		return
	}
	m.known, m.focused = true, focused
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
	m.enqueueLocked(&clientEvent{kind: kind, at: now, incident: seq})
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
	return v
}

// isLocked says whether the exam is locked, by the application itself or by the server.
func (m *monitor) isLocked() bool { m.mu.Lock(); defer m.mu.Unlock(); return m.locked }

// lockLocked locks the exam. The alarm of the absence the student is in, if any, keeps going until the student returns.
func (m *monitor) lockLocked(reason string, since time.Time) {
	m.locked, m.lockReason, m.lockSince, m.lockIncident = true, reason, since, 0
	if m.known && !m.focused && m.incident != nil {
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
		}
		m.lockConfirmed = true
		return
	}
	if m.locked && m.lockConfirmed {
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
		if m.options.Sound != nil {
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
			if m.ready && m.recovering && m.known {
				m.baselineLocked(time.Now())
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
				detail := map[string]any{"client_time": event.at.UTC().Format(time.RFC3339Nano), "local_incident": event.incident, "source": "native_window"}
				if event.kind == "away_lock" && event.reason != "" {
					detail["reason"] = event.reason
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
			if err != nil {
				m.dropped++
				m.eventError = "An event could not be confirmed after bounded retries; monitoring history may be incomplete."
				if failureCode(err) == 401 || failureCode(err) == 403 {
					m.stopLocked("Telemetry authorization ended; waiting for authoritative session status.")
					signal(m.pollNow)
				}
			} else {
				m.lastContact = time.Now()
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
		capture := -1
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
				}
			}
			// The server is told about a lock it has not reported yet, again every heartbeat interval until it does: a report
			// lost on the way would otherwise leave the exam locked here and open on the proctor's screen.
			if m.locked && !m.lockConfirmed && !m.lockReportPending &&
				(m.lockReportedAt.IsZero() || now.Sub(m.lockReportedAt) >= time.Duration(m.policy.HeartbeatMillis)*time.Millisecond) {
				m.lockReportedAt = now
				m.lockReportPending = m.enqueueLocked(&clientEvent{kind: "away_lock", at: now, reason: m.lockReason})
			}
			// The alarm of the absence that caused the lock keeps going until the student returns to the window; a later
			// absence starts neither the alarm nor the screenshots.
			if m.known && !m.focused && m.incident != nil && (!m.locked || m.lockIncident != 0 && m.lockIncident == m.incident.Sequence) {
				// The alert repeats every AlertMillis and the screen is captured every CaptureMillis, both counted from the
				// threshold, until the student returns: focus regained replaces the incident.
				away := now.Sub(m.incident.ClientLostAt)
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
		var captured Incident
		if capture >= 0 && m.incident != nil {
			captured = *m.incident
		}
		m.mu.Unlock()
		if alert != nil && m.options.OnAlert != nil {
			m.options.OnAlert(*alert)
		}
		if capture >= 0 && m.options.OnCapture != nil {
			m.options.OnCapture(captured, capture)
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
func CloseMonitoring(s *Service)                 { s.monitor.close() }
func (s *Service) AcknowledgeRules()             { s.mu.Lock(); s.rulesAccepted = true; s.mu.Unlock() }
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
