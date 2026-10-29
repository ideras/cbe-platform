package client

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"
)

// SoundControl reads and changes the master volume and mute state of the default output device.
type SoundControl interface {
	Volume() (int, error)
	Muted() (bool, error)
	SetVolume(percent int) error
	SetMuted(muted bool) error
}

// SoundOptions configure how the application watches the system sound. The volume the exam requires is not an option:
// the server advertises it at login (see soundManager.activate).
type SoundOptions struct {
	Control           SoundControl
	Interval, Timeout time.Duration
	// HeartbeatWait is how long a heartbeat waits for the fresh check it asked for before it reports the sound as unknown;
	// zero means two seconds.
	HeartbeatWait time.Duration
	// Watch, when the platform has change notifications, calls changed whenever the default output's volume, mute or
	// device may have changed, until ctx ends. It only wakes the manager's worker; the worker reads and corrects.
	Watch   func(ctx context.Context, changed func()) error
	OnError func(operation string, err error)
}

func DefaultSoundOptions() SoundOptions {
	return SoundOptions{Interval: 5 * time.Second, Timeout: 3 * time.Second, HeartbeatWait: 2 * time.Second}
}
func (o SoundOptions) validate() error {
	if o.Control != nil && (o.Interval <= 0 || o.Timeout <= 0 || o.HeartbeatWait < 0) {
		return errors.New("Sound intervals must be positive.")
	}
	return nil
}

// SoundReport is the output level found by the latest check, before any automatic correction. A nil field is unknown.
type SoundReport struct {
	Volume *int  `json:"volume_percent,omitempty"`
	Muted  *bool `json:"volume_muted,omitempty"`
}

// SoundNotice tells the student what the application did about the volume. It is the newest correction outcome; Sequence
// grows with every one, so the window can show each once. Volume and Muted are what the check found before the correction.
type SoundNotice struct {
	Sequence uint64 `json:"sequence"`
	// Kind is "restored" (the required volume is back) or "failed" (it could not be restored).
	Kind string `json:"kind"`
	// Initial says that the correction came with the first check after login, not from a change during the exam.
	Initial bool  `json:"initial"`
	Minimum int   `json:"minimum_percent"`
	Volume  *int  `json:"volume_percent,omitempty"`
	Muted   *bool `json:"volume_muted,omitempty"`
}

var (
	errSoundBusy    = errors.New("the previous system sound request has not finished")
	errSoundTimeout = errors.New("the system sound did not answer in time")
)

// soundManager remembers the student's own sound settings when the application starts, keeps the exam audible once
// the server has said what the exam needs, and puts back whatever it changed. Until then it only watches. System calls
// can block (they run external tools), so they never run on the heartbeat path: a heartbeat asks the worker for a fresh
// check (refresh) and waits a bounded time for it.
type soundManager struct {
	options SoundOptions
	wake    chan struct{}
	opMu    sync.Mutex // serializes checks and the final restore
	mu      sync.Mutex
	busy    bool
	done    bool
	minimum int // percent the exam keeps the volume at; 0 changes nothing

	// asked counts the fresh checks requested; a check that starts after request n answers it. waiting holds the
	// requests not answered yet, at most one per heartbeat in flight.
	asked   uint64
	waiting []*soundWait
	// first says that the next check that reads the sound is the first after login. failed says that a correction
	// failed during the low episode in progress, and was announced.
	first, failed bool
	notice        *SoundNotice
	// found is the level a check found that it had to correct, until a heartbeat has carried it to the server. A later
	// check reads the corrected level, so without it the proctor would never see what the student had.
	found   *SoundReport
	foundAt time.Time

	originalVolume              *int
	originalMuted               *bool
	changedVolume, changedMuted bool
	observed                    SoundReport
	observedAt                  time.Time
}

func newSoundManager(options SoundOptions) *soundManager {
	if options.Control == nil {
		return nil
	}
	return &soundManager{options: options, wake: make(chan struct{}, 1)}
}

// soundWait is one heartbeat's request for a fresh check; done closes when a check that started after it has finished.
type soundWait struct {
	ticket uint64
	done   chan struct{}
}

// activate starts keeping the volume at or above percent (the exam's setting; 0 leaves it alone) and checks right away.
func (m *soundManager) activate(percent int) {
	if m == nil || percent < 0 || percent > 100 {
		return
	}
	m.mu.Lock()
	m.minimum = percent
	m.first, m.failed = true, false
	m.mu.Unlock()
	m.poke()
}

