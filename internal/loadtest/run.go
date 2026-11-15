package loadtest

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"
)

// Config is what a load test is.
type Config struct {
	// Server is the exam server's address (http://host:port); PIN is the exam's; Accounts are the students, one simulated student each.
	Server   string
	PIN      string
	Accounts []string
	// Duration is how long the steady state lasts, once every workspace is up. Ramp is how long the logins are spread over (a class does
	// not log in in the same second); ReadyWait is how long to wait for every workspace to come up.
	Duration, Ramp, ReadyWait time.Duration
	// Storms are the moments (from the start of the steady state) at which every student has the build of the whole project run at once: the
	// teacher who says "now run make".
	Storms []time.Duration
	// ThinkMin and ThinkMax bound the pause between two stretches of work; SaveGap is the time between the autosaves of one stretch;
	// Heartbeat is how often the application tells the server it is there.
	ThinkMin, ThinkMax, SaveGap, Heartbeat time.Duration
	Seed                                   int64
	// Sample, when set, is called every SampleEvery for what the machine is doing (the host's numbers); the values go to the time series.
	Sample      func() map[string]float64
	SampleEvery time.Duration
	// Logf, when set, is told how the test goes.
	Logf func(format string, args ...any)
	HTTP *http.Client
}

func (c *Config) defaults() {
	if c.Duration <= 0 {
		c.Duration = 30 * time.Minute
	}
	if c.Ramp <= 0 {
		c.Ramp = 20 * time.Second
	}
	if c.ReadyWait <= 0 {
		c.ReadyWait = 5 * time.Minute
	}
	if c.ThinkMin <= 0 {
		c.ThinkMin, c.ThinkMax = 4*time.Second, 14*time.Second
	}
	if c.SaveGap <= 0 {
		c.SaveGap = time.Second
	}
	if c.Heartbeat <= 0 {
		c.Heartbeat = 5 * time.Second
	}
	if c.SampleEvery <= 0 {
		c.SampleEvery = 10 * time.Second
	}
	if c.Logf == nil {
		c.Logf = func(string, ...any) {}
	}
	if c.HTTP == nil {
		c.HTTP = &http.Client{Timeout: 60 * time.Second, Transport: &http.Transport{MaxIdleConnsPerHost: 4}}
	}
}

// Point is one reading of the machine, a time from the start of the steady state (negative while the class was still logging in).
type Point struct {
	At     time.Duration
	Values map[string]float64
}

// Result is what a load test measured.
type Result struct {
	Students int
	Ready    int
	Started  time.Time
	Steady   time.Duration
	Summary  Summary
	Series   []Point
}

