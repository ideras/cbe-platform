package dashboard

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const (
	workspaceArchiveLimit = 1 << 30 // bytes both on the wire and after extraction
	workspaceArchiveFiles = 50_000  // the workspace snapshot limit
)

var (
	errBatchExists        = errors.New("A destination with this name already exists.")
	errBatchNoSubmission  = errors.New("No recorded submission.")
	errBatchNoFiles       = errors.New("The latest submission has no files to download.")
	errBatchNotGraded     = errors.New("The latest submission has no completed grading run.")
	errBatchNewSubmission = errors.New("The latest submission changed during the download. Run the batch again.")
	errBatchConnection    = errors.New("The server connection changed or the dashboard closed.")
)

type WorkspaceBatchIssue struct {
	Student Student `json:"student"`
	Status  string  `json:"status"`
	Reason  string  `json:"reason"`
}

type WorkspaceBatchResult struct {
	Cancelled bool                  `json:"cancelled"`
	Folder    string                `json:"folder"`
	Written   int                   `json:"written"`
	Skipped   int                   `json:"skipped"`
	Failed    int                   `json:"failed"`
	Issues    []WorkspaceBatchIssue `json:"issues"`
}

type WorkspaceBatchProgress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

// WorkspaceDownloadProgress is separate from the question-exam Markdown exporter.
func (s *Service) WorkspaceDownloadProgress() WorkspaceBatchProgress {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.workspaceBatchProgress
}

// DownloadWorkspaceBatch asks for one destination and exports the selected artifact from the entire exam.
// The exam and connection are captured before the picker opens. The caller never supplies a filesystem path.
func (s *Service) DownloadWorkspaceBatch(examID, server, kind string, overwrite bool) (WorkspaceBatchResult, error) {
	var result WorkspaceBatchResult
	suffix, title := "-files", "Download all student files"
	switch kind {
	case "files":
		if overwrite {
			return result, errors.New("Only evaluation reports can be overwritten.")
		}
	case "reports", "reports-json":
		suffix, title = "-report.md", "Download all evaluation reports"
		if kind == "reports-json" {
			suffix = "-report.json"
		}
	default:
		return result, errors.New("Invalid workspace download selection.")
	}
	s.mu.Lock()
	if s.workspaceBatchRunning {
		s.mu.Unlock()
		return result, errors.New("A workspace download is already running.")
	}
	if !validID(examID) || s.stopped || !s.view.Connected || !s.view.Loaded || s.view.Workspace == nil || s.view.ExamID != examID || s.view.Server != server {
		s.mu.Unlock()
		return result, errors.New("Load the workspace exam before starting the download.")
	}
	ctx, base, token, link, previous := s.connection, s.view.Server, s.token, s.link, s.downloadParent
	exam := ExamEntry{ID: examID}
	for _, entry := range s.view.Catalog.Exams {
		if entry.ID == examID {
			exam = entry
			break
		}
	}
	s.workspaceBatchRunning = true
	s.workspaceBatchProgress = WorkspaceBatchProgress{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.workspaceBatchRunning = false
		s.mu.Unlock()
	}()

	folder, err := s.chooseFolder(FolderDialog{Title: title, Directory: previous})
	if err != nil || folder == "" {
		result.Cancelled = err == nil
		return result, err
	}
	if err := s.stillLinked(link); err != nil {
		return result, err
	}
	folder, err = filepath.Abs(folder)
	if err != nil {
		return result, errors.New("The chosen folder is not usable.")
	}
	info, err := os.Lstat(folder)
	if err != nil || !info.IsDir() {
		return result, errors.New("Choose an existing folder, not a symbolic link.")
	}
	root, err := os.OpenRoot(folder)
	if err != nil {
		return result, errors.New("Cannot open the chosen folder.")
	}
	defer root.Close()
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		return result, errors.New("The chosen folder changed. Choose the folder again.")
	}

	var roster Roster
	if err := getJSON(ctx, s.client, base, token, http.MethodGet, "/admin/sessions?exam_id="+examID, nil, &roster); err != nil {
		return result, err
	}
	if err := validateRoster(roster, examID); err != nil {
		return result, err
	}
	if err := s.stillLinked(link); err != nil {
		return result, err
	}
	students := batchStudents(roster, suffix)
	s.mu.Lock()
	s.downloadParent = folder
	s.workspaceBatchProgress.Total = len(students)
	s.mu.Unlock()
	result.Folder = folder
	result.Issues = []WorkspaceBatchIssue{}
	// Keep the usual timeout for the roster and metadata, but allow time for the server to stream large artifacts.
	artifactClient := *s.client
	artifactClient.Timeout = max(artifactClient.Timeout, 5*time.Minute)
	for i, student := range students {
		var issue error
		status := "skipped"
		switch {
		case student.session == "":
			issue = errors.New("No session for this exam.")
		case ctx.Err() != nil:
			status, issue = "failed", errBatchConnection
		case student.problem != "":
			status, issue = "failed", errors.New(student.problem)
		default:
			// Skip existing destinations before transferring artifacts unless the proctor opted in to report replacement.
			if !overwrite {
				issue = batchDestAvailable(root, student.target)
			}
			if issue == nil {
				if kind == "files" {
					issue = s.exportWorkspaceFiles(ctx, base, token, examID, &artifactClient, root, student)
				} else {
					issue = s.exportWorkspaceReport(ctx, base, token, exam, root, student, kind, overwrite)
				}
			}
			if issue != nil && !errors.Is(issue, errBatchExists) && !errors.Is(issue, errBatchNoSubmission) && !errors.Is(issue, errBatchNoFiles) && !errors.Is(issue, errBatchNotGraded) && !errors.Is(issue, errBatchNewSubmission) {
				status = "failed"
			}
		}
		switch {
		case issue == nil:
			result.Written++
		case status == "skipped":
			result.Skipped++
		default:
			result.Failed++
		}
		if issue != nil {
			result.Issues = append(result.Issues, WorkspaceBatchIssue{Student: student.Student, Status: status, Reason: issue.Error()})
		}
		s.mu.Lock()
		s.workspaceBatchProgress.Done = i + 1
		s.mu.Unlock()
	}
	return result, nil
}

