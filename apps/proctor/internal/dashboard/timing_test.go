package dashboard

import (
	"testing"
	"time"
)

func TestNextEndTimeUsesLocalNextOccurrence(t *testing.T) {
	zone := time.FixedZone("local", -6*3600)
	now := time.Date(2026, 10, 31, 23, 50, 0, 0, zone)
	end, err := nextEndTime("00:30", now)
	if err != nil || end.Day() != 1 || end.Month() != time.November || end.Hour() != 0 || end.Location() != zone {
		t.Fatalf("%v %v", end, err)
	}
	for _, input := range []string{"24:00", "1:30", "tomorrow", "23:50:01"} {
		if _, err := nextEndTime(input, now); err == nil {
			t.Fatalf("accepted %q", input)
		}
	}
	if _, err := nextEndTime("23:50", now.Add(-30*time.Second)); err == nil {
		t.Fatal("accepted less than a minute away")
	}
}

func TestTimingActionsRequireExplicitConfirmation(t *testing.T) {
	s := &Service{}
	if err := s.SetEndTime("", "", false); err == nil {
		t.Fatal("no confirmation")
	}
	if _, err := s.ExtendGrace("", 1, "reason", false); err == nil {
		t.Fatal("no confirmation")
	}
	if _, err := s.ExtendExamGrace("", 1, "reason", false); err == nil {
		t.Fatal("no confirmation")
	}
	if _, err := s.PublishTimed("", "", false, false, "12:00"); err == nil {
		t.Fatal("no confirmation")
	}
}
