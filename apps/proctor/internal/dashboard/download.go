package dashboard

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"exam-platform/shared/examdoc"
)

// Downloading the students' submissions after an exam: one Markdown file per finished student, in a folder named after the
// class and the exam, with an _index.md acting as a grading checklist. The exam is the one imported into the class; its
// Markdown source is stored on the server so each file is the exam itself with the student's answers filled in. For an exam
// without a stored source the answers are listed under their slot IDs instead.

// indexFileName is the checklist written next to the students' files.
const indexFileName = "_index.md"

// DownloadPreview is what the download dialog shows before the proctor chooses a folder.
type DownloadPreview struct {
	ExamID    string `json:"exam_id"`
	Title     string `json:"title"`
	ClassName string `json:"class_name"`
	// FolderName is the folder name offered in the dialog. The proctor may change it before downloading.
	FolderName string `json:"folder_name"`
	// Finished sessions are downloaded; Working and Finishing ones are skipped; NeverLoggedIn ones appear only in _index.md.
	Finished      int `json:"finished"`
	Working       int `json:"working"`
	Finishing     int `json:"finishing"`
	NeverLoggedIn int `json:"never_logged_in"`
	// SourceAvailable says whether the exam's Markdown is on the server. Without it the files list only the answers.
	SourceAvailable bool `json:"source_available"`
}

// DownloadSkipped names a student whose session is not finished, so no file was written for them.
type DownloadSkipped struct {
	StudentName   string `json:"student_name"`
	AccountNumber string `json:"account_number"`
	// Status is "working" or "finishing" (the student's time is over but the server has not finalized the session).
	Status string `json:"status"`
}

// DownloadFailure names a student whose file could not be fetched, rendered or written.
type DownloadFailure struct {
	StudentName   string `json:"student_name"`
	AccountNumber string `json:"account_number"`
	Error         string `json:"error"`
}

// DownloadProgress is read while a download runs, so the dialog can show how many students have been processed.
type DownloadProgress struct {
	Running bool   `json:"running"`
	Done    int    `json:"done"`
	Total   int    `json:"total"`
	Current string `json:"current,omitempty"`
}

// DownloadResult is what one download did. NeedsConfirmation is true when the folder already exists and replace was false:
// the page then asks before replacing the files the earlier download wrote.
type DownloadResult struct {
	NeedsConfirmation bool              `json:"needs_confirmation"`
	Folder            string            `json:"folder"`
	FolderName        string            `json:"folder_name"`
	ReplaceCount      int               `json:"replace_count"`
	Files             []string          `json:"files"`
	Skipped           []DownloadSkipped `json:"skipped"`
	Failed            []DownloadFailure `json:"failed"`
	Warnings          []string          `json:"warnings"`
}

type examDetail struct {
	ExamID         string          `json:"exam_id"`
	Title          string          `json:"title"`
	ClassID        string          `json:"class_id"`
	ClassName      string          `json:"class_name"`
	Navigation     string          `json:"navigation"`
	Content        examdoc.Content `json:"content"`
	SourceMarkdown *string         `json:"source_markdown"`
}

type submissionVersion struct {
	VersionID int64             `json:"version_id"`
	Sequence  int64             `json:"version_seq"`
	SavedAt   time.Time         `json:"saved_at"`
	Answers   map[string]string `json:"answers"`
}

type submission struct {
	SessionID      string             `json:"session_id"`
	ExamID         string             `json:"exam_id"`
	AccountNumber  string             `json:"account_number"`
	StudentName    string             `json:"student_name"`
	Status         string             `json:"status"`
	Final          bool               `json:"final"`
	Reason         string             `json:"reason"`
	OperatorReason string             `json:"operator_reason"`
	SubmittedAt    *time.Time         `json:"submitted_at"`
	Version        *submissionVersion `json:"version"`
}

