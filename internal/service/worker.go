package service

import (
	"context"
	"fmt"
	"time"
)

type ReconcileReport struct {
	GraceTransitions int
	Finalizations    int
	NetworkGaps      int
	Locks            int
}

func (s *Sessions) Reconcile(ctx context.Context) (ReconcileReport, error) {
	var report ReconcileReport
	err := s.repository.WithSessionTransaction(ctx, func(tx SessionTransaction) error {
		now := s.clock.Now().UTC()
		sessions, err := tx.UnfinishedSessions(ctx)
		if err != nil {
			return err
		}
		for _, session := range sessions {
			session, gap, err := s.ensureGap(ctx, tx, session, now)
			if err != nil {
				return err
			}
			if gap {
				report.NetworkGaps++
			}
			principal, changed, err := s.reconcileSession(ctx, tx, session, now)
			if err != nil {
				return err
			}
			if changed {
				report.GraceTransitions++
			}
			if terminal(principal.Session) {
				report.Finalizations++
				continue
			}
			// The lock is the server's backstop against a modified student application.
			reason, err := lockReason(ctx, tx, principal.Session, now)
			if err != nil {
				return err
			}
			if reason == "" {
				continue
			}
			_, locked, err := lockSession(ctx, tx, principal.Session, reason, now, "server")
			if err != nil {
				return err
			}
			if locked {
				report.Locks++
			}
		}
		return nil
	})
	if err != nil {
		return ReconcileReport{}, err
	}
	return report, nil
}

// RunWorker catches up immediately and periodically reconciles idle sessions.
func (s *Sessions) RunWorker(ctx context.Context, interval time.Duration, observe func(ReconcileReport)) error {
	if interval <= 0 {
		return fmt.Errorf("worker interval must be positive")
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return nil
		}
		report, err := s.Reconcile(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("reconcile sessions: %w", err)
		}
		if observe != nil {
			observe(report)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
