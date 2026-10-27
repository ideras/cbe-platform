package httpapi

import (
	"context"
	"exam-platform/internal/domain"
	"time"

	"exam-platform/internal/clock"
	"exam-platform/internal/service"
)

// testSessions is the session service the tests use. The monitoring settings come from the store: the fixtures store
// testPolicy before they publish the exam, so the exam freezes it like the proctor's Settings page would.
func testSessions(repository service.SessionRepository, clock clock.Clock) *service.Sessions {
	sessions, err := service.NewSessionsWithLimits(repository, clock, service.DefaultAnswerLimits())
	if err != nil {
		panic(err)
	}
	return sessions
}

// testPolicy is the monitoring settings the tests run with: a 20-second focus threshold, as they were written for.
func testPolicy() domain.Policy {
	policy := domain.DefaultPolicy()
	policy.FocusThreshold = 20 * time.Second
	return policy
}

// storeSettings writes the monitoring settings of the server, as the proctor's Settings page does.
func storeSettings(repository service.SessionRepository, clock clock.Clock, policy domain.Policy) {
	err := repository.WithSessionTransaction(context.Background(), func(tx service.SessionTransaction) error {
		return tx.SaveSettings(context.Background(), policy.Normalized(), clock.Now())
	})
	if err != nil {
		panic(err)
	}
}
