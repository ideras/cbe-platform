package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"exam-platform/internal/domain"
	"exam-platform/internal/service"
	"exam-platform/internal/storage"
)

func answerFixture(t *testing.T) *fixture {
	t.Helper()
	f := newFixture(t, 0)
	setContent(t, f, domain.FreeNavigation, examContent([]string{"q1", "q2"}))
	return f
}

func finalVersion(t *testing.T, f *fixture, token string) domain.Version {
	t.Helper()
	session := f.storedSession(t, token)
	if session.Status != domain.Submitted && session.Status != domain.ForceSubmitted {
		t.Fatal("session not terminal")
	}
	if session.FinalVersionID == nil || session.SubmittedAt == nil {
		t.Fatal("incomplete terminal state")
	}
	var version domain.Version
	if err := f.store.WithSessionTransaction(t.Context(), func(tx service.SessionTransaction) error {
		var err error
		version, err = tx.Version(t.Context(), session.ID, *session.FinalVersionID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return version
}

func TestSaveResumeSubmitAndImmutableHistory(t *testing.T) {
	f := answerFixture(t)
	login := f.login(t, "0012")
	view, err := f.sessions.GetExam(t.Context(), login.Token)
	if err != nil || view.Latest != nil || len(view.Answers) != 2 || view.Answers["q1"] != "" {
		t.Fatalf("no-save view = %+v, %v", view, err)
	}
	for i, text := range []string{"first 世界", "second 🧪"} {
		f.clock.Set(epoch.Add(time.Duration(i) * time.Second))
		saved, err := f.sessions.Autosave(t.Context(), login.Token, domain.Answers{"q1": text})
		if err != nil || saved.Sequence != int64(i+1) || !saved.SavedAt.Equal(f.clock.Now()) {
			t.Fatalf("save = %+v, %v", saved, err)
		}
	}
	resume := f.login(t, "0012")
	view, err = f.sessions.GetExam(t.Context(), resume.Token)
	if err != nil || view.Latest.Sequence != 2 || view.Answers["q1"] != "second 🧪" || view.Answers["q2"] != "" {
		t.Fatalf("resume = %+v, %v", view, err)
	}
	f.clock.Set(epoch.Add(time.Minute))
	saved, err := f.sessions.Autosave(t.Context(), login.Token, domain.Answers{"q2": "grace save"})
	if err != nil || saved.Sequence != 3 || f.storedSession(t, login.Token).Status != domain.Grace {
		t.Fatalf("grace autosave = %+v, %v", saved, err)
	}
	view, err = f.sessions.GetExam(t.Context(), login.Token)
	if err != nil || view.Answers["q1"] != "" {
		t.Fatal("full snapshot incorrectly merged earlier answers")
	}
	submitted, err := f.sessions.Submit(t.Context(), login.Token, domain.Answers{"q1": "final", "q2": "done"})
	if err != nil || submitted.Sequence != 4 || submitted.Status != domain.Submitted {
		t.Fatalf("submit = %+v, %v", submitted, err)
	}
	f.clock.Set(epoch.Add(time.Hour))
	repeated, err := f.sessions.Submit(t.Context(), login.Token, domain.Answers{"not-owned": "ignore retry payload"})
	if err != nil || repeated != submitted {
		t.Fatalf("repeat = %+v, %v", repeated, err)
	}
	if _, err := f.sessions.Autosave(t.Context(), login.Token, domain.Answers{}); !errors.Is(err, service.ErrSessionLocked) {
		t.Fatalf("late save = %v", err)
	}
	if _, err := f.sessions.GetExam(t.Context(), login.Token); !errors.Is(err, service.ErrSessionLocked) {
		t.Fatalf("terminal exam = %v", err)
	}
	if f.count(t, `SELECT count(*) FROM submission_versions`) != 4 || f.count(t, `SELECT count(*) FROM event_log WHERE event_type='submitted'`) != 1 {
		t.Fatal("duplicate finalization or overwritten history")
	}
	var historical string
	if err := f.raw.QueryRow(`SELECT json_extract(answers_json, '$.q1') FROM submission_versions WHERE version_seq=1`).Scan(&historical); err != nil {
		t.Fatal(err)
	}
	if historical != "first 世界" || finalVersion(t, f, login.Token).Answers["q1"] != "final" {
		t.Fatal("history/final answer changed")
	}
}

func TestExactExpirySelectsLatestOrEmpty(t *testing.T) {
	for _, saves := range []int{0, 2} {
		t.Run(fmt.Sprint(saves), func(t *testing.T) {
			f := answerFixture(t)
			login := f.login(t, "0012")
			for i := range saves {
				if _, err := f.sessions.Autosave(t.Context(), login.Token, domain.Answers{"q1": fmt.Sprint(i)}); err != nil {
					t.Fatal(err)
				}
			}
			f.clock.Set(epoch.Add(90 * time.Second))
			if _, err := f.sessions.Autosave(t.Context(), login.Token, domain.Answers{"q1": "late"}); !errors.Is(err, service.ErrSessionLocked) {
				t.Fatalf("expiry save = %v", err)
			}
			version := finalVersion(t, f, login.Token)
			expectedRows := saves
			if expectedRows == 0 {
				expectedRows = 1
			}
			if f.count(t, `SELECT count(*) FROM submission_versions`) != expectedRows {
				t.Fatal("expiry copied or omitted a version")
			}
			if saves == 0 && len(version.Answers) != 0 {
				t.Fatal("no-save expiry was not explicitly empty")
			}
			if saves > 0 && version.Answers["q1"] != "1" {
				t.Fatal("expiry ignored latest committed work")
			}
			repeat, err := f.sessions.Submit(t.Context(), login.Token, domain.Answers{"q1": "late submit"})
			if err != nil || repeat.FinalVersionID != version.ID {
				t.Fatalf("expiry repeat = %+v, %v", repeat, err)
			}
			if f.count(t, `SELECT count(*) FROM event_log WHERE event_type='submitted' AND json_extract(detail,'$.reason')='grace_expired'`) != 1 {
				t.Fatal("expiry event missing/duplicated")
			}
		})
	}
}

func TestParallelAutosavesUseCommitSequence(t *testing.T) {
	f := answerFixture(t)
	login := f.login(t, "0012")
	const n = 32
	type outcome struct {
		save service.SaveResult
		text string
		err  error
	}
	results := make(chan outcome, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			text := fmt.Sprint(i)
			save, err := f.sessions.Autosave(t.Context(), login.Token, domain.Answers{"q1": text})
			results <- outcome{save, text, err}
		})
	}
	wg.Wait()
	close(results)
	seen := map[int64]bool{}
	for result := range results {
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.save.Sequence < 1 || result.save.Sequence > n || seen[result.save.Sequence] {
			t.Fatal("sequence collision")
		}
		seen[result.save.Sequence] = true
		var text string
		if err := f.raw.QueryRow(`SELECT json_extract(answers_json,'$.q1') FROM submission_versions WHERE version_seq=?`, result.save.Sequence).Scan(&text); err != nil {
			t.Fatal(err)
		}
		if text != result.text {
			t.Fatal("committed feedback did not identify the saved snapshot")
		}
	}
	if f.count(t, `SELECT count(*) FROM submission_versions`) != n {
		t.Fatal("save lost")
	}
}

func TestSaveSubmitAndDuplicateSubmitRace(t *testing.T) {
	f := answerFixture(t)
	login := f.login(t, "0012")
	const n = 32
	var wg sync.WaitGroup
	errs := make(chan error, 2*n)
	submissions := make(chan service.SubmitResult, n)
	saved := make(chan service.SaveResult, n)
	for i := range n {
		wg.Go(func() {
			result, err := f.sessions.Submit(t.Context(), login.Token, domain.Answers{"q1": "submit-" + fmt.Sprint(i)})
			errs <- err
			if err == nil {
				submissions <- result
			}
		})
		wg.Go(func() {
			result, err := f.sessions.Autosave(t.Context(), login.Token, domain.Answers{"q1": "save-" + fmt.Sprint(i)})
			if err != nil && !errors.Is(err, service.ErrSessionLocked) {
				errs <- err
			}
			if err == nil {
				saved <- result
			}
		})
	}
	wg.Wait()
	close(errs)
	close(submissions)
	close(saved)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var first service.SubmitResult
	for result := range submissions {
		if first.FinalVersionID == 0 {
			first = result
		}
		if result != first {
			t.Fatal("duplicate submits chose different finals")
		}
	}
	if first.FinalVersionID == 0 || f.count(t, `SELECT count(*) FROM submission_versions`) != len(saved)+1 {
		t.Fatal("extra or missing submit version")
	}
	if f.count(t, `SELECT count(*) FROM event_log WHERE event_type='submitted'`) != 1 {
		t.Fatal("multiple terminal transitions")
	}
	if finalVersion(t, f, login.Token).Sequence != first.Sequence {
		t.Fatal("final reference not authoritative")
	}
}

func TestSaveAndSubmitRaceExpiry(t *testing.T) {
	for _, submit := range []bool{false, true} {
		for iteration := range 12 {
			t.Run(fmt.Sprintf("submit=%t/%d", submit, iteration), func(t *testing.T) {
				f := answerFixture(t)
				login := f.login(t, "0012")
				if _, err := f.sessions.Autosave(t.Context(), login.Token, domain.Answers{"q1": "original"}); err != nil {
					t.Fatal(err)
				}
				f.clock.Set(epoch.Add(90*time.Second - time.Nanosecond))
				start := make(chan struct{})
				errs := make(chan error, 2)
				go func() {
					<-start
					var err error
					if submit {
						_, err = f.sessions.Submit(t.Context(), login.Token, domain.Answers{"q1": "racing"})
					} else {
						_, err = f.sessions.Autosave(t.Context(), login.Token, domain.Answers{"q1": "racing"})
					}
					if errors.Is(err, service.ErrSessionLocked) {
						err = nil
					}
					errs <- err
				}()
				go func() {
					<-start
					f.clock.Set(epoch.Add(90 * time.Second))
					_, err := f.sessions.Reconcile(t.Context())
					errs <- err
				}()
				close(start)
				for range 2 {
					if err := <-errs; err != nil {
						t.Fatal(err)
					}
				}
				version := finalVersion(t, f, login.Token)
				latest, err := f.store.LatestVersion(t.Context(), login.SessionID)
				if err != nil || latest.ID != version.ID || f.count(t, `SELECT count(*) FROM event_log WHERE event_type='submitted'`) != 1 {
					t.Fatal("expiry race changed final version")
				}
				if _, err := f.sessions.Autosave(t.Context(), login.Token, domain.Answers{}); !errors.Is(err, service.ErrSessionLocked) {
					t.Fatalf("post-race save = %v", err)
				}
			})
		}
	}
}

type failingAnswerTx struct {
	service.SessionTransaction
	operation string
}

func (tx failingAnswerTx) AppendVersion(ctx context.Context, id string, answers domain.Answers, now time.Time) (domain.Version, error) {
	version, err := tx.SessionTransaction.AppendVersion(ctx, id, answers, now)
	if err == nil && tx.operation == "append" {
		err = errors.New("injected append failure")
	}
	return version, err
}
func (tx failingAnswerTx) Finalize(ctx context.Context, id string, version int64, status domain.SessionStatus, now time.Time) error {
	if err := tx.SessionTransaction.Finalize(ctx, id, version, status, now); err != nil {
		return err
	}
	if tx.operation == "finalize" {
		return errors.New("injected finalize failure")
	}
	return nil
}
func (tx failingAnswerTx) AddEvent(ctx context.Context, event domain.Event) error {
	if err := tx.SessionTransaction.AddEvent(ctx, event); err != nil {
		return err
	}
	if tx.operation == "event" && event.Type == "submitted" {
		return errors.New("injected submission event failure")
	}
	return nil
}
func (tx failingAnswerTx) LatestVersion(ctx context.Context, id string) (domain.Version, error) {
	version, err := tx.SessionTransaction.LatestVersion(ctx, id)
	if err == nil && tx.operation == "ownership" {
		version.SessionID = "another-session"
	}
	return version, err
}

func TestAnswerFinalizationFailuresRollBackTogether(t *testing.T) {
	for _, operation := range []string{"append", "finalize", "event", "ownership"} {
		for _, expiry := range []bool{false, true} {
			if operation == "ownership" && !expiry {
				continue
			}
			t.Run(fmt.Sprintf("%s/expiry=%t", operation, expiry), func(t *testing.T) {
				f := answerFixture(t)
				login := f.login(t, "0012")
				if operation == "ownership" {
					if _, err := f.sessions.Autosave(t.Context(), login.Token, domain.Answers{"q1": "valid"}); err != nil {
						t.Fatal(err)
					}
				}
				beforeRows := f.count(t, `SELECT count(*) FROM submission_versions`)
				repo := wrappedRepository{f.store, func(tx service.SessionTransaction) service.SessionTransaction { return failingAnswerTx{tx, operation} }}
				failing := testSessions(repo, f.clock)
				var err error
				if expiry {
					f.clock.Set(epoch.Add(90 * time.Second))
					_, err = failing.Reconcile(t.Context())
				} else {
					_, err = failing.Submit(t.Context(), login.Token, domain.Answers{"q1": "rollback"})
				}
				if err == nil {
					t.Fatal("storage failure not propagated")
				}
				stored := f.storedSession(t, login.Token)
				if stored.Status != domain.Active || stored.FinalVersionID != nil || stored.SubmittedAt != nil || f.count(t, `SELECT count(*) FROM submission_versions`) != beforeRows || f.count(t, `SELECT count(*) FROM event_log`) != 1 {
					t.Fatal("partial finalization survived rollback")
				}
			})
		}
	}
}

func TestAnswerValidationLimitsAndNoPartialWrites(t *testing.T) {
	f := answerFixture(t)
	login := f.login(t, "0012")
	limited, err := service.NewSessionsWithLimits(f.store, f.clock, service.AnswerLimits{PerAnswerBytes: 4, TotalBytes: 6})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		answers domain.Answers
		err     error
	}{
		{nil, service.ErrInvalidInput}, {domain.Answers{"other": "x"}, service.ErrInvalidInput},
		{domain.Answers{"q1": "12345"}, service.ErrAnswerTooLarge}, {domain.Answers{"q1": "1234", "q2": "123"}, service.ErrAnswerTooLarge},
		{domain.Answers{"q1": string([]byte{255})}, service.ErrInvalidInput},
	} {
		if _, err := limited.Autosave(t.Context(), login.Token, test.answers); !errors.Is(err, test.err) {
			t.Fatalf("validation = %v", err)
		}
		if _, err := limited.Submit(t.Context(), login.Token, test.answers); !errors.Is(err, test.err) {
			t.Fatalf("submit validation = %v", err)
		}
	}
	if f.count(t, `SELECT count(*) FROM submission_versions`) != 0 || f.storedSession(t, login.Token).Status != domain.Active {
		t.Fatal("invalid input wrote partial answers")
	}
	if _, err := limited.Autosave(t.Context(), login.Token, domain.Answers{"q1": "🧪", "q2": "12"}); err != nil {
		t.Fatal("exact UTF-8 limit rejected:", err)
	}
	if _, err := service.NewSessionsWithLimits(f.store, f.clock, service.AnswerLimits{}); err == nil {
		t.Fatal("invalid limits accepted")
	}
}

