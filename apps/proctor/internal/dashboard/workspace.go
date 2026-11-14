package dashboard

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Workspace exams (docs/workspace/): what the dashboard shows of the students' workspaces. The roster of an exam's workspaces is read
// with the sessions, on every pass, and shown beside them; a student's workspace (its snapshots, submissions and terminal
// transcripts, with the files of each) is read when the review window asks. Nothing here changes anything on the server.

// WorkspaceStudent is one student's workspace in the roster. TodoDone is what the student ticked: the server cannot check it, and the
// dashboard says so wherever it shows it.
type WorkspaceStudent struct {
	SessionID        string     `json:"session_id"`
	Account          string     `json:"account_number"`
	State            string     `json:"state"`
	Connected        bool       `json:"connected"`
	AgentRestarts    int        `json:"agent_restarts"`
	LastActivity     *time.Time `json:"last_activity"`
	CPUPercent       *float64   `json:"cpu_percent"`
	MemoryMB         *int       `json:"memory_mb"`
	TodoDone         int        `json:"todo_done"`
	TodoTotal        int        `json:"todo_total"`
	LastSnapshotAt   *time.Time `json:"last_snapshot_at"`
	Submissions      int        `json:"submissions"`
	LastSubmissionAt *time.Time `json:"last_submission_at"`
	Final            bool       `json:"submitted_final"`
}

// WorkspaceRoster is the roster of an exam's workspaces; nil in the view when the exam is not a workspace exam.
type WorkspaceRoster struct {
	Students []WorkspaceStudent `json:"students"`
}

func validWorkspaceRoster(roster WorkspaceRoster) error {
	seen := map[string]bool{}
	for _, row := range roster.Students {
		if !validID(row.SessionID) || seen[row.SessionID] || row.State == "" || row.TodoDone < 0 || row.TodoDone > row.TodoTotal || row.Submissions < 0 {
			return errors.New("Invalid workspace roster.")
		}
		seen[row.SessionID] = true
	}
	return nil
}

// workspaceRoster reads the roster of an exam's workspaces. A server that has no such route, or an exam that is a question exam, says
// 404: that is not a failure, it is the answer "this exam has no workspaces", and it is not asked again for the exam.
func (s *Service) workspaceRoster(ctx context.Context, base, token, examID string) (roster *WorkspaceRoster, isWorkspace bool, err error) {
	s.mu.Lock()
	known := s.notWorkspace[base+"|"+examID]
	s.mu.Unlock()
	if known {
		return nil, false, nil
	}
	var read WorkspaceRoster
	err = getJSON(ctx, s.client, base, token, http.MethodGet, "/admin/workspace/exam/"+url.PathEscape(examID)+"/roster", nil, &read)
	if errors.Is(err, errMissing) {
		s.mu.Lock()
		s.notWorkspace[base+"|"+examID] = true
		s.mu.Unlock()
		return nil, false, nil
	}
	if err != nil {
		return nil, true, err
	}
	if err := validWorkspaceRoster(read); err != nil {
		return nil, true, err
	}
	return &read, true, nil
}

// WorkspaceSnapshot is a row of a student's snapshot list, WorkspaceSubmission one of the submission list, WorkspaceTerminal one of the
// transcript list.
type WorkspaceSnapshot struct {
	ID     int64     `json:"id"`
	Reason string    `json:"reason"`
	At     time.Time `json:"taken_at"`
	Files  int       `json:"files"`
	Bytes  int64     `json:"bytes"`
}
type WorkspaceSubmission struct {
	ID         int64     `json:"id"`
	SnapshotID int64     `json:"snapshot"`
	Final      bool      `json:"final"`
	At         time.Time `json:"submitted_at"`
	Files      int       `json:"files"`
	Bytes      int64     `json:"bytes"`
}
type WorkspaceTerminal struct {
	ID        int64      `json:"id"`
	Kind      string     `json:"kind"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at"`
	ExitCode  *int       `json:"exit_code"`
	Bytes     int64      `json:"bytes"`
	Truncated bool       `json:"truncated"`
}

