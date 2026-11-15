package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"exam-platform/internal/runner"
)

// Grading (workspace plan, Phase 6). A proctor asks for one student or the whole class to be graded; each submission becomes a run of
// the exam's hidden tests (grading/grade) in a fresh container made by cbe-runner, with no network. The exam server decides what a run
// means: it reads the JSON the script printed (section 5.3 of the plan), and a script that did not print it, did not end in time, or
// crashed makes that run fail, with what it printed, and never holds up another student. A grade is advisory until a proctor confirms it.

// ErrNoGrading is returned when an exam is asked to be graded and it has no grading folder.
var ErrNoGrading = errors.New("workspace: the exam has no grading folder")

// ErrNoSubmission is returned when a student is asked to be graded and has submitted nothing.
var ErrNoSubmission = errors.New("workspace: the student has no submission")

// Limits of what a run's output is kept as.
const (
	// DefaultGradeTimeout is the plan's timeout of a grading run (Appendix A); an exam may set its own (grade_timeout).
	DefaultGradeTimeout = 5 * time.Minute
	// maxResultLog is the most of a TODO's log that is kept.
	maxResultLog = 8 << 10
	// maxGradingQueue is how many runs may wait for a worker; more than a whole school's exams at once.
	maxGradingQueue = 10000
)

// GraderOptions are what a Grader is made of.
type GraderOptions struct {
	// Parallel is how many runs the exam server has going at once. The runner bounds them too (grade_parallel); the lower of the two rules.
	Parallel       int
	DefaultTimeout time.Duration
}

// Grader runs the gradings of the exam server: the queue, the workers, and what each run's output means.
type Grader struct {
	m    *Manager
	opts GraderOptions
	jobs chan int64
	log  *slog.Logger
}

// NewGrader makes the grader of a manager's workspace exams.
func NewGrader(m *Manager, opts GraderOptions) *Grader {
	if opts.Parallel < 1 {
		opts.Parallel = 2
	}
	if opts.DefaultTimeout <= 0 {
		opts.DefaultTimeout = DefaultGradeTimeout
	}
	return &Grader{m: m, opts: opts, jobs: make(chan int64, maxGradingQueue), log: m.opts.Logger}
}

// Recover marks the runs that were waiting or going when the exam server last stopped as failed (interrupted): the proctor runs them
// again. It is called once, before the server takes requests.
func (g *Grader) Recover(ctx context.Context) {
	count, err := g.m.opts.Repo.InterruptGradings(ctx, g.m.opts.Now().UTC())
	if err != nil {
		g.log.Error("workspace: runs of the last life could not be marked interrupted", "error", err)
		return
	}
	if count > 0 {
		g.log.Warn("workspace: grading runs that a stop had interrupted were marked failed", "count", count)
	}
}

// Run takes runs from the queue with Parallel workers, until ctx ends.
func (g *Grader) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for range g.opts.Parallel {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case id := <-g.jobs:
					g.runOne(ctx, id)
				}
			}
		}()
	}
	wg.Wait()
}

// Skip says why a student was not queued for grading.
type Skip struct {
	SessionID string `json:"session_id"`
	Account   string `json:"account_number"`
	Reason    string `json:"reason"`
}

// Started is what asking for gradings did: the runs queued and the students left out.
type Started struct {
	Queued  []int64 `json:"queued"`
	Skipped []Skip  `json:"skipped"`
}

// gradable reads an exam and says whether it can be graded.
func (g *Grader) gradable(ctx context.Context, examID string) (ExamRecord, error) {
	exam, err := g.m.opts.Repo.Exam(ctx, examID)
	if err != nil {
		return ExamRecord{}, err
	}
	if exam.GradingHash == "" || !g.m.opts.Blobs.Has(exam.GradingHash) {
		return ExamRecord{}, ErrNoGrading
	}
	return exam, nil
}