func TestForceSubmitSeamAndExpiredReason(t *testing.T) {
	for _, expired := range []bool{false, true} {
		f := answerFixture(t)
		login := f.login(t, "0012")
		if _, err := f.sessions.ForceSubmit(t.Context(), login.SessionID, ""); !errors.Is(err, service.ErrInvalidInput) {
			t.Fatal("empty operator reason allowed")
		}
		if expired {
			f.clock.Set(epoch.Add(90 * time.Second))
		}
		result, err := f.sessions.ForceSubmit(t.Context(), login.SessionID, "operator intervention")
		if err != nil {
			t.Fatal(err)
		}
		expected := domain.ForceSubmitted
		if expired {
			expected = domain.Submitted
		}
		if result.Status != expected || len(finalVersion(t, f, login.Token).Answers) != 0 {
			t.Fatal("incorrect force/expiry selection")
		}
		var detail string
		if err := f.raw.QueryRow(`SELECT detail FROM event_log WHERE event_type=?`, string(expected)).Scan(&detail); err != nil {
			t.Fatal(err)
		}
		var cause struct {
			Reason   string `json:"reason"`
			Operator string `json:"operator_reason"`
		}
		if err := json.Unmarshal([]byte(detail), &cause); err != nil {
			t.Fatal(err)
		}
		if expired && (cause.Reason != "grace_expired" || cause.Operator != "") {
			t.Fatal("late force relabeled expiry")
		}
		if !expired && (cause.Reason != "force_submit" || cause.Operator == "") {
			t.Fatal("operator reason missing")
		}
		repeat, err := f.sessions.ForceSubmit(t.Context(), login.SessionID, "different reason")
		if err != nil || repeat != result {
			t.Fatal("force replaced final state")
		}
	}
}

