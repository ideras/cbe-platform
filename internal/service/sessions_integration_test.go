package service_test

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"path/filepath"
	"regexp"
	"sync"
	"testing"
	"time"

	"exam-platform/internal/domain"
	"exam-platform/internal/service"
	"exam-platform/internal/storage"
)

const testExamID = "00000000-0000-4000-8000-000000000001"
const testClassID = "00000000-0000-4000-8000-0000000000c1"

var epoch = time.Date(2026, 1, 2, 3, 4, 5, 123, time.UTC)

type mutableClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *mutableClock) Now() time.Time    { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *mutableClock) Set(now time.Time) { c.mu.Lock(); defer c.mu.Unlock(); c.now = now }

type fixture struct {
	store    *storage.Store
	raw      *sql.DB
	clock    *mutableClock
	sessions *service.Sessions
	path     string
}

// newFixture has the test exam published, as after the proctor clicked Publish.
func newFixture(t *testing.T, grace int64) *fixture {
	t.Helper()
	f := newUnpublishedFixture(t, grace)
	f.publish(t, testExamID)
	return f
}

// publish admits the students of the exam's class to it, replacing whatever was published, through the same path the
// dashboard uses.
func (f *fixture) publish(t *testing.T, id string) {
	t.Helper()
	publisher := service.NewPublisher(f.store, f.clock)
	catalog, err := publisher.Catalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := publisher.Publish(t.Context(), id, catalog.CurrentExamID, true); err != nil {
		t.Fatal(err)
	}
}

// addClass stores another class with the given students.
func (f *fixture) addClass(t *testing.T, id, name string, students ...domain.Student) {
	t.Helper()
	if err := f.store.CreateClass(t.Context(), domain.Class{ID: id, Name: name, CreatedAt: epoch}, students); err != nil {
		t.Fatal(err)
	}
}

// newUnpublishedFixture stores the test exam but admits nobody.
func newUnpublishedFixture(t *testing.T, grace int64) *fixture {
	t.Helper()
	f := &fixture{path: filepath.Join(t.TempDir(), "exam.db"), clock: &mutableClock{now: epoch}}
	var err error
	f.store, err = storage.Open(t.Context(), f.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.store.Close() })
	f.raw, err = sql.Open("sqlite", f.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.raw.Close() })
	// The monitoring settings are stored before the exam is published, so the exam freezes them.
	storeSettings(f.store, f.clock, testPolicy())
	f.addClass(t, testClassID, "Test class", domain.Student{AccountNumber: "0012", Name: "Ada"}, domain.Student{AccountNumber: "0034", Name: "Grace"})
	if grace == 0 {
		_, err = f.raw.Exec(`INSERT INTO exams(exam_id, class_id, title, pin, duration_seconds, content_json, created_at) VALUES (?, ?, 'Test', 'exam-secret', 60, jsonb('{"exercises":[]}'), ?)`, testExamID, testClassID, epoch.Format(time.RFC3339Nano))
	} else {
		err = f.store.CreateExam(t.Context(), domain.Exam{ID: testExamID, ClassID: testClassID, Title: "Test", PIN: "exam-secret", DurationSeconds: 60, GraceSeconds: grace, CreatedAt: epoch})
	}
	if err != nil {
		t.Fatal(err)
	}
	f.sessions = testSessions(f.store, f.clock)
	return f
}