// Run makes the class take the exam. It returns when every student has submitted and left, or when ctx ends.
func Run(ctx context.Context, cfg Config) (*Result, error) {
	cfg.defaults()
	if len(cfg.Accounts) == 0 {
		return nil, fmt.Errorf("no students")
	}
	rec := NewRecorder()
	result := &Result{Students: len(cfg.Accounts), Started: time.Now()}
	start := make(chan struct{})
	storms := make([]chan struct{}, len(cfg.Storms))
	for i := range storms {
		storms[i] = make(chan struct{})
	}
	var ready, done sync.WaitGroup
	var readyCount int
	var countMu sync.Mutex
	stop, endWork := context.WithCancel(ctx)
	defer endWork()
	finish, endFinish := context.WithCancel(ctx)
	defer endFinish()

	// The machine, from the first login on (the times are made relative to the steady state's start at the end).
	type reading struct {
		at     time.Time
		values map[string]float64
	}
	var readings []reading
	var seriesMu sync.Mutex
	samplerDone := make(chan struct{})
	samplerStop := make(chan struct{})
	go func() {
		defer close(samplerDone)
		if cfg.Sample == nil {
			return
		}
		ticker := time.NewTicker(cfg.SampleEvery)
		defer ticker.Stop()
		for {
			values := cfg.Sample()
			seriesMu.Lock()
			readings = append(readings, reading{at: time.Now(), values: values})
			seriesMu.Unlock()
			select {
			case <-ticker.C:
			case <-samplerStop:
				return
			}
		}
	}()

	for i, account := range cfg.Accounts {
		ready.Add(1)
		done.Add(1)
		s := newStudent(&cfg, rec, account, cfg.Seed+int64(i)*7919)
		login := time.Duration(float64(cfg.Ramp) * float64(i) / float64(len(cfg.Accounts)))
		go func() {
			defer done.Done()
			counted := false
			markReady := func(ok bool) {
				if counted {
					return
				}
				counted = true
				if ok {
					countMu.Lock()
					readyCount++
					countMu.Unlock()
				}
				ready.Done()
			}
			defer markReady(false)
			if !s.sleep(ctx, login) {
				return
			}
			if err := s.login(ctx); err != nil {
				cfg.Logf("%s: %v", s.account, err)
				return
			}
			heartbeats, endHeartbeats := context.WithCancel(ctx)
			defer endHeartbeats()
			go s.heartbeats(heartbeats)
			if err := s.dial(ctx); err != nil {
				rec.Add("connect", 0, failure(err))
				cfg.Logf("%s: %v", s.account, err)
				return
			}
			defer s.close()
			if err := s.ready(ctx, cfg.ReadyWait); err != nil {
				cfg.Logf("%s: %v", s.account, err)
				return
			}
			if err := s.discover(ctx); err != nil {
				cfg.Logf("%s: %v", s.account, err)
				return
			}
			markReady(true)
			select {
			case <-start:
			case <-ctx.Done():
				return
			}
			go s.typist(stop)
			next := 0
			for stop.Err() == nil {
				if _, closed := s.connection(); closed != nil {
					select {
					case <-closed:
						if !s.reconnect(stop) {
							rec.Event("student lost for good")
							return
						}
					default:
					}
				}
				// A storm everyone answers at once, otherwise a stretch of work.
				if next < len(storms) {
					select {
					case <-storms[next]:
						next++
						if err := s.touchEverything(stop); err == nil {
							_ = s.make(stop, OpStorm, "make")
						}
						continue
					default:
					}
				}
				if err := s.cycle(stop); err != nil && stop.Err() == nil {
					rec.Event("a stretch of work failed: " + failure(err))
				}
				wait := time.NewTimer(s.between(cfg.ThinkMin, cfg.ThinkMax))
				if next < len(storms) {
					select {
					case <-wait.C:
					case <-storms[next]:
						wait.Stop()
					case <-stop.Done():
					}
				} else {
					select {
					case <-wait.C:
					case <-stop.Done():
					}
				}
			}
			// The end of the exam: everyone submits, which takes a snapshot of /work for each of them.
			if _, err := s.timed(finish, OpSubmit, "submit", nil); err != nil {
				cfg.Logf("%s: submit: %v", s.account, err)
			}
		}()
	}

	cfg.Logf("%d students are logging in over %v", len(cfg.Accounts), cfg.Ramp)
	readyDone := make(chan struct{})
	go func() { ready.Wait(); close(readyDone) }()
	select {
	case <-readyDone:
	case <-time.After(cfg.Ramp + cfg.ReadyWait + time.Minute):
		cfg.Logf("not every student is ready: going on with those who are")
	case <-ctx.Done():
	}
	countMu.Lock()
	result.Ready = readyCount
	countMu.Unlock()
	cfg.Logf("%d of %d students are working; the steady state starts", result.Ready, result.Students)
	rec.Reset()
	steady := time.Now()
	close(start)
	timers := []*time.Timer{}
	for i, at := range cfg.Storms {
		i := i
		timers = append(timers, time.AfterFunc(at, func() {
			cfg.Logf("storm %d: every student builds the whole project now", i+1)
			close(storms[i])
		}))
	}
	select {
	case <-time.After(cfg.Duration):
	case <-ctx.Done():
	}
	for _, timer := range timers {
		timer.Stop()
	}
	result.Steady = time.Since(steady)
	cfg.Logf("the steady state is over after %v: the students submit", result.Steady.Round(time.Second))
	endWork()
	finished := make(chan struct{})
	go func() { done.Wait(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(5 * time.Minute):
		cfg.Logf("some students did not finish in 5 minutes")
		endFinish()
	}
	close(samplerStop)
	<-samplerDone
	result.Summary = rec.Summary()
	seriesMu.Lock()
	for _, r := range readings {
		result.Series = append(result.Series, Point{At: r.at.Sub(steady), Values: r.values})
	}
	seriesMu.Unlock()
	sort.Slice(result.Series, func(i, j int) bool { return result.Series[i].At < result.Series[j].At })
	return result, nil
}

// WriteSeries writes the machine's readings as CSV: one line for each, with a column for each thing read.
func (r *Result) WriteSeries(w *csv.Writer) error {
	names := map[string]bool{}
	for _, point := range r.Series {
		for name := range point.Values {
			names[name] = true
		}
	}
	var columns []string
	for name := range names {
		columns = append(columns, name)
	}
	sort.Strings(columns)
	if err := w.Write(append([]string{"seconds_from_steady_state"}, columns...)); err != nil {
		return err
	}
	for _, point := range r.Series {
		row := []string{strconv.FormatFloat(point.At.Seconds(), 'f', 1, 64)}
		for _, name := range columns {
			if value, ok := point.Values[name]; ok {
				row = append(row, strconv.FormatFloat(value, 'f', 3, 64))
			} else {
				row = append(row, "")
			}
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

// Column is what the machine's readings of one thing came to during the steady state.
type Column struct {
	Name string
	Mean float64
	P95  float64
	Max  float64
	N    int
}

// Machine summarizes the readings taken during the steady state, one column for each thing read, by name.
func (r *Result) Machine() []Column {
	values := map[string][]float64{}
	for _, point := range r.Series {
		if point.At < 0 {
			continue
		}
		for name, value := range point.Values {
			values[name] = append(values[name], value)
		}
	}
	var names []string
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	columns := make([]Column, 0, len(names))
	for _, name := range names {
		series := values[name]
		sort.Float64s(series)
		var total float64
		for _, value := range series {
			total += value
		}
		rank := int(float64(len(series))*0.95+0.999999) - 1
		columns = append(columns, Column{Name: name, N: len(series), Mean: total / float64(len(series)), P95: series[max(0, min(rank, len(series)-1))], Max: series[len(series)-1]})
	}
	return columns
}

// MachineMarkdown writes the machine's table.
func (r *Result) MachineMarkdown(w io.Writer) {
	fmt.Fprintln(w, "| Reading | mean | 95th percentile | max | readings |")
	fmt.Fprintln(w, "|---|---:|---:|---:|---:|")
	for _, column := range r.Machine() {
		fmt.Fprintf(w, "| %s | %.1f | %.1f | %.1f | %d |\n", column.Name, column.Mean, column.P95, column.Max, column.N)
	}
}
