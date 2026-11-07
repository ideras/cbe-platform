package remotewatch

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func names(findings []Finding) []string {
	var list []string
	for _, finding := range findings {
		list = append(list, finding.Name)
	}
	return list
}

// An entry is a name, or the start of one with a trailing star; case and ".exe" do not matter, and a name that only contains the
// entry is not a match.
func TestAnEntryNamesAProgramOrTheStartOfOne(t *testing.T) {
	for _, test := range []struct {
		process, entry string
		want           bool
	}{
		{"AnyDesk.exe", "anydesk", true},
		{"anydesk", "AnyDesk.EXE", true},
		{"TeamViewer_Service.exe", "teamviewer*", true},
		{"TeamViewer.exe", "teamviewer*", true},
		{"teamviewer", "teamviewer*", true},
		{"MyTeamViewer.exe", "teamviewer*", false},
		{"anydesk2", "anydesk", false},
		{"mstsc.exe", "mstsc", true},
		{"Screen Sharing", "screen sharing", true},
		{"anything", "*", false},
		{"anything", "", false},
		{"  AnyDesk.exe ", "anydesk", true},
	} {
		if got := matches(test.process, test.entry); got != test.want {
			t.Errorf("matches(%q, %q) = %v, want %v", test.process, test.entry, got, test.want)
		}
	}
}

// Each entry that has a match gives one finding, named by the first program that matches it, and a program is never reported twice.
func TestTheListGivesOneFindingPerEntryAndNeverTheSameProgramTwice(t *testing.T) {
	running := []string{"explorer.exe", "TeamViewer_Service.exe", "TeamViewer.exe", "AnyDesk.exe", "notepad.exe", "svchost.exe"}
	// "teamviewer*" names the first of the two TeamViewer programs, in name order; the entry for the service finds the other.
	got := matchTools(running, []string{"teamviewer*", "anydesk", "teamviewer_service", "rustdesk", "rdpclip*"})
	if want := []string{"TeamViewer.exe", "AnyDesk.exe", "TeamViewer_Service.exe"}; !slices.Equal(names(got), want) {
		t.Fatalf("findings = %v, want %v", names(got), want)
	}
	for _, finding := range got {
		if finding.Kind != KindTool || finding.Detail != "" {
			t.Fatalf("finding = %+v", finding)
		}
	}
	// Two entries that name the same program find it once.
	if got := matchTools(running, []string{"anydesk", "anydesk*", "AnyDesk.exe"}); !slices.Equal(names(got), []string{"AnyDesk.exe"}) {
		t.Fatalf("a program named by three entries was found as %v", names(got))
	}
	if got := matchTools(running, nil); len(got) != 0 {
		t.Fatalf("an empty list found %v", got)
	}
	if got := matchTools(nil, []string{"anydesk"}); len(got) != 0 {
		t.Fatalf("no programs found %v", got)
	}
}

// A name that is too long is cut without breaking a character.
func TestAProgramNameIsKeptShortAndWhole(t *testing.T) {
	long := "anydesk" + strings.Repeat("é", 60)
	got := matchTools([]string{long}, []string{"anydesk*"})
	if len(got) != 1 || len(got[0].Name) > maxName || !strings.HasPrefix(long, got[0].Name) {
		t.Fatalf("finding = %+v", got)
	}
	if strings.ContainsRune(got[0].Name, '�') {
		t.Fatalf("the name was cut in the middle of a character: %q", got[0].Name)
	}
}

// looks is a scripted sequence of what the computer shows on each look; the last one repeats.
type looks struct {
	mu   sync.Mutex
	list [][]Finding
	at   int
}

func (l *looks) look() []Finding {
	l.mu.Lock()
	defer l.mu.Unlock()
	current := l.list[min(l.at, len(l.list)-1)]
	l.at++
	return current
}
func (l *looks) taken() int { l.mu.Lock(); defer l.mu.Unlock(); return l.at }

type reported struct {
	mu   sync.Mutex
	list [][]Finding
}

func (r *reported) add(findings []Finding) {
	r.mu.Lock()
	r.list = append(r.list, slices.Clone(findings))
	r.mu.Unlock()
}
func (r *reported) all() [][]Finding { r.mu.Lock(); defer r.mu.Unlock(); return slices.Clone(r.list) }

func runPoll(t *testing.T, script [][]Finding, until int) [][]Finding {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	l := &looks{list: script}
	var got reported
	go poll(ctx, time.Millisecond, l.look, got.add)
	deadline := time.Now().Add(3 * time.Second)
	for l.taken() < until && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if l.taken() < until {
		t.Fatalf("only %d looks were taken", l.taken())
	}
	return got.all()
}

