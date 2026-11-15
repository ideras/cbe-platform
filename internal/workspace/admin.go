package workspace

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"exam-platform/internal/runner"
)

// What a proctor sees of the workspaces of an exam: the roster, the files of a snapshot or a submission, the downloads and the
// transcripts. Everything here is read-only, and is reached only with the proctor's credential.

// RosterEntry is one student's workspace in the roster. TodoDone is what the student ticked: the server cannot check it.
type RosterEntry struct {
	SessionID        string     `json:"session_id"`
	Account          string     `json:"account_number"`
	State            string     `json:"state"`
	Connected        bool       `json:"connected"`
	AgentRestarts    int        `json:"agent_restarts"`
	LastActivity     *time.Time `json:"last_activity,omitempty"`
	CPUPercent       *float64   `json:"cpu_percent,omitempty"`
	MemoryMB         *int       `json:"memory_mb,omitempty"`
	TodoDone         int        `json:"todo_done"`
	TodoTotal        int        `json:"todo_total"`
	LastSnapshotAt   *time.Time `json:"last_snapshot_at,omitempty"`
	Submissions      int        `json:"submissions"`
	LastSubmissionAt *time.Time `json:"last_submission_at,omitempty"`
	Final            bool       `json:"submitted_final"`
	// Grade is the student's grade as it stands, advisory until Confirmed: the confirmed run, or the latest.
	Grade *RosterGrade `json:"grade,omitempty"`
}

// RosterGrade is a student's grade in the roster.
type RosterGrade struct {
	GradingID int64     `json:"grading"`
	State     string    `json:"state"`
	Failure   string    `json:"failure,omitempty"`
	Points    *float64  `json:"points,omitempty"`
	Max       *float64  `json:"max_points,omitempty"`
	Confirmed bool      `json:"confirmed"`
	At        time.Time `json:"at"`
}

// Roster lists the workspaces of an exam's students, those who have logged in. ErrNotFound says the exam is not a workspace exam.
func (s *Service) Roster(ctx context.Context, examID string) ([]RosterEntry, error) {
	exam, err := s.Repo.Exam(ctx, examID)
	if err != nil {
		return nil, err
	}
	rows, err := s.Repo.Roster(ctx, examID)
	if err != nil {
		return nil, err
	}
	total := len(exam.Document.Content.Todos)
	out := make([]RosterEntry, 0, len(rows))
	for _, row := range rows {
		entry := RosterEntry{SessionID: row.SessionID, Account: row.Account, State: row.State, AgentRestarts: row.AgentRestarts, LastSnapshotAt: row.LastSnapshotAt,
			TodoTotal: total, Submissions: row.Submissions, LastSubmissionAt: row.LastSubmission, Final: row.Final}
		for _, item := range exam.Document.Content.Todos {
			if row.Todo[item.ID] {
				entry.TodoDone++
			}
		}
		if g := row.Grade; g != nil {
			entry.Grade = &RosterGrade{GradingID: g.GradingID, State: g.State, Failure: g.Failure, Points: g.Points, Max: g.Max, Confirmed: g.Confirmed, At: g.At}
		}
		entry.Connected = s.Manager.Connected(row.SessionID)
		last := s.Manager.LastActivity(row.SessionID)
		if last.IsZero() && row.LastEvent != nil {
			last = *row.LastEvent
		}
		if !last.IsZero() {
			entry.LastActivity = &last
		}
		if stats, ok := s.Manager.StatsOf(row.SessionID); ok {
			memory := int(stats.MemoryBytes >> 20)
			entry.MemoryMB = &memory
			if stats.CPUKnown {
				cpu := stats.CPUPercent
				entry.CPUPercent = &cpu
			}
		}
		out = append(out, entry)
	}
	return out, nil
}

// Connected says whether the student has the workspace open now.
func (m *Manager) Connected(session string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.relays[session] != nil
}

// StatsOf is what the container used when the manager last looked (every ten seconds or so).
func (m *Manager) StatsOf(session string) (runner.Stats, bool) {
	m.statsMu.Lock()
	defer m.statsMu.Unlock()
	stats, ok := m.stats[session]
	return stats, ok && m.opts.Now().Sub(m.statsAt) < time.Minute
}

// RefreshStats asks the runner what each running container of each exam uses; the supervisor does it every ten seconds.
func (m *Manager) RefreshStats(ctx context.Context) { m.refreshStats(ctx) }

func (m *Manager) refreshStats(ctx context.Context) {
	sessions, err := m.opts.Repo.Sessions(ctx)
	if err != nil {
		return
	}
	exams := map[string]bool{}
	for _, info := range sessions {
		if info.State == StateReady {
			exams[info.ExamID] = true
		}
	}
	fresh := map[string]runner.Stats{}
	for exam := range exams {
		list, err := m.opts.Runner.ExamStats(ctx, exam)
		if err != nil {
			continue
		}
		for _, stats := range list {
			fresh[stats.Session] = stats
		}
	}
	m.statsMu.Lock()
	m.stats, m.statsAt = fresh, m.opts.Now()
	m.statsMu.Unlock()
}

// TreeEntry is a file or folder of a snapshot or a submission, as the proctor's tree lists it.
type TreeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"`
	Size int64  `json:"size,omitempty"`
	Link string `json:"link,omitempty"`
}

