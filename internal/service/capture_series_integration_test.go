package service_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"testing"
	"time"

	"exam-platform/internal/domain"
	"exam-platform/internal/service"
)

// captureFixture keeps the tests' 20-second threshold and 10-second capture interval, with limits small enough to reach.
// The locks are off here: these tests are about the captures, and the away lock would work out the per-absence count.
func captureFixture(t *testing.T, incident, session int) *fixture {
	t.Helper()
	f := newFixture(t, 0)
	policy := domain.DefaultPolicy()
	policy.FocusThreshold, policy.CaptureInterval = 20*time.Second, 10*time.Second
	policy.IncidentCaptures, policy.SessionCaptures = incident, session
	policy.LockAwayAfter, policy.LockNoContactAfter = 0, 0
	storeSettings(f.store, f.clock, policy)
	f.publish(t, testExamID)
	var err error
	if f.sessions, err = service.NewSessionsWithLimits(f.store, f.clock, service.DefaultAnswerLimits()); err != nil {
		t.Fatal(err)
	}
	setContent(t, f, domain.FreeNavigation, examContent([]string{"q1", "q2"}))
	return f
}

// at moves the clock and keeps the student's heartbeat fresh, so the focus loss is not cut short by a network gap.
func at(t *testing.T, f *fixture, token string, elapsed time.Duration) {
	t.Helper()
	f.clock.Set(epoch.Add(elapsed))
	heartbeat(t, f, token)
}

func TestAFocusLossIsCapturedRepeatedlyAsJPEG(t *testing.T) {
	f := captureFixture(t, 3, 200)
	login := f.login(t, "0012")
	shots, _ := imageService(t, f)
	loss := eligibleLoss(t, f, login.Token)
	upload := func(index int) (service.ScreenshotResult, error) {
		return shots.Upload(t.Context(), login.Token, loss, index, nil, bytes.NewReader(imagePNG(t)))
	}

	at(t, f, login.Token, 21*time.Second)
	first, err := upload(0)
	if err != nil || first.CaptureIndex != 0 || first.ContentType != "image/jpeg" {
		t.Fatalf("first capture = %+v, %v", first, err)
	}
	// The second capture only needs the threshold, allowing for a client clock a little ahead of the server's.
	if second, err := upload(1); err != nil || second.CaptureIndex != 1 || second.ID == first.ID {
		t.Fatalf("second capture = %+v, %v", second, err)
	}
	var wait *service.ScreenshotWait
	if _, err := upload(2); !errors.As(err, &wait) || wait.Seconds != 10 {
		t.Fatalf("a third capture before another interval = %v", err)
	}
	at(t, f, login.Token, 30*time.Second+time.Millisecond)
	third, err := upload(2)
	if err != nil {
		t.Fatal(err)
	}
	if again, err := upload(2); err != nil || !again.Duplicate || again.ID != third.ID {
		t.Fatalf("a repeated capture = %+v, %v", again, err)
	}
	if _, err := upload(3); !errors.Is(err, service.ErrScreenshotIneligible) {
		t.Fatalf("a capture past the incident limit = %v", err)
	}

	data, kind, err := shots.Get(t.Context(), first.ID, false)
	if err != nil || kind != "image/jpeg" {
		t.Fatalf("stored = %q, %v", kind, err)
	}
	if config, err := jpeg.DecodeConfig(bytes.NewReader(data)); err != nil || config.Width != 24 || config.Height != 12 {
		t.Fatalf("stored image = %+v, %v", config, err)
	}
	if thumb, kind, err := shots.Get(t.Context(), first.ID, true); err != nil || kind != "image/jpeg" || len(thumb) == 0 {
		t.Fatalf("thumbnail = %q, %v", kind, err)
	}

	page, err := f.sessions.ProctorTimeline(t.Context(), login.SessionID, 100, "")
	if err != nil {
		t.Fatal(err)
	}
	var series []domain.ScreenshotMetadata
	events := 0
	for _, item := range page.Items {
		if item.Event != nil && item.Event.ID == loss {
			events++
			series = item.Event.Screenshots
		}
	}
	if events != 1 || len(series) != 3 || series[0].CaptureIndex != 0 || series[2].CaptureIndex != 2 || series[1].ContentType != "image/jpeg" {
		t.Fatalf("history = %d events, series %+v", events, series)
	}
}

