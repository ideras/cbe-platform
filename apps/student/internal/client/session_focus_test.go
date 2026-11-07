package client

import (
	"sync/atomic"
	"testing"
	"time"
)

// focusReports are the focus reports the fake server has received, as the kind of each and what it came from.
func focusReports(f *fixture) [][2]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var reports [][2]string
	for _, record := range f.telemetry {
		if record.path == "/event" && (record.kind == "focus_lost" || record.kind == "focus_regained") {
			source, _ := record.detail["source"].(string)
			reports = append(reports, [2]string{record.kind, source})
		}
	}
	return reports
}

// A student who switches to a text console or to another user leaves the exam as much as one who switches to another window,
// though the window keeps its focus: it is reported as leaving, with what it came from, the alarm sounds, and coming back ends it.
func TestLeavingTheSessionCountsAsLeavingTheWindow(t *testing.T) {
	f := newFixture(t)
	var alerts atomic.Int64
	s := monitorClient(t, f, func(o *Options) { o.Monitoring.OnAlert = func(Incident) { alerts.Add(1) } })
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.HeartbeatAt != nil && v.Queued == 0 })
	// The student switches to a text console: the window keeps its focus, and the session is no longer on screen.
	ReportSessionActive(s, false, "other_console")
	waitMonitor(t, s, func(v MonitoringView) bool { return v.Focus == "unfocused" && v.Queued == 0 })
	waitUntil(t, func() bool { return alerts.Load() > 0 })
	last := focusReports(f)
	if len(last) == 0 || last[len(last)-1] != [2]string{"focus_lost", "session_inactive"} {
		t.Fatalf("reports = %v", last)
	}
	// And back: the alarm stops, and the return says what it came from.
	ReportSessionActive(s, true, "")
	waitMonitor(t, s, func(v MonitoringView) bool { return v.Focus == "focused" && v.Queued == 0 })
	reports := focusReports(f)
	if reports[len(reports)-1] != [2]string{"focus_regained", "session_active"} {
		t.Fatalf("reports = %v", reports)
	}
	time.Sleep(100 * time.Millisecond)
	settled := alerts.Load()
	time.Sleep(150 * time.Millisecond)
	if alerts.Load() != settled {
		t.Fatal("the alarm went on after the student came back")
	}
}

// The student is in the exam window only when both hold: the window has the focus and the session is on screen. Neither
// coming back alone is a return, and a state reported again is not a new report.
func TestTheStudentIsBackOnlyWhenTheWindowAndTheSessionAre(t *testing.T) {
	f := newFixture(t)
	s := monitorClient(t, f, nil)
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.HeartbeatAt != nil && v.Queued == 0 })
	ReportSessionActive(s, false, "other_console")
	ReportSessionActive(s, false, "other_console")
	waitMonitor(t, s, func(v MonitoringView) bool { return v.Focus == "unfocused" && v.Queued == 0 })
	// The window loses its focus while the session is away, and gets it back: still away, nothing reported.
	ReportNativeFocus(s, false)
	ReportNativeFocus(s, true)
	time.Sleep(100 * time.Millisecond)
	if v := s.Snapshot().Monitoring; v.Focus != "unfocused" {
		t.Fatalf("focus = %q with the session off screen", v.Focus)
	}
	// The window is unfocused when the session comes back: still not in the exam window.
	ReportNativeFocus(s, false)
	ReportSessionActive(s, true, "")
	time.Sleep(100 * time.Millisecond)
	if v := s.Snapshot().Monitoring; v.Focus != "unfocused" {
		t.Fatalf("focus = %q with the window unfocused", v.Focus)
	}
	ReportNativeFocus(s, true)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.Focus == "focused" && v.Queued == 0 })
	var lost, regained int
	for _, report := range focusReports(f) {
		switch report[0] {
		case "focus_lost":
			lost++
		case "focus_regained":
			regained++
		}
	}
	// The baseline at the login is a focus_regained; after it, one loss and one return, however the two inputs moved.
	if lost != 1 || regained != 2 {
		t.Fatalf("losses = %d, returns = %d: %v", lost, regained, focusReports(f))
	}
}

// Whether the session is on screen is not known before the window has said anything: it changes nothing until then.
func TestTheSessionAloneSaysNothingBeforeTheWindowHasReported(t *testing.T) {
	f := newFixture(t)
	s := monitorClient(t, f, nil)
	ReportSessionActive(s, false, "other_console")
	loginClient(t, s, f)
	time.Sleep(100 * time.Millisecond)
	if v := s.Snapshot().Monitoring; v.Focus != "unknown" {
		t.Fatalf("focus = %q", v.Focus)
	}
	// The window reports it has the focus; the session is off screen, so the student is not in the exam window.
	ReportNativeFocus(s, true)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.Focus == "unfocused" })
}

