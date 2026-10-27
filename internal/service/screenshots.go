package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png" // uploads may still be PNG
	"io"
	"os"
	"time"

	"exam-platform/internal/domain"
)

const MaxScreenshotBytes int64 = 16 << 20
const MaxScreenshotDimension = 16384
const MaxScreenshotPixels int64 = 40000000

// Stored screenshots are canonical JPEG of at most two 1080p screens' worth of pixels, the longer side at most 3840; the
// student application scales larger desktops down before uploading.
const (
	MaxStoredPixels      int64 = 2 * 1920 * 1080
	MaxStoredSide              = 3840
	storedJPEGQuality          = 80
	thumbnailJPEGQuality       = 70
)
const PendingScreenshotWindow = 60 * time.Second

var ErrScreenshotIneligible = errors.New("screenshot incident is not eligible")

type ScreenshotWait struct{ Seconds int64 }

func (e *ScreenshotWait) Error() string { return "screenshot threshold has not elapsed" }
func (e *ScreenshotWait) Unwrap() error { return ErrScreenshotIneligible }

var ErrScreenshotTooLarge = errors.New("screenshot exceeds image limits")
var ErrScreenshotsPurged = errors.New("screenshots for this exam were purged")
var ErrScreenshotUnavailable = errors.New("screenshot is unavailable")

type ScreenshotTransaction interface {
	SessionTransaction
	ScreenshotStoreID(context.Context) (string, error)
	FocusEvent(context.Context, string, int64) (domain.Event, error)
	FocusBoundary(context.Context, string, int64) (domain.Event, error)
	ScreenshotByCapture(context.Context, string, int64, int) (domain.Screenshot, error)
	SessionScreenshotCount(context.Context, string) (int, error)
	ScreenshotByID(context.Context, int64) (domain.Screenshot, error)
	ScreenshotRows(context.Context, string) ([]domain.Screenshot, error)
	InsertScreenshot(context.Context, domain.Screenshot) (int64, error)
	DeleteScreenshot(context.Context, int64) error
	ScreenshotsPurged(context.Context, string) (bool, error)
	MarkScreenshotPurge(context.Context, string, time.Time) error
}
type ScreenshotRepository interface {
	WithScreenshotTransaction(context.Context, func(ScreenshotTransaction) error) error
}
type Screenshots struct {
	repository ScreenshotRepository
	sessions   *Sessions
	root       *os.Root
	gate       chan struct{}
}
type ScreenshotResult struct {
	domain.ScreenshotMetadata
	Duplicate bool `json:"duplicate"`
}
type PurgeResult struct {
	ExamID   string  `json:"exam_id"`
	Deleted  int     `json:"deleted"`
	Failed   []int64 `json:"failed_screenshot_ids"`
	Complete bool    `json:"complete"`
}

