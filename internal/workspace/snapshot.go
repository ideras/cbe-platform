package workspace

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }

// SnapshotLimits bound what a snapshot may hold, whatever a student has made: the volume has its own quota, and these keep the exam
// server's disk and memory safe if the quota is raised.
type SnapshotLimits struct {
	MaxFiles int
	MaxBytes int64
	// MaxFileBytes is the largest single file.
	MaxFileBytes int64
}

// DefaultSnapshotLimits are above the largest /work the runner allows (500 MiB, 20,000 files).
func DefaultSnapshotLimits() SnapshotLimits {
	return SnapshotLimits{MaxFiles: 50_000, MaxBytes: 600 << 20, MaxFileBytes: 500 << 20}
}

// ErrSnapshotTooLarge means a student's files went over the snapshot limits.
var ErrSnapshotTooLarge = errors.New("workspace: the student's files are too large to snapshot")

// Entry is one path of a snapshot.
type Entry struct {
	Path string `json:"path"`
	// Type is "file", "dir" or "link".
	Type string `json:"type"`
	Mode int64  `json:"mode"`
	Size int64  `json:"size,omitempty"`
	// Hash names the content of a file in the blob store.
	Hash string `json:"hash,omitempty"`
	// Link is where a symbolic link points. It is only ever data: nothing follows it.
	Link string `json:"link,omitempty"`
}

// Manifest lists everything in a snapshot, sorted by path. Its hash identifies the snapshot's content.
type Manifest struct {
	Version int     `json:"version"`
	Entries []Entry `json:"entries"`
}

// Runner is what snapshots need of the runner: a tar stream of a path under /work (the archive endpoint, decision D14).
type Runner interface {
	Archive(ctx context.Context, session, path string) (io.ReadCloser, error)
}

// Snapshots takes and restores snapshots of a student's /work.
type Snapshots struct {
	repo   Repository
	blobs  Blobs
	runner Runner
	limits SnapshotLimits
	now    func() time.Time
}

// NewSnapshots makes the snapshot store over a blob store.
func NewSnapshots(repo Repository, blobs Blobs, runner Runner, limits SnapshotLimits, now func() time.Time) *Snapshots {
	return &Snapshots{repo: repo, blobs: blobs, runner: runner, limits: limits, now: now}
}

// safeName cleans a name from the tar of a student's files, which are theirs to name as they like: it returns the name as a
// slash-separated relative path, or "" when the entry is the folder itself or the name is one no snapshot can carry.
func safeName(name string) (string, bool) {
	name = strings.TrimPrefix(name, "./")
	name = strings.TrimSuffix(name, "/")
	if name == "" || name == "." {
		return "", true
	}
	if len(name) > 4096 || strings.HasPrefix(name, "/") || !utf8.ValidString(name) {
		return "", false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." || len(part) > 255 {
			return "", false
		}
		for _, r := range part {
			if r < ' ' || r == 0x7f || r == '\\' {
				return "", false
			}
		}
	}
	return name, true
}

func validLink(target string) bool {
	return target != "" && len(target) <= 4096 && utf8.ValidString(target) && !strings.ContainsRune(target, 0)
}

// Take reads /work through the archive endpoint and records a snapshot for the reason. A periodic snapshot of files that have not
// changed since the last one adds nothing (the last one is touched); the other reasons always leave a row, because they say why.
// created says whether a new row was made.
func (s *Snapshots) Take(ctx context.Context, session, reason string) (record SnapshotRecord, created bool, err error) {
	stream, err := s.runner.Archive(ctx, session, "/work/.")
	if err != nil {
		return SnapshotRecord{}, false, err
	}
	defer stream.Close()
	manifest, bytes, skipped, err := s.ingest(stream)
	if err != nil {
		return SnapshotRecord{}, false, err
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return SnapshotRecord{}, false, err
	}
	hash, err := s.blobs.PutBytes(encoded)
	if err != nil {
		return SnapshotRecord{}, false, err
	}
	now := s.now().UTC()
	if reason == ReasonPeriodic {
		if latest, err := s.repo.LatestSnapshot(ctx, session); err == nil && latest.ManifestHash == hash {
			return latest, false, s.repo.TouchSnapshot(ctx, session, now)
		}
	}
	files := 0
	for _, e := range manifest.Entries {
		if e.Type != "dir" {
			files++
		}
	}
	record = SnapshotRecord{SessionID: session, Reason: reason, TakenAt: now, ManifestHash: hash, Files: files, Bytes: bytes, Skipped: skipped}
	if record.ID, err = s.repo.RecordSnapshot(ctx, record); err != nil {
		return SnapshotRecord{}, false, err
	}
	return record, true, nil
}