// PrepareDownload reads the exam and its sessions and counts who will be downloaded, storing nothing.
func (s *Service) PrepareDownload(examID string) (DownloadPreview, error) {
	examID = strings.TrimSpace(examID)
	if !validID(examID) {
		return DownloadPreview{}, errors.New("Invalid exam selection.")
	}
	ctx, base, token, _, err := s.adminConnection()
	if err != nil {
		return DownloadPreview{}, err
	}
	detail, roster, err := readDownloadInputs(ctx, s, base, token, examID)
	if err != nil {
		return DownloadPreview{}, err
	}
	preview := DownloadPreview{ExamID: detail.ExamID, Title: detail.Title, ClassName: detail.ClassName,
		FolderName: downloadFolderName(detail.ClassName, detail.Title), NeverLoggedIn: len(roster.Missing), SourceAvailable: detail.SourceMarkdown != nil}
	for _, session := range roster.Sessions {
		switch {
		case session.Final != nil:
			preview.Finished++
		case unfinishedState(session) == "finishing":
			preview.Finishing++
		default:
			preview.Working++
		}
	}
	return preview, nil
}

// ChooseDownloadFolder asks the proctor for the parent folder of the next download and remembers it, so the page never names
// a path itself. An empty path means the dialog was cancelled. It stores nothing else and starts no download.
func (s *Service) ChooseDownloadFolder() (string, error) {
	s.mu.Lock()
	busy, previous := s.view.Busy, s.downloadParent
	s.mu.Unlock()
	if busy {
		return "", errors.New("Wait for the current action to finish.")
	}
	path, err := s.chooseFolder(FolderDialog{Title: "Choose where to create the output folder", Directory: previous})
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", nil
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", errors.New("The chosen folder is not usable.")
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.IsDir() {
		return "", errors.New("Choose an existing folder.")
	}
	s.mu.Lock()
	s.downloadParent = absolute
	s.mu.Unlock()
	return absolute, nil
}

// DownloadProgress reports how far the running download has got. It stays available after the download finishes, with
// Running false and Done equal to Total.
func (s *Service) DownloadProgress() DownloadProgress {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.downloadProgress
}

func (s *Service) setDownloadProgress(progress DownloadProgress) {
	s.mu.Lock()
	s.downloadProgress = progress
	s.mu.Unlock()
}

