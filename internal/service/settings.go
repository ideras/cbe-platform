package service

import (
	"context"
	"errors"
	"time"

	"exam-platform/internal/domain"
)

// ErrSettingsChanged means another proctor saved the settings since the caller read them.
var ErrSettingsChanged = errors.New("the settings changed; reload them and try again")

// Settings is the monitoring settings of the server, as the proctor's Settings page shows them.
type Settings struct {
	domain.PolicyMillis
	// UpdatedAt is when a proctor last saved the settings; nil when nobody has.
	UpdatedAt *time.Time `json:"updated_at"`
	// AverageScreenshotBytes is the mean size of the stored screenshots; nil when there are none yet.
	AverageScreenshotBytes *int64 `json:"average_screenshot_bytes"`
}

// SettingsUpdate is what the proctor's Settings page sends. The expected update time keeps two proctors from overwriting
// each other.
type SettingsUpdate struct {
	domain.PolicyMillis
	// ExpectedUpdatedAt is the update time the page loaded; nil when the settings were never saved.
	ExpectedUpdatedAt *time.Time `json:"expected_updated_at"`
}

// settingsReader reads the stored monitoring settings; both the sessions and the publisher can.
type settingsReader interface {
	Settings(context.Context) (domain.Policy, time.Time, error)
}

// currentSettings is the monitoring settings of the server: the stored ones, or the built-in defaults and no update time
// when no proctor has saved any.
func currentSettings(ctx context.Context, tx settingsReader) (domain.Policy, time.Time, error) {
	policy, updated, err := tx.Settings(ctx)
	if err != nil {
		return domain.Policy{}, time.Time{}, err
	}
	if policy.IsZero() {
		return domain.DefaultPolicy(), time.Time{}, nil
	}
	return policy.Normalized(), updated, nil
}

// settingsOf renders a stored policy as the settings the page works with.
func settingsOf(policy domain.Policy, updated time.Time, average *int64) Settings {
	view := Settings{PolicyMillis: policy.Millis(), AverageScreenshotBytes: average}
	if !updated.IsZero() {
		view.UpdatedAt = &updated
	}
	return view
}

// Settings reads the monitoring settings of the server.
func (s *Sessions) Settings(ctx context.Context) (Settings, error) {
	var view Settings
	err := s.repository.WithSessionTransaction(ctx, func(tx SessionTransaction) error {
		policy, updated, err := currentSettings(ctx, tx)
		if err != nil {
			return err
		}
		average, err := tx.AverageScreenshotBytes(ctx)
		if err != nil {
			return err
		}
		view = settingsOf(policy, updated, average)
		return nil
	})
	if err != nil {
		return Settings{}, err
	}
	return view, nil
}

// SaveSettings stores the monitoring settings of the server. Every problem is reported at once, and a save that would
// overwrite another proctor's change is refused.
func (s *Sessions) SaveSettings(ctx context.Context, update SettingsUpdate) (Settings, error) {
	policy := update.Policy().Normalized()
	var problems ValidationError
	for _, problem := range policy.Problems() {
		problems.add("%s", problem)
	}
	if len(problems.Problems) > 0 {
		return Settings{}, &problems
	}
	var view Settings
	err := s.repository.WithSessionTransaction(ctx, func(tx SessionTransaction) error {
		_, updated, err := currentSettings(ctx, tx)
		if err != nil {
			return err
		}
		if !sameMoment(updated, update.ExpectedUpdatedAt) {
			return ErrSettingsChanged
		}
		now := s.clock.Now().UTC()
		if !updated.IsZero() && !now.After(updated) {
			// The update time moves forward on every save, even when the clock stands still, so another proctor's save is
			// always noticed.
			now = updated.Add(time.Nanosecond)
		}
		if err := tx.SaveSettings(ctx, policy, now); err != nil {
			return err
		}
		average, err := tx.AverageScreenshotBytes(ctx)
		if err != nil {
			return err
		}
		view = settingsOf(policy, now, average)
		return nil
	})
	if err != nil {
		return Settings{}, err
	}
	return view, nil
}

// sameMoment compares an update time with the one the caller expected; both empty mean "never saved".
func sameMoment(stored time.Time, expected *time.Time) bool {
	if stored.IsZero() {
		return expected == nil || expected.IsZero()
	}
	return expected != nil && expected.Equal(stored)
}

// DefaultGraceSeconds is the grace period a new exam gets when its file names none.
func (s *Sessions) DefaultGraceSeconds(ctx context.Context) (int64, error) {
	var seconds int64
	err := s.repository.WithSessionTransaction(ctx, func(tx SessionTransaction) error {
		policy, _, err := currentSettings(ctx, tx)
		if err != nil {
			return err
		}
		seconds = int64(policy.DefaultGrace / time.Second)
		return nil
	})
	return seconds, err
}

// Rules are the times the student application shows before the exam starts.
type Rules struct {
	// AlarmMillis is how long a student may be out of the exam window before the alarm starts.
	AlarmMillis int64 `json:"alarm_millis"`
	// CaptureMillis is how often the screen is captured during an absence.
	CaptureMillis int64 `json:"capture_millis"`
	// LockAwayMillis is how long one absence may last before the exam locks; 0 means the lock is off.
	LockAwayMillis int64 `json:"lock_away_millis"`
	// LockNoContactMillis is how long the application may be out of contact before the exam locks; 0 means off.
	LockNoContactMillis int64 `json:"lock_no_contact_millis"`
}

// Rules reads the times the student rules show: the published exam's frozen ones, or the current settings when no exam is
// published.
func (s *Sessions) Rules(ctx context.Context) (Rules, error) {
	var rules Rules
	err := s.repository.WithSessionTransaction(ctx, func(tx SessionTransaction) error {
		policy, _, err := currentSettings(ctx, tx)
		if err != nil {
			return err
		}
		examID, _, err := tx.CurrentExam(ctx)
		if err != nil {
			return err
		}
		if examID != "" {
			exam, err := tx.Exam(ctx, examID)
			if err != nil {
				return err
			}
			policy = exam.Policy
		}
		rules = Rules{AlarmMillis: policy.FocusThreshold.Milliseconds(), CaptureMillis: policy.CaptureInterval.Milliseconds(),
			LockAwayMillis: policy.LockAwayAfter.Milliseconds(), LockNoContactMillis: policy.LockNoContactAfter.Milliseconds()}
		return nil
	})
	if err != nil {
		return Rules{}, err
	}
	return rules, nil
}
