package client

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// The monitoring settings the integration tests need are faster than the proctor's Settings page allows, so the tests
// write them into the database directly. The values are the same ones the API stores: whole milliseconds.
type testPolicy struct {
	HeartbeatMillis  int64 `json:"heartbeat_interval_ms"`
	OverdueMillis    int64 `json:"overdue_threshold_ms"`
	FocusMillis      int64 `json:"focus_threshold_ms"`
	AlertMillis      int64 `json:"alert_interval_ms"`
	CaptureMillis    int64 `json:"capture_interval_ms"`
	LockAwayMillis   int64 `json:"lock_away_after_ms"`
	LockNoContact    int64 `json:"lock_no_contact_after_ms"`
	DefaultGrace     int64 `json:"default_grace_ms"`
	IncidentCaptures int   `json:"incident_captures"`
	SessionCaptures  int   `json:"session_captures"`
}

// fastPolicy is the policy the integration tests run with: a heartbeat every 40 ms and a focus threshold of 50 ms, so the
// tests do not have to wait seconds.
func fastPolicy() testPolicy { return fastPolicyWith(400) }

// fastPolicyWith is the same policy with a different connection-lost threshold.
func fastPolicyWith(overdueMillis int64) testPolicy {
	return testPolicy{HeartbeatMillis: 40, OverdueMillis: overdueMillis, FocusMillis: 50, AlertMillis: 30, CaptureMillis: 100,
		DefaultGrace: 30000, IncidentCaptures: 30, SessionCaptures: 200}
}

// storeTestPolicy writes the monitoring settings into the server's database, before it starts.
func storeTestPolicy(t *testing.T, dir string, policy testPolicy) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "exam.db")+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	encoded, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO settings(singleton, policy_json, updated_at) VALUES (1, ?, ?)
		ON CONFLICT(singleton) DO UPDATE SET policy_json = excluded.policy_json, updated_at = excluded.updated_at`,
		string(encoded), time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z")); err != nil {
		t.Fatal(err)
	}
}
