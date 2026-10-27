package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"exam-platform/internal/service"
)

type clockAdvancingReader struct {
	io.Reader
	advance func()
}

func (r *clockAdvancingReader) Read(p []byte) (int, error) {
	if r.advance != nil {
		r.advance()
		r.advance = nil
	}
	return r.Reader.Read(p)
}

func TestAnswerTransactionRechecksTimeAfterMiddleware(t *testing.T) {
	for _, path := range []string{"/autosave", "/submit"} {
		f, _ := fullFixture(t)
		login := f.login(t, "0012")
		if request(f.router, "POST", "/autosave", `{"answers":{"q1":"earlier"}}`, "Bearer "+login.SessionToken).Code != http.StatusOK {
			t.Fatal("initial save failed")
		}
		f.clock.Set(login.DeadlineAt.Add(29 * time.Second))
		body := &clockAdvancingReader{Reader: strings.NewReader(`{"answers":{"q1":"late"}}`), advance: func() { f.clock.Set(login.DeadlineAt.Add(30 * time.Second)) }}
		r := httptest.NewRequest("POST", path, body)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+login.SessionToken)
		w := httptest.NewRecorder()
		f.router.ServeHTTP(w, r)
		expected := http.StatusForbidden
		if path == "/submit" {
			expected = http.StatusOK
		}
		if w.Code != expected {
			t.Fatalf("delayed %s = %d", path, w.Code)
		}
		final, err := f.sessions.Submit(t.Context(), login.SessionToken, map[string]string{"q1": "ignored"})
		if err != nil || final.Sequence != 1 {
			t.Fatal("late body appended a version")
		}
		if err := f.store.WithSessionTransaction(t.Context(), func(tx service.SessionTransaction) error {
			version, err := tx.Version(t.Context(), login.SessionID, final.FinalVersionID)
			if err == nil && version.Answers["q1"] != "earlier" {
				t.Error("late request replaced final answer")
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
}

type preCommitFailure struct {
	repository service.SessionRepository
	calls      int
}

func (r *preCommitFailure) WithSessionTransaction(ctx context.Context, fn func(service.SessionTransaction) error) error {
	return r.repository.WithSessionTransaction(ctx, func(tx service.SessionTransaction) error {
		r.calls++
		if err := fn(tx); err != nil {
			return err
		}
		if r.calls == 2 {
			return errors.New("injected pre-commit failure")
		}
		return nil
	})
}

func TestNoHTTPSuccessBeforeAnswerTransactionCommits(t *testing.T) {
	for _, path := range []string{"/autosave", "/submit"} {
		f, _ := fullFixture(t)
		login := f.login(t, "0012")
		f.sessions = testSessions(&preCommitFailure{repository: f.store}, f.clock)
		f.router = New(f.clock, f.sessions, f.logger)
		w := request(f.router, "POST", path, `{"answers":{"q1":"rollback"}}`, "Bearer "+login.SessionToken)
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("failed transaction returned %d", w.Code)
		}
		view, err := f.sessions.GetExam(t.Context(), login.SessionToken)
		if err != nil || view.Latest != nil {
			t.Fatal("failed transaction partially committed")
		}
		principal, err := f.sessions.Authenticate(t.Context(), login.SessionToken)
		if err != nil || principal.Session.Status != "active" || principal.Session.FinalVersionID != nil {
			t.Fatal("failed transaction left a final state")
		}
	}
}