type workspaceBatchStudent struct {
	Student
	session, target, problem string
}

func batchStudents(roster Roster, suffix string) []workspaceBatchStudent {
	sessions := make(map[string]string, len(roster.Sessions))
	for _, row := range roster.Sessions {
		if _, exists := sessions[row.Account]; !exists {
			sessions[row.Account] = row.ID
		}
	}
	students := make([]workspaceBatchStudent, 0, len(sessions)+len(roster.Missing))
	stems := map[string]int{}
	for _, row := range downloadStudents(roster) {
		stem := batchNameStem(row.Name)
		stems[batchNameKey(stem)]++
		students = append(students, workspaceBatchStudent{Student: Student{Name: row.Name, Account: row.Account}, session: sessions[row.Account], target: stem})
	}
	targets := map[string]int{}
	for i := range students {
		student := &students[i]
		stem := student.target
		if !batchComponent(stem) {
			student.problem = "The roster name cannot be used as a safe destination name."
			continue
		}
		if stems[batchNameKey(stem)] > 1 {
			if !batchComponent(student.Account) {
				student.problem = "The account number cannot be used to distinguish this roster name."
				continue
			}
			stem += "-" + student.Account
		}
		student.target = stem + suffix
		if !batchComponent(student.target) {
			student.problem = "The destination name is invalid or too long."
			continue
		}
		targets[batchNameKey(student.target)]++
	}
	for i := range students {
		if targets[batchNameKey(students[i].target)] > 1 {
			students[i].problem = "The destination names still collide after adding account numbers."
		}
	}
	return students
}

func batchNameStem(name string) string {
	name = strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Mn, r) {
			return -1
		}
		return r
	}, norm.NFD.String(name))
	return norm.NFC.String(strings.Join(strings.Fields(name), "_"))
}

var batchCaseFold = cases.Fold()

func batchNameKey(name string) string {
	return norm.NFC.String(batchCaseFold.String(norm.NFC.String(strings.TrimRight(name, ". "))))
}

