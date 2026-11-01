package client

import (
	"testing"
	"time"
)

func TestRealServerCappedTimingStillReportsFullGraceExtension(t *testing.T) {
	server := setupRealServer(t)
	exam := server.create(t, 3600, 30)
	server.start(t, exam, fastPolicy())
	client := realClient(t, server.base)
	initial, err := client.Login(t.Context(), server.base, "00123", "synthetic PIN")
	if err != nil {
		t.Fatal(err)
	}
	end := time.Now().Add(2 * time.Minute).UTC()
	server.admin(t, "PUT", "/admin/exam/"+exam+"/end-time", map[string]any{"ends_at": end}, nil, 200)
	capped := waitView(t, client, func(v View) bool { return v.Status != nil && v.Status.EndsAt != nil })
	if capped.StatusObservation == 0 || capped.StatusAgeMillis < 0 || !capped.Status.DeadlineAt.Equal(end) || !capped.Status.GraceUntil.Equal(end) {
		t.Fatalf("invalid cap: %+v", capped.Status)
	}
	server.admin(t, "POST", "/admin/exam/"+exam+"/extend-grace", map[string]any{"minutes": 5, "reason": "class interruption"}, nil, 200)
	extended := waitView(t, client, func(v View) bool { return len(v.TimeNotices) > 0 && v.TimeNotices[0].Kind == "grace" })
	if extended.TimeNotices[0].AddedSeconds != 300 || !extended.Status.GraceUntil.Equal(end) || !extended.Status.OwnGraceUntil.Equal(initial.Status.OwnGraceUntil.Add(5*time.Minute)) {
		t.Fatal("cap hid the extension")
	}
	sequence := extended.TimeNotices[0].Sequence
	for i := 0; i < 3; i++ {
		client.pollOnce()
		if client.Snapshot().TimeNotices[0].Sequence != sequence {
			t.Fatal("notice repeated")
		}
	}
	server.admin(t, "PUT", "/admin/exam/"+exam+"/end-time", map[string]any{"ends_at": nil}, nil, 200)
	restored := waitView(t, client, func(v View) bool { return v.Status.EndsAt == nil })
	if !restored.Status.DeadlineAt.Equal(initial.Status.DeadlineAt) || !restored.Status.GraceUntil.Equal(initial.Status.OwnGraceUntil.Add(5*time.Minute)) {
		t.Fatal("clear did not restore entitlement")
	}
}
