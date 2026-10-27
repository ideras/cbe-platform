package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"runtime"
	"time"
)

const screenshotOwnerFile = ".exam-platform-owner"

func (s *Screenshots) claimRoot() error {
	info, err := s.root.Stat(".")
	if err != nil {
		return err
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("screenshot root must be private (chmod 700)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var id string
	if err := s.repository.WithScreenshotTransaction(ctx, func(tx ScreenshotTransaction) error { var err error; id, err = tx.ScreenshotStoreID(ctx); return err }); err != nil {
		return err
	}
	if len(id) != 32 {
		return fmt.Errorf("invalid screenshot store identity")
	}
	expected := []byte("exam-platform screenshots v1\n" + id + "\n")
	if _, err := s.root.Lstat(screenshotOwnerFile); errors.Is(err, fs.ErrNotExist) {
		dir, e := s.root.Open(".")
		if e != nil {
			return e
		}
		entries, e := dir.ReadDir(1)
		dir.Close()
		if e != nil && e != io.EOF {
			return e
		}
		if len(entries) > 0 {
			return fmt.Errorf("unowned screenshot root is not empty; audit it and choose a private empty directory")
		}
		file, e := s.root.OpenFile(screenshotOwnerFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		_, writeErr := file.Write(expected)
		syncErr := file.Sync()
		closeErr := file.Close()
		if e := errors.Join(writeErr, syncErr, closeErr); e != nil {
			return e
		}
		if e := s.syncDirectory("."); e != nil {
			return e
		}
	} else if err != nil {
		return err
	}
	owner, err := s.root.Lstat(screenshotOwnerFile)
	if err != nil {
		return err
	}
	if !owner.Mode().IsRegular() {
		return fmt.Errorf("invalid screenshot ownership marker")
	}
	file, err := s.root.Open(screenshotOwnerFile)
	if err != nil {
		return err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 129))
	if err != nil {
		return err
	}
	if string(data) != string(expected) {
		return fmt.Errorf("screenshot directory belongs to another database; do not mix image roots")
	}
	return nil
}