// ingest reads the tar of a student's files into the blob store and returns the manifest. The tar comes from a filesystem the student
// controls: a name that is unsafe, a type that is not a file, a folder or a link, and a hard link whose target is not in the archive
// are left out and counted, never followed and never an error.
func (s *Snapshots) ingest(r io.Reader) (Manifest, int64, int, error) {
	reader := tar.NewReader(r)
	byPath := map[string]Entry{}
	var total int64
	skipped := 0
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Manifest{}, 0, 0, fmt.Errorf("reading the archive of /work: %w", err)
		}
		name, ok := safeName(header.Name)
		if !ok {
			skipped++
			continue
		}
		if name == "" {
			continue
		}
		if len(byPath) >= s.limits.MaxFiles {
			return Manifest{}, 0, 0, ErrSnapshotTooLarge
		}
		entry := Entry{Path: name, Mode: header.Mode & 0o777}
		switch header.Typeflag {
		case tar.TypeDir:
			entry.Type = "dir"
		case tar.TypeReg:
			if header.Size > s.limits.MaxFileBytes || total+header.Size > s.limits.MaxBytes {
				return Manifest{}, 0, 0, ErrSnapshotTooLarge
			}
			hash, size, err := s.blobs.Put(reader, s.limits.MaxFileBytes)
			if err != nil {
				if errors.Is(err, ErrTooLarge) {
					return Manifest{}, 0, 0, ErrSnapshotTooLarge
				}
				return Manifest{}, 0, 0, err
			}
			total += size
			entry.Type, entry.Size, entry.Hash = "file", size, hash
		case tar.TypeSymlink:
			if !validLink(header.Linkname) {
				skipped++
				continue
			}
			entry.Type, entry.Link = "link", header.Linkname
		case tar.TypeLink:
			// The first name of an inode is a file in the archive and the others are links to it: the content is the same.
			target, found := safeName(header.Linkname)
			prior, known := byPath[target]
			if !found || !known || prior.Type != "file" {
				skipped++
				continue
			}
			entry.Type, entry.Size, entry.Hash = "file", prior.Size, prior.Hash
			total += prior.Size
		default:
			skipped++
			continue
		}
		byPath[name] = entry
	}
	manifest := Manifest{Version: 1, Entries: make([]Entry, 0, len(byPath))}
	for _, e := range byPath {
		manifest.Entries = append(manifest.Entries, e)
	}
	sort.Slice(manifest.Entries, func(i, j int) bool { return manifest.Entries[i].Path < manifest.Entries[j].Path })
	return manifest, total, skipped, nil
}

// Manifest reads the manifest of a snapshot.
func (s *Snapshots) Manifest(ctx context.Context, id int64) (SnapshotRecord, Manifest, error) {
	record, err := s.repo.Snapshot(ctx, id)
	if err != nil {
		return SnapshotRecord{}, Manifest{}, err
	}
	data, err := s.blobs.Read(record.ManifestHash, 64<<20)
	if err != nil {
		return record, Manifest{}, err
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != record.ManifestHash {
		return record, Manifest{}, fmt.Errorf("workspace: the manifest of snapshot %d does not match its hash", id)
	}
	var manifest Manifest
	return record, manifest, json.Unmarshal(data, &manifest)
}

// WriteTar writes a snapshot as a tar archive, deterministically: the same snapshot is always the same bytes. Files are owned by
// root here; whoever extracts them decides the owner. Links are written as links and are never followed.
func (s *Snapshots) WriteTar(ctx context.Context, id int64, w io.Writer) error {
	record, manifest, err := s.Manifest(ctx, id)
	if err != nil {
		return err
	}
	out := tar.NewWriter(w)
	for _, e := range manifest.Entries {
		header := &tar.Header{Name: e.Path, Mode: e.Mode, ModTime: record.TakenAt.Truncate(time.Second), Format: tar.FormatPAX}
		switch e.Type {
		case "dir":
			header.Typeflag, header.Name = tar.TypeDir, e.Path+"/"
		case "link":
			header.Typeflag, header.Linkname, header.Mode = tar.TypeSymlink, e.Link, 0o777
		default:
			header.Typeflag, header.Size = tar.TypeReg, e.Size
		}
		if err := out.WriteHeader(header); err != nil {
			return err
		}
		if e.Type == "file" {
			f, err := s.blobs.Open(e.Hash)
			if err != nil {
				return fmt.Errorf("snapshot %d: %s: %w", id, e.Path, err)
			}
			_, err = io.Copy(out, f)
			f.Close()
			if err != nil {
				return err
			}
		}
	}
	return out.Close()
}