// WorkspaceSession is what a proctor reads of one student's workspace.
type WorkspaceSession struct {
	SessionID     string                `json:"session_id"`
	State         string                `json:"state"`
	AgentRestarts int                   `json:"agent_restarts"`
	Snapshots     []WorkspaceSnapshot   `json:"snapshots"`
	Submissions   []WorkspaceSubmission `json:"submissions"`
	Terminals     []WorkspaceTerminal   `json:"terminals"`
	Done          map[string]bool       `json:"done"`
}

// WorkspaceSession reads a student's workspace: the snapshots, the submissions, the transcripts, and the TODO ticks.
func (s *Service) WorkspaceSession(sessionID string) (WorkspaceSession, error) {
	ctx, base, token, link, err := s.linkState()
	if err != nil {
		return WorkspaceSession{}, err
	}
	if !validID(sessionID) {
		return WorkspaceSession{}, errors.New("Invalid session selection.")
	}
	var out WorkspaceSession
	if err := getJSON(ctx, s.client, base, token, http.MethodGet, "/admin/workspace/session/"+sessionID, nil, &out); err != nil {
		return WorkspaceSession{}, err
	}
	if err := s.stillLinked(link); err != nil {
		return WorkspaceSession{}, err
	}
	if out.SessionID != sessionID || out.Snapshots == nil || out.Submissions == nil || out.Terminals == nil {
		return WorkspaceSession{}, errors.New("Invalid workspace response.")
	}
	return out, nil
}

// WorkspaceEntry is a file or folder in the tree of a snapshot or a submission.
type WorkspaceEntry struct {
	Path string `json:"path"`
	Type string `json:"type"`
	Size int64  `json:"size"`
	Link string `json:"link"`
}

// WorkspaceFile is a file as the viewer shows it; a file that is not text or is too big has no content and says so.
type WorkspaceFile struct {
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	Binary   bool   `json:"binary"`
	TooLarge bool   `json:"too_large"`
	Content  string `json:"content"`
}

func workspaceKindPath(kind string, id int64) (string, error) {
	if id <= 0 || (kind != "snapshot" && kind != "submission") {
		return "", errors.New("Invalid workspace selection.")
	}
	return "/admin/workspace/" + kind + "/" + strconv.FormatInt(id, 10), nil
}

// WorkspaceTree lists the files of a snapshot (everything the container held) or of a submission (what the exam submits).
func (s *Service) WorkspaceTree(kind string, id int64) ([]WorkspaceEntry, error) {
	ctx, base, token, link, err := s.linkState()
	if err != nil {
		return nil, err
	}
	path, err := workspaceKindPath(kind, id)
	if err != nil {
		return nil, err
	}
	var out struct {
		Entries []WorkspaceEntry `json:"entries"`
	}
	if err := getJSON(ctx, s.client, base, token, http.MethodGet, path+"/tree", nil, &out); err != nil {
		return nil, err
	}
	if err := s.stillLinked(link); err != nil {
		return nil, err
	}
	for _, entry := range out.Entries {
		if entry.Path == "" || entry.Path[0] == '/' || strings.Contains(entry.Path, "..") || (entry.Type != "file" && entry.Type != "dir" && entry.Type != "link") {
			return nil, errors.New("Invalid workspace tree.")
		}
	}
	if out.Entries == nil {
		out.Entries = []WorkspaceEntry{}
	}
	return out.Entries, nil
}

// WorkspaceFile reads one file of a snapshot or a submission. Its text is the student's: it is shown as text and never as markup.
func (s *Service) WorkspaceFile(kind string, id int64, name string) (WorkspaceFile, error) {
	ctx, base, token, link, err := s.linkState()
	if err != nil {
		return WorkspaceFile{}, err
	}
	path, err := workspaceKindPath(kind, id)
	if err != nil {
		return WorkspaceFile{}, err
	}
	if name == "" || len(name) > 4096 || !utf8.ValidString(name) {
		return WorkspaceFile{}, errors.New("Invalid workspace selection.")
	}
	var out WorkspaceFile
	if err := getJSON(ctx, s.client, base, token, http.MethodGet, path+"/file?path="+url.QueryEscape(name), nil, &out); err != nil {
		return WorkspaceFile{}, err
	}
	if err := s.stillLinked(link); err != nil {
		return WorkspaceFile{}, err
	}
	if out.Path != name || out.Size < 0 || (out.Binary || out.TooLarge) && out.Content != "" {
		return WorkspaceFile{}, errors.New("Invalid workspace file.")
	}
	return out, nil
}