// Use portable components even when the local filesystem would accept a device name or treat a backslash as literal.
func batchComponent(name string) bool {
	if name == "" || len(name) > 255 || !utf8.ValidString(name) || strings.HasSuffix(name, " ") || strings.HasSuffix(name, ".") || strings.ContainsAny(name, `/\:*?"<>|`) || strings.ContainsFunc(name, unicode.IsControl) {
		return false
	}
	stem, _, _ := strings.Cut(name, ".")
	stem = strings.ToUpper(strings.TrimRight(stem, " "))
	switch stem {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		return false
	}
	for _, prefix := range []string{"COM", "LPT"} {
		if digit, ok := strings.CutPrefix(stem, prefix); ok && digit != "" && strings.Contains("123456789¹²³", digit) && utf8.RuneCountInString(digit) == 1 {
			return false
		}
	}
	return filepath.IsLocal(name)
}

func batchDestAvailable(root *os.Root, name string) error {
	_, err := batchExistingDestination(root, name, false)
	return err
}

// Return the existing spelling when replacement is allowed; never guess which file to replace if several names match.
func batchExistingDestination(root *os.Root, name string, overwrite bool) (string, error) {
	dir, err := root.Open(".")
	if err != nil {
		return "", errors.New("Cannot read the destination folder.")
	}
	defer dir.Close()
	key, existing := batchNameKey(name), ""
	for {
		entries, err := dir.Readdirnames(256)
		for _, entry := range entries {
			if batchNameKey(entry) != key {
				continue
			}
			if !overwrite {
				return "", errBatchExists
			}
			if existing != "" || !batchComponent(entry) {
				return "", errors.New("The existing report name is unsafe or ambiguous.")
			}
			existing = entry
		}
		if errors.Is(err, io.EOF) {
			return existing, nil
		}
		if err != nil {
			return "", errors.New("Cannot read the destination folder.")
		}
	}
}

func (s *Service) exportWorkspaceReport(ctx context.Context, base, token string, exam ExamEntry, root *os.Root, student workspaceBatchStudent, kind string, overwrite bool) error {
	latest, _, err := s.batchLatestSubmission(ctx, base, token, exam.ID, student.session)
	if err != nil {
		return err
	}
	gradings, run, err := s.batchReportGrading(ctx, base, token, student.session, latest)
	if err != nil {
		return err
	}
	current, ticked, err := s.batchLatestSubmission(ctx, base, token, exam.ID, student.session)
	if err != nil {
		return err
	}
	if current != latest {
		return errBatchNewSubmission
	}
	var data []byte
	if kind == "reports-json" {
		data, err = renderStudentEvaluationJSON(exam, student.Student, gradings, run, ticked)
		if err != nil {
			return err
		}
	} else {
		data = renderStudentEvaluation(exam.Title, student.Name, gradings.Todos, run, ticked)
	}
	existing, err := batchExistingDestination(root, student.target, overwrite)
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return errBatchConnection
	}
	if existing != "" {
		return replaceBatchReport(ctx, root, existing, data)
	}
	out, err := root.OpenFile(student.target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return errBatchExists
	}
	if err != nil {
		return errors.New("Cannot create the report in the chosen folder.")
	}
	info, infoErr := out.Stat()
	_, writeErr := out.Write(data)
	closeErr := out.Close()
	if ctx.Err() != nil || infoErr != nil || writeErr != nil || closeErr != nil {
		return batchRemoveFailed(root, student.target, info, false, batchContextError(ctx, errors.New("Could not finish writing the report.")))
	}
	return nil
}

func replaceBatchReport(ctx context.Context, root *os.Root, name string, data []byte) error {
	original, err := root.Lstat(name)
	if err != nil || !original.Mode().IsRegular() {
		return errors.New("The existing destination is not a regular file; it was not replaced.")
	}
	// Stage the complete new report next to the original. A bad write never truncates the existing report.
	stage := ".cbe-report-" + rand.Text() + ".tmp"
	out, err := root.OpenFile(stage, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return errors.New("Cannot stage the replacement report.")
	}
	defer root.Remove(stage)
	_, writeErr := out.Write(data)
	closeErr := out.Close()
	if ctx.Err() != nil || writeErr != nil || closeErr != nil {
		return batchContextError(ctx, errors.New("Could not finish writing the replacement report."))
	}
	current, err := root.Lstat(name)
	if err != nil || !current.Mode().IsRegular() || !os.SameFile(original, current) {
		return errors.New("The existing destination changed; it was not replaced.")
	}
	if ctx.Err() != nil {
		return errBatchConnection
	}
	// Rename replaces the directory entry without following it or modifying other names linked to the original file.
	if err := root.Rename(stage, name); err != nil {
		return errors.New("Could not replace the existing report.")
	}
	return nil
}

