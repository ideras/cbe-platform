package storage

import (
	"database/sql"
	"math/rand"
	"strings"
	"testing"
	"time"

	"exam-platform/internal/service"
)

// Phase 8, found by the load test: the newest event of a session among a few types was a walk back through the session's events (thousands of file
// events, none of the kind asked for), made for every session every second. It is a seek of the index for each type now.
func TestTheNewestEventOfAFewTypesIsASeekOfTheIndexAndTheSameRowAsBefore(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	// The events are written for sessions this test does not make; the database has one connection, so this holds for all that follows.
	if _, err := s.db.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatal(err)
	}
	rnd := rand.New(rand.NewSource(7))
	kinds := append(append([]string{}, focusEventTypes...), "file.saved", "login", "answer_version", "remote_changed", "pty.opened")
	insert, err := s.db.PrepareContext(ctx, `INSERT INTO event_log(session_id, event_type, occurred_at, detail) VALUES (?, ?, ?, NULL)`)
	if err != nil {
		t.Fatal(err)
	}
	defer insert.Close()
	sessions := []string{"s-with-focus", "s-with-none", "s-with-one-early"}
	at := testNow
	for i := 0; i < 6000; i++ {
		session := sessions[rnd.Intn(len(sessions))]
		kind := "file.saved"
		switch {
		case session == "s-with-one-early" && i == 3:
			kind = "app_started"
		case session == "s-with-focus" && rnd.Intn(40) == 0:
			kind = kinds[rnd.Intn(len(kinds))]
		case session == "s-with-one-early" || session == "s-with-none":
			kind = []string{"file.saved", "file.saved", "pty.opened", "login", "answer_version"}[rnd.Intn(5)]
		}
		at = at.Add(time.Millisecond)
		if _, err := insert.ExecContext(ctx, session, kind, timestamp(at)); err != nil {
			t.Fatal(err)
		}
	}
	// The old query, the one that walked, is the standard of what the answer is.
	const old = `SELECT id, event_type FROM event_log WHERE session_id = ? AND event_type IN ('focus_lost', 'focus_regained', 'app_started', 'app_closed', 'network_connected', 'network_gap', 'network_reconnected', 'connection_changed') ORDER BY id DESC LIMIT 1`
	for _, session := range append(sessions, "nobody") {
		var wantID int64
		var wantType string
		err := s.db.QueryRowContext(ctx, old, session).Scan(&wantID, &wantType)
		event, found := latestFocusOf(t, s, session)
		switch {
		case err == sql.ErrNoRows:
			if found {
				t.Errorf("%s: an event %+v where there is none", session, event)
			}
		case err != nil:
			t.Fatal(err)
		case !found || event.ID != wantID || event.Type != wantType:
			t.Errorf("%s: got %+v (found %v), want %d %s", session, event, found, wantID, wantType)
		}
	}
	// What the query does is checked in the plan: a seek of event_log_session_type_id for each type, and no walk of the session's events.
	rows, err := s.db.QueryContext(ctx, "EXPLAIN QUERY PLAN "+strings.Replace(latestFocusSQL, "?1", "'s-with-none'", -1))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	text := strings.Join(plan, "\n")
	// The other two queries that were made the same way, for the proctor's reads.
	for name, query := range map[string]string{"latest reported focus": latestReportedFocusSQL, "finalization": finalizationEventSQL} {
		planRows, err := s.db.QueryContext(ctx, "EXPLAIN QUERY PLAN "+strings.Replace(query, "?1", "'s-with-none'", -1))
		if err != nil {
			t.Fatal(err)
		}
		var details []string
		for planRows.Next() {
			var id, parent, unused int
			var detail string
			if err := planRows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			details = append(details, detail)
		}
		planRows.Close()
		if joined := strings.Join(details, "\n"); strings.Contains(joined, "sqlite_autoindex") || !strings.Contains(joined, "event_log_session_type_id") {
			t.Errorf("%s, the plan:\n%s", name, joined)
		}
	}
	if strings.Contains(text, "sqlite_autoindex") || !strings.Contains(text, "event_log_session_type_id") || strings.Count(text, "event_log_session_type_id") < len(focusEventTypes) {
		t.Errorf("the plan:\n%s", text)
	}
}

// latestFocusOf asks for the newest focus event the way the service does, inside a session transaction.
func latestFocusOf(t *testing.T, s *Store, session string) (struct {
	ID   int64
	Type string
}, bool) {
	t.Helper()
	var out struct {
		ID   int64
		Type string
	}
	found := false
	err := s.WithSessionTransaction(t.Context(), func(tx service.SessionTransaction) error {
		event, err := tx.LatestFocusEvent(t.Context(), session)
		if err == nil {
			out.ID, out.Type, found = event.ID, event.Type, true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out, found
}