// StartExam queues the latest submission of each of the exam's students (of those named, when sessions is not empty). A student with no
// submission, or whose last submission is already waiting or being graded, is left out and named.
func (g *Grader) StartExam(ctx context.Context, examID string, sessions []string) (Started, error) {
	if _, err := g.gradable(ctx, examID); err != nil {
		return Started{}, err
	}
	rows, err := g.m.opts.Repo.Roster(ctx, examID)
	if err != nil {
		return Started{}, err
	}
	// Those named are taken off a second list as they are met, so that what is left at the end is who is not a student of the exam.
	named, unmet := len(sessions) > 0, map[string]bool{}
	for _, id := range sessions {
		unmet[id] = true
	}
	out := Started{Queued: []int64{}, Skipped: []Skip{}}
	for _, row := range rows {
		if named && !unmet[row.SessionID] {
			continue
		}
		id, err := g.queue(ctx, row.SessionID, 0)
		switch {
		case err == nil:
			out.Queued = append(out.Queued, id)
		case errors.Is(err, ErrNoSubmission):
			out.Skipped = append(out.Skipped, Skip{SessionID: row.SessionID, Account: row.Account, Reason: "has not submitted anything"})
		case errors.Is(err, errBusy):
			out.Skipped = append(out.Skipped, Skip{SessionID: row.SessionID, Account: row.Account, Reason: "is already waiting to be graded or being graded"})
		default:
			return out, err
		}
		delete(unmet, row.SessionID)
	}
	for id := range unmet {
		out.Skipped = append(out.Skipped, Skip{SessionID: id, Reason: "is not a student of this exam"})
	}
	return out, nil
}

// StartSession queues one student's submission: the one named, or the latest (submission 0).
func (g *Grader) StartSession(ctx context.Context, session string, submission int64) (int64, error) {
	info, err := g.m.opts.Repo.Session(ctx, session)
	if err != nil {
		return 0, err
	}
	if _, err := g.gradable(ctx, info.ExamID); err != nil {
		return 0, err
	}
	id, err := g.queue(ctx, session, submission)
	if errors.Is(err, errBusy) {
		return 0, fmt.Errorf("%w: this student is already waiting to be graded or being graded", ErrBusy)
	}
	return id, err
}

// ErrBusy is returned when a student is asked to be graded and a run of theirs is waiting or going.
var ErrBusy = errors.New("workspace: a grading of this student is waiting or going")

var errBusy = ErrBusy

// queue makes a run of a session's submission (the latest when submission is 0) and puts it on the queue.
func (g *Grader) queue(ctx context.Context, session string, submission int64) (int64, error) {
	repo := g.m.opts.Repo
	submissions, err := repo.Submissions(ctx, session)
	if err != nil {
		return 0, err
	}
	var chosen *SubmissionRecord
	for i := range submissions {
		if submission == 0 || submissions[i].ID == submission {
			chosen = &submissions[i]
		}
	}
	if chosen == nil {
		return 0, ErrNoSubmission
	}
	earlier, err := repo.Gradings(ctx, session)
	if err != nil {
		return 0, err
	}
	for _, run := range earlier {
		if run.State == GradingQueued || run.State == GradingRunning {
			return 0, errBusy
		}
	}
	id, err := repo.CreateGrading(ctx, session, chosen.ID, g.m.opts.Now().UTC())
	if err != nil {
		return 0, err
	}
	select {
	case g.jobs <- id:
	default:
		// The queue is full: the run cannot wait, and it is failed in words rather than left waiting for ever.
		g.finishFailed(ctx, GradingRecord{ID: id, SessionID: session}, FailRunner, "too many runs are waiting; try again later", nil)
		return 0, fmt.Errorf("workspace: too many gradings are waiting")
	}
	return id, nil
}

// runOne is one run, from its start to its record. It never returns an error: what went wrong is the run's failure.
func (g *Grader) runOne(ctx context.Context, id int64) {
	repo := g.m.opts.Repo
	record, err := repo.Grading(ctx, id)
	if err != nil || record.State != GradingQueued {
		return
	}
	if err := repo.StartGrading(ctx, id, g.m.opts.Now().UTC()); err != nil {
		g.log.Error("workspace: a grading run could not be started", "grading", id, "error", err)
		return
	}
	// What is recorded is recorded even when the server is stopping: the run does not end in a state it never finished.
	done := func(record GradingRecord, failure, text string, result *runner.GradeResult) {
		bg, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		g.finishFailed(bg, record, failure, text, result)
	}
	info, err := repo.Session(ctx, record.SessionID)
	if err != nil {
		done(record, FailRunner, "the student's session could not be read", nil)
		return
	}
	exam, err := g.gradable(ctx, info.ExamID)
	if err != nil {
		done(record, FailRunner, "the exam has no grading folder any more", nil)
		return
	}
	bundle, err := g.m.opts.Blobs.Open(exam.GradingHash)
	if err != nil {
		done(record, FailRunner, "the exam's grading bundle cannot be read", nil)
		return
	}
	defer bundle.Close()
	settings := exam.Document.Workspace
	timeout := g.opts.DefaultTimeout
	if settings.GradeTimeoutSeconds != nil {
		timeout = time.Duration(*settings.GradeTimeoutSeconds) * time.Second
	}
	// The submission goes to the runner as it is read from the blob store: a tar of the files the exam submits.
	pipeReader, pipeWriter := io.Pipe()
	go func() { _ = pipeWriter.CloseWithError(g.m.SubmissionTar(ctx, record.SubmissionID, pipeWriter)) }()
	// The runner enforces the timeout itself; this one is for a runner that does not answer (its set-up and clean-up included).
	runCtx, cancel := context.WithTimeout(ctx, timeout+3*time.Minute)
	defer cancel()
	result, err := g.m.opts.Runner.Grade(runCtx, runner.GradeRequest{Grading: strconv.FormatInt(id, 10), Exam: info.ExamID, Image: settings.Image, TimeoutSeconds: int(timeout / time.Second),
		Resources: runner.Resources{CPU: settings.Resources.CPU, MemoryMB: settings.Resources.MemoryMB, DiskMB: settings.Resources.DiskMB}}, pipeReader, bundle)
	_ = pipeReader.Close()
	if err != nil {
		text := "the runner could not grade it: " + err.Error()
		if ctx.Err() != nil {
			done(record, FailInterrupted, "the exam server stopped before this run ended; run it again", nil)
			return
		}
		done(record, FailRunner, text, nil)
		return
	}
	g.settle(record, exam, timeout, result, done)
}

