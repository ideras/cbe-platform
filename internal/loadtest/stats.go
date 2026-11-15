// Package loadtest simulates a class taking a workspace exam, for the Phase 8 load test: many students, each logging in, opening files,
// typing (autosave), running make in a terminal and finally submitting, through the exam server's own HTTP and WebSocket routes (the ones the
// student's application uses), and reports how long each thing took. The same code runs inside deploy/workspace/test-load.sh (a throwaway
// server on the container host) and as cmd/cbe-loadtest, which can be run on another machine to put the router between the students and the server.
package loadtest

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

// Operation names, as they appear in the report.
const (
	OpLogin     = "login"
	OpReady     = "workspace ready after login"
	OpHeartbeat = "heartbeat (HTTP)"
	OpList      = "open a folder (fs.list)"
	OpOpen      = "open a file (fs.read)"
	OpSave      = "autosave (fs.write)"
	OpCreate    = "new file (fs.create)"
	OpDelete    = "delete a file (fs.delete)"
	OpEcho      = "terminal echo"
	OpBuild     = "make after an edit"
	OpStorm     = "make of everything, all at once"
	OpSubmit    = "submit (a snapshot)"
	OpTodo      = "tick a TODO"
)

// sample is one measurement and when, from the start of the steady state, it was made.
type sample struct {
	at time.Duration
	d  time.Duration
}

type opStats struct {
	samples []sample
	errors  map[string]int
}

// Recorder collects measurements from every student.
type Recorder struct {
	mu    sync.Mutex
	start time.Time
	ops   map[string]*opStats
	// Events counts what is not a measurement: a connection that ended, an agent that restarted, and the like.
	events map[string]int
}

// NewRecorder starts a recorder; times of samples are taken from now.
func NewRecorder() *Recorder {
	return &Recorder{start: time.Now(), ops: map[string]*opStats{}, events: map[string]int{}}
}

// Reset says that the steady state starts now: what was measured before (the ramp-up) is kept, with negative times.
func (r *Recorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.start = time.Now()
}

// Add records a measurement of an operation. A failure is counted under its reason and its time is not a measurement.
func (r *Recorder) Add(op string, d time.Duration, failure string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.ops[op]
	if s == nil {
		s = &opStats{errors: map[string]int{}}
		r.ops[op] = s
	}
	if failure != "" {
		s.errors[failure]++
		return
	}
	s.samples = append(s.samples, sample{at: time.Since(r.start), d: d})
}

// Event counts something that happened.
func (r *Recorder) Event(name string) {
	r.mu.Lock()
	r.events[name]++
	r.mu.Unlock()
}

// OpSummary is what is said of one operation.
type OpSummary struct {
	Op       string         `json:"op"`
	Count    int            `json:"count"`
	Errors   map[string]int `json:"errors,omitempty"`
	MinMS    float64        `json:"min_ms"`
	P50MS    float64        `json:"p50_ms"`
	P95MS    float64        `json:"p95_ms"`
	P99MS    float64        `json:"p99_ms"`
	MaxMS    float64        `json:"max_ms"`
	MeanMS   float64        `json:"mean_ms"`
	Failures int            `json:"failures"`
}

// Minute is the 95th percentile of an operation in one minute of the steady state.
type Minute struct {
	Minute int     `json:"minute"`
	Count  int     `json:"count"`
	P50MS  float64 `json:"p50_ms"`
	P95MS  float64 `json:"p95_ms"`
	MaxMS  float64 `json:"max_ms"`
}

// Summary is everything measured.
type Summary struct {
	Ops     []OpSummary    `json:"ops"`
	Events  map[string]int `json:"events"`
	Minutes []Minute       `json:"saves_by_minute"`
}

func millis(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

// percentile is the nearest-rank percentile of sorted durations.
func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	rank := int(math.Ceil(p/100*float64(len(sorted)))) - 1
	return sorted[max(0, min(rank, len(sorted)-1))]
}