func TestRestartPreservesSnapshotsAndFinalReference(t *testing.T) {
	f := answerFixture(t)
	login := f.login(t, "0012")
	for _, text := range []string{"one", "two"} {
		if _, err := f.sessions.Autosave(t.Context(), login.Token, domain.Answers{"q1": text}); err != nil {
			t.Fatal(err)
		}
	}
	reopen := func() {
		t.Helper()
		if err := f.store.Close(); err != nil {
			t.Fatal(err)
		}
		var err error
		f.store, err = storage.Open(t.Context(), f.path)
		if err != nil {
			t.Fatal(err)
		}
		f.sessions = testSessions(f.store, f.clock)
	}
	reopen()
	view, err := f.sessions.GetExam(t.Context(), login.Token)
	if err != nil || view.Latest.Sequence != 2 || view.Answers["q1"] != "two" {
		t.Fatal("restart lost latest snapshot")
	}
	submitted, err := f.sessions.Submit(t.Context(), login.Token, domain.Answers{"q2": "final"})
	if err != nil {
		t.Fatal(err)
	}
	reopen()
	repeated, err := f.sessions.Submit(t.Context(), login.Token, domain.Answers{"q1": "replacement"})
	if err != nil || repeated != submitted || finalVersion(t, f, login.Token).Answers["q2"] != "final" {
		t.Fatal("restart lost authoritative final reference")
	}
	if f.count(t, `SELECT count(*) FROM submission_versions`) != 3 {
		t.Fatal("restart discarded history")
	}
}
