package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"exam-platform/internal/domain"
)

type finalization struct {
	Status                 domain.SessionStatus
	Reason, OperatorReason string
}

func (s *Sessions) finalize(ctx context.Context, tx SessionTransaction, session domain.Session, now time.Time, candidate *domain.Version, cause finalization) (domain.Session, error) {
	if terminal(session) {
		return session, nil
	}
	var err error
	session, _, err = s.ensureGap(ctx, tx, session, now)
	if err != nil {
		return domain.Session{}, err
	}
	var version domain.Version
	if candidate != nil {
		version = *candidate
	} else {
		var err error
		version, err = tx.LatestVersion(ctx, session.ID)
		if errors.Is(err, domain.ErrNotFound) {
			version, err = tx.AppendVersion(ctx, session.ID, domain.Answers{}, now)
		}
		if err != nil {
			return domain.Session{}, err
		}
	}
	if err := ValidateFinalVersion(session.ID, version); err != nil {
		return domain.Session{}, err
	}
	if err := tx.Finalize(ctx, session.ID, version.ID, cause.Status, now); err != nil {
		return domain.Session{}, err
	}
	detail, err := json.Marshal(struct {
		Reason         string `json:"reason"`
		OperatorReason string `json:"operator_reason,omitempty"`
		VersionID      int64  `json:"final_version_id"`
		Sequence       int64  `json:"version_seq"`
	}{cause.Reason, cause.OperatorReason, version.ID, version.Sequence})
	if err != nil {
		return domain.Session{}, err
	}
	if err := tx.AddEvent(ctx, domain.Event{SessionID: session.ID, Type: string(cause.Status), OccurredAt: now, Detail: detail}); err != nil {
		return domain.Session{}, err
	}
	session.Status, session.SubmittedAt, session.FinalVersionID = cause.Status, &now, &version.ID
	return session, nil
}
