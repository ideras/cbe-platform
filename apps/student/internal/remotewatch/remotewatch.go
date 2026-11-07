// Package remotewatch says whether the exam computer is being watched or controlled from somewhere else: a Remote Desktop
// session on Windows, and programs on a list (remote-control and screen-sharing software) that are running. It reports what it
// finds and nothing else: no process list, no window titles, no input.
//
// None of it is proof. A program can be renamed, and a computer can be watched by hardware or by another computer pointed at
// its screen. It is a deterrent and a record for the instructor.
package remotewatch

import (
	"context"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// KindSession is the student's own session being a Remote Desktop one: someone connected to it, or it started that way.
	KindSession = "remote_session"
	// KindTool is a program of the list running.
	KindTool = "remote_tool"
)

const (
	// maxFindings and maxName keep what is reported small: the names are shown to the instructor as plain text.
	maxFindings = 10
	maxName     = 64
	maxDetail   = 64
)

// Finding is one thing that says the computer is watched or controlled from elsewhere.
type Finding struct {
	// Kind is KindSession or KindTool.
	Kind string `json:"kind"`
	// Name is "Remote Desktop" for a session and the name of the program (as the system calls it) for a tool.
	Name string `json:"name"`
	// Detail says more when the system does: the name of the computer a Remote Desktop session comes from.
	Detail string `json:"detail,omitempty"`
}

// interval is how often the computer is looked at.
const interval = 3 * time.Second

// Watch looks at the computer every few seconds until ctx ends, and calls report with everything it finds each time that set
// changes (an empty set when it is all gone). tools is the list of program names to look for, asked again on every look, as
// it is only known after the login. A finding is reported once it has been there for two looks in a row, and withdrawn once it
// has been gone for two, so a program that starts and stops within a moment says nothing.
func Watch(ctx context.Context, tools func() []string, report func([]Finding)) {
	poll(ctx, interval, func() []Finding { return scan(tools()) }, report)
}

// scan is one look at the computer.
func scan(tools []string) []Finding {
	var found []Finding
	if finding, ok := remoteSession(); ok {
		found = append(found, finding)
	}
	if len(tools) > 0 {
		if names, ok := processNames(); ok {
			found = append(found, matchTools(names, tools)...)
		}
	}
	return found
}

// ident is what makes a finding the same one from look to look, whatever the system says about it meanwhile.
type ident struct{ kind, name string }

// rememberDetails fills in what the system has stopped saying: a Remote Desktop session that its client has left is still that
// session, but no longer names the computer it came from, and it is the same session with the same client, not a new finding that
// says less. The detail kept is the latest one given, per finding; details is cleared of a finding by forget once it is gone.
func rememberDetails(findings []Finding, details map[ident]string) []Finding {
	filled := make([]Finding, len(findings))
	for i, finding := range findings {
		key := ident{finding.Kind, finding.Name}
		if finding.Detail != "" {
			details[key] = finding.Detail
		} else {
			finding.Detail = details[key]
		}
		filled[i] = finding
	}
	return filled
}

// forget drops the remembered details of findings that are neither reported nor in sight.
func forget(details map[ident]string, sets ...map[Finding]struct{}) {
	alive := map[ident]bool{}
	for _, set := range sets {
		for finding := range set {
			alive[ident{finding.Kind, finding.Name}] = true
		}
	}
	for key := range details {
		if !alive[key] {
			delete(details, key)
		}
	}
}

// poll calls look every interval and reports the findings that have held steady, each time they change.
func poll(ctx context.Context, every time.Duration, look func() []Finding, report func([]Finding)) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	var previous, reported map[Finding]struct{}
	details := map[ident]string{}
	for {
		current := toSet(rememberDetails(look(), details))
		next := map[Finding]struct{}{}
		for finding := range current {
			_, wasReported := reported[finding]
			_, wasThere := previous[finding]
			if wasReported || wasThere {
				next[finding] = struct{}{}
			}
		}
		for finding := range reported {
			if _, there := previous[finding]; there {
				next[finding] = struct{}{}
			}
		}
		forget(details, next, current)
		if !sameSet(next, reported) {
			reported = next
			report(sorted(next))
		}
		previous = current
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func toSet(findings []Finding) map[Finding]struct{} {
	set := make(map[Finding]struct{}, len(findings))
	for _, finding := range findings {
		set[finding] = struct{}{}
	}
	return set
}

func sameSet(a, b map[Finding]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for finding := range a {
		if _, ok := b[finding]; !ok {
			return false
		}
	}
	return true
}

// sorted is the set in a fixed order, kept to the most that is reported.
func sorted(set map[Finding]struct{}) []Finding {
	list := make([]Finding, 0, len(set))
	for finding := range set {
		list = append(list, finding)
	}
	slices.SortFunc(list, func(a, b Finding) int {
		if c := strings.Compare(a.Kind, b.Kind); c != 0 {
			return c
		}
		if c := strings.Compare(a.Name, b.Name); c != 0 {
			return c
		}
		return strings.Compare(a.Detail, b.Detail)
	})
	if len(list) > maxFindings {
		list = list[:maxFindings]
	}
	return list
}

// normalize is a program name as the list and the match see it: lower case, trimmed, and without a ".exe".
func normalize(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	return strings.TrimSuffix(name, ".exe")
}

// matches says whether a program name is the one the list entry names. An entry is a name, or the start of one when it ends
// with "*" ("teamviewer*" is TeamViewer.exe, TeamViewer_Service.exe and so on). Neither side's case or ".exe" matters.
func matches(process, entry string) bool {
	return matchesNormalized(normalize(process), normalize(entry))
}

// matchesNormalized is matches for a program name and an entry that have been through normalize already.
func matchesNormalized(process, entry string) bool {
	if entry == "" || entry == "*" {
		return false
	}
	if prefix, ok := strings.CutSuffix(entry, "*"); ok {
		return strings.HasPrefix(process, prefix)
	}
	return process == entry
}

// matchTools is the programs among the running ones that the list names: one finding per entry that has a match, named by the
// first program (in name order) that matches it, and no program twice.
func matchTools(processes, tools []string) []Finding {
	// Every name is normalized once here, not once for each entry it is compared with: there are hundreds of programs and dozens of
	// entries, and this runs every few seconds.
	type program struct{ name, key string }
	programs := make([]program, len(processes))
	for i, name := range processes {
		programs[i] = program{name, normalize(name)}
	}
	slices.SortFunc(programs, func(a, b program) int { return strings.Compare(a.name, b.name) })
	var found []Finding
	seen := map[string]bool{}
	for _, entry := range tools {
		entry = normalize(entry)
		for _, process := range programs {
			if seen[process.key] || !matchesNormalized(process.key, entry) {
				continue
			}
			seen[process.key] = true
			found = append(found, Finding{Kind: KindTool, Name: truncate(process.name, maxName)})
			break
		}
	}
	return found
}

// truncate cuts a string to at most n bytes without splitting a character.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