func treeOf(manifest Manifest) []TreeEntry {
	out := make([]TreeEntry, 0, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		out = append(out, TreeEntry{Path: entry.Path, Type: entry.Type, Size: entry.Size, Link: entry.Link})
	}
	return out
}

// SnapshotTree lists the files of a snapshot (everything the container held), and SubmissionTree those of a submission.
func (s *Service) SnapshotTree(ctx context.Context, id int64) ([]TreeEntry, error) {
	_, manifest, err := s.Manager.Snapshots.Manifest(ctx, id)
	return treeOf(manifest), err
}

func (s *Service) SubmissionTree(ctx context.Context, id int64) ([]TreeEntry, error) {
	_, manifest, err := s.Manager.SubmissionManifest(ctx, id)
	return treeOf(manifest), err
}

// FileContent is a file as the proctor's viewer shows it. A file that is not text, or is larger than a viewer should hold, comes
// without its content, and says so.
type FileContent struct {
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	Binary   bool   `json:"binary,omitempty"`
	TooLarge bool   `json:"too_large,omitempty"`
	Content  string `json:"content"`
}

// MaxViewedFile is the largest file the viewer, and the report, take in as text.
const MaxViewedFile = 1 << 20

func (m *Manager) readText(entry Entry) (FileContent, error) {
	file := FileContent{Path: entry.Path, Size: entry.Size}
	if entry.Type != "file" {
		return file, ErrNotFound
	}
	if entry.Size > MaxViewedFile {
		file.TooLarge = true
		return file, nil
	}
	data, err := m.opts.Blobs.Read(entry.Hash, MaxViewedFile+1)
	if err != nil {
		return file, err
	}
	if !utf8.Valid(data) || strings.IndexByte(string(data), 0) >= 0 {
		file.Binary = true
		return file, nil
	}
	file.Content = string(data)
	return file, nil
}

func find(manifest Manifest, name string) (Entry, bool) {
	i := sort.Search(len(manifest.Entries), func(i int) bool { return manifest.Entries[i].Path >= name })
	if i < len(manifest.Entries) && manifest.Entries[i].Path == name {
		return manifest.Entries[i], true
	}
	return Entry{}, false
}

// SnapshotFile and SubmissionFile read one file of a snapshot or a submission.
func (s *Service) SnapshotFile(ctx context.Context, id int64, name string) (FileContent, error) {
	_, manifest, err := s.Manager.Snapshots.Manifest(ctx, id)
	if err != nil {
		return FileContent{}, err
	}
	entry, ok := find(manifest, name)
	if !ok {
		return FileContent{}, ErrNotFound
	}
	return s.Manager.readText(entry)
}

func (s *Service) SubmissionFile(ctx context.Context, id int64, name string) (FileContent, error) {
	_, manifest, err := s.Manager.SubmissionManifest(ctx, id)
	if err != nil {
		return FileContent{}, err
	}
	entry, ok := find(manifest, name)
	if !ok {
		return FileContent{}, ErrNotFound
	}
	return s.Manager.readText(entry)
}

// SubmissionZip writes the files of a submission as a zip: regular files only, in the order of their names, each with the mode it had
// and the time of the submission. Symbolic links are left out (an unzipped link is a link on the proctor's computer, and where it
// points is the student's to choose); the report lists them.
func (s *Service) SubmissionZip(ctx context.Context, id int64, w io.Writer) error {
	record, manifest, err := s.Manager.SubmissionManifest(ctx, id)
	if err != nil {
		return err
	}
	archive := zip.NewWriter(w)
	for _, entry := range manifest.Entries {
		if entry.Type != "file" || path.Clean(entry.Path) != entry.Path || strings.HasPrefix(entry.Path, "../") || strings.HasPrefix(entry.Path, "/") {
			continue
		}
		header := &zip.FileHeader{Name: entry.Path, Method: zip.Deflate, Modified: record.SubmittedAt}
		header.SetMode(os.FileMode(entry.Mode & 0o777))
		out, err := archive.CreateHeader(header)
		if err != nil {
			return err
		}
		file, err := s.Manager.opts.Blobs.Open(entry.Hash)
		if err != nil {
			return err
		}
		_, err = io.Copy(out, file)
		file.Close()
		if err != nil {
			return err
		}
	}
	return archive.Close()
}

// OpenCast opens a terminal's transcript.
func (s *Service) OpenCast(ctx context.Context, id int64) (*os.File, TerminalRecord, error) {
	record, err := s.Repo.Terminal(ctx, id)
	if err != nil {
		return nil, record, err
	}
	root := s.Manager.opts.TranscriptDir
	if root == "" {
		return nil, record, ErrNotFound
	}
	full := filepath.Join(root, filepath.FromSlash(record.CastFile))
	if relative, err := filepath.Rel(root, full); err != nil || strings.HasPrefix(relative, "..") {
		return nil, record, fmt.Errorf("workspace: the transcript %d is named outside its folder", id)
	}
	file, err := os.Open(full)
	if errors.Is(err, os.ErrNotExist) {
		return nil, record, ErrNotFound
	}
	return file, record, err
}
