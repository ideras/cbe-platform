package service_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"exam-platform/internal/domain"
	"exam-platform/internal/service"
)

func TestProctorListCommittedStatesAndFocusTransitions(t *testing.T) {
	for _, state := range []domain.SessionStatus{domain.Active, domain.Grace, domain.Submitted, domain.ForceSubmitted} {
		t.Run(string(state), func(t *testing.T) {
			f := answerFixture(t)
			login := f.login(t, "0012")
			switch state {
			case domain.Grace:
				f.clock.Set(login.DeadlineAt)
				if _, err := f.sessions.Authenticate(t.Context(), login.Token); err != nil {
					t.Fatal(err)
				}
			case domain.Submitted:
				if _, err := f.sessions.Submit(t.Context(), login.Token, domain.Answers{}); err != nil {
					t.Fatal(err)
				}
			case domain.ForceSubmitted:
				if _, err := f.sessions.ForceSubmit(t.Context(), login.SessionID, "reason"); err != nil {
					t.Fatal(err)
				}
			}
			view := proctorList(t, f).Sessions[0]
			if view.Status != state || view.PersistedStatus != state || view.FinalizationPending {
				t.Fatalf("state = %+v", view)
			}
		})
	}
	f := answerFixture(t)
	login := f.login(t, "0012")
	heartbeat(t, f, login.Token)
	clientEvent(t, f, login.Token, "focus_lost")
	f.clock.Set(epoch.Add(15 * time.Second))
	heartbeat(t, f, login.Token)
	f.clock.Set(epoch.Add(21 * time.Second))
	view := proctorList(t, f).Sessions[0]
	if view.Presence.Focus.State != domain.FocusUnfocused || !view.FocusWarning || view.NetworkWarning || *view.Presence.Focus.DurationMillis != 21000 {
		t.Fatal("unfocused projection")
	}
	clientEvent(t, f, login.Token, "focus_regained")
	view = proctorList(t, f).Sessions[0]
	if view.Presence.Focus.State != domain.FocusFocused || view.FocusWarning || view.LastFocusEvent.Type != "focus_regained" {
		t.Fatal("focused projection")
	}
	clientEvent(t, f, login.Token, "app_started")
	if view := proctorList(t, f).Sessions[0]; view.Presence.Focus.State != domain.FocusUnknown || view.FocusWarning {
		t.Fatal("unknown projection")
	}
}

func TestLaterGapDoesNotReviveInvalidatedFocusWarning(t *testing.T) {
	for _, reset := range []string{"app_started", "app_closed", "connection_changed", "network_reconnected"} {
		t.Run(reset, func(t *testing.T) {
			f := answerFixture(t)
			login := f.login(t, "0012")
			heartbeat(t, f, login.Token)
			clientEvent(t, f, login.Token, "focus_lost")
			f.clock.Set(epoch.Add(10 * time.Second))
			switch reset {
			case "connection_changed":
				if _, err := f.sessions.Heartbeat(t.Context(), login.Token, "changed-host", "192.0.2.1", domain.Sound{}); err != nil {
					t.Fatal(err)
				}
				f.clock.Set(epoch.Add(41 * time.Second))
			case "network_reconnected":
				f.clock.Set(epoch.Add(31 * time.Second))
				heartbeat(t, f, login.Token)
				f.clock.Set(epoch.Add(62 * time.Second))
			default:
				clientEvent(t, f, login.Token, reset)
				f.clock.Set(epoch.Add(31 * time.Second))
			}
			if _, err := f.sessions.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			view := proctorList(t, f).Sessions[0]
			if !view.NetworkWarning || view.FocusWarning || view.LastKnownUnfocusedMillis != nil || view.Presence.Focus.State != domain.FocusUnknown {
				t.Fatalf("stale focus revived: %+v", view)
			}
		})
	}
}

func TestTimelineByteBudgetAndEverySnapshot(t *testing.T) {
	f := answerFixture(t)
	login := f.login(t, "0012")
	limited, err := service.NewSessionsWithLimits(f.store, f.clock, service.AnswerLimits{PerAnswerBytes: 512 << 10, TotalBytes: 512 << 10})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Repeat("x", 512<<10)
	for range 6 {
		if _, err := limited.Autosave(t.Context(), login.Token, domain.Answers{"q1": text}); err != nil {
			t.Fatal(err)
		}
	}
	page := proctorTimeline(t, f, login.SessionID, 200, "")
	if page.NextCursor == nil || len(page.Items) >= 7 {
		t.Fatal("byte budget did not bound large history")
	}
	versions := 0
	for {
		body, err := json.Marshal(page.Items)
		if err != nil {
			t.Fatal(err)
		}
		if len(body) > service.TimelinePageBytes {
			t.Fatalf("page bytes = %d", len(body))
		}
		for _, item := range page.Items {
			if item.Version != nil {
				versions++
				if item.Version.Answers["q1"] != text {
					t.Fatal("snapshot truncated")
				}
			}
		}
		if page.NextCursor == nil {
			break
		}
		page = proctorTimeline(t, f, login.SessionID, 200, *page.NextCursor)
	}
	if versions != 6 {
		t.Fatalf("versions = %d", versions)
	}
}

