package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

var hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Blobs stores content by its SHA-256 under one folder of the exam server: a file's content once, whatever number of snapshots hold it,
// and the starter and grading bundles and the snapshots' manifests too. A blob is written to a temporary file and renamed into place,
// so a reader never sees half of one, and the same content written twice is the same file.
type Blobs struct{ dir string }

// NewBlobs makes the folder and returns the store in it.
func NewBlobs(dir string) (Blobs, error) {
	if err := os.MkdirAll(filepath.Join(dir, "tmp"), 0o700); err != nil {
		return Blobs{}, err
	}
	return Blobs{dir: dir}, nil
}

func (b Blobs) path(hash string) string { return filepath.Join(b.dir, hash[:2], hash[2:]) }

// ErrTooLarge means a blob went over the limit it was written with.
var ErrTooLarge = errors.New("workspace: the content is larger than the limit")

// Put stores what r holds, at most limit bytes, and returns its hash (64 hex digits) and size.
func (b Blobs) Put(r io.Reader, limit int64) (string, int64, error) {
	temp, err := os.CreateTemp(filepath.Join(b.dir, "tmp"), "blob-")
	if err != nil {
		return "", 0, err
	}
	defer os.Remove(temp.Name())
	sum := sha256.New()
	n, err := io.Copy(io.MultiWriter(temp, sum), io.LimitReader(r, limit+1))
	if err != nil {
		temp.Close()
		return "", 0, err
	}
	if n > limit {
		temp.Close()
		return "", 0, ErrTooLarge
	}
	if err := temp.Close(); err != nil {
		return "", 0, err
	}
	hash := hex.EncodeToString(sum.Sum(nil))
	target := b.path(hash)
	if _, err := os.Stat(target); err == nil {
		return hash, n, nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return "", 0, err
	}
	if err := os.Chmod(temp.Name(), 0o600); err != nil {
		return "", 0, err
	}
	if err := os.Rename(temp.Name(), target); err != nil {
		return "", 0, err
	}
	return hash, n, nil
}

// PutBytes stores a small value.
func (b Blobs) PutBytes(data []byte) (string, error) {
	hash, _, err := b.Put(bytesReader(data), int64(len(data)))
	return hash, err
}

// Open opens a blob by its hash.
func (b Blobs) Open(hash string) (*os.File, error) {
	if !hashPattern.MatchString(hash) {
		return nil, fmt.Errorf("workspace: %q is not a blob hash", hash)
	}
	return os.Open(b.path(hash))
}

// Has says whether a blob is there.
func (b Blobs) Has(hash string) bool {
	if !hashPattern.MatchString(hash) {
		return false
	}
	_, err := os.Stat(b.path(hash))
	return err == nil
}

// Read reads a small blob whole, at most limit bytes.
func (b Blobs) Read(hash string, limit int64) ([]byte, error) {
	f, err := b.Open(hash)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, ErrTooLarge
	}
	return data, nil
}