// DownloadSubmissions writes one Markdown file per finished student, plus _index.md, in the folder folderName inside the
// parent folder chosen with ChooseDownloadFolder. It refuses to replace an existing folder until replace is true, because the
// instructor may have written grading notes in those files; it never deletes a file it did not write. Progress is reported
// through DownloadProgress while it runs.
func (s *Service) DownloadSubmissions(examID, folderName string, replace, confirmed bool) (DownloadResult, error) {
	examID, folderName = strings.TrimSpace(examID), strings.TrimSpace(folderName)
	if !confirmed {
		return DownloadResult{}, errors.New("Explicit confirmation is required; nothing was sent.")
	}
	if !validID(examID) {
		return DownloadResult{}, errors.New("Invalid exam selection; nothing was sent.")
	}
	if problem := downloadFolderProblem(folderName); problem != "" {
		return DownloadResult{}, errors.New(problem)
	}
	s.mu.Lock()
	listed := false
	for _, exam := range s.view.Catalog.Exams {
		listed = listed || exam.ID == examID
	}
	parent := s.downloadParent
	s.mu.Unlock()
	if !listed {
		return DownloadResult{}, errors.New("That exam is not on the server's list; nothing was sent. Look at the exam list again.")
	}
	if parent == "" {
		return DownloadResult{}, errors.New("Choose a folder for the download first.")
	}
	ctx, base, token, err := s.beginAdmin()
	if err != nil {
		return DownloadResult{}, err
	}
	defer s.endAction()
	// A poll may reach the service before the slower server reads finish; say a download is preparing instead of showing the
	// previous download's completed progress.
	s.setDownloadProgress(DownloadProgress{Running: true})
	started := false
	defer func() {
		if !started {
			s.setDownloadProgress(DownloadProgress{})
		}
	}()
	detail, roster, err := readDownloadInputs(ctx, s, base, token, examID)
	if err != nil {
		return DownloadResult{}, err
	}
	students := downloadStudents(roster)
	names := studentFileNames(students)
	folder := filepath.Join(parent, folderName)
	existing, exists, err := existingDownloadFiles(folder, names)
	if err != nil {
		return DownloadResult{}, err
	}
	if exists && !replace {
		return DownloadResult{NeedsConfirmation: true, Folder: folder, FolderName: folderName, ReplaceCount: len(existing)}, nil
	}
	if err := os.MkdirAll(folder, 0o700); err != nil {
		return DownloadResult{}, errors.New("Cannot create the download folder.")
	}
	total, done := 0, 0
	for _, session := range roster.Sessions {
		if session.Final != nil {
			total++
		}
	}
	started = true
	s.setDownloadProgress(DownloadProgress{Running: true, Total: total})
	defer func() { s.setDownloadProgress(DownloadProgress{Total: total, Done: done}) }()
	now := time.Now()
	result := DownloadResult{Folder: folder, FolderName: folderName, Files: []string{}, Skipped: []DownloadSkipped{}, Failed: []DownloadFailure{}, Warnings: []string{}}
	fellBack := false
	for _, session := range roster.Sessions {
		if session.Final == nil {
			result.Skipped = append(result.Skipped, DownloadSkipped{StudentName: session.Name, AccountNumber: session.Account, Status: unfinishedState(session)})
			continue
		}
		s.setDownloadProgress(DownloadProgress{Running: true, Total: total, Done: done, Current: session.Name})
		sub, err := readSubmission(ctx, s, base, token, session.ID)
		if err != nil {
			result.Failed = append(result.Failed, DownloadFailure{StudentName: session.Name, AccountNumber: session.Account, Error: err.Error()})
			done++
			continue
		}
		if sub.SessionID != session.ID || sub.ExamID != examID || !sub.Final || sub.SubmittedAt == nil || sub.Version == nil {
			result.Failed = append(result.Failed, DownloadFailure{StudentName: session.Name, AccountNumber: session.Account, Error: "The server's submission is incomplete."})
			done++
			continue
		}
		name := names[sub.AccountNumber]
		if name == "" {
			name = normalizeFileName(sub.StudentName, sub.AccountNumber) + ".md"
		}
		data, reason, err := renderSubmission(detail, sub, session.Locks, now)
		if err != nil {
			result.Failed = append(result.Failed, DownloadFailure{StudentName: session.Name, AccountNumber: session.Account, Error: err.Error()})
			done++
			continue
		}
		if reason != "" && !fellBack {
			fellBack = true
			result.Warnings = append(result.Warnings, fmt.Sprintf("This exam's stored Markdown no longer matches its stored answers (%s); its files list the answers without the questions.", reason))
		}
		if err := writePrivateFile(filepath.Join(folder, name), data); err != nil {
			result.Failed = append(result.Failed, DownloadFailure{StudentName: session.Name, AccountNumber: session.Account, Error: "Cannot write the file."})
			done++
			continue
		}
		result.Files = append(result.Files, name)
		done++
	}
	// Open folder is for the folder that holds students' files. A download that wrote none (nobody finished, or every one
	// failed) leaves the folder of the earlier download in place.
	if len(result.Files) > 0 {
		s.mu.Lock()
		s.downloadFolder = folder
		s.mu.Unlock()
	}
	index, err := renderIndex(detail, roster, names, now)
	if err != nil {
		result.Failed = append(result.Failed, DownloadFailure{StudentName: indexFileName, Error: err.Error()})
	} else if err := writePrivateFile(filepath.Join(folder, indexFileName), index); err != nil {
		result.Failed = append(result.Failed, DownloadFailure{StudentName: indexFileName, Error: "Cannot write the file."})
	}
	return result, nil
}

// downloadFolderProblem says what is wrong with a proctor-chosen output folder name, or "" when it is a safe single path
// segment. The name is used as typed; only names that escapes the chosen parent or that a file system refuses are rejected.
func downloadFolderProblem(name string) string {
	switch {
	case name == "":
		return "Enter a name for the output folder."
	case name == "." || name == "..":
		return "The output folder name must be a real name, not . or .."
	case len(name) > 255:
		return "The output folder name must be at most 255 bytes."
	case !utf8.ValidString(name) || strings.ContainsFunc(name, unicode.IsControl):
		return "The output folder name must be plain text."
	case strings.ContainsAny(name, `/\:*?"<>|`):
		return `The output folder name cannot contain / \ : * ? " < > or |.`
	case strings.HasSuffix(name, ".") || strings.HasSuffix(name, " "):
		return "The output folder name cannot end with a dot or a space."
	}
	base := name
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	upper := strings.ToUpper(base)
	switch upper {
	case "CON", "PRN", "AUX", "NUL":
		return "That name is reserved by Windows; choose another one."
	}
	if len(upper) == 4 && (strings.HasPrefix(upper, "COM") || strings.HasPrefix(upper, "LPT")) && upper[3] >= '1' && upper[3] <= '9' {
		return "That name is reserved by Windows; choose another one."
	}
	return ""
}