// sessionReasons are what the fake server has been told took the session off screen, with the kind of each report.
func sessionReasons(f *fixture) [][2]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var reasons [][2]string
	for _, record := range f.telemetry {
		if record.path == "/event" && (record.kind == "focus_lost" || record.kind == "focus_regained") {
			reason, _ := record.detail["session_reason"].(string)
			reasons = append(reasons, [2]string{record.kind, reason})
		}
	}
	return reasons
}

// The report of the session leaving says what took it off screen, and so does the one of its return.
func TestTheReasonTheSessionLeftTravelsWithTheAbsence(t *testing.T) {
	f := newFixture(t)
	s := monitorClient(t, f, nil)
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.HeartbeatAt != nil && v.Queued == 0 })
	ReportSessionActive(s, false, "other_session")
	waitMonitor(t, s, func(v MonitoringView) bool { return v.Focus == "unfocused" && v.Queued == 0 })
	ReportSessionActive(s, true, "")
	waitMonitor(t, s, func(v MonitoringView) bool { return v.Focus == "focused" && v.Queued == 0 })
	got := sessionReasons(f)
	// The baseline at the login first, then the loss and the return with the reason.
	want := [][2]string{{"focus_regained", ""}, {"focus_lost", "other_session"}, {"focus_regained", "other_session"}}
	if len(got) != len(want) || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("reasons = %v, want %v", got, want)
	}
}

// Switching user can take the focus from the window before the session is seen to leave: the loss then says nothing of the
// session, and the return, which does, is where the reason is found. An absence of the window alone afterwards carries none.
func TestTheReasonIsKeptWhenTheWindowLostItsFocusFirst(t *testing.T) {
	f := newFixture(t)
	s := monitorClient(t, f, nil)
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.HeartbeatAt != nil && v.Queued == 0 })
	ReportNativeFocus(s, false)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.Focus == "unfocused" && v.Queued == 0 })
	ReportSessionActive(s, false, "other_session")
	// The session comes back first; the student is still not in the window.
	ReportSessionActive(s, true, "")
	time.Sleep(100 * time.Millisecond)
	ReportNativeFocus(s, true)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.Focus == "focused" && v.Queued == 0 })
	// A later absence of the window alone does not inherit it.
	ReportNativeFocus(s, false)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.Focus == "unfocused" && v.Queued == 0 })
	ReportNativeFocus(s, true)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.Focus == "focused" && v.Queued == 0 })
	want := [][2]string{{"focus_regained", ""}, {"focus_lost", ""}, {"focus_regained", "other_session"}, {"focus_lost", ""}, {"focus_regained", ""}}
	got := sessionReasons(f)
	if len(got) != len(want) {
		t.Fatalf("reasons = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("reasons = %v, want %v", got, want)
		}
	}
}

// failedCaptureReasons are what the fake server was told took the session off screen, for each capture failure it has heard of.
func failedCaptureReasons(f *fixture) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var reasons []string
	for _, record := range f.telemetry {
		if record.path == "/event" && record.kind == "capture_failed" {
			reason, _ := record.detail["session_reason"].(string)
			reasons = append(reasons, reason)
		}
	}
	return reasons
}

// A screen cannot be captured while another user's session has it, and the record of the failure says that was the case: it is why
// the capture failed. A capture that fails with the session on screen says nothing of the kind.
func TestAFailedCaptureSaysWhenTheSessionWasOffScreen(t *testing.T) {
	f := newFixture(t)
	var failing atomic.Bool
	s := requiredCaptureClient(t, f, &failing)
	verifyRequired(t, s)
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	waitView(t, s, func(v View) bool { return !v.Capture.Blocked && v.Monitoring.Queued == 0 })
	failing.Store(true)
	ReportSessionActive(s, false, "other_session")
	waitUntil(t, func() bool { return len(failedCaptureReasons(f)) == 1 })
	// The student is back on screen, and the retest passes.
	ReportSessionActive(s, true, "")
	failing.Store(false)
	waitView(t, s, func(v View) bool { return v.Capture.PendingReports == 0 && v.Monitoring.Focus == "focused" })
	verifyRequired(t, s)
	// A capture that fails now, with the session on screen, is no different from any other failure.
	failing.Store(true)
	ReportNativeFocus(s, false)
	waitUntil(t, func() bool { return len(failedCaptureReasons(f)) == 2 })
	if got := failedCaptureReasons(f); got[0] != "other_session" || got[1] != "" {
		t.Fatalf("reasons of the failures = %q, want [other_session \"\"]", got)
	}
}