// NewScreenshots binds a dedicated private image root to this database; use one live instance per root.
func NewScreenshots(repository ScreenshotRepository, sessions *Sessions, directory string) (*Screenshots, error) {
	info, err := os.Lstat(directory)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("screenshot root must be a real directory")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	s := &Screenshots{repository: repository, sessions: sessions, root: root, gate: make(chan struct{}, 1)}
	if err := s.claimRoot(); err != nil {
		root.Close()
		return nil, err
	}
	return s, nil
}
func (s *Screenshots) Close() error { return s.root.Close() }
func (s *Screenshots) lock(ctx context.Context) error {
	select {
	case s.gate <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (s *Screenshots) unlock() { <-s.gate }
func screenshotPath(session string, event int64, index int) string {
	return fmt.Sprintf("%s/%d-%d.jpg", session, event, index)
}

// eligible says whether capture index of a focus loss may be stored now: the loss must have lasted past the session's
// threshold, and past one more capture interval for each capture after the second (a client's clock and its event
// delivery can lag the server's by a little, so capture 1 only needs the threshold too).
func (s *Screenshots) eligible(ctx context.Context, tx ScreenshotTransaction, token string, eventID int64, index int, now time.Time) (domain.Session, error) {
	session, err := tx.SessionByToken(ctx, token)
	if errors.Is(err, domain.ErrNotFound) {
		return session, ErrInvalidToken
	}
	if err != nil {
		return session, err
	}
	policy := session.Policy
	if index >= policy.IncidentCaptures {
		return session, ErrScreenshotIneligible
	}
	purged, err := tx.ScreenshotsPurged(ctx, session.ExamID)
	if err != nil {
		return session, err
	}
	if purged {
		return session, ErrScreenshotsPurged
	}
	loss, err := tx.FocusEvent(ctx, session.ID, eventID)
	if err != nil {
		return session, err
	}
	if loss.Type != "focus_lost" {
		return session, ErrScreenshotIneligible
	}
	if session.LockedAt != nil {
		// The screenshots stop at the lock: only the pending uploads of the absence the student was in when the exam locked
		// are still accepted, and only the captures that were due before the lock (the lock ends the absence below).
		atLock, err := tx.FocusEventAtLock(ctx, session.ID)
		if errors.Is(err, domain.ErrNotFound) {
			return session, ErrScreenshotIneligible
		}
		if err != nil {
			return session, err
		}
		if atLock.ID != eventID {
			return session, ErrScreenshotIneligible
		}
	}
	var detail struct {
		Unconfirmed bool `json:"connection_unconfirmed"`
	}
	if err := json.Unmarshal(loss.Detail, &detail); err != nil {
		return session, err
	}
	if detail.Unconfirmed || session.LastSeen == nil {
		return session, ErrScreenshotIneligible
	}
	effective := session.GraceUntil
	if session.SubmittedAt != nil && session.SubmittedAt.Before(effective) {
		effective = *session.SubmittedAt
	}
	if !now.Before(effective) && now.After(effective.Add(PendingScreenshotWindow)) {
		return session, ErrScreenshotIneligible
	}
	until := now
	if until.After(effective) {
		until = effective
	}
	boundary, err := tx.FocusBoundary(ctx, session.ID, eventID)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return session, err
	}
	if err == nil {
		end := boundary.OccurredAt
		if boundary.Type == "network_gap" {
			var d struct {
				Since time.Time `json:"overdue_after"`
			}
			if err := json.Unmarshal(boundary.Detail, &d); err != nil {
				return session, err
			}
			if !d.Since.IsZero() && d.Since.Before(end) {
				end = d.Since
			}
		}
		if end.Before(until) {
			until = end
		}
	}
	gap := session.LastSeen.Add(policy.OverdueThreshold)
	if gap.Before(until) {
		until = gap
	}
	if session.LockedAt != nil && session.LockedAt.Before(until) {
		until = *session.LockedAt
	}
	elapsed := domain.NonnegativeElapsed(until, loss.OccurredAt)
	required := policy.FocusThreshold + time.Duration(max(index-1, 0))*policy.CaptureInterval
	if elapsed <= required {
		if errors.Is(err, domain.ErrNotFound) && until.Equal(now) && now.Before(effective) {
			return session, &ScreenshotWait{Seconds: int64((required-elapsed)/time.Second) + 1}
		}
		return session, ErrScreenshotIneligible
	}
	return session, nil
}

// readScreenshot reads an uploaded or stored PNG or JPEG within the dimension limits.
func readScreenshot(input io.Reader) ([]byte, image.Image, error) {
	data, err := io.ReadAll(io.LimitReader(input, MaxScreenshotBytes+1))
	if err != nil {
		return nil, nil, err
	}
	if int64(len(data)) > MaxScreenshotBytes {
		return nil, nil, ErrScreenshotTooLarge
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || format != "png" && format != "jpeg" {
		return nil, nil, ErrInvalidInput
	}
	if config.Width < 1 || config.Height < 1 || config.Width > MaxScreenshotDimension || config.Height > MaxScreenshotDimension || int64(config.Width)*int64(config.Height) > MaxScreenshotPixels {
		return nil, nil, ErrScreenshotTooLarge
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, nil, ErrInvalidInput
	}
	return data, img, nil
}

// storable says whether an image is small enough to keep as it is; larger desktops are scaled down by the client.
func storable(img image.Image) bool {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	return int64(w)*int64(h) <= MaxStoredPixels && max(w, h) <= MaxStoredSide
}

// Upload stores capture index of the focus loss eventID. A capture past the incident or session limit is ineligible.
func (s *Screenshots) Upload(ctx context.Context, token string, eventID int64, index int, clientAt *time.Time, input io.Reader) (ScreenshotResult, error) {
	if eventID <= 0 || index < 0 {
		return ScreenshotResult{}, ErrInvalidInput
	}
	if clientAt != nil {
		at := clientAt.UTC()
		if _, err := at.MarshalText(); err != nil {
			return ScreenshotResult{}, ErrInvalidInput
		}
		clientAt = &at
	}
	if err := s.lock(ctx); err != nil {
		return ScreenshotResult{}, err
	}
	defer s.unlock()
	var session domain.Session
	var existing domain.Screenshot
	err := s.repository.WithScreenshotTransaction(ctx, func(tx ScreenshotTransaction) error {
		var err error
		session, err = s.eligible(ctx, tx, token, eventID, index, s.sessions.clock.Now().UTC())
		if err != nil {
			return err
		}
		existing, err = tx.ScreenshotByCapture(ctx, session.ID, eventID, index)
		if errors.Is(err, domain.ErrNotFound) {
			count, err := tx.SessionScreenshotCount(ctx, session.ID)
			if err != nil {
				return err
			}
			if count >= session.Policy.SessionCaptures {
				return ErrScreenshotIneligible
			}
			return nil
		}
		return err
	})
	if err != nil {
		return ScreenshotResult{}, err
	}
	_, img, err := readScreenshot(input)
	if err != nil {
		return ScreenshotResult{}, err
	}
	if !storable(img) {
		return ScreenshotResult{}, ErrScreenshotTooLarge
	}
	if existing.ID != 0 {
		if existing.State != "ready" {
			return ScreenshotResult{}, ErrScreenshotsPurged
		}
		if _, err := s.read(existing); err != nil {
			return ScreenshotResult{}, err
		}
		return ScreenshotResult{existing.ScreenshotMetadata, true}, nil
	}
	if err := ctx.Err(); err != nil {
		return ScreenshotResult{}, err
	}
	// Re-encoding keeps only the pixels: no metadata the client put in the file is stored.
	var canonical bytes.Buffer
	if err := jpeg.Encode(&canonical, img, &jpeg.Options{Quality: storedJPEGQuality}); err != nil {
		return ScreenshotResult{}, err
	}
	if int64(canonical.Len()) > MaxScreenshotBytes {
		return ScreenshotResult{}, ErrScreenshotTooLarge
	}
	sum := sha256.Sum256(canonical.Bytes())
	record := domain.Screenshot{SessionID: session.ID, ExamID: session.ExamID, Path: screenshotPath(session.ID, eventID, index), SHA256: hex.EncodeToString(sum[:]), State: "ready", ScreenshotMetadata: domain.ScreenshotMetadata{EventLogID: eventID, CaptureIndex: index, ContentType: "image/jpeg", ScreenshotInfo: domain.ScreenshotInfo{Width: img.Bounds().Dx(), Height: img.Bounds().Dy(), Bytes: int64(canonical.Len()), ClientCapturedAt: clientAt}}}
	if err := ctx.Err(); err != nil {
		return ScreenshotResult{}, err
	}
	if err := s.writeAtomic(record, canonical.Bytes()); err != nil {
		return ScreenshotResult{}, err
	}
	err = s.repository.WithScreenshotTransaction(ctx, func(tx ScreenshotTransaction) error {
		now := s.sessions.clock.Now().UTC()
		if _, err := s.eligible(ctx, tx, token, eventID, index, now); err != nil {
			return err
		}
		record.CapturedAt = now
		id, err := tx.InsertScreenshot(ctx, record)
		record.ID = id
		return err
	})
	if err != nil {
		// A failed commit can be ambiguous; only remove the file after proving no row committed.
		check, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var found bool
		lookup := s.repository.WithScreenshotTransaction(check, func(tx ScreenshotTransaction) error {
			_, e := tx.ScreenshotByCapture(check, session.ID, eventID, index)
			found = e == nil
			if errors.Is(e, domain.ErrNotFound) {
				return nil
			}
			return e
		})
		if lookup == nil && !found {
			_ = s.remove(record.Path)
		}
		return ScreenshotResult{}, err
	}
	return ScreenshotResult{record.ScreenshotMetadata, false}, nil
}

// Get returns a stored screenshot, or a JPEG thumbnail of it, with its content type.
func (s *Screenshots) Get(ctx context.Context, id int64, thumbnail bool) ([]byte, string, error) {
	if id <= 0 {
		return nil, "", ErrInvalidInput
	}
	if err := s.lock(ctx); err != nil {
		return nil, "", err
	}
	defer s.unlock()
	var record domain.Screenshot
	err := s.repository.WithScreenshotTransaction(ctx, func(tx ScreenshotTransaction) error {
		var e error
		record, e = tx.ScreenshotByID(ctx, id)
		if e != nil {
			return e
		}
		purged, e := tx.ScreenshotsPurged(ctx, record.ExamID)
		if purged {
			return domain.ErrNotFound
		}
		return e
	})
	if err != nil {
		return nil, "", err
	}
	if record.State != "ready" {
		return nil, "", domain.ErrNotFound
	}
	data, err := s.read(record)
	if err != nil {
		return nil, "", err
	}
	if !thumbnail {
		return data, record.ContentType, nil
	}
	_, img, err := readScreenshot(bytes.NewReader(data))
	if err != nil {
		return nil, "", err
	}
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	scale := max(float64(w)/640, float64(h)/480, 1)
	tw, th := max(1, int(float64(w)/scale)), max(1, int(float64(h)/scale))
	out := image.NewRGBA(image.Rect(0, 0, tw, th))
	for y := 0; y < th; y++ {
		for x := 0; x < tw; x++ {
			out.Set(x, y, img.At(img.Bounds().Min.X+x*w/tw, img.Bounds().Min.Y+y*h/th))
		}
	}
	var b bytes.Buffer
	err = jpeg.Encode(&b, out, &jpeg.Options{Quality: thumbnailJPEGQuality})
	return b.Bytes(), "image/jpeg", err
}
func (s *Screenshots) Purge(ctx context.Context, exam string) (PurgeResult, error) {
	result := PurgeResult{ExamID: exam, Failed: []int64{}}
	if !validProctorID(exam) {
		return result, ErrInvalidInput
	}
	if err := s.lock(ctx); err != nil {
		return result, err
	}
	defer s.unlock()
	var rows []domain.Screenshot
	err := s.repository.WithScreenshotTransaction(ctx, func(tx ScreenshotTransaction) error {
		if _, err := tx.Exam(ctx, exam); err != nil {
			return err
		}
		if err := tx.MarkScreenshotPurge(ctx, exam, s.sessions.clock.Now().UTC()); err != nil {
			return err
		}
		var err error
		rows, err = tx.ScreenshotRows(ctx, exam)
		return err
	})
	if err != nil {
		return result, err
	}
	for _, record := range rows {
		if err := s.delete(ctx, record); err != nil {
			result.Failed = append(result.Failed, record.ID)
		} else {
			result.Deleted++
		}
	}
	result.Complete = len(result.Failed) == 0
	return result, nil
}