// maxTranscript is the most of a transcript the dashboard reads: the server keeps at most 4 MiB of a terminal's output, which JSON
// escapes make a little larger.
const maxTranscript = 16 << 20

// WorkspaceTranscript reads a terminal's transcript, an asciinema v2 file, as text for the player of the review window.
func (s *Service) WorkspaceTranscript(id int64) (string, error) {
	ctx, base, token, link, err := s.linkState()
	if err != nil {
		return "", err
	}
	if id <= 0 {
		return "", errors.New("Invalid workspace selection.")
	}
	resp, err := request(ctx, s.client, base, token, http.MethodGet, "/admin/workspace/terminal/"+strconv.FormatInt(id, 10)+"/cast", nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxTranscript+1))
	if err != nil || len(data) > maxTranscript || !utf8.Valid(data) || !strings.HasPrefix(string(data), `{"version":2`) {
		return "", errors.New("Transcript response invalid or too large.")
	}
	if err := s.stillLinked(link); err != nil {
		return "", err
	}
	return string(data), nil
}

// downloadName is a file name from a student's name and account, as the submission downloads name theirs.
func downloadName(name, account, extension string) string {
	return normalizeFileName(name, account) + extension
}

func (s *Service) rosterName(sessionID string) (string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, row := range s.view.Roster.Sessions {
		if row.ID == sessionID {
			return row.Student.Name, row.Student.Account
		}
	}
	return "", ""
}

// SaveWorkspaceZip writes the files of a submission to a zip the proctor chooses, and returns its path, or "" when they cancel.
func (s *Service) SaveWorkspaceZip(sessionID string, submissionID int64) (string, error) {
	return s.saveWorkspace(sessionID, "/admin/workspace/submission/"+strconv.FormatInt(submissionID, 10)+"/zip", submissionID > 0, "-files.zip", "Zip archive", "*.zip", "Save submitted files", 1<<30)
}

// SaveWorkspaceReport writes the Markdown report of a student's latest submission to a file the proctor chooses.
func (s *Service) SaveWorkspaceReport(sessionID string) (string, error) {
	return s.saveWorkspace(sessionID, "/admin/workspace/session/"+sessionID+"/report", true, "-report.md", "Markdown", "*.md", "Save report", 32<<20)
}

func (s *Service) saveWorkspace(sessionID, path string, valid bool, suffix, filter, pattern, title string, limit int64) (string, error) {
	if s.opts.Save == nil {
		return "", errors.New("Saving files is not available in this build.")
	}
	ctx, base, token, link, err := s.linkState()
	if err != nil {
		return "", err
	}
	if !validID(sessionID) || !valid {
		return "", errors.New("Invalid workspace selection.")
	}
	name, account := s.rosterName(sessionID)
	if account == "" {
		name, account = "student", sessionID[:8]
	}
	target, err := s.pickSave(SaveDialog{Title: title, Filename: strings.TrimSuffix(downloadName(name, account, ""), ".md") + suffix, FilterName: filter, Pattern: pattern})
	if err != nil || target == "" {
		return "", err
	}
	resp, err := request(ctx, s.client, base, token, http.MethodGet, path, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	// What is written is written beside its final name first, so a download that fails half way leaves no half file under the name the
	// proctor chose.
	temporary, err := os.CreateTemp(filepath.Dir(target), ".download-*")
	if err != nil {
		return "", errors.New("The file could not be written there.")
	}
	defer os.Remove(temporary.Name())
	written, copyErr := io.Copy(temporary, io.LimitReader(resp.Body, limit+1))
	closeErr := temporary.Close()
	if copyErr != nil || closeErr != nil || written > limit {
		return "", errors.New("The download failed or is larger than the dashboard takes.")
	}
	if err := s.stillLinked(link); err != nil {
		return "", err
	}
	if err := os.Chmod(temporary.Name(), 0o600); err != nil {
		return "", errors.New("The file could not be written there.")
	}
	if err := os.Rename(temporary.Name(), target); err != nil {
		return "", errors.New("The file could not be written there.")
	}
	return target, nil
}
