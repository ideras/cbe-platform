package client

import (
	"context"
	"errors"
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
	OnError           func(operation string, err error)
}

func DefaultSoundOptions() SoundOptions {
	return SoundOptions{Interval: 5 * time.Second, Timeout: 3 * time.Second}
}
func (o SoundOptions) validate() error {
	if o.Control != nil && (o.Interval <= 0 || o.Timeout <= 0) {
		return errors.New("Sound intervals must be positive.")
	}
	return nil
}

// SoundReport is the output level found by the latest check, before any automatic correction. A nil field is unknown.
type SoundReport struct {
	Volume *int  `json:"volume_percent,omitempty"`
	Muted  *bool `json:"volume_muted,omitempty"`
}

var (
	errSoundBusy    = errors.New("the previous system sound request has not finished")
	errSoundTimeout = errors.New("the system sound did not answer in time")
)

// soundManager remembers the student's own sound settings when the application starts, keeps the exam audible once
// the server has said what the exam needs, and puts back whatever it changed. Until then it only watches. System calls
// can block (they run external tools), so they never run on the heartbeat path: the heartbeat only reads the latest
// cached check.
type soundManager struct {
	options SoundOptions
	wake    chan struct{}
	opMu    sync.Mutex // serializes checks and the final restore
	mu      sync.Mutex
	busy    bool
	done    bool
	minimum int // percent the exam keeps the volume at; 0 changes nothing

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

// activate starts keeping the volume at or above percent (the exam's setting; 0 leaves it alone) and checks right away.
func (m *soundManager) activate(percent int) {
	if m == nil || percent < 0 || percent > 100 {
		return
	}
	m.mu.Lock()
	m.minimum = percent
	m.mu.Unlock()
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

// check reads the current level, remembers the first reading as the student's original, and then raises the sound
// when it is muted or below the minimum. Only values whose original is known are ever changed, so they can be restored.
func (m *soundManager) check() {
	if m == nil {
		return
	}
	m.opMu.Lock()
	defer m.opMu.Unlock()
	if m.finished() {
		return
	}
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
	m.mu.Unlock()
	if minimum <= 0 {
		return
	}
	if report.Muted != nil && *report.Muted && originalMuted != nil {
		m.change("unmute", &m.changedMuted, func() error { return m.options.Control.SetMuted(false) })
	}
	if report.Volume != nil && *report.Volume < minimum && originalVolume != nil {
		m.change("raise volume", &m.changedVolume, func() error { return m.options.Control.SetVolume(minimum) })
	}
}

// change records that a value is about to be touched before touching it, so a timed-out request that lands late
// is still reverted by restore.
func (m *soundManager) change(operation string, changed *bool, apply func() error) {
	m.mu.Lock()
	if m.done {
		m.mu.Unlock()
		return
	}
	*changed = true
	m.mu.Unlock()
	if _, err := soundCall(m, func() (struct{}, error) { return struct{}{}, apply() }); err != nil {
		m.fail(operation, err)
	}
}

// report returns the latest check, or nothing when the checks have stopped answering.
func (m *soundManager) report() SoundReport {
	if m == nil {
		return SoundReport{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
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
