package service_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"exam-platform/internal/domain"
	"exam-platform/internal/service"
)

// The proctor's Settings are stored on the server: every proctor computer sees the same values.
func TestSettingsRoundTripAndTheWorkedOutAbsenceCount(t *testing.T) {
	f := newFixture(t, 0)
	view, err := f.sessions.Settings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if view.HeartbeatInterval != 15000 || view.OverdueThreshold != 30000 || view.FocusThreshold != 20000 || view.CaptureInterval != 5000 {
		t.Fatalf("settings = %+v", view)
	}
	if view.LockAwayAfter != 300000 || view.LockNoContactAfter != 300000 || view.DefaultGrace != 30000 {
		t.Fatalf("lock settings = %+v", view)
	}
	if view.UpdatedAt == nil {
		t.Fatal("the settings have no update time")
	}
	// With the away lock on, the per-absence count covers the whole absence up to the lock.
	away := 5 * time.Minute
	focus := 20 * time.Second
	capture := 5 * time.Second
	if got := int64(away-focus)/int64(capture) + 1; got != int64(view.IncidentCaptures) {
		t.Fatalf("absence count = %d, want %d", view.IncidentCaptures, got)
	}
	// A save is stored and read back.
	saved, err := f.sessions.SaveSettings(t.Context(), service.SettingsUpdate{PolicyMillis: domain.PolicyMillis{HeartbeatInterval: 10000, OverdueThreshold: 25000,
		FocusThreshold: 3000, AlertInterval: 2000, CaptureInterval: 4000, LockAwayAfter: 600000, LockNoContactAfter: 420000,
		IncidentCaptures: 30, SessionCaptures: 300, DefaultGrace: 45000}, ExpectedUpdatedAt: view.UpdatedAt})
	if err != nil {
		t.Fatal(err)
	}
	if saved.HeartbeatInterval != 10000 || saved.CaptureInterval != 4000 || saved.LockAwayAfter != 600000 {
		t.Fatalf("saved = %+v", saved)
	}
	// The count is worked out again: (600 - 3) / 4 + 1 = 150.
	if saved.IncidentCaptures != 150 {
		t.Fatalf("worked out count = %d", saved.IncidentCaptures)
	}
	again, err := f.sessions.Settings(t.Context())
	if err != nil || again.HeartbeatInterval != 10000 {
		t.Fatalf("settings = %+v, %v", again, err)
	}
}

// A save that would overwrite another proctor's change is refused.
func TestASettingsSaveThatIsOutOfDateIsRefused(t *testing.T) {
	f := newFixture(t, 0)
	view, err := f.sessions.Settings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	update := service.SettingsUpdate{PolicyMillis: domain.PolicyMillis{HeartbeatInterval: 10000, OverdueThreshold: 25000, FocusThreshold: 3000,
		AlertInterval: 2000, CaptureInterval: 4000, IncidentCaptures: 30, SessionCaptures: 300, DefaultGrace: 30000},
		ExpectedUpdatedAt: view.UpdatedAt}
	if _, err := f.sessions.SaveSettings(t.Context(), update); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sessions.SaveSettings(t.Context(), update); !errors.Is(err, service.ErrSettingsChanged) {
		t.Fatalf("a stale save = %v", err)
	}
}

