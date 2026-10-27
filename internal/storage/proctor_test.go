package storage

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"exam-platform/internal/service"
)

type proctorClock struct{ now time.Time }

func (c proctorClock) Now() time.Time { return c.now }

func TestProctorMigrationPreservesFinalAnswersEventsAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "phase4.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE schema_migrations(version TEXT PRIMARY KEY NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	// Every migration but 003 adds, renames or rebuilds what the current queries select; the fixture runs current service
	// code on this older schema, and 003 is applied on the upgrade.
	for _, name := range []string{"001_foundation.sql", "002_presence.sql", "004_screenshots.sql", "005_sound.sql", "006_exam_volume.sql", "007_publishing.sql", "008_classes.sql", "009_exam_content.sql", "010_capture_series.sql", "011_exam_class.sql", "012_exam_source.sql", "013_settings_and_lock.sql", "014_unlock_and_policy_copies.sql"} {
		script, err := migrations.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(script)); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations VALUES (?)`, name); err != nil {
			t.Fatal(err)
		}
	}
	legacy := &Store{db: db}
	seed(t, legacy)
	version, err := appendTest(t.Context(), legacy, sessionID, map[string]string{"q1": "preserved 世界"})
	if err != nil {
		t.Fatal(err)
	}
	original, err := service.NewSessions(legacy, proctorClock{testNow}).ForceSubmit(t.Context(), sessionID, "migration fixture")
	if err != nil {
		t.Fatal(err)
	}
	if original.FinalVersionID != version.ID {
		t.Fatal("fixture final did not select existing version")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	var cursor string
	for i := 0; i < 2; i++ {
		current, err := Open(t.Context(), path)
		if err != nil {
			t.Fatal(err)
		}
		svc := service.NewSessions(current, proctorClock{testNow.Add(time.Second)})
		page, err := svc.ProctorTimeline(t.Context(), sessionID, 1, cursor)
		if err != nil {
			current.Close()
			t.Fatal(err)
		}
		if page.Session.Final == nil || page.Session.Final.VersionID != version.ID || page.Session.Final.Reason != "force_submit" || page.Session.Final.OperatorReason != "migration fixture" {
			t.Fatal("migration changed finalization")
		}
		if i == 0 {
			if page.NextCursor == nil {
				t.Fatal("expected continuation")
			}
			cursor = *page.NextCursor
		} else if len(page.Items) != 1 || page.Items[0].Event == nil || page.Items[0].Event.Type != "force_submitted" {
			t.Fatal("cursor did not survive restart")
		}
		var count int
		if err := current.db.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&count); err != nil || count != migrationCount(t) {
			t.Fatal("migration tracking")
		}
		if err := current.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTimelineQueriesUseChronologicalIndexes(t *testing.T) {
	s := openTest(t)
	seed(t, s)
	for _, test := range []struct{ table, column, index string }{
		{"event_log", "occurred_at", "events_session_timeline"},
		{"submission_versions", "saved_at", "versions_session_timeline"},
	} {
		key := timelineKey(test.column)
		rows, err := s.db.Query(`EXPLAIN QUERY PLAN SELECT id FROM `+test.table+` WHERE session_id=? AND id<=? AND (`+key+`,id)>(?,?) ORDER BY `+key+`,id LIMIT ?`, sessionID, 1000000, "", 0, 101)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(detail, test.index) {
				found = true
			}
			if strings.Contains(detail, "TEMP B-TREE") {
				t.Fatalf("unbounded sort: %s", detail)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			t.Fatal(err)
		}
		if !found {
			t.Fatalf("missing chronological index for %s", test.table)
		}
	}
}
