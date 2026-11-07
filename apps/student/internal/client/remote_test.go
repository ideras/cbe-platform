package client

import (
	"slices"
	"testing"
	"time"
)

// remoteReports are the remote_changed reports the fake server has received: the names it was told were found, in order. A report
// of nothing found is an empty list, not a missing one.
func remoteReports(f *fixture) [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var reports [][]string
	for _, record := range f.telemetry {
		if record.path != "/event" || record.kind != "remote_changed" {
			continue
		}
		active, ok := record.detail["active"].([]any)
		if !ok {
			reports = append(reports, nil)
			continue
		}
		names := []string{}
		for _, item := range active {
			names = append(names, item.(map[string]any)["name"].(string))
		}
		reports = append(reports, names)
	}
	return reports
}

var anyDesk = RemoteFinding{Kind: "remote_tool", Name: "AnyDesk.exe"}

// What the watch finds is told to the server when it changes, with everything found at that moment, and an empty list when it is
// all gone; the same findings again are not told twice, and the student's view carries them.
func TestWhatTheWatchFindsIsToldToTheServerWhenItChanges(t *testing.T) {
	f := newFixture(t)
	s := monitorClient(t, f, nil)
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.HeartbeatAt != nil && v.Queued == 0 })
	if got := remoteReports(f); len(got) != 0 {
		t.Fatalf("a computer with nothing found told the server %v", got)
	}
	rdp := RemoteFinding{Kind: "remote_session", Name: "Remote Desktop", Detail: "LAPTOP-1"}
	ReportRemote(s, []RemoteFinding{rdp, anyDesk})
	ReportRemote(s, []RemoteFinding{rdp, anyDesk})
	waitMonitor(t, s, func(v MonitoringView) bool { return v.Queued == 0 && len(v.Remote) == 2 })
	if v := s.Snapshot().Monitoring; v.Remote[0] != rdp || v.Remote[1] != anyDesk {
		t.Fatalf("the view says %+v", v.Remote)
	}
	ReportRemote(s, nil)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.Queued == 0 && len(v.Remote) == 0 })
	time.Sleep(50 * time.Millisecond)
	got := remoteReports(f)
	if len(got) != 2 || !slices.Equal(got[0], []string{"Remote Desktop", "AnyDesk.exe"}) || got[1] == nil || len(got[1]) != 0 {
		t.Fatalf("reports = %#v, want the two names and then an empty list", got)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, record := range f.telemetry {
		if record.kind == "remote_changed" && record.detail["source"] != "remote_watch" {
			t.Fatalf("source = %v", record.detail["source"])
		}
	}
}

// What was found before the login is told once the exam is running, as the server cannot have heard of it.
func TestWhatWasFoundBeforeTheLoginIsToldAtTheLogin(t *testing.T) {
	f := newFixture(t)
	s := monitorClient(t, f, nil)
	ReportNativeFocus(s, true)
	ReportRemote(s, []RemoteFinding{anyDesk})
	time.Sleep(50 * time.Millisecond)
	if got := remoteReports(f); len(got) != 0 {
		t.Fatalf("told the server before the login: %v", got)
	}
	loginClient(t, s, f)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.HeartbeatAt != nil && v.Queued == 0 })
	if got := remoteReports(f); len(got) != 1 || !slices.Equal(got[0], []string{"AnyDesk.exe"}) {
		t.Fatalf("reports = %v", got)
	}
}

// The programs to look for come from the server at the login: none before it, and none from an older server.
func TestTheProgramsToLookForComeFromTheLogin(t *testing.T) {
	f := newFixture(t)
	f.remoteTools = []string{"anydesk*", "mstsc"}
	s := monitorClient(t, f, nil)
	if got := RemoteTools(s); len(got) != 0 {
		t.Fatalf("tools before the login: %v", got)
	}
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	if got := RemoteTools(s); !slices.Equal(got, []string{"anydesk*", "mstsc"}) {
		t.Fatalf("tools = %v", got)
	}
	older := newFixture(t)
	s2 := monitorClient(t, older, nil)
	ReportNativeFocus(s2, true)
	loginClient(t, s2, older)
	if got := RemoteTools(s2); len(got) != 0 {
		t.Fatalf("an older server named tools: %v", got)
	}
}

// A report the server could not be reached for is told again once contact is back: the state is what counts, not the moment.
func TestWhatWasFoundIsToldAgainAfterTheServerWasUnreachable(t *testing.T) {
	f := newFixture(t)
	var c connection
	c.install(f)
	s := monitorClient(t, f, nil)
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.HeartbeatAt != nil && v.Queued == 0 })
	c.sever.Store(true)
	ReportRemote(s, []RemoteFinding{anyDesk})
	waitMonitor(t, s, func(v MonitoringView) bool { return v.Queued == 0 })
	// The fake server logs every attempt, the ones it hung up on included. The report was given up on while it was unreachable...
	attempts := len(remoteReports(f))
	if attempts == 0 {
		t.Fatal("the report was never tried")
	}
	time.Sleep(100 * time.Millisecond)
	if got := len(remoteReports(f)); got != attempts {
		t.Fatalf("the report went on being tried: %d attempts, then %d", attempts, got)
	}
	// ...and told again, as what was found, once contact is back.
	c.sever.Store(false)
	waitUntil(t, func() bool { return len(remoteReports(f)) > attempts })
	if got := remoteReports(f); !slices.Equal(got[len(got)-1], []string{"AnyDesk.exe"}) {
		t.Fatalf("reports = %v", got)
	}
	if v := s.Snapshot().Monitoring; v.Dropped != 0 {
		t.Fatalf("a report lost to an unreachable server is called lost history: %+v", v)
	}
}

// What the watch found and told the server stays on the proctor's roster until it is told that it is gone. When it went while
// the server could not be reached, that report is the one that is lost, so it is sent again once the connection is back.
func TestAFindingThatWentWhileTheServerWasUnreachableIsToldAsGoneAfterwards(t *testing.T) {
	f := newFixture(t)
	f.status.Policy = networkPolicy(60000)
	var link connection
	link.install(f)
	s := monitorClient(t, f, nil)
	ReportNativeFocus(s, true)
	loginClient(t, s, f)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.HeartbeatAt != nil && v.Queued == 0 })
	ReportRemote(s, []RemoteFinding{anyDesk})
	waitUntil(t, func() bool { return len(remoteReports(f)) == 1 })
	waitMonitor(t, s, func(v MonitoringView) bool { return v.Queued == 0 })
	link.sever.Store(true)
	ReportRemote(s, nil) // the program is closed while the student changes network
	waitMonitor(t, s, func(v MonitoringView) bool { return v.Queued == 0 && len(v.Remote) == 0 })
	// The fake server logs every attempt, the ones it hung up on included.
	attempts := len(remoteReports(f))
	link.sever.Store(false)
	waitUntil(t, func() bool {
		reports := remoteReports(f)
		return len(reports) > attempts && reports[len(reports)-1] != nil && len(reports[len(reports)-1]) == 0
	})
	// Nothing else is said once it has been: a computer with nothing found does not repeat it.
	told := len(remoteReports(f))
	time.Sleep(150 * time.Millisecond)
	if reports := remoteReports(f); len(reports) != told {
		t.Fatalf("reports = %v", reports)
	}
}