func TestTimelineCursorValidation(t *testing.T) {
	f := answerFixture(t)
	login := f.login(t, "0012")
	if _, err := f.sessions.Autosave(t.Context(), login.Token, domain.Answers{}); err != nil {
		t.Fatal(err)
	}
	first := proctorTimeline(t, f, login.SessionID, 1, "")
	bytes, err := base64.RawURLEncoding.DecodeString(*first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"version", "kind", "timestamp", "future_watermark", "foreign_id", "unknown_field"} {
		t.Run(field, func(t *testing.T) {
			var value map[string]any
			if err := json.Unmarshal(bytes, &value); err != nil {
				t.Fatal(err)
			}
			switch field {
			case "version":
				value["v"] = 2
			case "kind":
				value["after"].(map[string]any)["kind"] = "screenshot"
			case "timestamp":
				value["after"].(map[string]any)["sort_time"] = "garbage"
			case "future_watermark":
				value["watermark"].(map[string]any)["event_log_id"] = 9999999
			case "foreign_id":
				value["after"].(map[string]any)["id"] = 9999999
			case "unknown_field":
				value["token"] = "not a credential"
			}
			body, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.sessions.ProctorTimeline(t.Context(), login.SessionID, 1, base64.RawURLEncoding.EncodeToString(body)); !errors.Is(err, service.ErrInvalidInput) {
				t.Fatalf("cursor validation = %v", err)
			}
		})
	}
	for _, limit := range []int{0, -1, 201} {
		if _, err := f.sessions.ProctorTimeline(t.Context(), login.SessionID, limit, ""); !errors.Is(err, service.ErrInvalidInput) {
			t.Fatal("invalid limit accepted")
		}
	}
}

type noAnswerReadTx struct{ service.SessionTransaction }

func (tx noAnswerReadTx) Version(context.Context, string, int64) (domain.Version, error) {
	return domain.Version{}, errors.New("unexpected full answer read")
}
func TestProctorListUsesOnlyFinalVersionMetadata(t *testing.T) {
	f := answerFixture(t)
	login := f.login(t, "0012")
	if _, err := f.sessions.ForceSubmit(t.Context(), login.SessionID, "reason"); err != nil {
		t.Fatal(err)
	}
	repo := wrappedRepository{f.store, func(tx service.SessionTransaction) service.SessionTransaction { return noAnswerReadTx{tx} }}
	result, err := testSessions(repo, f.clock).ProctorSessions(t.Context(), testExamID)
	if err != nil || result.Sessions[0].Final == nil {
		t.Fatal("list loaded full answers instead of metadata")
	}
}

type precommitProctorRepository struct{ store service.SessionRepository }

func (r precommitProctorRepository) WithSessionTransaction(ctx context.Context, fn func(service.SessionTransaction) error) error {
	return r.store.WithSessionTransaction(ctx, func(tx service.SessionTransaction) error {
		if err := fn(tx); err != nil {
			return err
		}
		return errors.New("injected pre-commit failure")
	})
}
func TestProctorReadsClockAfterAcquiringTransaction(t *testing.T) {
	for _, operation := range []string{"list", "timeline", "force"} {
		t.Run(operation, func(t *testing.T) {
			f := answerFixture(t)
			login := f.login(t, "0012")
			until := login.DeadlineAt.Add(30 * time.Second)
			f.clock.Set(until.Add(-time.Nanosecond))
			repo := wrappedRepository{f.store, func(tx service.SessionTransaction) service.SessionTransaction {
				f.clock.Set(until)
				return tx
			}}
			svc := testSessions(repo, f.clock)
			switch operation {
			case "list":
				result, err := svc.ProctorSessions(t.Context(), testExamID)
				if err != nil || !result.ServerTime.Equal(until) || !result.Sessions[0].FinalizationPending {
					t.Fatal("list used pre-transaction time")
				}
			case "timeline":
				result, err := svc.ProctorTimeline(t.Context(), login.SessionID, 100, "")
				if err != nil || !result.ServerTime.Equal(until) || !result.Session.FinalizationPending {
					t.Fatal("timeline used pre-transaction time")
				}
			case "force":
				result, err := svc.ForceSubmit(t.Context(), login.SessionID, "after lock wait")
				if err != nil || result.Status != domain.Submitted {
					t.Fatal("force used pre-transaction time")
				}
			}
		})
	}
}

func TestProctorDoesNotAcknowledgeFailedTransactions(t *testing.T) {
	f := answerFixture(t)
	login := f.login(t, "0012")
	failing := testSessions(precommitProctorRepository{f.store}, f.clock)
	if result, err := failing.ForceSubmit(t.Context(), login.SessionID, "reason"); err == nil || result.FinalVersionID != 0 {
		t.Fatal("force acknowledged failed commit")
	}
	if list, err := failing.ProctorSessions(t.Context(), testExamID); err == nil || len(list.Sessions) != 0 {
		t.Fatal("list acknowledged failed transaction")
	}
	if page, err := failing.ProctorTimeline(t.Context(), login.SessionID, 100, ""); err == nil || len(page.Items) != 0 {
		t.Fatal("timeline acknowledged failed transaction")
	}
	if f.storedSession(t, login.Token).Status != domain.Active || f.count(t, `SELECT count(*) FROM submission_versions`) != 0 || f.count(t, `SELECT count(*) FROM event_log`) != 1 {
		t.Fatal("failed force committed state")
	}
}