var (
	anyDesk = Finding{Kind: KindTool, Name: "AnyDesk.exe"}
	viewer  = Finding{Kind: KindTool, Name: "TeamViewer.exe"}
	rdp     = Finding{Kind: KindSession, Name: "Remote Desktop", Detail: "LAPTOP-1"}
)

// A finding is reported once it has been seen on two looks in a row, and withdrawn once it has been gone for two.
func TestAFindingIsReportedAfterTwoLooksAndWithdrawnAfterTwoMore(t *testing.T) {
	none := []Finding{}
	got := runPoll(t, [][]Finding{none, {anyDesk}, {anyDesk}, {anyDesk}, none, none, none}, 12)
	want := [][]Finding{{anyDesk}, {}}
	if !equal(got, want) {
		t.Fatalf("reports = %v, want %v", got, want)
	}
}

// A program that starts and stops within a look says nothing, and one that blinks out for a single look stays reported.
func TestAMomentaryProgramSaysNothingAndABlinkDoesNotWithdrawOne(t *testing.T) {
	none := []Finding{}
	got := runPoll(t, [][]Finding{none, {viewer}, none, none, {anyDesk}, {anyDesk}, none, {anyDesk}, {anyDesk}, none, none, none}, 14)
	want := [][]Finding{{anyDesk}, {}}
	if !equal(got, want) {
		t.Fatalf("reports = %v, want %v", got, want)
	}
}

// Everything present is reported together, in a fixed order, each time the set changes.
func TestTheWholeSetIsReportedWhenItChanges(t *testing.T) {
	both := []Finding{viewer, rdp}
	got := runPoll(t, [][]Finding{{rdp}, {rdp}, both, both, both, {viewer}, {viewer}, {viewer}, {viewer}}, 14)
	// A session sorts before a tool.
	want := [][]Finding{{rdp}, {rdp, viewer}, {viewer}}
	if !equal(got, want) {
		t.Fatalf("reports = %v, want %v", got, want)
	}
}

var rdpBare = Finding{Kind: KindSession, Name: "Remote Desktop"}

// A Remote Desktop session its client has left is still that session: it stops naming the computer, and that is not a new finding
// that says less.
func TestASessionKeepsItsClientWhenTheSystemStopsNamingIt(t *testing.T) {
	got := runPoll(t, [][]Finding{{rdp}, {rdp}, {rdpBare}, {rdpBare}, {rdpBare}, {}, {}, {}}, 14)
	if want := [][]Finding{{rdp}, {}}; !equal(got, want) {
		t.Fatalf("reports = %v, want %v", got, want)
	}
}

// A client that is only named later is reported once it is known.
func TestAClientNamedLaterIsReportedWhenItIsKnown(t *testing.T) {
	got := runPoll(t, [][]Finding{{rdpBare}, {rdpBare}, {rdp}, {rdp}, {rdp}}, 10)
	if want := [][]Finding{{rdpBare}, {rdp}}; !equal(got, want) {
		t.Fatalf("reports = %v, want %v", got, want)
	}
}

// A session that is gone takes its client with it: the next one is not given the name of the one before.
func TestTheClientIsForgottenWhenTheSessionIsGone(t *testing.T) {
	got := runPoll(t, [][]Finding{{rdp}, {rdp}, {}, {}, {}, {rdpBare}, {rdpBare}, {rdpBare}}, 14)
	if want := [][]Finding{{rdp}, {}, {rdpBare}}; !equal(got, want) {
		t.Fatalf("reports = %v, want %v", got, want)
	}
}

// A new client while the session is still there replaces the one before.
func TestANewClientReplacesTheOneBefore(t *testing.T) {
	other := Finding{Kind: KindSession, Name: "Remote Desktop", Detail: "LAPTOP-2"}
	got := runPoll(t, [][]Finding{{rdp}, {rdp}, {rdpBare}, {other}, {other}, {other}}, 10)
	if want := [][]Finding{{rdp}, {other}}; !equal(got, want) {
		t.Fatalf("reports = %v, want %v", got, want)
	}
}

// A computer where nothing is found reports nothing, not even an empty set.
func TestNothingFoundSaysNothing(t *testing.T) {
	if got := runPoll(t, [][]Finding{{}}, 8); len(got) != 0 {
		t.Fatalf("reports = %v", got)
	}
}

// No more than the most that is reported are sent.
func TestOnlyTheMostIsReported(t *testing.T) {
	var many []Finding
	for i := 0; i < maxFindings+5; i++ {
		many = append(many, Finding{Kind: KindTool, Name: string(rune('a' + i))})
	}
	got := runPoll(t, [][]Finding{many}, 6)
	if len(got) != 1 || len(got[0]) != maxFindings {
		t.Fatalf("reports = %v", got)
	}
}

func equal(a, b [][]Finding) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !slices.Equal(a[i], b[i]) {
			return false
		}
	}
	return true
}
