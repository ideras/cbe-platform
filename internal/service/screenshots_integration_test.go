package service_test

import (
	"bytes"
	"context"
	"errors"
	"exam-platform/internal/domain"
	"exam-platform/internal/service"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

func privateImageDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}
func fmtID(id int64) string { return strconv.FormatInt(id, 10) }
func imagePNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 24, 12))
	img.Set(1, 1, color.RGBA{R: 255, A: 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func imageService(t *testing.T, f *fixture) (*service.Screenshots, string) {
	t.Helper()
	root := privateImageDir(t)
	s, err := service.NewScreenshots(f.store, f.sessions, root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, root
}
func eligibleLoss(t *testing.T, f *fixture, token string) int64 {
	t.Helper()
	heartbeat(t, f, token)
	return clientEvent(t, f, token, "focus_lost").EventLogID
}
func TestScreenshotsThresholdIdentityCanonicalImageAndHistory(t *testing.T) {
	f := answerFixture(t)
	login := f.login(t, "0012")
	other := f.login(t, "0034")
	shots, root := imageService(t, f)
	loss := eligibleLoss(t, f, login.Token)
	data := imagePNG(t)
	for _, elapsed := range []time.Duration{0, 20 * time.Second} {
		f.clock.Set(epoch.Add(elapsed))
		if _, err := shots.Upload(t.Context(), login.Token, loss, 0, nil, bytes.NewReader(data)); !errors.Is(err, service.ErrScreenshotIneligible) {
			t.Fatalf("early capture: %v", err)
		}
	}
	f.clock.Set(epoch.Add(20*time.Second + time.Nanosecond))
	fake := epoch.Add(100 * time.Hour)
	result, err := shots.Upload(t.Context(), login.Token, loss, 0, &fake, bytes.NewReader(append(data, []byte("untrusted trailing bytes")...)))
	if err != nil {
		t.Fatal(err)
	}
	if result.ID <= 0 || result.Width != 24 || result.Height != 12 || result.ClientCapturedAt == nil || !result.CapturedAt.Equal(f.clock.Now()) {
		t.Fatal("incorrect image metadata")
	}
	full, _, err := shots.Get(t.Context(), result.ID, false)
	if err != nil || bytes.Contains(full, []byte("untrusted trailing bytes")) {
		t.Fatal("image was not canonicalized")
	}
	if _, _, err := shots.Get(t.Context(), result.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := shots.Upload(t.Context(), other.Token, loss, 0, nil, bytes.NewReader(data)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("cross-session image accepted")
	}
	duplicate, err := shots.Upload(t.Context(), login.Token, loss, 0, nil, bytes.NewReader(data))
	if err != nil || !duplicate.Duplicate || duplicate.ID != result.ID {
		t.Fatal("retry replaced image")
	}
	info, err := os.Stat(filepath.Join(root, login.SessionID, fmtID(loss)+"-0.jpg"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("image permissions")
	}
	page, err := f.sessions.ProctorTimeline(t.Context(), login.SessionID, 100, "")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range page.Items {
		if item.Event != nil && item.Event.ID == loss {
			found = len(item.Event.Screenshots) == 1 && item.Event.Screenshots[0].Width == 24
		}
	}
	if !found {
		t.Fatal("missing contextual metadata")
	}
}
func TestScreenshotsIntervalsTerminalExceptionAndImmutableAnswers(t *testing.T) {
	for _, kind := range []string{"regain_short", "gap_short", "app_short", "unconfirmed", "explicit", "force", "expiry", "expiry_late"} {
		t.Run(kind, func(t *testing.T) {
			f := answerFixture(t)
			login := f.login(t, "0012")
			shots, _ := imageService(t, f)
			var loss int64
			if kind == "unconfirmed" {
				loss = clientEvent(t, f, login.Token, "focus_lost").EventLogID
			} else {
				loss = eligibleLoss(t, f, login.Token)
			}
			switch kind {
			case "regain_short":
				f.clock.Set(epoch.Add(10 * time.Second))
				clientEvent(t, f, login.Token, "focus_regained")
			case "app_short":
				f.clock.Set(epoch.Add(10 * time.Second))
				clientEvent(t, f, login.Token, "app_started")
			case "gap_short":
				f.clock.Set(epoch.Add(15 * time.Second))
				clientEvent(t, f, login.Token, "focus_regained")
				f.clock.Set(epoch.Add(16 * time.Second))
				loss = clientEvent(t, f, login.Token, "focus_lost").EventLogID
				f.clock.Set(epoch.Add(31 * time.Second))
				heartbeat(t, f, login.Token)
			case "explicit", "force":
				f.clock.Set(epoch.Add(21 * time.Second))
				if kind == "explicit" {
					if _, err := f.sessions.Submit(t.Context(), login.Token, domain.Answers{"q1": "final"}); err != nil {
						t.Fatal(err)
					}
				} else {
					if _, err := f.sessions.ForceSubmit(t.Context(), login.SessionID, "fixture"); err != nil {
						t.Fatal(err)
					}
				}
			case "expiry", "expiry_late":
				f.clock.Set(epoch.Add(15 * time.Second))
				heartbeat(t, f, login.Token)
				f.clock.Set(epoch.Add(30 * time.Second))
				heartbeat(t, f, login.Token)
				f.clock.Set(epoch.Add(45 * time.Second))
				heartbeat(t, f, login.Token)
				f.clock.Set(epoch.Add(60 * time.Second))
				heartbeat(t, f, login.Token)
				f.clock.Set(epoch.Add(75 * time.Second))
				heartbeat(t, f, login.Token)
				f.clock.Set(epoch.Add(150 * time.Second))
				if _, err := f.sessions.Authenticate(t.Context(), login.Token); err != nil {
					t.Fatal(err)
				}
				if kind == "expiry_late" {
					f.clock.Set(epoch.Add(150*time.Second + time.Nanosecond))
				}
			}
			if f.clock.Now().Before(epoch.Add(25 * time.Second)) {
				f.clock.Set(epoch.Add(25 * time.Second))
			}
			_, err := shots.Upload(t.Context(), login.Token, loss, 0, nil, bytes.NewReader(imagePNG(t)))
			expected := kind == "explicit" || kind == "force" || kind == "expiry"
			if expected && err != nil {
				t.Fatal(err)
			}
			if !expected && !errors.Is(err, service.ErrScreenshotIneligible) {
				t.Fatalf("ineligible interval accepted: %v", err)
			}
			if expected {
				if _, err := f.sessions.Autosave(t.Context(), login.Token, domain.Answers{}); !errors.Is(err, service.ErrSessionLocked) {
					t.Fatal("screenshot reopened answers")
				}
				before := finalVersion(t, f, login.Token)
				if _, _, err := shots.Get(t.Context(), 1, false); err != nil {
					t.Fatal(err)
				}
				if finalVersion(t, f, login.Token).ID != before.ID {
					t.Fatal("final changed")
				}
			}
		})
	}
}
func TestScreenshotsInvalidEncodingDimensionsAndConcurrentDuplicate(t *testing.T) {
	f := newFixture(t, 0)
	login := f.login(t, "0012")
	shots, _ := imageService(t, f)
	loss := eligibleLoss(t, f, login.Token)
	f.clock.Set(epoch.Add(21 * time.Second))
	for _, data := range [][]byte{nil, []byte("JPEG/notPNG"), make([]byte, service.MaxScreenshotBytes+1), imagePNG(t)[:30]} {
		if _, err := shots.Upload(t.Context(), login.Token, loss, 0, nil, bytes.NewReader(data)); err == nil {
			t.Fatal("invalid image accepted")
		}
	}
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			if _, err := shots.Upload(t.Context(), login.Token, loss, 0, nil, bytes.NewReader(imagePNG(t))); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if f.count(t, `SELECT count(*) FROM screenshots`) != 1 {
		t.Fatal("duplicate rows")
	}
}
func TestScreenshotPurgeAndRestartReconcile(t *testing.T) {
	f := answerFixture(t)
	login := f.login(t, "0012")
	shots, root := imageService(t, f)
	loss := eligibleLoss(t, f, login.Token)
	f.clock.Set(epoch.Add(21 * time.Second))
	saved, err := shots.Upload(t.Context(), login.Token, loss, 0, nil, bytes.NewReader(imagePNG(t)))
	if err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(root, login.SessionID, "9999.png")
	temporary := filepath.Join(root, login.SessionID, ".pending-orphan")
	if err := os.WriteFile(orphan, imagePNG(t), 0600); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(temporary, []byte("partial"), 0600)
	if err := os.Remove(filepath.Join(root, login.SessionID, fmtID(loss)+"-0.jpg")); err != nil {
		t.Fatal(err)
	}
	restarted, err := service.NewScreenshots(f.store, f.sessions, root)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	report, err := restarted.Reconcile(t.Context())
	if err != nil || report.RemovedRows != 1 || report.RemovedOrphans != 2 {
		t.Fatalf("reconcile %+v %v", report, err)
	}
	if _, _, err := shots.Get(t.Context(), saved.ID, false); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("missing-file metadata retained")
	}
	if _, err := shots.Upload(t.Context(), login.Token, loss, 0, nil, bytes.NewReader(imagePNG(t))); err != nil {
		t.Fatal(err)
	}
	events := f.count(t, `SELECT count(*) FROM event_log`)
	result, err := shots.Purge(t.Context(), testExamID)
	if err != nil || !result.Complete || result.Deleted != 1 {
		t.Fatal("purge failed", err)
	}
	again, err := shots.Purge(t.Context(), testExamID)
	if err != nil || again.Deleted != 0 || !again.Complete {
		t.Fatal("purge not idempotent")
	}
	if _, err := shots.Upload(t.Context(), login.Token, loss, 0, nil, bytes.NewReader(imagePNG(t))); !errors.Is(err, service.ErrScreenshotsPurged) {
		t.Fatal("purge allowed image resurrection")
	}
	if f.count(t, `SELECT count(*) FROM event_log`) != events {
		t.Fatal("purge changed event history")
	}
}
func TestScreenshotSymlinkEscapeRefused(t *testing.T) {
	f := newFixture(t, 0)
	login := f.login(t, "0012")
	shots, root := imageService(t, f)
	loss := eligibleLoss(t, f, login.Token)
	f.clock.Set(epoch.Add(21 * time.Second))
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, login.SessionID)); err != nil {
		t.Skip(err)
	}
	if _, err := shots.Upload(t.Context(), login.Token, loss, 0, nil, bytes.NewReader(imagePNG(t))); err == nil {
		t.Fatal("symlink directory accepted")
	}
	entries, _ := os.ReadDir(outside)
	if len(entries) > 0 {
		t.Fatal("wrote outside screenshot root")
	}
}

type screenshotFailRepo struct {
	service.ScreenshotRepository
	insert, remove, ambiguous, wrote bool
}
type screenshotFailTx struct {
	service.ScreenshotTransaction
	repo *screenshotFailRepo
}

func (t screenshotFailTx) InsertScreenshot(ctx context.Context, r domain.Screenshot) (int64, error) {
	if t.repo.insert {
		return 0, errors.New("injected insert failure")
	}
	id, err := t.ScreenshotTransaction.InsertScreenshot(ctx, r)
	t.repo.wrote = err == nil
	return id, err
}
func (t screenshotFailTx) DeleteScreenshot(ctx context.Context, id int64) error {
	if t.repo.remove {
		return errors.New("injected delete failure")
	}
	return t.ScreenshotTransaction.DeleteScreenshot(ctx, id)
}
func (r *screenshotFailRepo) WithScreenshotTransaction(ctx context.Context, fn func(service.ScreenshotTransaction) error) error {
	err := r.ScreenshotRepository.WithScreenshotTransaction(ctx, func(tx service.ScreenshotTransaction) error { return fn(screenshotFailTx{tx, r}) })
	if err == nil && r.ambiguous && r.wrote {
		r.ambiguous = false
		return errors.New("ambiguous committed transaction")
	}
	return err
}
func TestScreenshotDatabaseFailuresCleanupAndRetryablePurge(t *testing.T) {
	f := newFixture(t, 0)
	login := f.login(t, "0012")
	root := privateImageDir(t)
	repo := &screenshotFailRepo{ScreenshotRepository: f.store, insert: true}
	shots, err := service.NewScreenshots(repo, f.sessions, root)
	if err != nil {
		t.Fatal(err)
	}
	defer shots.Close()
	loss := eligibleLoss(t, f, login.Token)
	f.clock.Set(epoch.Add(21 * time.Second))
	data := imagePNG(t)
	if _, err := shots.Upload(t.Context(), login.Token, loss, 0, nil, bytes.NewReader(data)); err == nil {
		t.Fatal("injected write succeeded")
	}
	if _, err := os.Stat(filepath.Join(root, login.SessionID, fmtID(loss)+"-0.jpg")); !os.IsNotExist(err) {
		t.Fatal("failed insert retained file")
	}
	repo.insert = false
	saved, err := shots.Upload(t.Context(), login.Token, loss, 0, nil, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	repo.remove = true
	purged, err := shots.Purge(t.Context(), testExamID)
	if err != nil || purged.Complete || len(purged.Failed) != 1 {
		t.Fatal("partial purge unreported")
	}
	if _, _, err := shots.Get(t.Context(), saved.ID, false); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("deleting image exposed")
	}
	repo.remove = false
	purged, err = shots.Purge(t.Context(), testExamID)
	if err != nil || !purged.Complete {
		t.Fatal("partial purge not retryable")
	}
}
