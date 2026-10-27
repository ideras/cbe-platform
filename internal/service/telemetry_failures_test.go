package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"exam-platform/internal/domain"
	"exam-platform/internal/service"
)

type failingTelemetryTx struct {
	service.SessionTransaction
	operation string
}

func (tx failingTelemetryTx) AddEvent(ctx context.Context, event domain.Event) error {
	if err := tx.SessionTransaction.AddEvent(ctx, event); err != nil {
		return err
	}
	if tx.operation == "event" {
		return errors.New("injected telemetry event failure")
	}
	return nil
}
func (tx failingTelemetryTx) RecordEvent(ctx context.Context, event domain.Event) (int64, error) {
	id, err := tx.SessionTransaction.RecordEvent(ctx, event)
	if err == nil && tx.operation == "client_event" {
		err = errors.New("injected client event failure")
	}
	return id, err
}
func (tx failingTelemetryTx) SetNetworkGap(ctx context.Context, id string, since time.Time) error {
	if err := tx.SessionTransaction.SetNetworkGap(ctx, id, since); err != nil {
		return err
	}
	if tx.operation == "gap" {
		return errors.New("injected gap metadata failure")
	}
	return nil
}
func (tx failingTelemetryTx) TouchHeartbeat(ctx context.Context, id, hostname, peer string, sound domain.Sound, now time.Time) error {
	if err := tx.SessionTransaction.TouchHeartbeat(ctx, id, hostname, peer, sound, now); err != nil {
		return err
	}
	if tx.operation == "touch" {
		return errors.New("injected heartbeat failure")
	}
	return nil
}
func failingTelemetry(f *fixture, operation string) *service.Sessions {
	repo := wrappedRepository{f.store, func(tx service.SessionTransaction) service.SessionTransaction {
		return failingTelemetryTx{tx, operation}
	}}
	return testSessions(repo, f.clock)
}

func TestTelemetryFailuresRollBackMetadataAndEvents(t *testing.T) {
	for _, operation := range []string{"event", "touch"} {
		f := newFixture(t, 0)
		login := f.login(t, "0012")
		if _, err := failingTelemetry(f, operation).Heartbeat(t.Context(), login.Token, "pc", "192.0.2.1", domain.Sound{}); err == nil {
			t.Fatal("injected initial heartbeat failure lost")
		}
		if f.storedSession(t, login.Token).LastSeen != nil || f.count(t, `SELECT count(*) FROM event_log`) != 1 {
			t.Fatal("partial initial connection survived")
		}
	}
	for _, operation := range []string{"event", "gap"} {
		f := newFixture(t, 0)
		login := f.login(t, "0012")
		heartbeat(t, f, login.Token)
		f.clock.Set(epoch.Add(31 * time.Second))
		if _, err := failingTelemetry(f, operation).Reconcile(t.Context()); err == nil {
			t.Fatal("injected scan failure lost")
		}
		if f.storedSession(t, login.Token).NetworkGapSince != nil || f.count(t, `SELECT count(*) FROM event_log WHERE event_type='network_gap'`) != 0 {
			t.Fatal("partial disconnect survived")
		}
	}
	for _, operation := range []string{"event", "touch"} {
		f := newFixture(t, 0)
		login := f.login(t, "0012")
		heartbeat(t, f, login.Token)
		f.clock.Set(epoch.Add(31 * time.Second))
		if _, err := f.sessions.Reconcile(t.Context()); err != nil {
			t.Fatal(err)
		}
		if _, err := failingTelemetry(f, operation).Heartbeat(t.Context(), login.Token, "pc", "192.0.2.1", domain.Sound{}); err == nil {
			t.Fatal("injected reconnection failure lost")
		}
		stored := f.storedSession(t, login.Token)
		if stored.NetworkGapSince == nil || !stored.LastSeen.Equal(epoch) || f.count(t, `SELECT count(*) FROM event_log WHERE event_type='network_reconnected'`) != 0 {
			t.Fatal("partial reconnect survived")
		}
	}
	f := newFixture(t, 0)
	login := f.login(t, "0012")
	heartbeat(t, f, login.Token)
	result, err := failingTelemetry(f, "client_event").ClientEvent(t.Context(), login.Token, service.EventInput{Type: "focus_lost"})
	if err == nil || result.EventLogID != 0 || f.count(t, `SELECT count(*) FROM event_log WHERE event_type='focus_lost'`) != 0 || presence(t, f, login.SessionID).Focus.State != domain.FocusUnknown {
		t.Fatal("uncommitted incident was acknowledged")
	}
}

func TestTelemetryValidationAndAuthority(t *testing.T) {
	f := newFixture(t, 0)
	first := f.login(t, "0012")
	other := f.login(t, "0034")
	if _, err := f.sessions.Heartbeat(t.Context(), "invalid", "pc", "192.0.2.1", domain.Sound{}); !errors.Is(err, service.ErrInvalidToken) {
		t.Fatal("unknown bearer accepted")
	}
	for _, name := range []string{"", strings.Repeat("x", 256), "pc\x00name"} {
		if _, err := f.sessions.Heartbeat(t.Context(), first.Token, name, "192.0.2.1", domain.Sound{}); !errors.Is(err, service.ErrInvalidInput) {
			t.Fatalf("hostname validation = %v", err)
		}
	}
	for _, volume := range []int{-1, 101} {
		if _, err := f.sessions.Heartbeat(t.Context(), first.Token, "pc", "192.0.2.1", domain.Sound{VolumePercent: &volume}); !errors.Is(err, service.ErrInvalidInput) {
			t.Fatalf("volume %d accepted: %v", volume, err)
		}
	}
	for _, kind := range []string{"login", "submitted", "force_submitted", "network_gap", "network_connected", "network_reconnected", "connection_changed", "grace_started", "heartbeat", "FOCUS_LOST", "unknown"} {
		if _, err := f.sessions.ClientEvent(t.Context(), first.Token, service.EventInput{Type: kind}); !errors.Is(err, service.ErrInvalidInput) {
			t.Fatal("reserved event accepted:", kind)
		}
	}
	for _, detail := range []string{"null", "[]", "1", "true", `"text"`, `{}`, `{"x":1} {}`} {
		_, err := f.sessions.ClientEvent(t.Context(), first.Token, service.EventInput{Type: "app_started", Detail: json.RawMessage(detail)})
		if detail == "{}" {
			if err != nil {
				t.Fatal(err)
			}
		} else if !errors.Is(err, service.ErrInvalidInput) {
			t.Fatalf("detail validation = %v", err)
		}
	}
	if _, err := f.sessions.ClientEvent(t.Context(), first.Token, service.EventInput{Type: "app_started", Detail: json.RawMessage(`{"x":"` + strings.Repeat("x", service.MaxEventDetailBytes) + `"}`)}); !errors.Is(err, service.ErrTelemetryTooLarge) {
		t.Fatal("detail size not bounded")
	}
	heartbeat(t, f, first.Token)
	if f.storedSession(t, other.Token).LastSeen != nil {
		t.Fatal("heartbeat altered another session")
	}
	if _, err := f.sessions.StudentsWithoutSession(t.Context(), "nonexistent"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("invalid exam treated as missing roster")
	}
}
