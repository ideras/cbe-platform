package domain

import (
	"strings"
	"testing"
	"time"
)

// problemsOf are the problems of the default settings after a change.
func problemsOf(change func(*Policy)) []string {
	policy := DefaultPolicy()
	change(&policy)
	return policy.Problems()
}

func mention(problems []string, text string) bool {
	for _, problem := range problems {
		if strings.Contains(problem, text) {
			return true
		}
	}
	return false
}

// The away lock may be as short as one second by its own range. What keeps it from being shorter than the alarm and one screenshot is
// the rule that it comes after them, which is checked apart.
func TestTheAwayLockMayBeSetInSecondsAndKeepsItsOtherLimits(t *testing.T) {
	const rangeProblem = "lock after away must be off or between 1s and 60m"
	const afterTheAlarm = "must come after the alarm starts"
	tight := func(p *Policy) { p.FocusThreshold, p.CaptureInterval = time.Second, 3*time.Second }

	for _, test := range []struct {
		name   string
		change func(*Policy)
		want   string // "" for settings with no problem; otherwise a text one of the problems has
		not    string // a text no problem has
	}{
		{"the alarm and one screenshot of the defaults, 10 s", func(p *Policy) { p.LockAwayAfter = 10 * time.Second }, "", ""},
		{"the least that the tightest alarm and screenshot allow, 4 s", func(p *Policy) { tight(p); p.LockAwayAfter = 4 * time.Second }, "", ""},
		{"a moment short of that", func(p *Policy) { tight(p); p.LockAwayAfter = 3999 * time.Millisecond }, afterTheAlarm, rangeProblem},
		{"a moment short of the defaults", func(p *Policy) { p.LockAwayAfter = 9999 * time.Millisecond }, afterTheAlarm, rangeProblem},
		{"one second, within its range but before the alarm", func(p *Policy) { tight(p); p.LockAwayAfter = time.Second }, afterTheAlarm, rangeProblem},
		{"less than a second", func(p *Policy) { tight(p); p.LockAwayAfter = 999 * time.Millisecond }, rangeProblem, ""},
		{"an hour", func(p *Policy) { p.LockAwayAfter, p.SessionCaptures = 60*time.Minute, 2000 }, "", ""},
		{"more than an hour", func(p *Policy) { p.LockAwayAfter, p.SessionCaptures = 60*time.Minute+time.Millisecond, 2000 }, rangeProblem, ""},
		{"off", func(p *Policy) { p.LockAwayAfter = 0 }, "", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			problems := problemsOf(test.change)
			if test.want == "" && len(problems) != 0 {
				t.Fatalf("settings that should be accepted have problems: %v", problems)
			}
			if test.want != "" && !mention(problems, test.want) {
				t.Fatalf("problems = %v, want one about %q", problems, test.want)
			}
			if test.not != "" && mention(problems, test.not) {
				t.Fatalf("problems = %v, none should be about %q", problems, test.not)
			}
		})
	}
}

// The no contact lock may be as short as one second by its own range, and in practice is longer than the time after which the
// connection counts as lost.
func TestTheNoContactLockMayBeSetInSecondsAndKeepsItsOtherLimits(t *testing.T) {
	const rangeProblem = "lock after no contact must be off or between 1s and 60m"
	const afterTheConnection = "must be longer than the connection lost threshold"
	quick := func(p *Policy) { p.HeartbeatInterval, p.OverdueThreshold = 5*time.Second, 10*time.Second }

	for _, test := range []struct {
		name   string
		change func(*Policy)
		want   string
		not    string
	}{
		{"a moment longer than the connection lost threshold of the defaults, 31 s", func(p *Policy) { p.LockNoContactAfter = 31 * time.Second }, "", ""},
		{"as long as that threshold", func(p *Policy) { p.LockNoContactAfter = 30 * time.Second }, afterTheConnection, rangeProblem},
		{"the least that the quickest heartbeat allows, 11 s", func(p *Policy) { quick(p); p.LockNoContactAfter = 11 * time.Second }, "", ""},
		{"as long as the quickest threshold", func(p *Policy) { quick(p); p.LockNoContactAfter = 10 * time.Second }, afterTheConnection, rangeProblem},
		{"less than a second", func(p *Policy) { quick(p); p.LockNoContactAfter = 999 * time.Millisecond }, rangeProblem, ""},
		{"an hour", func(p *Policy) { p.LockNoContactAfter = 60 * time.Minute }, "", ""},
		{"more than an hour", func(p *Policy) { p.LockNoContactAfter = 60*time.Minute + time.Millisecond }, rangeProblem, ""},
		{"off", func(p *Policy) { p.LockNoContactAfter = 0 }, "", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			problems := problemsOf(test.change)
			if test.want == "" && len(problems) != 0 {
				t.Fatalf("settings that should be accepted have problems: %v", problems)
			}
			if test.want != "" && !mention(problems, test.want) {
				t.Fatalf("problems = %v, want one about %q", problems, test.want)
			}
			if test.not != "" && mention(problems, test.not) {
				t.Fatalf("problems = %v, none should be about %q", problems, test.not)
			}
		})
	}
}

// With a short away lock the screenshots of an absence still cover it from the alarm to the lock.
func TestAShortAwayLockKeepsTheScreenshotsOfTheWholeAbsence(t *testing.T) {
	policy := DefaultPolicy()
	policy.LockAwayAfter = 10 * time.Second
	policy = policy.Normalized()
	// The alarm starts at 5 s and a screenshot is taken every 5 s: at 5 s and at 10 s.
	if got := policy.IncidentCaptures; got != 2 {
		t.Fatalf("screenshots per absence = %d, want 2", got)
	}
	if problems := policy.Problems(); len(problems) != 0 {
		t.Fatalf("problems = %v", problems)
	}
}
