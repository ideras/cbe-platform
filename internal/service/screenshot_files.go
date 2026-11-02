package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"exam-platform/internal/domain"
)

func (s *Screenshots) validPath(r domain.Screenshot) bool {
	return validProctorID(r.SessionID) && r.EventLogID > 0 && r.CaptureIndex >= 0 && r.Path == screenshotPath(r.SessionID, r.EventLogID, r.CaptureIndex)
}
func (s *Screenshots) directory(name string) error {
	info, err := s.root.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		err = s.root.Mkdir(name, 0700)
		if err != nil {
			return err
		}
		if err := s.syncDirectory("."); err != nil {
			return err
		}
		info, err = s.root.Lstat(name)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("invalid screenshot session directory")
	}
	return nil
}
func (s *Screenshots) remove(name string) error {
	err := s.root.Remove(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
func (s *Screenshots) writeAtomic(record domain.Screenshot, data []byte) error {
	if !s.validPath(record) {
		return ErrInvalidInput
	}
	if err := s.directory(record.SessionID); err != nil {
		return err
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return err
	}
	temporary := record.SessionID + "/.pending-" + hex.EncodeToString(random)
	f, err := s.root.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer s.remove(temporary)
	_, writeErr := f.Write(data)
	syncErr := f.Sync()
	closeErr := f.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	if info, err := s.root.Lstat(record.Path); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("invalid existing screenshot path")
		}
		if err := s.remove(record.Path); err != nil {
			return err
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := s.root.Rename(temporary, record.Path); err != nil {
		return err
	}
	return s.syncDirectory(record.SessionID)
}
func (s *Screenshots) syncDirectory(name string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	dir, err := s.root.Open(name)
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
}
func (s *Screenshots) read(record domain.Screenshot) ([]byte, error) {
	if !s.validPath(record) {
		return nil, ErrScreenshotUnavailable
	}
	dir, err := s.root.Lstat(record.SessionID)
	if err != nil {
		return nil, err
	}
	if !dir.IsDir() || dir.Mode()&os.ModeSymlink != 0 {
		return nil, ErrScreenshotUnavailable
	}
	info, err := s.root.Lstat(record.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrScreenshotUnavailable
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() != record.Bytes || info.Size() > MaxScreenshotBytes || record.SHA256 == "" {
		return nil, ErrScreenshotUnavailable
	}
	f, err := s.root.Open(record.Path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxScreenshotBytes+1))
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	if int64(len(data)) != record.Bytes || hex.EncodeToString(sum[:]) != record.SHA256 {
		return nil, ErrScreenshotUnavailable
	}
	return data, nil
}
func (s *Screenshots) delete(ctx context.Context, record domain.Screenshot) error {
	if !s.validPath(record) {
		return fmt.Errorf("invalid stored screenshot path")
	}
	dir, err := s.root.Lstat(record.SessionID)
	if err == nil && (!dir.IsDir() || dir.Mode()&os.ModeSymlink != 0) {
		return fmt.Errorf("invalid screenshot session directory")
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := s.remove(record.Path); err != nil {
		return err
	}
	return s.repository.WithScreenshotTransaction(ctx, func(tx ScreenshotTransaction) error { return tx.DeleteScreenshot(ctx, record.ID) })
}

// deletedFileName is the shape of a screenshot file name this server writes: <session>/<event>-<capture>.jpg.
var deletedFileName = regexp.MustCompile(`^[0-9]+-[0-9]+\.jpg$`)

// RemoveDeletedFiles removes the files of screenshots whose exam was deleted. Their paths stay listed in the database until the
// file is gone, so whatever cannot be removed now is tried again by the next pass and nothing is left behind for good. It
// returns how many paths are still listed, or -1 when that could not be told, and the first reason a file stayed.
func (s *Screenshots) RemoveDeletedFiles(ctx context.Context) (int, error) {
	if err := s.lock(ctx); err != nil {
		return -1, err
	}
	defer s.unlock()
	return s.removeDeletedFiles(ctx)
}

// removeDeletedFiles is RemoveDeletedFiles for a caller that holds the lock.
func (s *Screenshots) removeDeletedFiles(ctx context.Context) (int, error) {
	var names []string
	if err := s.repository.WithScreenshotTransaction(ctx, func(tx ScreenshotTransaction) error {
		var err error
		names, err = tx.DeletedFiles(ctx)
		return err
	}); err != nil {
		return -1, err
	}
	var first error
	pending := 0
	for i, name := range names {
		if err := ctx.Err(); err != nil {
			return pending + len(names) - i, err
		}
		if err := s.removeDeletedFile(ctx, name); err != nil {
			pending++
			if first == nil {
				first = err
			}
		}
	}
	return pending, first
}

// removeDeletedFile removes one listed file and forgets its path. A name that is not a file this server writes cannot be
// addressed safely, so it is only forgotten: the orphan scan of Reconcile still removes the images of earlier versions.
func (s *Screenshots) removeDeletedFile(ctx context.Context, name string) error {
	dir := path.Dir(name)
	if validProctorID(dir) && deletedFileName.MatchString(path.Base(name)) {
		if err := s.remove(name); err != nil {
			return err
		}
		// The session is gone, so nothing writes to its folder again; an empty one is removed, a folder that is not empty stays.
		_ = s.root.Remove(dir)
	}
	return s.repository.WithScreenshotTransaction(ctx, func(tx ScreenshotTransaction) error { return tx.ForgetDeletedFile(ctx, name) })
}

type ScreenshotReconcileReport struct {
	RemovedRows    int
	RemovedOrphans int
	// RemovedDeleted counts files of deleted exams that were removed.
	RemovedDeleted int
}

func (s *Screenshots) Reconcile(ctx context.Context) (ScreenshotReconcileReport, error) {
	var report ScreenshotReconcileReport
	if err := s.lock(ctx); err != nil {
		return report, err
	}
	defer s.unlock()
	// Files of deleted exams first: they have no rows left to be found by the scan below.
	var listed int
	if err := s.repository.WithScreenshotTransaction(ctx, func(tx ScreenshotTransaction) error {
		names, err := tx.DeletedFiles(ctx)
		listed = len(names)
		return err
	}); err != nil {
		return report, err
	}
	// A file that cannot be removed is reported at the end, so it does not stop the rest of the pass.
	var stuck error
	if listed > 0 {
		pending, err := s.removeDeletedFiles(ctx)
		if pending >= 0 {
			report.RemovedDeleted = listed - pending
		}
		stuck = err
	}
	var rows []domain.Screenshot
	if err := s.repository.WithScreenshotTransaction(ctx, func(tx ScreenshotTransaction) error {
		var err error
		rows, err = tx.ScreenshotRows(ctx, "")
		return err
	}); err != nil {
		return report, err
	}
	expected := map[string]bool{}
	for _, r := range rows {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		expected[r.Path] = true
		if r.State == "deleting" {
			if err := s.delete(ctx, r); err != nil {
				return report, err
			}
			delete(expected, r.Path)
			report.RemovedRows++
			continue
		}
		_, err := s.read(r)
		if err == nil {
			continue
		}
		if !errors.Is(err, ErrScreenshotUnavailable) && !errors.Is(err, fs.ErrNotExist) {
			return report, err
		}
		if s.validPath(r) {
			if err := s.delete(ctx, r); err != nil {
				return report, err
			}
		} else {
			if err := s.repository.WithScreenshotTransaction(ctx, func(tx ScreenshotTransaction) error { return tx.DeleteScreenshot(ctx, r.ID) }); err != nil {
				return report, err
			}
		}
		delete(expected, r.Path)
		report.RemovedRows++
	}
	err := fs.WalkDir(s.root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			if name != "." && !validProctorID(name) {
				return fs.SkipDir
			}
			return nil
		}
		parent := path.Dir(name)
		if !validProctorID(parent) || expected[name] {
			return nil
		}
		base := path.Base(name)
		id, parseErr := strconv.ParseInt(strings.TrimSuffix(base, ".png"), 10, 64)
		generated := strings.HasPrefix(base, ".pending-") || (parseErr == nil && id > 0 && base == strconv.FormatInt(id, 10)+".png")
		if generated {
			if !entry.Type().IsRegular() {
				return fmt.Errorf("unexpected nonregular screenshot entry")
			}
			if err := s.remove(name); err != nil {
				return err
			}
			report.RemovedOrphans++
		}
		return nil
	})
	if err == nil {
		err = stuck
	}
	return report, err
}
func (s *Screenshots) RunWorker(ctx context.Context, interval time.Duration, onReport func(ScreenshotReconcileReport), onError func(error)) error {
	if interval <= 0 {
		return ErrInvalidInput
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			report, err := s.Reconcile(ctx)
			if err != nil {
				if onError != nil {
					onError(err)
				}
				continue
			}
			if onReport != nil {
				onReport(report)
			}
		}
	}
}