// OpenDownloadFolder opens the folder of the last download in the system's file manager. It takes no path from the page, so
// the page cannot make the dashboard open arbitrary folders, and it needs no server connection.
func (s *Service) OpenDownloadFolder() error {
	s.mu.Lock()
	folder := s.downloadFolder
	stopped := s.stopped
	s.mu.Unlock()
	if stopped {
		return errors.New("Dashboard is closing.")
	}
	if folder == "" {
		return errors.New("No submissions have been downloaded in this run of the dashboard.")
	}
	info, err := os.Stat(folder)
	if err != nil || !info.IsDir() {
		return errors.New("The download folder is no longer there.")
	}
	if s.opts.OpenFolder == nil {
		return errors.New("Opening a folder is not available in this build.")
	}
	if err := s.opts.OpenFolder(folder); err != nil {
		return errors.New("The download folder could not be opened.")
	}
	return nil
}

// chooseFolder opens the native folder dialog once at a time; an empty path means cancelled.
func (s *Service) chooseFolder(dialog FolderDialog) (string, error) {
	if s.opts.ChooseFolder == nil {
		return "", errors.New("Choosing a folder is not available in this build.")
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return "", errors.New("Dashboard is closing.")
	}
	if s.picking {
		s.mu.Unlock()
		return "", errors.New("A file dialog is already open.")
	}
	s.picking = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.picking = false; s.mu.Unlock() }()
	path, err := s.opts.ChooseFolder(dialog)
	if err != nil {
		return "", errors.New("The folder dialog failed.")
	}
	return path, nil
}

func readDownloadInputs(ctx context.Context, s *Service, base, token, examID string) (examDetail, Roster, error) {
	var detail examDetail
	if err := getJSON(ctx, s.client, base, token, "GET", "/admin/exam/"+examID, nil, &detail); err != nil {
		return examDetail{}, Roster{}, err
	}
	var roster Roster
	if err := getJSON(ctx, s.client, base, token, "GET", "/admin/sessions?exam_id="+examID, nil, &roster); err != nil {
		return examDetail{}, Roster{}, err
	}
	if detail.ExamID != examID || strings.TrimSpace(detail.Title) == "" || strings.TrimSpace(detail.ClassName) == "" || roster.ExamID != examID {
		return examDetail{}, Roster{}, errors.New("The server's answer was invalid. Look at the exam list and try again.")
	}
	return detail, roster, nil
}

func readSubmission(ctx context.Context, s *Service, base, token, sessionID string) (submission, error) {
	var sub submission
	if err := getJSON(ctx, s.client, base, token, "GET", "/admin/session/"+sessionID+"/submission", nil, &sub); err != nil {
		return submission{}, err
	}
	return sub, nil
}

// unfinishedState says how a session that is not final is reported.
func unfinishedState(session Session) string {
	if session.FinalizationPending || session.Status == "grace" {
		return "finishing"
	}
	return "working"
}

// downloadStudents is every student of the class plus anyone with a session who has since left it, without duplicates.
func downloadStudents(roster Roster) []downloadStudent {
	seen := map[string]bool{}
	students := make([]downloadStudent, 0, len(roster.Sessions)+len(roster.Missing))
	for _, session := range roster.Sessions {
		if seen[session.Account] {
			continue
		}
		seen[session.Account] = true
		students = append(students, downloadStudent{Account: session.Account, Name: session.Name})
	}
	for _, student := range roster.Missing {
		if seen[student.Account] {
			continue
		}
		seen[student.Account] = true
		students = append(students, downloadStudent{Account: student.Account, Name: student.Name})
	}
	return students
}

