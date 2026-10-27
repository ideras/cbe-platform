package storage

import (
	"testing"
	"time"

	"exam-platform/internal/domain"
	"exam-platform/internal/service"
)

// The average size of the stored screenshots is a fraction, and the settings page reads it as a whole number of bytes.
func TestTheAverageScreenshotSizeIsAWholeNumberOfBytes(t *testing.T) {
	s := openTest(t)
	seed(t, s)
	ctx := t.Context()
	// Three screenshots whose mean is not a whole number.
	for index, size := range []int64{100, 200, 300001} {
		if _, err := s.db.ExecContext(ctx, `INSERT INTO event_log(session_id, event_type, occurred_at) VALUES (?, 'focus_lost', ?)`, sessionID, timestamp(testNow)); err != nil {
			t.Fatal(err)
		}
		if _, err := s.db.ExecContext(ctx, `INSERT INTO screenshots(session_id, event_log_id, file_path, content_type, captured_at, byte_size, state)
			VALUES (?, ?, ?, 'image/jpeg', ?, ?, 'ready')`, sessionID, index+1, "seed/"+string(rune('a'+index))+".jpg", timestamp(testNow), size); err != nil {
			t.Fatal(err)
		}
	}
	average := averageScreenshotBytesForTest(t, s)
	if average == nil {
		t.Fatal("the average size was not read")
	}
	// (100 + 200 + 300001) / 3 = 100100.33…, rounded to the nearest byte.
	if *average != 100100 {
		t.Fatalf("the average size = %d, want 100100", *average)
	}
}

// With no stored screenshot there is no average at all.
func TestThereIsNoAverageWithoutScreenshots(t *testing.T) {
	s := openTest(t)
	seed(t, s)
	if average := averageScreenshotBytesForTest(t, s); average != nil {
		t.Fatalf("the average size = %d, want none", *average)
	}
}

// The monitoring settings are stored once and read back; without a stored row the built-in default applies.
func TestTheSettingsAreStoredAndReadBack(t *testing.T) {
	s := openTest(t)
	ctx := t.Context()
	var stored domain.Policy
	var updated time.Time
	if err := s.WithSessionTransaction(ctx, func(tx service.SessionTransaction) error {
		var err error
		stored, updated, err = tx.Settings(ctx)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if !stored.IsZero() || !updated.IsZero() {
		t.Fatalf("settings without a stored row = %+v, %v", stored, updated)
	}
	policy := domain.DefaultPolicy()
	policy.FocusThreshold = 3 * time.Second
	at := testNow
	if err := s.WithSessionTransaction(ctx, func(tx service.SessionTransaction) error {
		return tx.SaveSettings(ctx, policy, at)
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.WithSessionTransaction(ctx, func(tx service.SessionTransaction) error {
		var err error
		stored, updated, err = tx.Settings(ctx)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if stored.FocusThreshold != 3*time.Second || !updated.Equal(at) {
		t.Fatalf("stored settings = %+v, %v", stored, updated)
	}
}

// averageScreenshotBytesForTest reads the average through the same seam the settings service uses.
func averageScreenshotBytesForTest(t *testing.T, s *Store) *int64 {
	t.Helper()
	var average *int64
	if err := s.WithSessionTransaction(t.Context(), func(tx service.SessionTransaction) error {
		var err error
		average, err = tx.AverageScreenshotBytes(t.Context())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return average
}
