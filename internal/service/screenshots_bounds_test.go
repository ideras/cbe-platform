package service_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"exam-platform/internal/domain"
	"exam-platform/internal/service"
	"hash/crc32"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestScreenshotDeclaredDimensionsRejectedBeforeAllocationAndWrongEventType(t *testing.T) {
	f := newFixture(t, 0)
	a := f.login(t, "0012")
	shots, _ := imageService(t, f)
	loss := eligibleLoss(t, f, a.Token)
	f.clock.Set(epoch.Add(21 * time.Second))
	for _, dims := range [][2]uint32{{16385, 12}, {10000, 10000}} {
		data := imagePNG(t)
		binary.BigEndian.PutUint32(data[16:20], dims[0])
		binary.BigEndian.PutUint32(data[20:24], dims[1])
		binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
		if _, err := shots.Upload(t.Context(), a.Token, loss, 0, nil, bytes.NewReader(data)); !errors.Is(err, service.ErrScreenshotTooLarge) {
			t.Fatal("unsafe declared dimensions accepted", err)
		}
	}
	regain := clientEvent(t, f, a.Token, "focus_regained")
	if _, err := shots.Upload(t.Context(), a.Token, regain.EventLogID, 0, nil, bytes.NewReader(imagePNG(t))); !errors.Is(err, service.ErrScreenshotIneligible) {
		t.Fatal("wrong event type accepted")
	}
}
func TestScreenshotRootOwnershipAndFileFailureCleanup(t *testing.T) {
	f := newFixture(t, 0)
	login := f.login(t, "0012")
	shots, root := imageService(t, f)
	other := newFixture(t, 0)
	if manager, err := service.NewScreenshots(other.store, other.sessions, root); err == nil {
		manager.Close()
		t.Fatal("image root reused by another database")
	}
	unowned := privateImageDir(t)
	os.WriteFile(filepath.Join(unowned, "keep.png"), []byte("unknown operator file"), 0600)
	if manager, err := service.NewScreenshots(f.store, f.sessions, unowned); err == nil {
		manager.Close()
		t.Fatal("unowned nonempty root claimed/destructively reconciled")
	}
	if _, err := os.Stat(filepath.Join(unowned, "keep.png")); err != nil {
		t.Fatal("unowned file removed")
	}
	loss := eligibleLoss(t, f, login.Token)
	f.clock.Set(epoch.Add(21 * time.Second))
	dir := filepath.Join(root, login.SessionID)
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, fmtID(loss)+"-0.jpg"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := shots.Upload(t.Context(), login.Token, loss, 0, nil, bytes.NewReader(imagePNG(t))); err == nil {
		t.Fatal("write failure unreported")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != fmtID(loss)+"-0.jpg" {
		t.Fatal("temporary file leaked")
	}
	if f.count(t, `SELECT count(*) FROM screenshots`) != 0 {
		t.Fatal("write failure committed metadata")
	}
}
func TestScreenshotPurgeIsExamScopedAndKeepsAnswers(t *testing.T) {
	f := answerFixture(t)
	a := f.login(t, "0012")
	shots, _ := imageService(t, f)
	first := eligibleLoss(t, f, a.Token)
	f.clock.Set(epoch.Add(21 * time.Second))
	if _, err := shots.Upload(t.Context(), a.Token, first, 0, nil, bytes.NewReader(imagePNG(t))); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sessions.Autosave(t.Context(), a.Token, domain.Answers{"q1": "preserve forever"}); err != nil {
		t.Fatal(err)
	}
	const otherExam = "00000000-0000-4000-8000-000000000099"
	if err := f.store.CreateExam(t.Context(), domain.Exam{ID: otherExam, ClassID: testClassID, Title: "other", PIN: "other", DurationSeconds: 120, GraceSeconds: 30, CreatedAt: f.clock.Now()}); err != nil {
		t.Fatal(err)
	}
	f.publish(t, otherExam)
	other := f.sessions
	b, err := other.Login(t.Context(), "0034", "other")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Heartbeat(t.Context(), b.Token, "other", "192.0.2.2", domain.Sound{}); err != nil {
		t.Fatal(err)
	}
	loss, err := other.ClientEvent(t.Context(), b.Token, service.EventInput{Type: "focus_lost"})
	if err != nil {
		t.Fatal(err)
	}
	f.clock.Set(epoch.Add(42 * time.Second))
	saved, err := shots.Upload(t.Context(), b.Token, loss.EventLogID, 0, nil, bytes.NewReader(imagePNG(t)))
	if err != nil {
		t.Fatal(err)
	}
	count := f.count(t, `SELECT count(*) FROM event_log`)
	if r, err := shots.Purge(t.Context(), testExamID); err != nil || !r.Complete {
		t.Fatal(err)
	}
	if _, _, err := shots.Get(t.Context(), saved.ID, false); err != nil {
		t.Fatal("other exam image purged", err)
	}
	if f.count(t, `SELECT count(*) FROM screenshots`) != 1 || f.count(t, `SELECT count(*) FROM submission_versions`) != 1 || f.count(t, `SELECT count(*) FROM event_log`) != count {
		t.Fatal("purge crossed scope or removed history")
	}
}
