package service_test

import (
	"bytes"
	"context"
	"errors"
	"exam-platform/internal/domain"
	"exam-platform/internal/service"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

type screenshotStepClock struct{ calls int }

func (c *screenshotStepClock) Now() time.Time {
	c.calls++
	if c.calls <= 2 {
		return epoch.Add(81 * time.Second)
	}
	return epoch.Add(time.Hour)
}
func TestScreenshotAcceptanceUsesOneTransactionClockCheckpoint(t *testing.T) {
	f := answerFixture(t)
	a := f.login(t, "0012")
	loss := eligibleLoss(t, f, a.Token)
	f.clock.Set(epoch.Add(21 * time.Second))
	if _, err := f.sessions.Submit(t.Context(), a.Token, domain.Answers{}); err != nil {
		t.Fatal(err)
	}
	clock := &screenshotStepClock{}
	sessions := testSessions(f.store, clock)
	shots, err := service.NewScreenshots(f.store, sessions, privateImageDir(t))
	if err != nil {
		t.Fatal(err)
	}
	defer shots.Close()
	saved, err := shots.Upload(t.Context(), a.Token, loss, 0, nil, bytes.NewReader(imagePNG(t)))
	if err != nil || !saved.CapturedAt.Equal(epoch.Add(81*time.Second)) || clock.calls != 2 {
		t.Fatal("acceptance and metadata used different transaction times", err)
	}
}

type imageReaderFunc func([]byte) (int, error)

func (r imageReaderFunc) Read(p []byte) (int, error) { return r(p) }
func TestScreenshotReadingCannotHoldAnswerTransaction(t *testing.T) {
	f := answerFixture(t)
	a := f.login(t, "0012")
	shots, _ := imageService(t, f)
	loss := eligibleLoss(t, f, a.Token)
	f.clock.Set(epoch.Add(21 * time.Second))
	started, release := make(chan struct{}), make(chan struct{})
	var once bool
	reader := io.MultiReader(imageReaderFunc(func([]byte) (int, error) {
		if !once {
			once = true
			close(started)
			<-release
		}
		return 0, io.EOF
	}), bytes.NewReader(imagePNG(t)))
	done := make(chan error, 1)
	go func() { _, err := shots.Upload(t.Context(), a.Token, loss, 0, nil, reader); done <- err }()
	<-started
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	_, err := f.sessions.Autosave(ctx, a.Token, domain.Answers{"q1": "not blocked by image I/O"})
	close(release)
	if err != nil {
		t.Fatal("image read held SQLite transaction", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
func TestScreenshotRechecksTerminalWindowAfterFileAndPreservesAmbiguousCommit(t *testing.T) {
	f := answerFixture(t)
	a := f.login(t, "0012")
	root := privateImageDir(t)
	repo := &screenshotFailRepo{ScreenshotRepository: f.store}
	shots, err := service.NewScreenshots(repo, f.sessions, root)
	if err != nil {
		t.Fatal(err)
	}
	defer shots.Close()
	loss := eligibleLoss(t, f, a.Token)
	f.clock.Set(epoch.Add(21 * time.Second))
	if _, err := f.sessions.Submit(t.Context(), a.Token, domain.Answers{}); err != nil {
		t.Fatal(err)
	}
	reader := io.MultiReader(bytes.NewReader(imagePNG(t)), imageReaderFunc(func([]byte) (int, error) { f.clock.Set(epoch.Add(81*time.Second + time.Nanosecond)); return 0, io.EOF }))
	if _, err := shots.Upload(t.Context(), a.Token, loss, 0, nil, reader); !errors.Is(err, service.ErrScreenshotIneligible) {
		t.Fatal("body/file latency widened upload window", err)
	}
	if _, err := os.Stat(filepath.Join(root, a.SessionID, fmtID(loss)+"-0.jpg")); !os.IsNotExist(err) {
		t.Fatal("rejected late upload leaked file")
	}
	f.clock.Set(epoch.Add(22 * time.Second))
	repo.ambiguous = true
	if _, err := shots.Upload(t.Context(), a.Token, loss, 0, nil, bytes.NewReader(imagePNG(t))); err == nil {
		t.Fatal("ambiguous commit incorrectly acknowledged")
	}
	saved, err := shots.Upload(t.Context(), a.Token, loss, 0, nil, bytes.NewReader(imagePNG(t)))
	if err != nil || !saved.Duplicate {
		t.Fatal("committed file removed after ambiguous commit", err)
	}
	if _, _, err := shots.Get(t.Context(), saved.ID, false); err != nil {
		t.Fatal(err)
	}
}

type screenshotWorkerRepo struct {
	service.ScreenshotRepository
	failed atomic.Bool
}
type screenshotWorkerTx struct {
	service.ScreenshotTransaction
	repo *screenshotWorkerRepo
}

func (r *screenshotWorkerRepo) WithScreenshotTransaction(ctx context.Context, fn func(service.ScreenshotTransaction) error) error {
	return r.ScreenshotRepository.WithScreenshotTransaction(ctx, func(tx service.ScreenshotTransaction) error { return fn(screenshotWorkerTx{tx, r}) })
}
func (t screenshotWorkerTx) ScreenshotRows(ctx context.Context, exam string) ([]domain.Screenshot, error) {
	if t.repo.failed.Load() {
		return nil, errors.New("injected cleanup failure")
	}
	return t.ScreenshotTransaction.ScreenshotRows(ctx, exam)
}
func TestScreenshotCleanupWorkerRetriesWithoutStoppingExam(t *testing.T) {
	f := answerFixture(t)
	a := f.login(t, "0012")
	repo := &screenshotWorkerRepo{ScreenshotRepository: f.store}
	shots, err := service.NewScreenshots(repo, f.sessions, privateImageDir(t))
	if err != nil {
		t.Fatal(err)
	}
	defer shots.Close()
	repo.failed.Store(true)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	reports, failures := make(chan struct{}, 1), make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- shots.RunWorker(ctx, 5*time.Millisecond, func(service.ScreenshotReconcileReport) {
			select {
			case reports <- struct{}{}:
			default:
			}
		}, func(error) {
			select {
			case failures <- struct{}{}:
			default:
			}
		})
	}()
	select {
	case <-failures:
	case <-time.After(time.Second):
		t.Fatal("cleanup failure not reported")
	}
	if _, err := f.sessions.Autosave(t.Context(), a.Token, domain.Answers{"q1": "cleanup failure cannot stop exam"}); err != nil {
		t.Fatal(err)
	}
	repo.failed.Store(false)
	select {
	case <-reports:
	case <-time.After(time.Second):
		t.Fatal("cleanup worker did not retry")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