// settle turns what the runner returned into the run's record.
func (g *Grader) settle(record GradingRecord, exam ExamRecord, timeout time.Duration, result runner.GradeResult, done func(GradingRecord, string, string, *runner.GradeResult)) {
	switch {
	case result.TimedOut:
		done(record, FailTimeout, fmt.Sprintf("grade did not finish within %s and was stopped", timeout.Round(time.Second)), &result)
		return
	case result.OOMKilled:
		done(record, FailOOM, "grade ran out of memory and was stopped", &result)
		return
	case result.ExitCode == 137:
		// SIGKILL. The runtime does not always say it was for memory, which is what ends a program that grows past the run's limit.
		done(record, FailCrash, fmt.Sprintf("grade was killed (exit code 137): most likely it used more memory than the run's limit of %d MiB", exam.Document.Workspace.Resources.MemoryMB), &result)
		return
	case result.ExitCode != 0:
		done(record, FailCrash, fmt.Sprintf("grade ended with exit code %d (it must end with 0 after printing its JSON)", result.ExitCode), &result)
		return
	}
	results, points, max, err := ParseGrade(result.Stdout, exam.Document.Content.Todos)
	if err != nil {
		done(record, FailMalformed, err.Error(), &result)
		return
	}
	now := g.m.opts.Now().UTC()
	code := result.ExitCode
	record.State, record.FinishedAt, record.ExitCode = GradingDone, &now, &code
	record.DurationMS, record.Stdout, record.Stderr = result.DurationMS, result.Stdout, result.Stderr
	record.StdoutTruncated, record.StderrTruncated, record.Skipped = result.StdoutTruncated, result.StderrTruncated, result.Skipped
	record.Results, record.Points, record.Max = results, &points, max
	bg, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := g.m.opts.Repo.FinishGrading(bg, record); err != nil {
		g.log.Error("workspace: a grading could not be recorded", "grading", record.ID, "error", err)
		return
	}
	g.m.event(record.SessionID, EventGradingFinished, map[string]any{"grading": record.ID, "submission": record.SubmissionID, "state": GradingDone, "points": points, "max": max})
	g.log.Info("workspace: graded", "grading", record.ID, "session", record.SessionID, "points", points)
}

// finishFailed records a run as failed, with what it printed when there is that.
func (g *Grader) finishFailed(ctx context.Context, record GradingRecord, failure, text string, result *runner.GradeResult) {
	now := g.m.opts.Now().UTC()
	record.State, record.Failure, record.FailureText, record.FinishedAt = GradingFailed, failure, text, &now
	record.Results, record.Points, record.Max = nil, nil, nil
	if result != nil {
		code := result.ExitCode
		record.ExitCode, record.TimedOut, record.DurationMS = &code, result.TimedOut, result.DurationMS
		record.Stdout, record.Stderr, record.Skipped = result.Stdout, result.Stderr, result.Skipped
		record.StdoutTruncated, record.StderrTruncated = result.StdoutTruncated, result.StderrTruncated
	}
	if err := g.m.opts.Repo.FinishGrading(ctx, record); err != nil {
		g.log.Error("workspace: a failed grading could not be recorded", "grading", record.ID, "error", err)
		return
	}
	g.m.event(record.SessionID, EventGradingFinished, map[string]any{"grading": record.ID, "submission": record.SubmissionID, "state": GradingFailed, "failure": failure})
	g.log.Warn("workspace: a grading failed", "grading", record.ID, "session", record.SessionID, "failure", failure, "why", text)
}