// existingDownloadFiles lists the files this download would write that the folder already holds.
func existingDownloadFiles(folder string, names map[string]string) ([]string, bool, error) {
	info, err := os.Stat(folder)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, errors.New("Cannot read the download folder.")
	}
	if !info.IsDir() {
		return nil, true, errors.New("A file with the download folder's name is in the way. Choose another folder.")
	}
	wanted := []string{indexFileName}
	for _, name := range names {
		wanted = append(wanted, name)
	}
	existing := []string{}
	for _, name := range wanted {
		if _, err := os.Stat(filepath.Join(folder, name)); err == nil {
			existing = append(existing, name)
		}
	}
	return existing, true, nil
}

// writePrivateFile writes one of the students' files through a temporary file and renames it, so a half-written file is never
// left behind. The files hold students' names and answers, so they are private.
func writePrivateFile(path string, data []byte) error {
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		os.Remove(temporary)
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		os.Remove(temporary)
		return err
	}
	return nil
}

// renderSubmission is the student's file: the exam with their answers filled in, or (when the source is missing or no longer
// matches) the answers listed under their slot IDs. reason is set when the source was available but had to be left out.
func renderSubmission(detail examDetail, sub submission, locks []LockRecord, now time.Time) (data []byte, reason string, err error) {
	answers := map[string]string{}
	sequence := int64(0)
	if sub.Version != nil {
		answers, sequence = sub.Version.Answers, sub.Version.Sequence
	}
	if answers == nil {
		answers = map[string]string{}
	}
	header := submissionHeader(detail, sub, locks, sequence, now)
	if detail.SourceMarkdown != nil {
		filled, err := examdoc.FillChecked([]byte(*detail.SourceMarkdown), answers, detail.Content.SlotIDs(), header)
		if err == nil {
			return filled, "", nil
		}
		reason = err.Error()
	}
	filled, err := examdoc.Fallback(detail.Content, answers, header)
	if err != nil {
		return nil, "", errors.New("The answers do not match the exam's stored questions.")
	}
	return filled, reason, nil
}

func submissionHeader(detail examDetail, sub submission, locks []LockRecord, sequence int64, now time.Time) string {
	var out strings.Builder
	out.WriteString("---\n")
	fmt.Fprintf(&out, "exam: %s\n", yamlString(detail.Title))
	fmt.Fprintf(&out, "class: %s\n", yamlString(detail.ClassName))
	fmt.Fprintf(&out, "student: %s\n", yamlString(sub.StudentName))
	fmt.Fprintf(&out, "account_number: %s\n", yamlString(sub.AccountNumber))
	fmt.Fprintf(&out, "status: %s\n", yamlString(statusPhrase(sub)))
	fmt.Fprintf(&out, "submitted_at: %s\n", yamlString(localStamp(*sub.SubmittedAt)))
	fmt.Fprintf(&out, "answer_version: %d\n", sequence)
	// The locks of the exam: how many times it was locked, and, for each unlock, what the proctor said and gave back.
	fmt.Fprintf(&out, "times_locked: %d\n", lockCount(locks))
	listed := false
	for _, record := range locks {
		if record.Type != "unlocked" {
			continue
		}
		if !listed {
			out.WriteString("unlocks:\n")
			listed = true
		}
		fmt.Fprintf(&out, "  - reason: %s\n", yamlString(record.OperatorReason))
		fmt.Fprintf(&out, "    minutes_given_back: %s\n", minutesGivenBack(record.ExtendedMillis))
	}
	fmt.Fprintf(&out, "downloaded_at: %s\n", yamlString(localStamp(now)))
	out.WriteString("---")
	return out.String()
}

// minutesGivenBack is the time an unlock gave back, in minutes to one decimal: 30 seconds are 0.5, not 0.
func minutesGivenBack(millis int64) string {
	return strconv.FormatFloat(math.Round(float64(millis)/6000)/10, 'f', -1, 64)
}

// lockCount is how many times the exam was locked.
func lockCount(locks []LockRecord) int {
	count := 0
	for _, record := range locks {
		if record.Type == "locked" {
			count++
		}
	}
	return count
}

// indexHeader is the exam's and the class's front matter, without the student fields.
func indexHeader(detail examDetail, now time.Time) string {
	return "---\n" +
		"exam: " + yamlString(detail.Title) + "\n" +
		"class: " + yamlString(detail.ClassName) + "\n" +
		"downloaded_at: " + yamlString(localStamp(now)) + "\n" +
		"---"
}

