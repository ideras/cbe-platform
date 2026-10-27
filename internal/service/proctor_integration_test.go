package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"exam-platform/internal/domain"
	"exam-platform/internal/service"
)

func proctorList(t *testing.T, f *fixture) service.SessionList {
	t.Helper()
	result, err := f.sessions.ProctorSessions(t.Context(), testExamID)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func proctorTimeline(t *testing.T, f *fixture, id string, limit int, cursor string) service.TimelinePage {
	t.Helper()
	result, err := f.sessions.ProctorTimeline(t.Context(), id, limit, cursor)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func TestProctorListProjectionDoesNotMutateAndSeparatesWarnings(t *testing.T) {
	f := newFixture(t, 0)
	login := f.login(t, "0012")
	initial := proctorList(t, f)
	if len(initial.Sessions) != 1 || len(initial.StudentsWithoutSession) != 1 || initial.StudentsWithoutSession[0].AccountNumber != "0034" || initial.Sessions[0].Presence.Connectivity != "no_heartbeat" || initial.Sessions[0].NetworkWarning || initial.Sessions[0].Presence.Focus.State != domain.FocusUnknown {
		t.Fatalf("initial = %+v", initial)
	}
	heartbeat(t, f, login.Token)
	clientEvent(t, f, login.Token, "focus_lost")
	f.clock.Set(epoch.Add(15 * time.Second))
	heartbeat(t, f, login.Token)
	f.clock.Set(epoch.Add(46 * time.Second))
	for _, scan := range []bool{false, true} {
		if scan {
			if _, err := f.sessions.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
		}
		count := f.count(t, `SELECT count(*) FROM event_log`)
		for range 5 {
			result := proctorList(t, f)
			view := result.Sessions[0]
			if !view.NetworkWarning || !view.FocusWarning || view.Presence.Focus.State != domain.FocusUnknown || view.LastKnownUnfocusedMillis == nil || *view.LastKnownUnfocusedMillis != 45000 || view.LastFocusEvent == nil || view.LastFocusEvent.Type != "focus_lost" || view.SecondsRemaining != 14 || !result.ServerTime.Equal(f.clock.Now()) {
				t.Fatalf("warnings = %+v", view)
			}
			proctorTimeline(t, f, login.SessionID, 1, "")
		}
		if f.count(t, `SELECT count(*) FROM event_log`) != count {
			t.Fatal("reads created incidents")
		}
	}
	f.clock.Set(login.DeadlineAt)
	if view := proctorList(t, f).Sessions[0]; view.Status != domain.Grace || view.PersistedStatus != domain.Active || view.SecondsRemaining != 0 || view.FinalizationPending {
		t.Fatalf("grace projection = %+v", view)
	}
	f.clock.Set(login.DeadlineAt.Add(30 * time.Second))
	view := proctorList(t, f).Sessions[0]
	if view.Status != domain.Submitted || !view.FinalizationPending || view.Final != nil || view.PersistedStatus != domain.Active || view.Presence.Monitoring || view.NetworkWarning || view.FocusWarning {
		t.Fatalf("expiry projection = %+v", view)
	}
	if f.storedSession(t, login.Token).Status != domain.Active || f.count(t, `SELECT count(*) FROM submission_versions`) != 0 {
		t.Fatal("read finalized or reset a session")
	}
	if _, err := f.sessions.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	view = proctorList(t, f).Sessions[0]
	if view.FinalizationPending || view.Final == nil || view.Final.Reason != "grace_expired" || view.PersistedStatus != domain.Submitted {
		t.Fatalf("committed expiry = %+v", view)
	}
}

func TestProctorExamIsolationRosterAndCredentialProjection(t *testing.T) {
	f := newFixture(t, 0)
	login := f.login(t, "0012")
	otherID, otherClass := "00000000-0000-4000-8000-000000000002", "00000000-0000-4000-8000-0000000000c2"
	f.addClass(t, otherClass, "Other class", domain.Student{AccountNumber: "0034", Name: "Grace"})
	if err := f.store.CreateExam(t.Context(), domain.Exam{ID: otherID, ClassID: otherClass, Title: "Other", PIN: "other-secret", DurationSeconds: 60, GraceSeconds: 30, CreatedAt: epoch}); err != nil {
		t.Fatal(err)
	}
	f.publish(t, otherID)
	if _, err := f.sessions.Login(t.Context(), "0034", "other-secret"); err != nil {
		t.Fatal(err)
	}
	list := proctorList(t, f)
	if len(list.Sessions) != 1 || list.Sessions[0].ID != login.SessionID || len(list.StudentsWithoutSession) != 1 || list.StudentsWithoutSession[0].AccountNumber != "0034" {
		t.Fatal("exam sessions mixed")
	}
	// Nothing removes a student from a class yet; a record that fell out of it is hidden from the exam's own roster.
	if _, err := f.raw.Exec(`DELETE FROM class_students WHERE account_number = '0012'`); err != nil {
		t.Fatal(err)
	}
	list = proctorList(t, f)
	if list.Sessions[0].RosterActive || list.Sessions[0].Name != "Ada" {
		t.Fatal("inactive historical session hidden")
	}
	for _, value := range []any{list, proctorTimeline(t, f, login.SessionID, 100, "")} {
		body, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{login.Token, "exam-secret", "other-secret", `"pin"`, `"token"`, `"session_token"`} {
			if strings.Contains(string(body), secret) {
				t.Fatal("credential leaked in DTO")
			}
		}
	}
	if _, err := f.sessions.ProctorSessions(t.Context(), "00000000-0000-4000-8000-000000000099"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown exam = %v", err)
	}
	if _, err := f.sessions.ProctorTimeline(t.Context(), "00000000-0000-4000-8000-000000000099", 100, ""); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown session = %v", err)
	}
}

func TestTimelineNanosecondTiesPagingSnapshotAndScreenshotSlots(t *testing.T) {
	f := answerFixture(t)
	base := epoch.Truncate(time.Second)
	f.clock.Set(base)
	login := f.login(t, "0012")
	heartbeat(t, f, login.Token)
	loss := clientEvent(t, f, login.Token, "focus_lost")
	for _, nano := range []int64{0, 1, 9, 10, 100, 1000} {
		f.clock.Set(base.Add(time.Duration(nano)))
		if _, err := f.sessions.Autosave(t.Context(), login.Token, domain.Answers{"q1": fmt.Sprint(nano)}); err != nil {
			t.Fatal(err)
		}
		if err := f.store.WithSessionTransaction(t.Context(), func(tx service.SessionTransaction) error {
			return tx.AddEvent(t.Context(), domain.Event{SessionID: login.SessionID, Type: "marker", OccurredAt: f.clock.Now()})
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.raw.Exec(`INSERT INTO screenshots(session_id,event_log_id,file_path,captured_at) VALUES (?,?,?,?)`, login.SessionID, loss.EventLogID, "private/must-not-be-returned.png", base.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	all := proctorTimeline(t, f, login.SessionID, 200, "")
	seen := map[string]bool{}
	foundScreenshot := false
	for i, item := range all.Items {
		if seen[item.ID] {
			t.Fatal("ambiguous timeline identity")
		}
		seen[item.ID] = true
		if i > 0 {
			prev := all.Items[i-1]
			if item.OccurredAt.Before(prev.OccurredAt) || item.OccurredAt.Equal(prev.OccurredAt) && item.Kind < prev.Kind {
				t.Fatal("incorrect timestamp/tie ordering")
			}
		}
		if item.Event != nil {
			if item.Event.Screenshots == nil {
				t.Fatal("missing screenshot metadata slot")
			}
			if item.Event.ID == loss.EventLogID {
				foundScreenshot = len(item.Event.Screenshots) == 1 && item.Event.Screenshots[0].EventLogID == loss.EventLogID
			}
		}
	}
	if !foundScreenshot || len(all.Items) != 15 {
		t.Fatalf("timeline count/metadata = %d, %v", len(all.Items), foundScreenshot)
	}
	first := proctorTimeline(t, f, login.SessionID, 1, "")
	// New backdated data must not enter a pagination snapshot already in progress.
	f.clock.Set(base.Add(-time.Second))
	if _, err := f.sessions.Autosave(t.Context(), login.Token, domain.Answers{"q1": "new snapshot"}); err != nil {
		t.Fatal(err)
	}
	clientEvent(t, f, login.Token, "app_started")
	items := append([]service.TimelineItem(nil), first.Items...)
	cursor := first.NextCursor
	for cursor != nil {
		page := proctorTimeline(t, f, login.SessionID, 2, *cursor)
		items = append(items, page.Items...)
		cursor = page.NextCursor
	}
	if len(items) != len(all.Items) {
		t.Fatalf("paged rows = %d, want %d", len(items), len(all.Items))
	}
	for i, item := range items {
		if item.ID != all.Items[i].ID {
			t.Fatal("pagination skipped, duplicated, or reordered a row")
		}
	}
	if len(proctorTimeline(t, f, login.SessionID, 200, "").Items) != 17 {
		t.Fatal("fresh read omitted new data")
	}
	other := f.login(t, "0034")
	if _, err := f.sessions.ProctorTimeline(t.Context(), other.SessionID, 2, *first.NextCursor); !errors.Is(err, service.ErrInvalidInput) {
		t.Fatal("cursor crossed sessions")
	}
	body, _ := json.Marshal(all)
	if strings.Contains(string(body), "private/must-not-be-returned.png") {
		t.Fatal("filesystem path exposed")
	}
}

func TestProctorForceSubmitConcurrentOutcomes(t *testing.T) {
	for _, operation := range []string{"save", "submit", "expiry", "force"} {
		t.Run(operation, func(t *testing.T) {
			f := answerFixture(t)
			login := f.login(t, "0012")
			if _, err := f.sessions.Autosave(t.Context(), login.Token, domain.Answers{"q1": "initial"}); err != nil {
				t.Fatal(err)
			}
			if operation == "expiry" {
				f.clock.Set(login.DeadlineAt.Add(30 * time.Second))
			}
			start := make(chan struct{})
			errs := make(chan error, 2)
			var wg sync.WaitGroup
			wg.Add(2)
			go func() {
				defer wg.Done()
				<-start
				_, err := f.sessions.ForceSubmit(t.Context(), login.SessionID, "operator reason")
				errs <- err
			}()
			go func() {
				defer wg.Done()
				<-start
				var err error
				switch operation {
				case "save":
					_, err = f.sessions.Autosave(t.Context(), login.Token, domain.Answers{"q1": "racing save"})
				case "submit":
					_, err = f.sessions.Submit(t.Context(), login.Token, domain.Answers{"q1": "racing submit"})
				case "expiry":
					_, err = f.sessions.Reconcile(t.Context())
				case "force":
					_, err = f.sessions.ForceSubmit(t.Context(), login.SessionID, "second reason")
				}
				errs <- err
			}()
			close(start)
			wg.Wait()
			close(errs)
			for err := range errs {
				if err != nil && !errors.Is(err, service.ErrSessionLocked) {
					t.Fatal(err)
				}
			}
			version := finalVersion(t, f, login.Token)
			if f.count(t, `SELECT count(*) FROM event_log WHERE event_type IN ('submitted','force_submitted')`) != 1 {
				t.Fatal("more than one finalization")
			}
			result, err := f.sessions.ForceSubmit(t.Context(), login.SessionID, "retry reason")
			if err != nil || result.FinalVersionID != version.ID {
				t.Fatal("retry changed final answer")
			}
			view := proctorTimeline(t, f, login.SessionID, 200, "").Session
			if view.Final == nil || view.Final.VersionID != version.ID || view.FinalizationPending {
				t.Fatal("final answer not identifiable")
			}
			if operation == "expiry" && (view.Status != domain.Submitted || view.Final.Reason != "grace_expired") {
				t.Fatal("force request relabelled expiry")
			}
			if _, err := f.sessions.Autosave(t.Context(), login.Token, domain.Answers{}); !errors.Is(err, service.ErrSessionLocked) {
				t.Fatal("late answer write allowed")
			}
		})
	}
}

type failingProctorTx struct{ failingAnswerTx }

func (tx failingProctorTx) AddEvent(ctx context.Context, event domain.Event) error {
	if err := tx.SessionTransaction.AddEvent(ctx, event); err != nil {
		return err
	}
	if tx.operation == "event" {
		return errors.New("injected force event failure")
	}
	return nil
}
func TestProctorForceWriteFailuresRollback(t *testing.T) {
	f := answerFixture(t)
	login := f.login(t, "0012")
	for _, operation := range []string{"append", "finalize", "event"} {
		repo := wrappedRepository{f.store, func(tx service.SessionTransaction) service.SessionTransaction {
			return failingProctorTx{failingAnswerTx{SessionTransaction: tx, operation: operation}}
		}}
		_, err := testSessions(repo, f.clock).ForceSubmit(context.Background(), login.SessionID, "reason")
		if err == nil {
			t.Fatalf("%s failure lost", operation)
		}
		if f.storedSession(t, login.Token).Status != domain.Active || f.count(t, `SELECT count(*) FROM submission_versions`) != 0 || f.count(t, `SELECT count(*) FROM event_log`) != 1 {
			t.Fatal("partial force submission committed")
		}
	}
}