func summarize(op string, s *opStats) OpSummary {
	out := OpSummary{Op: op, Errors: s.errors}
	for _, count := range s.errors {
		out.Failures += count
	}
	if len(s.errors) == 0 {
		out.Errors = nil
	}
	durations := make([]time.Duration, len(s.samples))
	var total time.Duration
	for i, sample := range s.samples {
		durations[i] = sample.d
		total += sample.d
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	out.Count = len(durations)
	if len(durations) > 0 {
		out.MinMS, out.MaxMS = millis(durations[0]), millis(durations[len(durations)-1])
		out.P50MS, out.P95MS, out.P99MS = millis(percentile(durations, 50)), millis(percentile(durations, 95)), millis(percentile(durations, 99))
		out.MeanMS = millis(total) / float64(len(durations))
	}
	return out
}

// Summary computes the report's numbers. Operations are in the order of the Op constants, then the rest by name.
func (r *Recorder) Summary() Summary {
	r.mu.Lock()
	defer r.mu.Unlock()
	order := []string{OpLogin, OpReady, OpHeartbeat, OpList, OpOpen, OpSave, OpCreate, OpDelete, OpTodo, OpEcho, OpBuild, OpStorm, OpSubmit}
	var names []string
	seen := map[string]bool{}
	for _, name := range order {
		if r.ops[name] != nil {
			names = append(names, name)
			seen[name] = true
		}
	}
	var rest []string
	for name := range r.ops {
		if !seen[name] {
			rest = append(rest, name)
		}
	}
	sort.Strings(rest)
	names = append(names, rest...)
	summary := Summary{Events: map[string]int{}}
	for _, name := range names {
		summary.Ops = append(summary.Ops, summarize(name, r.ops[name]))
	}
	for name, count := range r.events {
		summary.Events[name] = count
	}
	if saves := r.ops[OpSave]; saves != nil {
		byMinute := map[int][]time.Duration{}
		for _, s := range saves.samples {
			if s.at < 0 {
				continue
			}
			byMinute[int(s.at/time.Minute)] = append(byMinute[int(s.at/time.Minute)], s.d)
		}
		var minutes []int
		for minute := range byMinute {
			minutes = append(minutes, minute)
		}
		sort.Ints(minutes)
		for _, minute := range minutes {
			durations := byMinute[minute]
			sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
			summary.Minutes = append(summary.Minutes, Minute{Minute: minute, Count: len(durations), P50MS: millis(percentile(durations, 50)), P95MS: millis(percentile(durations, 95)), MaxMS: millis(durations[len(durations)-1])})
		}
	}
	return summary
}

// Markdown writes the summary as the tables of a report.
func (s Summary) Markdown(w io.Writer) {
	fmt.Fprintln(w, "| Operation | Count | Failures | min | p50 | p95 | p99 | max |")
	fmt.Fprintln(w, "|---|---:|---:|---:|---:|---:|---:|---:|")
	for _, op := range s.Ops {
		fmt.Fprintf(w, "| %s | %d | %d | %s | %s | %s | %s | %s |\n", op.Op, op.Count, op.Failures, human(op.MinMS), human(op.P50MS), human(op.P95MS), human(op.P99MS), human(op.MaxMS))
	}
	for _, op := range s.Ops {
		if len(op.Errors) > 0 {
			var reasons []string
			for reason, count := range op.Errors {
				reasons = append(reasons, fmt.Sprintf("%s ×%d", reason, count))
			}
			sort.Strings(reasons)
			fmt.Fprintf(w, "\nFailures of *%s*: %s\n", op.Op, strings.Join(reasons, "; "))
		}
	}
	if len(s.Events) > 0 {
		var names []string
		for name := range s.Events {
			names = append(names, name)
		}
		sort.Strings(names)
		fmt.Fprintln(w, "\n| Event | Times |\n|---|---:|")
		for _, name := range names {
			fmt.Fprintf(w, "| %s | %d |\n", name, s.Events[name])
		}
	}
	if len(s.Minutes) > 0 {
		fmt.Fprintln(w, "\n| Minute of the test | Autosaves | p50 | p95 | max |\n|---:|---:|---:|---:|---:|")
		for _, m := range s.Minutes {
			fmt.Fprintf(w, "| %d | %d | %s | %s | %s |\n", m.Minute, m.Count, human(m.P50MS), human(m.P95MS), human(m.MaxMS))
		}
	}
}

func human(ms float64) string {
	switch {
	case ms >= 10000:
		return fmt.Sprintf("%.1f s", ms/1000)
	case ms >= 1000:
		return fmt.Sprintf("%.2f s", ms/1000)
	case ms >= 100:
		return fmt.Sprintf("%.0f ms", ms)
	default:
		return fmt.Sprintf("%.1f ms", ms)
	}
}