// poke wakes the worker. Requests that arrive while it is busy share its next check.
func (m *soundManager) poke() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func soundCall[T any](m *soundManager, fn func() (T, error)) (T, error) {
	var zero T
	m.mu.Lock()
	if m.busy {
		m.mu.Unlock()
		return zero, errSoundBusy
	}
	m.busy = true
	m.mu.Unlock()
	type outcome struct {
		value T
		err   error
	}
	result := make(chan outcome, 1)
	go func() {
		value, err := fn()
		m.mu.Lock()
		m.busy = false
		m.mu.Unlock()
		result <- outcome{value, err}
	}()
	timer := time.NewTimer(m.options.Timeout)
	defer timer.Stop()
	select {
	case r := <-result:
		return r.value, r.err
	case <-timer.C:
		return zero, errSoundTimeout
	}
}
func (m *soundManager) fail(operation string, err error) {
	if m.options.OnError != nil {
		m.options.OnError(operation, err)
	}
}
func (m *soundManager) finished() bool { m.mu.Lock(); defer m.mu.Unlock(); return m.done }

// read takes the level of the default output: volume and mute, each nil when it could not be read.
func (m *soundManager) read() SoundReport {
	var report SoundReport
	if volume, err := soundCall(m, m.options.Control.Volume); err != nil {
		m.fail("read volume", err)
	} else {
		report.Volume = &volume
	}
	if muted, err := soundCall(m, m.options.Control.Muted); err != nil {
		m.fail("read mute", err)
	} else {
		report.Muted = &muted
	}
	return report
}

// check reads the current level, remembers the first reading as the student's original, and then raises the sound
// when it is muted or below the minimum. Only values whose original is known are ever changed, so they can be restored.
// A correction is read back before it is announced, and one low episode is announced once.
func (m *soundManager) check() {
	if m == nil {
		return
	}
	m.opMu.Lock()
	defer m.opMu.Unlock()
	if m.finished() {
		return
	}
	m.mu.Lock()
	asked := m.asked
	m.mu.Unlock()
	// Every heartbeat that asked before this check began is answered by it, whatever it found.
	defer m.answer(asked)
	report := m.read()
	m.mu.Lock()
	if m.originalVolume == nil && report.Volume != nil {
		original := *report.Volume
		m.originalVolume = &original
	}
	if m.originalMuted == nil && report.Muted != nil {
		original := *report.Muted
		m.originalMuted = &original
	}
	originalVolume, originalMuted, minimum := m.originalVolume, m.originalMuted, m.minimum
	reported := report
	if reported.Volume != nil {
		clamped := min(max(*reported.Volume, 0), 100)
		reported.Volume = &clamped
	}
	m.observed, m.observedAt = reported, time.Now()
	initial := m.first
	if report.Volume != nil || report.Muted != nil {
		m.first = false
	}
	m.mu.Unlock()
	if minimum <= 0 {
		return
	}
	unmute := report.Muted != nil && *report.Muted && originalMuted != nil
	raise := report.Volume != nil && *report.Volume < minimum && originalVolume != nil
	if !unmute && !raise {
		m.mu.Lock()
		if report.Volume != nil || report.Muted != nil {
			m.failed = false // the episode is over: a later drop is announced again
		}
		m.mu.Unlock()
		return
	}
	m.mu.Lock()
	m.found, m.foundAt = &reported, time.Now()
	m.mu.Unlock()
	ok := true
	if unmute {
		ok = m.change("unmute", &m.changedMuted, func() error { return m.options.Control.SetMuted(false) }) && ok
	}
	if raise {
		ok = m.change("raise volume", &m.changedVolume, func() error { return m.options.Control.SetVolume(minimum) }) && ok
	}
	if ok {
		// Success is what the system says now, not that the request was sent.
		after := m.read()
		ok = after.Volume != nil && *after.Volume >= minimum && after.Muted != nil && !*after.Muted
	}
	m.announce(ok, initial, minimum, report)
}

// announce records the outcome of a correction for the student. A failure that goes on is announced once.
func (m *soundManager) announce(ok, initial bool, minimum int, found SoundReport) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.done || !ok && m.failed {
		return
	}
	m.failed = !ok
	notice := SoundNotice{Kind: "restored", Initial: initial, Minimum: minimum, Volume: found.Volume, Muted: found.Muted}
	if !ok {
		notice.Kind = "failed"
	}
	if m.notice != nil {
		notice.Sequence = m.notice.Sequence
	}
	notice.Sequence++
	m.notice = &notice
}

// latestNotice is the newest correction outcome, or nil when there has been none.
func (m *soundManager) latestNotice() *SoundNotice {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.notice == nil {
		return nil
	}
	copy := *m.notice
	return &copy
}