func (f *fixture) login(t *testing.T, account string) service.LoginResult {
	t.Helper()
	result, err := f.sessions.Login(t.Context(), account, "exam-secret")
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func (f *fixture) storedSession(t *testing.T, token string) domain.Session {
	t.Helper()
	var session domain.Session
	err := f.store.WithSessionTransaction(t.Context(), func(tx service.SessionTransaction) error {
		var err error
		session, err = tx.SessionByToken(t.Context(), token)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func (f *fixture) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := f.raw.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestLoginResumeAndRandomCredentials(t *testing.T) {
	f := newFixture(t, 0)
	first := f.login(t, " 0012 ")
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(first.SessionID) {
		t.Fatalf("not a UUIDv4: %q", first.SessionID)
	}
	token, err := base64.RawURLEncoding.DecodeString(first.Token)
	if err != nil || len(token) != 32 {
		t.Fatalf("token entropy bytes = %d, %v", len(token), err)
	}
	stored := f.storedSession(t, first.Token)
	if stored.AccountNumber != "0012" || !stored.StartedAt.Equal(epoch) || !stored.DeadlineAt.Equal(epoch.Add(time.Minute)) || !stored.GraceUntil.Equal(epoch.Add(90*time.Second)) {
		t.Fatalf("boundaries = %+v", stored)
	}
	f.clock.Set(epoch.Add(27 * time.Second))
	resume := f.login(t, "0012")
	if resume.SessionID != first.SessionID || resume.Token != first.Token || !resume.DeadlineAt.Equal(first.DeadlineAt) || !resume.ServerTime.Equal(f.clock.Now()) {
		t.Fatal("resume changed identity or deadline")
	}
	other := f.login(t, "0034")
	if other.Token == first.Token || other.SessionID == first.SessionID {
		t.Fatal("credentials reused across students")
	}
	var firstDetail, secondDetail string
	if err := f.raw.QueryRow(`SELECT detail FROM event_log WHERE session_id = ? ORDER BY id LIMIT 1`, first.SessionID).Scan(&firstDetail); err != nil {
		t.Fatal(err)
	}
	if err := f.raw.QueryRow(`SELECT detail FROM event_log WHERE session_id = ? ORDER BY id DESC LIMIT 1`, first.SessionID).Scan(&secondDetail); err != nil {
		t.Fatal(err)
	}
	if firstDetail != `{"resumed":false}` || secondDetail != `{"resumed":true}` {
		t.Fatalf("login details = %s / %s", firstDetail, secondDetail)
	}
}

func TestConcurrentFirstLogins(t *testing.T) {
	f := newFixture(t, 7)
	const clients = 64
	results := make(chan service.LoginResult, clients)
	errors := make(chan error, clients)
	var wg sync.WaitGroup
	for range clients {
		wg.Go(func() {
			result, err := f.sessions.Login(t.Context(), "0012", "exam-secret")
			results <- result
			errors <- err
		})
	}
	wg.Wait()
	close(results)
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	var first service.LoginResult
	for result := range results {
		if first.Token == "" {
			first = result
		}
		if result != first {
			t.Fatal("concurrent logins created different identities or deadlines")
		}
	}
	if f.count(t, "SELECT count(*) FROM sessions") != 1 || f.count(t, "SELECT count(*) FROM event_log WHERE event_type = 'login'") != clients {
		t.Fatal("incorrect session or login event count")
	}
	if !f.storedSession(t, first.Token).GraceUntil.Equal(epoch.Add(67 * time.Second)) {
		t.Fatal("non-default grace ignored")
	}
}

func TestCredentialFailuresAndNoCurrentExam(t *testing.T) {
	f := newFixture(t, 0)
	for _, input := range [][2]string{{"missing", "exam-secret"}, {"0012", "wrong"}, {"", ""}} {
		if _, err := f.sessions.Login(t.Context(), input[0], input[1]); !errors.Is(err, service.ErrInvalidCredentials) {
			t.Fatalf("error = %v", err)
		}
	}
	// Only the students of the exam's class can log in, even with the right PIN.
	f.addClass(t, "00000000-0000-4000-8000-0000000000c2", "Another class", domain.Student{AccountNumber: "0056", Name: "Alan"})
	if _, err := f.sessions.Login(t.Context(), "0056", "exam-secret"); !errors.Is(err, service.ErrInvalidCredentials) {
		t.Fatalf("student outside the exam's class: %v", err)
	}
	unpublished := newUnpublishedFixture(t, 0)
	if _, err := unpublished.sessions.Login(t.Context(), "missing", "wrong"); !errors.Is(err, service.ErrNoCurrentExam) {
		t.Fatalf("nothing published: %v", err)
	}
	if f.count(t, "SELECT count(*) FROM sessions") != 0 || f.count(t, "SELECT count(*) FROM event_log") != 0 {
		t.Fatal("failed credentials changed persistence")
	}
}

func TestAdmissionWindows(t *testing.T) {
	future, past, now := epoch.Add(time.Second), epoch.Add(-time.Second), epoch
	for _, test := range []struct {
		name          string
		opens, closes *time.Time
		denied        bool
	}{
		{"no gates", nil, nil, false}, {"opens now", &now, nil, false}, {"opens later", &future, nil, true},
		{"closed", nil, &past, true}, {"closes now", nil, &now, true}, {"closes later", nil, &future, false},
		{"inside window", &past, &future, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture(t, 0)
			_, err := f.raw.Exec(`UPDATE exams SET opens_at = ?, closes_at = ?`, optionalTime(test.opens), optionalTime(test.closes))
			if err != nil {
				t.Fatal(err)
			}
			_, err = f.sessions.Login(t.Context(), "0012", "exam-secret")
			if test.denied && !errors.Is(err, service.ErrAdmissionClosed) {
				t.Fatalf("admission error = %v", err)
			}
			if !test.denied && err != nil {
				t.Fatal(err)
			}
			if test.denied {
				if f.count(t, "SELECT count(*) FROM sessions") != 0 {
					t.Fatal("closed admission created session")
				}
				for _, account := range []string{"missing", "0012"} {
					if _, err := f.sessions.Login(t.Context(), account, "wrong"); !errors.Is(err, service.ErrInvalidCredentials) {
						t.Fatalf("closed gate exposed credential distinction: %v", err)
					}
				}
			}
		})
	}
}

func optionalTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func TestResumeAfterAdmissionClosesAndExpiry(t *testing.T) {
	f := newFixture(t, 0)
	if _, err := f.raw.Exec(`UPDATE exams SET closes_at = ?`, epoch.Add(10*time.Second).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	first := f.login(t, "0012")
	for _, elapsed := range []time.Duration{20 * time.Second, 60 * time.Second, 89 * time.Second} {
		f.clock.Set(epoch.Add(elapsed))
		resume := f.login(t, "0012")
		if resume.Token != first.Token || !resume.DeadlineAt.Equal(first.DeadlineAt) {
			t.Fatal("admission gate changed resume boundaries")
		}
	}
	f.clock.Set(epoch.Add(90 * time.Second))
	if _, err := f.sessions.Login(t.Context(), "0012", "exam-secret"); !errors.Is(err, service.ErrAlreadySubmitted) {
		t.Fatalf("expired login = %v", err)
	}
	principal, err := f.sessions.Authenticate(t.Context(), first.Token)
	if err != nil || principal.Lifecycle.Status != domain.Submitted || principal.Lifecycle.FinalizationPending {
		t.Fatalf("expired state = %+v, %v", principal.Lifecycle, err)
	}
	stored := f.storedSession(t, first.Token)
	if stored.Status != domain.Submitted || stored.FinalVersionID == nil || stored.SubmittedAt == nil {
		t.Fatal("expiry did not persist a complete final answer")
	}
	if f.count(t, `SELECT count(*) FROM event_log WHERE event_type = 'grace_started'`) != 1 || f.count(t, `SELECT count(*) FROM event_log WHERE event_type = 'login'`) != 4 {
		t.Fatal("duplicated transition or failed login event")
	}
}

func TestRestartCatchup(t *testing.T) {
	for _, elapsed := range []time.Duration{63 * time.Second, 90 * time.Second, 100 * time.Second} {
		t.Run(elapsed.String(), func(t *testing.T) {
			f := newFixture(t, 0)
			first := f.login(t, "0012")
			f.clock.Set(epoch.Add(elapsed))
			if err := f.store.Close(); err != nil {
				t.Fatal(err)
			}
			var err error
			f.store, err = storage.Open(t.Context(), f.path)
			if err != nil {
				t.Fatal(err)
			}
			f.sessions = testSessions(f.store, f.clock)
			report, err := f.sessions.Reconcile(t.Context())
			if err != nil || report.GraceTransitions != 1 {
				t.Fatalf("catchup = %+v, %v", report, err)
			}
			principal, err := f.sessions.Authenticate(t.Context(), first.Token)
			if err != nil {
				t.Fatal(err)
			}
			expected := domain.Grace
			if elapsed >= 90*time.Second {
				expected = domain.Submitted
			}
			if principal.Lifecycle.Status != expected || !principal.Session.DeadlineAt.Equal(first.DeadlineAt) || !principal.Session.StartedAt.Equal(epoch) {
				t.Fatal("restart changed timing")
			}
			report, err = f.sessions.Reconcile(t.Context())
			if err != nil || report.GraceTransitions != 0 {
				t.Fatalf("duplicate catchup = %+v, %v", report, err)
			}
			if f.count(t, `SELECT count(*) FROM event_log WHERE event_type = 'grace_started'`) != 1 {
				t.Fatal("restart duplicated transition")
			}
		})
	}
}

func TestPersistedTerminalSessionsStayTerminal(t *testing.T) {
	for _, status := range []domain.SessionStatus{domain.Submitted, domain.ForceSubmitted} {
		t.Run(string(status), func(t *testing.T) {
			f := newFixture(t, 0)
			first := f.login(t, "0012")
			result, err := f.raw.Exec(`INSERT INTO submission_versions(session_id, version_seq, answers_json, saved_at) VALUES (?, 1, jsonb('{}'), ?)`, first.SessionID, epoch.Format(time.RFC3339Nano))
			if err != nil {
				t.Fatal(err)
			}
			id, err := result.LastInsertId()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.raw.Exec(`UPDATE sessions SET status = ?, submitted_at = ?, final_version_id = ? WHERE session_id = ?`, status, epoch.Format(time.RFC3339Nano), id, first.SessionID); err != nil {
				t.Fatal(err)
			}
			for _, elapsed := range []time.Duration{time.Second, time.Hour} {
				f.clock.Set(epoch.Add(elapsed))
				principal, err := f.sessions.Authenticate(t.Context(), first.Token)
				if err != nil || principal.Lifecycle.Status != status || principal.Lifecycle.FinalizationPending {
					t.Fatalf("final state = %+v, %v", principal.Lifecycle, err)
				}
				if _, err := f.sessions.Login(t.Context(), "0012", "exam-secret"); !errors.Is(err, service.ErrAlreadySubmitted) {
					t.Fatalf("terminal login = %v", err)
				}
				if _, err := f.sessions.Login(t.Context(), "0012", "wrong"); !errors.Is(err, service.ErrInvalidCredentials) {
					t.Fatalf("terminal credential leak = %v", err)
				}
			}
			if report, err := f.sessions.Reconcile(t.Context()); err != nil || report != (service.ReconcileReport{}) {
				t.Fatalf("terminal reconciliation = %+v, %v", report, err)
			}
			if f.count(t, `SELECT count(*) FROM event_log`) != 1 {
				t.Fatal("terminal login added successful event")
			}
		})
	}
}

type wrappedRepository struct {
	store *storage.Store
	wrap  func(service.SessionTransaction) service.SessionTransaction
}

func (r wrappedRepository) WithSessionTransaction(ctx context.Context, fn func(service.SessionTransaction) error) error {
	return r.store.WithSessionTransaction(ctx, func(tx service.SessionTransaction) error { return fn(r.wrap(tx)) })
}

type failEventTx struct{ service.SessionTransaction }

func (failEventTx) AddEvent(context.Context, domain.Event) error {
	return errors.New("event write failed")
}

func TestEventFailureRollsBackSessionAndGrace(t *testing.T) {
	f := newFixture(t, 0)
	failing := testSessions(wrappedRepository{f.store, func(tx service.SessionTransaction) service.SessionTransaction { return failEventTx{tx} }}, f.clock)
	if _, err := failing.Login(t.Context(), "0012", "exam-secret"); err == nil {
		t.Fatal("expected event failure")
	}
	if f.count(t, "SELECT count(*) FROM sessions") != 0 {
		t.Fatal("session insert survived failed login event")
	}
	first := f.login(t, "0012")
	f.clock.Set(epoch.Add(time.Minute))
	if _, err := failing.Authenticate(t.Context(), first.Token); err == nil {
		t.Fatal("expected grace event failure")
	}
	if f.storedSession(t, first.Token).Status != domain.Active {
		t.Fatal("grace transition survived failed event")
	}
}

func TestClockReadInsideTransaction(t *testing.T) {
	f := newFixture(t, 0)
	afterLock := epoch.Add(time.Hour)
	repository := wrappedRepository{f.store, func(tx service.SessionTransaction) service.SessionTransaction { f.clock.Set(afterLock); return tx }}
	sessions := testSessions(repository, f.clock)
	result, err := sessions.Login(t.Context(), "0012", "exam-secret")
	if err != nil {
		t.Fatal(err)
	}
	if !result.ServerTime.Equal(afterLock) || !result.DeadlineAt.Equal(afterLock.Add(time.Minute)) {
		t.Fatal("clock sampled before transaction")
	}
	f.clock.Set(afterLock.Add(time.Minute))
	afterLock = afterLock.Add(90 * time.Second)
	principal, err := sessions.Authenticate(t.Context(), result.Token)
	if err != nil || principal.Lifecycle.Status != domain.Submitted || principal.Lifecycle.FinalizationPending {
		t.Fatalf("late request granted time: %+v, %v", principal.Lifecycle, err)
	}
}

func TestWorkerImmediatePeriodicAndCancellation(t *testing.T) {
	f := newFixture(t, 0)
	f.login(t, "0012")
	f.clock.Set(epoch.Add(time.Minute))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	reports := make(chan service.ReconcileReport, 16)
	done := make(chan error, 1)
	go func() {
		done <- f.sessions.RunWorker(ctx, 5*time.Millisecond, func(report service.ReconcileReport) {
			select {
			case reports <- report:
			case <-ctx.Done():
			}
		})
	}()
	select {
	case report := <-reports:
		if report.GraceTransitions != 1 || report.Finalizations != 0 {
			t.Fatalf("initial report = %+v", report)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not catch up immediately")
	}
	f.clock.Set(epoch.Add(90 * time.Second))
	deadline := time.After(3 * time.Second)
	for {
		select {
		case report := <-reports:
			if report.Finalizations == 1 {
				cancel()
				select {
				case err := <-done:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(time.Second):
					t.Fatal("worker did not stop")
				}
				return
			}
		case <-deadline:
			t.Fatal("worker did not reconcile periodic expiry")
		}
	}
}

func TestWorkerRejectsInvalidIntervalAndPropagatesFailure(t *testing.T) {
	f := newFixture(t, 0)
	if err := f.sessions.RunWorker(t.Context(), 0, nil); err == nil {
		t.Fatal("zero interval accepted")
	}
	failing := testSessions(wrappedRepository{f.store, func(tx service.SessionTransaction) service.SessionTransaction { return failEventTx{tx} }}, f.clock)
	f.login(t, "0012")
	f.clock.Set(epoch.Add(time.Minute))
	if err := failing.RunWorker(t.Context(), time.Second, nil); err == nil {
		t.Fatal("worker swallowed storage error")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := f.sessions.RunWorker(ctx, time.Second, nil); err != nil {
		t.Fatalf("cancelled worker = %v", err)
	}
}