func TestCapturesStopAtTheSessionLimit(t *testing.T) {
	f := captureFixture(t, 2, 3)
	login := f.login(t, "0012")
	shots, _ := imageService(t, f)
	loss := eligibleLoss(t, f, login.Token)
	at(t, f, login.Token, 21*time.Second)
	for index := range 2 {
		if _, err := shots.Upload(t.Context(), login.Token, loss, index, nil, bytes.NewReader(imagePNG(t))); err != nil {
			t.Fatal(err)
		}
	}
	clientEvent(t, f, login.Token, "focus_regained")
	at(t, f, login.Token, 25*time.Second)
	next := clientEvent(t, f, login.Token, "focus_lost").EventLogID
	at(t, f, login.Token, 46*time.Second)
	if _, err := shots.Upload(t.Context(), login.Token, next, 0, nil, bytes.NewReader(imagePNG(t))); err != nil {
		t.Fatal(err)
	}
	if _, err := shots.Upload(t.Context(), login.Token, next, 1, nil, bytes.NewReader(imagePNG(t))); !errors.Is(err, service.ErrScreenshotIneligible) {
		t.Fatalf("a capture past the session limit = %v", err)
	}
}

func TestOversizedDesktopsMustBeScaledDownByTheClient(t *testing.T) {
	f := captureFixture(t, 3, 200)
	login := f.login(t, "0012")
	shots, _ := imageService(t, f)
	loss := eligibleLoss(t, f, login.Token)
	at(t, f, login.Token, 21*time.Second)
	var wide bytes.Buffer
	if err := png.Encode(&wide, image.NewGray(image.Rect(0, 0, service.MaxStoredSide+1, 10))); err != nil {
		t.Fatal(err)
	}
	if _, err := shots.Upload(t.Context(), login.Token, loss, 0, nil, bytes.NewReader(wide.Bytes())); !errors.Is(err, service.ErrScreenshotTooLarge) {
		t.Fatalf("an oversized desktop = %v", err)
	}
}

// The settings are frozen when an exam is published: a change afterwards reaches the next publish, never a running exam.
func TestTheSettingsAreFrozenAtThePublishAndCopiedAtLogin(t *testing.T) {
	f := newFixture(t, 0)
	login := f.login(t, "0012")
	if login.Policy.FocusMillis != 20000 || login.Policy.CaptureMillis != 5000 || login.Policy.AlertMillis != 2000 {
		t.Fatalf("policy = %+v", login.Policy)
	}
	heartbeat(t, f, login.Token)
	clientEvent(t, f, login.Token, "focus_lost")
	f.clock.Set(epoch.Add(20*time.Second + time.Millisecond))
	if !presence(t, f, login.SessionID).Focus.ThresholdExceeded {
		t.Fatal("the published threshold was not applied")
	}
	list, err := f.sessions.ProctorSessions(t.Context(), testExamID)
	if err != nil || list.Policy.FocusThresholdMillis != 20000 {
		t.Fatalf("proctor policy = %+v, %v", list.Policy, err)
	}
	// A proctor changes the settings and publishes again: the students already in the exam keep their own copy.
	changed := testPolicy()
	changed.FocusThreshold = 30 * time.Second
	storeSettings(f.store, f.clock, changed)
	f.publish(t, testExamID)
	principal, err := f.sessions.Authenticate(t.Context(), login.Token)
	if err != nil || principal.Policy.FocusMillis != 20000 {
		t.Fatalf("resumed policy = %+v, %v", principal.Policy, err)
	}
	// A student who logs in afterwards follows the new rules.
	second := f.login(t, "0034")
	if second.Policy.FocusMillis != 30000 {
		t.Fatalf("new session policy = %+v", second.Policy)
	}
	encoded, _ := json.Marshal(principal.Policy)
	for _, field := range []string{"alert_interval_ms", "capture_interval_ms", "incident_captures", "session_captures", "lock_away_after_ms", "lock_no_contact_after_ms"} {
		if !bytes.Contains(encoded, []byte(field)) {
			t.Fatalf("policy JSON lacks %s: %s", field, encoded)
		}
	}
}
