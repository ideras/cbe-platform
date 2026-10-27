package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"exam-platform/internal/domain"
	"exam-platform/internal/service"
)

func TestPersistedFinalStatusRemainsReadable(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, status := range []domain.SessionStatus{domain.Submitted, domain.ForceSubmitted} {
		session := domain.Session{ID: "own-session", Status: status, DeadlineAt: fixedClock{}.Now().Add(time.Minute), GraceUntil: fixedClock{}.Now().Add(90 * time.Second)}
		principal := service.Principal{Session: session, Lifecycle: domain.EvaluateSession(session, fixedClock{}.Now()), ServerTime: fixedClock{}.Now()}
		sessions := stubSessions{authenticate: func(context.Context, string) (service.Principal, error) { return principal, nil }}
		w := request(New(fixedClock{}, sessions, logger), "GET", "/session-status", "", "Bearer valid-token")
		var body statusResponse
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if w.Code != http.StatusOK || body.Status != status || body.SecondsRemaining != 0 || body.FinalizationPending {
			t.Fatalf("final status = %+v, %d", body, w.Code)
		}
	}
}