func (s *Service) batchReportGrading(ctx context.Context, base, token, session string, submission int64) (WorkspaceGradings, Grading, error) {
	var out WorkspaceGradings
	err := getJSON(ctx, s.client, base, token, http.MethodGet, "/admin/workspace/session/"+session+"/gradings", nil, &out)
	if err != nil {
		return out, Grading{}, batchContextError(ctx, fmt.Errorf("Could not read grading results: %w", err))
	}
	invalid := errors.New("Invalid grading response.")
	if out.SessionID != session || out.Todos == nil || out.Gradings == nil {
		return out, Grading{}, invalid
	}
	for _, todo := range out.Todos {
		if todo.ID == "" || len(todo.ID) > 256 || len(todo.Text) > 64<<10 || !finite(todo.Points) {
			return out, Grading{}, invalid
		}
	}
	// The server lists runs newest first. Use the confirmed run for this submission, or the newest completed one.
	var selected Grading
	for _, run := range out.Gradings {
		if !validGrading(run) {
			return out, Grading{}, invalid
		}
		if run.SubmissionID == submission && run.State == "done" && (selected.ID == 0 || run.Confirmed && !selected.Confirmed) {
			selected = run
		}
	}
	if selected.ID == 0 {
		return out, Grading{}, errBatchNotGraded
	}
	return out, selected, nil
}

// Read the same last recorded submission as Student Review, using the batch's captured connection, not a new link lookup.
func (s *Service) batchLatestSubmission(ctx context.Context, base, token, examID, session string) (int64, map[string]bool, error) {
	var detail struct {
		SessionID   string                `json:"session_id"`
		ExamID      string                `json:"exam_id"`
		Submissions []WorkspaceSubmission `json:"submissions"`
		Done        map[string]bool       `json:"done"`
	}
	err := getJSON(ctx, s.client, base, token, http.MethodGet, "/admin/workspace/session/"+session, nil, &detail)
	if errors.Is(err, errMissing) {
		return 0, nil, errBatchNoSubmission
	}
	if err != nil {
		return 0, nil, batchContextError(ctx, err)
	}
	if detail.SessionID != session || detail.ExamID != examID || detail.Submissions == nil {
		return 0, nil, errors.New("Invalid workspace submission list.")
	}
	if len(detail.Submissions) == 0 {
		return 0, nil, errBatchNoSubmission
	}
	latest := detail.Submissions[len(detail.Submissions)-1].ID
	if latest <= 0 {
		return 0, nil, errors.New("Invalid workspace submission ID.")
	}
	return latest, detail.Done, nil
}

func (s *Service) exportWorkspaceFiles(ctx context.Context, base, token, examID string, client *http.Client, root *os.Root, student workspaceBatchStudent) error {
	latest, _, err := s.batchLatestSubmission(ctx, base, token, examID, student.session)
	if err != nil {
		return err
	}
	path, err := workspaceKindPath("submission", latest)
	if err != nil {
		return err
	}
	response, err := request(ctx, client, base, token, http.MethodGet, path+"/zip", nil)
	if errors.Is(err, errMissing) {
		return errBatchNoFiles
	}
	if err != nil {
		return batchContextError(ctx, err)
	}
	defer response.Body.Close()
	// Stage the transient ZIP in the chosen folder, not in the system temp directory or in memory.
	stage := ".cbe-workspace-" + rand.Text() + ".tmp"
	archive, err := root.OpenFile(stage, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return errors.New("Cannot stage the submission archive in the chosen folder.")
	}
	defer root.Remove(stage)
	defer archive.Close()
	size, err := io.Copy(archive, io.LimitReader(response.Body, workspaceArchiveLimit+1))
	if err != nil || size > workspaceArchiveLimit {
		return batchContextError(ctx, errors.New("The submission archive is incomplete or too large."))
	}
	reader, err := zip.NewReader(archive, size)
	if err != nil {
		return errors.New("The submission archive is corrupt or contains an unsafe path.")
	}
	return exportWorkspaceArchive(ctx, root, student.target, reader.File)
}

func batchContextError(ctx context.Context, fallback error) error {
	if ctx.Err() != nil {
		return errBatchConnection
	}
	return fallback
}