// ParseGrade reads what grade printed (plan, 5.3): one JSON object whose keys are TODO IDs and whose values are
// {"passed": true|false, "points": number, "log": text}. It says what is wrong with anything else, in words a teacher can act on:
// nothing but the JSON may be on standard output (noise belongs on standard error), "passed" is required, and points are numbers
// that are not negative. A TODO's points are capped at what it is worth. A key that is not a TODO of the exam is kept and said so
// (Known false) and does not count; a TODO the output does not mention has no result. It returns the results, the points of the TODOs
// graded, and the points the TODOs are worth together (nil when none has points).
func ParseGrade(output string, todos []Todo) ([]GradingResult, float64, *float64, error) {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return nil, 0, nil, errors.New("grade printed nothing on standard output; it must print one JSON object")
	}
	decoder := json.NewDecoder(strings.NewReader(trimmed))
	var raw map[string]json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		return nil, 0, nil, fmt.Errorf("what grade printed is not one JSON object: %v (anything else on standard output, such as the output of make, spoils it: print that to standard error)", err)
	}
	if decoder.More() {
		return nil, 0, nil, errors.New("grade printed something after its JSON object; nothing else may be on standard output")
	}
	byID := map[string]Todo{}
	var max float64
	anyPoints := false
	for _, todo := range todos {
		byID[todo.ID] = todo
		if todo.Points != nil {
			max += *todo.Points
			anyPoints = true
		}
	}
	ids := make([]string, 0, len(raw))
	for id := range raw {
		ids = append(ids, id)
	}
	sortStrings(ids)
	var results []GradingResult
	var points float64
	for _, id := range ids {
		var item struct {
			Passed *bool    `json:"passed"`
			Points *float64 `json:"points"`
			Log    *string  `json:"log"`
		}
		if err := json.Unmarshal(raw[id], &item); err != nil || item.Passed == nil {
			return nil, 0, nil, fmt.Errorf("the result of %q is not {\"passed\": true or false, \"points\": a number, \"log\": text}", truncate(id, 40))
		}
		todo, known := byID[id]
		result := GradingResult{TodoID: id, Known: known, Passed: *item.Passed}
		switch {
		case item.Points != nil:
			if math.IsNaN(*item.Points) || math.IsInf(*item.Points, 0) || *item.Points < 0 {
				return nil, 0, nil, fmt.Errorf("the points of %q are not a number that is zero or more", truncate(id, 40))
			}
			result.Points = *item.Points
		case result.Passed && known && todo.Points != nil:
			result.Points = *todo.Points
		}
		if item.Log != nil {
			result.Log = *item.Log
		}
		if known && todo.Points != nil && result.Points > *todo.Points {
			result.Log = strings.TrimRight(result.Log, "\n") + fmt.Sprintf("\n(the points were %g; this TODO is worth %g, so %g are counted)", result.Points, *todo.Points, *todo.Points)
			result.Points = *todo.Points
		}
		result.Log = cutLog(result.Log)
		if known {
			points += result.Points
		}
		results = append(results, result)
	}
	if !anyPoints {
		return results, points, nil, nil
	}
	return results, points, &max, nil
}

// cutLog keeps the first maxResultLog bytes of a TODO's log, whole characters, and says it cut.
func cutLog(log string) string {
	if len(log) <= maxResultLog {
		return log
	}
	cut := log[:maxResultLog]
	for !utf8.ValidString(cut) && len(cut) > 0 {
		cut = cut[:len(cut)-1]
	}
	return cut + "\n… (the log was cut here)"
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// Confirm makes a finished grading the student's grade, as the proctor says it stands: any other confirmation of the same student is
// taken back. Until then every grade is advisory.
func (g *Grader) Confirm(ctx context.Context, id int64) error {
	record, err := g.m.opts.Repo.Grading(ctx, id)
	if err != nil {
		return err
	}
	if err := g.m.opts.Repo.ConfirmGrading(ctx, id, g.m.opts.Now().UTC()); err != nil {
		return err
	}
	g.m.event(record.SessionID, EventGradingConfirmed, map[string]any{"grading": id, "points": record.Points, "max": record.Max})
	return nil
}

// Unconfirm takes a confirmation back: the grade is advisory again.
func (g *Grader) Unconfirm(ctx context.Context, id int64) error {
	record, err := g.m.opts.Repo.Grading(ctx, id)
	if err != nil {
		return err
	}
	if err := g.m.opts.Repo.UnconfirmGrading(ctx, id); err != nil {
		return err
	}
	g.m.event(record.SessionID, EventGradingUnconfirmed, map[string]any{"grading": id})
	return nil
}