// statusPhrase says how the student's session ended, in words.
func statusPhrase(sub submission) string {
	switch sub.Reason {
	case "explicit_submit":
		return "submitted by the student"
	case "grace_expired":
		return "submitted when time ran out"
	case "force_submit":
		if reason := strings.TrimSpace(sub.OperatorReason); reason != "" {
			return "submitted by the proctor: " + reason
		}
		return "submitted by the proctor"
	default:
		return "submitted"
	}
}

// indexStatus is the short status shown in _index.md.
func indexStatus(reason string) string {
	switch reason {
	case "grace_expired":
		return "time ran out"
	case "force_submit":
		return "submitted by proctor"
	default:
		return "submitted"
	}
}

// renderIndex is the grading checklist: one row per student with their status and file.
func renderIndex(detail examDetail, roster Roster, names map[string]string, now time.Time) ([]byte, error) {
	type row struct {
		name, key, account, status, submitted, file, locks string
	}
	rows := []row{}
	for _, session := range roster.Sessions {
		entry := row{name: session.Name, account: session.Account, submitted: "—"}
		switch {
		case session.Final != nil:
			entry.status = indexStatus(session.Final.Reason)
			entry.submitted = localStamp(session.Final.SubmittedAt)
			entry.file = names[session.Account]
			entry.locks = lockSummary(session.Locks)
		case unfinishedState(session) == "finishing":
			entry.status = "still finishing"
		default:
			entry.status = "still working"
		}
		rows = append(rows, entry)
	}
	for _, student := range roster.Missing {
		rows = append(rows, row{name: student.Name, account: student.Account, status: "never logged in", submitted: "—"})
	}
	for i := range rows {
		rows[i].key = nameSortKey(rows[i].name)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].key != rows[j].key {
			return rows[i].key < rows[j].key
		}
		if rows[i].name != rows[j].name {
			return rows[i].name < rows[j].name
		}
		return rows[i].account < rows[j].account
	})
	var out strings.Builder
	out.WriteString(indexHeader(detail, now))
	out.WriteString("\n\n")
	out.WriteString("Students of " + tableCell(detail.ClassName) + " in " + tableCell(detail.Title) + ", in name order. ")
	out.WriteString("Finished sessions have a file; the others are listed so every student can be checked off.\n\n")
	out.WriteString("| Student | Account | Status | Submitted | Locks | File |\n")
	out.WriteString("|---------|---------|--------|-----------|-------|------|\n")
	for _, entry := range rows {
		link := "—"
		if entry.file != "" {
			link = "[" + entry.file + "](" + entry.file + ")"
		}
		fmt.Fprintf(&out, "| %s | %s | %s | %s | %s | %s |\n", tableCell(entry.name), tableCell(entry.account), tableCell(entry.status), tableCell(entry.submitted), tableCell(entry.locks), link)
	}
	return []byte(out.String()), nil
}

// lockSummary is what the checklist says about the locks of one exam: how many times, and why.
func lockSummary(locks []LockRecord) string {
	count := 0
	reasons := []string{}
	for _, record := range locks {
		if record.Type != "locked" {
			continue
		}
		count++
		reason := record.Reason
		if reason == "no_contact" {
			reason = "no contact"
		} else if reason == "" {
			reason = "away"
		}
		reasons = append(reasons, reason)
	}
	if count == 0 {
		return "—"
	}
	return fmt.Sprintf("%d (%s)", count, strings.Join(reasons, ", "))
}

// tableCell keeps a value from ending or splitting a GFM table row.
func tableCell(value string) string {
	value = strings.ReplaceAll(value, "\\", `\\`)
	value = strings.ReplaceAll(value, "|", `\|`)
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	if strings.TrimSpace(value) == "" {
		return "—"
	}
	return value
}

// yamlString quotes a front matter value, escaping what YAML reads specially inside double quotes.
func yamlString(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	return `"` + value + `"`
}

// localStamp is a time in the dashboard's time zone with its offset, like the rest of the dashboard.
func localStamp(t time.Time) string { return t.Local().Format("2006-01-02 15:04:05 -07:00") }