// Remove only a destination created by this export, not anything that has replaced it since we opened it.
func batchRemoveFailed(root *os.Root, name string, created os.FileInfo, directory bool, cause error) error {
	current, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return cause
	}
	if err == nil && created != nil && os.SameFile(created, current) {
		if directory {
			err = root.RemoveAll(name)
		} else {
			err = root.Remove(name)
		}
		if err == nil {
			return cause
		}
	}
	return fmt.Errorf("%w Check the destination for unfinished output.", cause)
}

func exportWorkspaceArchive(ctx context.Context, root *os.Root, name string, archive []*zip.File) error {
	files, err := batchArchiveFiles(archive)
	if err != nil {
		return err
	}
	if err := batchDestAvailable(root, name); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return errBatchConnection
	}
	// Reserve the final name exclusively. Never replace or merge with even an empty existing folder.
	if err := root.Mkdir(name, 0o700); errors.Is(err, os.ErrExist) {
		return errBatchExists
	} else if err != nil {
		return errors.New("Cannot create the student's folder.")
	}
	info, err := root.Lstat(name)
	if err != nil || !info.IsDir() {
		return errors.New("The student's folder changed before it could be written.")
	}
	dir, err := root.OpenRoot(name)
	if err != nil {
		return batchRemoveFailed(root, name, info, true, errors.New("Cannot open the student's folder."))
	}
	opened, err := dir.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		dir.Close()
		return batchRemoveFailed(root, name, info, true, errors.New("The student's folder changed before it could be written."))
	}
	writeErr := extractWorkspaceFiles(ctx, dir, files)
	closeErr := dir.Close()
	if ctx.Err() != nil || writeErr != nil || closeErr != nil {
		return batchRemoveFailed(root, name, info, true, batchContextError(ctx, errors.New("Could not finish writing the student's files.")))
	}
	return nil
}

// Validate the entire archive before writing anything. Only regular files are exported, as in the server's ZIP.
func batchArchiveFiles(archive []*zip.File) ([]*zip.File, error) {
	type entry struct {
		path string
		dir  bool
	}
	seen := map[string]entry{}
	var size uint64
	var files []*zip.File
	if len(archive) > workspaceArchiveFiles {
		return nil, errors.New("The submission archive has too many entries.")
	}
	for _, file := range archive {
		mode := file.Mode()
		dir := mode.IsDir() && mode.Type() == os.ModeDir && strings.HasSuffix(file.Name, "/")
		name := file.Name
		if dir {
			name = strings.TrimSuffix(name, "/")
		}
		if (!dir && !mode.IsRegular()) || len(name) > 4096 || strings.Count(name, "/") > 127 {
			return nil, errors.New("The submission archive contains an unsafe path or file type.")
		}
		parts := strings.Split(name, "/")
		key := ""
		end := 0
		for i, part := range parts {
			if !batchComponent(part) {
				return nil, errors.New("The submission archive contains an unsafe path component.")
			}
			end += len(part)
			key += "/" + batchNameKey(part)
			item := entry{name[:end], dir || i < len(parts)-1}
			end++
			if old, exists := seen[key]; exists && (old != item || !item.dir) {
				return nil, errors.New("The submission archive contains colliding paths.")
			}
			seen[key] = item
		}
		if !dir {
			if file.UncompressedSize64 > workspaceArchiveLimit-size {
				return nil, errors.New("The extracted submission is too large.")
			}
			size += file.UncompressedSize64
			files = append(files, file)
		}
	}
	if len(files) == 0 {
		return nil, errBatchNoFiles
	}
	return files, nil
}

type batchReader struct {
	ctx context.Context
	io.Reader
}

func (r batchReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(p)
}

func extractWorkspaceFiles(ctx context.Context, dir *os.Root, files []*zip.File) error {
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := filepath.FromSlash(file.Name)
		if err := dir.MkdirAll(filepath.Dir(name), 0o700); err != nil {
			return err
		}
		in, err := file.Open()
		if err != nil {
			return err
		}
		out, err := dir.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			in.Close()
			return err
		}
		limit := int64(file.UncompressedSize64)
		n, copyErr := io.Copy(out, io.LimitReader(batchReader{ctx, in}, limit+1))
		closeErr := errors.Join(in.Close(), out.Close())
		if copyErr != nil || closeErr != nil || n != limit {
			return errors.New("Incomplete or corrupt submission file.")
		}
	}
	return nil
}