// Every range of the Settings page is checked, and every problem is reported at once.
func TestEverySettingsRangeIsChecked(t *testing.T) {
	f := newFixture(t, 0)
	// Each case changes one value of valid settings, so the problem reported is the one the case is about.
	valid := domain.DefaultPolicy().Millis()
	with := func(change func(*domain.PolicyMillis)) service.SettingsUpdate {
		values := valid
		change(&values)
		return service.SettingsUpdate{PolicyMillis: values}
	}
	cases := []struct {
		name  string
		value service.SettingsUpdate
	}{
		{"heartbeat too short", with(func(v *domain.PolicyMillis) { v.HeartbeatInterval = 4000 })},
		{"heartbeat too long", with(func(v *domain.PolicyMillis) { v.HeartbeatInterval, v.OverdueThreshold = 61000, 130000 })},
		{"connection lost too soon", with(func(v *domain.PolicyMillis) { v.OverdueThreshold = 20000 })},
		{"connection lost too late", with(func(v *domain.PolicyMillis) { v.OverdueThreshold, v.LockNoContactAfter = 600001, 0 })},
		{"alarm start too soon", with(func(v *domain.PolicyMillis) { v.FocusThreshold = 500 })},
		{"alarm start too late", with(func(v *domain.PolicyMillis) { v.FocusThreshold, v.LockAwayAfter = 600001, 0 })},
		{"alarm repeat too fast", with(func(v *domain.PolicyMillis) { v.AlertInterval = 500 })},
		{"alarm repeat too slow", with(func(v *domain.PolicyMillis) { v.AlertInterval = 11000 })},
		{"screenshot too fast", with(func(v *domain.PolicyMillis) { v.CaptureInterval = 2000 })},
		{"screenshot too slow", with(func(v *domain.PolicyMillis) { v.CaptureInterval = 61000 })},
		{"away lock too short", with(func(v *domain.PolicyMillis) { v.LockAwayAfter = 30000 })},
		{"away lock too long", with(func(v *domain.PolicyMillis) { v.LockAwayAfter, v.SessionCaptures = 3600001, 2000 })},
		{"away lock before the alarm and one screenshot", with(func(v *domain.PolicyMillis) { v.FocusThreshold, v.LockAwayAfter = 58000, 60000 })},
		{"no contact lock too short", with(func(v *domain.PolicyMillis) { v.LockNoContactAfter = 45000 })},
		{"no contact lock not later than the connection", with(func(v *domain.PolicyMillis) { v.OverdueThreshold, v.LockNoContactAfter = 120000, 120000 })},
		{"too few screenshots per absence", with(func(v *domain.PolicyMillis) { v.LockAwayAfter, v.IncidentCaptures = 0, 0 })},
		{"too many screenshots per absence", with(func(v *domain.PolicyMillis) { v.LockAwayAfter, v.IncidentCaptures, v.SessionCaptures = 0, 501, 600 })},
		{"fewer screenshots per student than per absence", with(func(v *domain.PolicyMillis) { v.SessionCaptures = 59 })},
		{"too many screenshots per student", with(func(v *domain.PolicyMillis) { v.SessionCaptures = 2001 })},
		{"no grace", with(func(v *domain.PolicyMillis) { v.DefaultGrace = 0 })},
		{"grace in part of a second", with(func(v *domain.PolicyMillis) { v.DefaultGrace = 1500 })},
		{"grace too long", with(func(v *domain.PolicyMillis) { v.DefaultGrace = 1800001 })},
	}
	if problems := valid.Policy().Problems(); len(problems) != 0 {
		t.Fatalf("the base settings are not valid: %v", problems)
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, err := f.sessions.SaveSettings(t.Context(), test.value)
			if !errors.Is(err, service.ErrInvalidInput) {
				t.Fatalf("an invalid setting was accepted: %v", err)
			}
			var problems *service.ValidationError
			if !errors.As(err, &problems) || len(problems.Problems) != 1 {
				t.Fatalf("one problem should be reported: %v", err)
			}
			// Every problem names the values that are wrong, so the proctor knows what to change.
			for _, problem := range problems.Problems {
				if !strings.Contains(problem, "it is") && !strings.Contains(problem, "(") {
					t.Fatalf("the problem does not name the values: %q", problem)
				}
			}
		})
	}
}

// A change of the settings after an exam is published does not reach the students already in it.
func TestASettingsChangeAfterThePublishDoesNotReachTheRunningExam(t *testing.T) {
	f := newFixture(t, 0)
	login := f.login(t, "0012")
	if login.Policy.FocusMillis != 20000 {
		t.Fatalf("policy = %+v", login.Policy)
	}
	view, err := f.sessions.Settings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	changed := service.SettingsUpdate{PolicyMillis: domain.PolicyMillis{HeartbeatInterval: 10000, OverdueThreshold: 25000, FocusThreshold: 8000,
		AlertInterval: 2000, CaptureInterval: 5000, IncidentCaptures: 30, SessionCaptures: 200, DefaultGrace: 30000},
		ExpectedUpdatedAt: view.UpdatedAt}
	if _, err := f.sessions.SaveSettings(t.Context(), changed); err != nil {
		t.Fatal(err)
	}
	principal, err := f.sessions.Authenticate(t.Context(), login.Token)
	if err != nil || principal.Policy.FocusMillis != 20000 {
		t.Fatalf("the running session followed the new settings: %+v, %v", principal.Policy, err)
	}
	list, err := f.sessions.ProctorSessions(t.Context(), testExamID)
	if err != nil || list.Policy.FocusThresholdMillis != 20000 {
		t.Fatalf("the proctor saw the new settings: %+v, %v", list.Policy, err)
	}
	// A student who logs in after the change follows the exam's frozen rules, not the new ones.
	second := f.login(t, "0034")
	if second.Policy.FocusMillis != 20000 {
		t.Fatalf("the new session did not follow the published rules: %+v", second.Policy)
	}
}

// The rules the student sees come from the published exam, or from the settings when nothing is published.
func TestTheRulesFollowThePublishedExam(t *testing.T) {
	f := newFixture(t, 0)
	rules, err := f.sessions.Rules(t.Context())
	if err != nil || rules.AlarmMillis != 20000 || rules.LockAwayMillis != 300000 {
		t.Fatalf("rules = %+v, %v", rules, err)
	}
}