// change records that a value is about to be touched before touching it, so a timed-out request that lands late
// is still reverted by restore. It says whether the system accepted the change.
func (m *soundManager) change(operation string, changed *bool, apply func() error) bool {
	m.mu.Lock()
	if m.done {
		m.mu.Unlock()
		return false
	}
	*changed = true
	m.mu.Unlock()
	if _, err := soundCall(m, func() (struct{}, error) { return struct{}{}, apply() }); err != nil {
		m.fail(operation, err)
		return false
	}
	return true
}

// answer releases the heartbeats whose request came before the check that has just finished.
func (m *soundManager) answer(asked uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	kept := m.waiting[:0]
	for _, w := range m.waiting {
		if w.ticket <= asked {
			close(w.done)
		} else {
			kept = append(kept, w)
		}
	}
	clear(m.waiting[len(kept):])
	m.waiting = kept
}

// refresh asks the worker for a check that starts now and waits for it for HeartbeatWait at most. A heartbeat gets the
// level that check found, or an unknown one when it did not finish in time: an older sample is never passed off as fresh.
// Requests that overlap share one check, and a worker stuck in a system call costs the heartbeat only the wait.
func (m *soundManager) refresh(ctx context.Context) SoundReport {
	if m == nil {
		return SoundReport{}
	}
	m.mu.Lock()
	if m.done {
		m.mu.Unlock()
		return SoundReport{}
	}
	m.asked++
	w := &soundWait{ticket: m.asked, done: make(chan struct{})}
	m.waiting = append(m.waiting, w)
	m.mu.Unlock()
	m.poke()
	wait := m.options.HeartbeatWait
	if wait == 0 {
		wait = 2 * time.Second
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-w.done:
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.carryLocked()
	case <-timer.C:
	case <-ctx.Done():
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if i := slices.Index(m.waiting, w); i >= 0 {
		m.waiting = slices.Delete(m.waiting, i, i+1)
		return SoundReport{}
	}
	return m.carryLocked() // answered while giving up
}

// carryLocked is the level a heartbeat reports: the finding of a correction not yet reported, else the latest check. It is
// the level before any correction, so the proctor sees the student lowering the sound even when the application restored it.
func (m *soundManager) carryLocked() SoundReport {
	found, at := m.found, m.foundAt
	m.found = nil
	if found != nil && time.Since(at) <= 3*m.options.Interval {
		return *found
	}
	return m.reportLocked()
}

// report returns the latest check, or nothing when the checks have stopped answering.
func (m *soundManager) report() SoundReport {
	if m == nil {
		return SoundReport{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.reportLocked()
}
func (m *soundManager) reportLocked() SoundReport {
	if m.observedAt.IsZero() || time.Since(m.observedAt) > 3*m.options.Interval {
		return SoundReport{}
	}
	return m.observed
}

func (m *soundManager) run(ctx context.Context) {
	if m == nil {
		return
	}
	ticker := time.NewTicker(m.options.Interval)
	defer ticker.Stop()
	if m.options.Watch != nil {
		// A change of the system's volume, mute or output only wakes this worker; the periodic check stays as the fallback.
		var watcher sync.WaitGroup
		watcher.Add(1)
		go func() {
			defer watcher.Done()
			if err := m.options.Watch(ctx, m.poke); err != nil && ctx.Err() == nil {
				m.fail("watch volume", err)
			}
		}()
		defer watcher.Wait()
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.check()
		case <-m.wake:
			m.check()
		}
	}
}

// restore puts back the original values this manager changed and stops all further changes. It is safe to repeat.
func (m *soundManager) restore() error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	m.done = true
	m.mu.Unlock()
	m.opMu.Lock()
	defer m.opMu.Unlock()
	m.mu.Lock()
	volume, muted := m.originalVolume, m.originalMuted
	changedVolume, changedMuted := m.changedVolume, m.changedMuted
	m.changedVolume, m.changedMuted = false, false
	m.mu.Unlock()
	var problems []error
	if changedVolume && volume != nil {
		if _, err := soundCall(m, func() (struct{}, error) { return struct{}{}, m.options.Control.SetVolume(*volume) }); err != nil {
			problems = append(problems, err)
			m.fail("restore volume", err)
		}
	}
	if changedMuted && muted != nil {
		if _, err := soundCall(m, func() (struct{}, error) { return struct{}{}, m.options.Control.SetMuted(*muted) }); err != nil {
			problems = append(problems, err)
			m.fail("restore mute", err)
		}
	}
	return errors.Join(problems...)
}
