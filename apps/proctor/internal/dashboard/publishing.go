package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"exam-platform/shared/examdoc"
)

// The proctor chooses exam and roster files on this computer; Go reads them, asks the server to check them, and keeps
// exactly what was checked until the proctor confirms, so what is stored is what the preview showed. Exams are written in
// Markdown and compiled here (shared/examdoc); the server only ever sees the compiled document.

const (
	maxExamFile   = 4 << 20
	maxRosterFile = 4 << 20 // the server's default roster upload limit
)

var byteOrderMark = []byte("\xef\xbb\xbf")

type pendingFile struct {
	name string
	data []byte
}

// ImportPreview is what the compiler and then the server made of a chosen exam file. Cancelled means the proctor closed
// the file dialog. Navigation is what the file asks for; the proctor may change it when importing. Content is the exam as
// students will see it, for the proctor to read before importing.
type ImportPreview struct {
	Cancelled        bool            `json:"cancelled"`
	File             string          `json:"file"`
	Valid            bool            `json:"valid"`
	Problems         []string        `json:"problems"`
	Title            string          `json:"title"`
	ExerciseCount    int             `json:"exercise_count"`
	SlotCount        int             `json:"slot_count"`
	Points           *float64        `json:"points"`
	Navigation       string          `json:"navigation"`
	DurationSeconds  int64           `json:"duration_seconds"`
	GraceSeconds     int64           `json:"grace_seconds"`
	MinVolumePercent int             `json:"min_volume_percent"`
	OpensAt          *time.Time      `json:"opens_at"`
	ClosesAt         *time.Time      `json:"closes_at"`
	Content          examdoc.Content `json:"content"`
	// Calculator is the exam's calculator with every default applied; nil when the exam has none.
	Calculator *CalculatorSummary `json:"calculator"`
}

// CalculatorSummary describes the calculator an exam gives its students, for the import preview and the exam
// row. Disabled counts the names the file switched off, the inspector included.
type CalculatorSummary struct {
	Modes    []string `json:"modes"`
	Angle    string   `json:"angle"`
	Disabled int      `json:"disabled"`
	// DisabledNames are the names the file switched off, so the preview can hide a panel like the inspector's.
	DisabledNames []string `json:"disabled_names,omitempty"`
}

type ImportResult struct {
	ExamID string `json:"exam_id"`
}

// DeletedExam says which exam was deleted.
type DeletedExam struct {
	ID    string `json:"exam_id"`
	Title string `json:"title"`
}

type RosterWarning struct {
	Row     int    `json:"row"`
	Account string `json:"account_number"`
	Message string `json:"message"`
}

// ClassPreview is what the server made of a chosen class roster file. Cancelled means the proctor closed the file dialog.
type ClassPreview struct {
	Cancelled bool   `json:"cancelled"`
	File      string `json:"file"`
	// SuggestedName is the file's name without its extension, offered as the class name.
	SuggestedName string          `json:"suggested_name"`
	Valid         bool            `json:"valid"`
	Problems      []string        `json:"problems"`
	Students      int             `json:"students"`
	WarningCount  int             `json:"warning_count"`
	Warnings      []RosterWarning `json:"warnings"`
}

// RosterChange compares a roster file with the roster of a class and, once applied, says what changed. Admitting is true while
// the class is the one students are being admitted from, so students removed from it cannot log in at all.
type RosterChange struct {
	Students     int             `json:"students"`
	Added        int             `json:"added"`
	Removed      int             `json:"removed"`
	WarningCount int             `json:"warning_count"`
	Warnings     []RosterWarning `json:"warnings"`
	Applied      bool            `json:"applied"`
	Admitting    bool            `json:"admitting"`
	Pruned       int             `json:"pruned"`
}

// RosterPreview is what the server made of a chosen roster file for an existing class. Cancelled means the proctor closed the
// file dialog.
type RosterPreview struct {
	Cancelled bool     `json:"cancelled"`
	File      string   `json:"file"`
	Valid     bool     `json:"valid"`
	Problems  []string `json:"problems"`
	RosterChange
}

// DeletedClass says what deleting a class took with it.
type DeletedClass struct {
	ID       string `json:"class_id"`
	Name     string `json:"name"`
	Students int    `json:"students"`
	Pruned   int    `json:"pruned"`
}

type PublishOutcome struct {
	// ExamID and ClassID are the exam now admitting students and the class it belongs to; empty after admission was closed.
	ExamID          string `json:"exam_id"`
	ClassID         string `json:"class_id"`
	ReplacedExamID  string `json:"replaced_exam_id"`
	ReplacedClassID string `json:"replaced_class_id"`
	ActiveSessions  int    `json:"active_sessions"`
}

func readLocalFile(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("Cannot open the chosen file.")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("Choose a regular file.")
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, errors.New("Cannot read the chosen file.")
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("The file is larger than %d MiB.", limit>>20)
	}
	data = bytes.TrimPrefix(data, byteOrderMark)
	if !utf8.Valid(data) {
		return nil, errors.New("The file is not UTF-8 text. Save it as UTF-8 and choose it again.")
	}
	return data, nil
}

// adminConnection returns the live connection for read-only requests; it needs no loaded exam.
func (s *Service) adminConnection() (context.Context, string, string, uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || !s.view.Connected {
		return nil, "", "", 0, errors.New("Connect to the server first.")
	}
	return s.connection, s.view.Server, s.token, s.gen, nil
}

// beginAdmin starts a change to the server's exams or roster. Polling pauses until endAction.
func (s *Service) beginAdmin() (context.Context, string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || !s.view.Connected || s.view.Busy {
		return nil, "", "", errors.New("Connect first and wait for the current action to finish.")
	}
	s.view.Busy = true
	s.epoch++
	return s.connection, s.view.Server, s.token, nil
}

// choose opens the file dialog for a file to send to the server; it needs a connection. An empty path means cancelled.
func (s *Service) choose(dialog FileDialog) (string, error) {
	return s.pick(dialog, true)
}

// pick opens the file dialog once at a time and returns the chosen path; an empty path means cancelled.
func (s *Service) pick(dialog FileDialog, needConnection bool) (string, error) {
	if s.opts.Choose == nil {
		return "", errors.New("Choosing a file is not available in this build.")
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return "", errors.New("Dashboard is closing.")
	}
	if needConnection && !s.view.Connected {
		s.mu.Unlock()
		return "", errors.New("Connect to the server first.")
	}
	if s.picking {
		s.mu.Unlock()
		return "", errors.New("A file dialog is already open.")
	}
	s.picking = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.picking = false; s.mu.Unlock() }()
	path, err := s.opts.Choose(dialog)
	if err != nil {
		return "", errors.New("The file dialog failed.")
	}
	return path, nil
}

// definite is true when the server answered and refused, so nothing changed. Server errors and lost connections leave the
// outcome unknown.
func definite(err error) bool {
	var reject *rejection
	return errors.As(err, &reject) && reject.Status < 500
}

func problems(err error) []string {
	var reject *rejection
	if errors.As(err, &reject) && reject.Status == http.StatusBadRequest {
		if len(reject.Problems) > 0 {
			return reject.Problems
		}
		return []string{reject.Message}
	}
	return nil
}

// ChooseExam lets the proctor pick an exam Markdown file to import into the class, compiles it, and shows what the server
// makes of it, storing nothing. The file is kept for that class until ImportExam or DiscardImport.
func (s *Service) ChooseExam(classID string) (ImportPreview, error) {
	classID = strings.TrimSpace(classID)
	if !validID(classID) {
		return ImportPreview{Problems: []string{}}, errors.New("Class ID must be a canonical lowercase UUID.")
	}
	path, err := s.choose(FileDialog{Title: "Choose the exam file", FilterName: "Exam files (Markdown)", Pattern: "*.md"})
	if err != nil {
		return ImportPreview{Problems: []string{}}, err
	}
	if path == "" {
		return ImportPreview{Cancelled: true, Problems: []string{}}, nil
	}
	preview := ImportPreview{File: filepath.Base(path), Problems: []string{}}
	data, err := readLocalFile(path, maxExamFile)
	if err != nil {
		return preview, err
	}
	s.mu.Lock()
	s.pendingExam, s.pendingExamClass, s.pendingExamSource = nil, "", nil
	s.mu.Unlock()
	// The pictures of the exam are the files next to it. They are embedded in the compiled exam, so nothing depends on the
	// folder once the exam is imported.
	exam, err := examdoc.CompileDir(data, filepath.Dir(path))
	var compileProblems examdoc.Problems
	if errors.As(err, &compileProblems) {
		preview.Problems = compileProblems
		return preview, nil
	}
	if err != nil {
		return preview, err
	}
	document, err := json.Marshal(exam)
	if err != nil {
		return preview, errors.New("The exam could not be prepared for the server.")
	}
	ctx, base, token, _, err := s.adminConnection()
	if err != nil {
		return preview, err
	}
	var checked struct {
		Title            string     `json:"title"`
		ExerciseCount    int        `json:"exercise_count"`
		SlotCount        int        `json:"slot_count"`
		DurationSeconds  int64      `json:"duration_seconds"`
		GraceSeconds     int64      `json:"grace_seconds"`
		MinVolumePercent int        `json:"min_volume_percent"`
		OpensAt          *time.Time `json:"opens_at"`
		ClosesAt         *time.Time `json:"closes_at"`
	}
	// The PIN is chosen when importing, so the check runs without one.
	if err := adminSend(ctx, s.client, base, token, http.MethodPost, "/admin/class/"+classID+"/exam?dry_run=true", "application/json", document, &checked); err != nil {
		if found := problems(err); found != nil {
			preview.Problems = found
			return preview, nil
		}
		return preview, err
	}
	if checked.Title == "" || checked.ExerciseCount != len(exam.Content.Exercises) || checked.SlotCount != exam.Content.SlotCount() || checked.DurationSeconds <= 0 {
		return preview, errors.New("The server's check of the exam was invalid; nothing was imported.")
	}
	preview.Valid, preview.Navigation, preview.Content = true, exam.Navigation, exam.Content
	preview.Title, preview.ExerciseCount, preview.SlotCount = checked.Title, checked.ExerciseCount, checked.SlotCount
	if total, ok := exam.Content.Points(); ok {
		preview.Points = &total
	}
	preview.DurationSeconds, preview.GraceSeconds = checked.DurationSeconds, checked.GraceSeconds
	preview.MinVolumePercent, preview.OpensAt, preview.ClosesAt = checked.MinVolumePercent, checked.OpensAt, checked.ClosesAt
	if exam.Tools != nil && exam.Tools.On() {
		effective := exam.Tools.Effective()
		preview.Calculator = &CalculatorSummary{Modes: effective.Calculator.Modes, Angle: effective.Calculator.AngleMode, Disabled: len(effective.Calculator.DisabledFunctions), DisabledNames: effective.Calculator.DisabledFunctions}
	}
	s.mu.Lock()
	s.pendingExam, s.pendingExamClass, s.pendingExamSource = &exam, classID, data
	s.mu.Unlock()
	return preview, nil
}

// DiscardImport forgets the file shown in the exam preview.
func (s *Service) DiscardImport() {
	s.mu.Lock()
	s.pendingExam, s.pendingExamClass, s.pendingExamSource = nil, "", nil
	s.mu.Unlock()
}

// ImportExam stores the previewed exam in the class it was previewed for, with the PIN students will log in with; the exam
// belongs to that class from then on. forwardOnly closes each exercise to a student once they move on from it. It does not
// publish the exam. The previewed file is used up whatever the server answers, so an unclear failure can never import the
// same exam twice by accident.
func (s *Service) ImportExam(classID, pin string, forwardOnly bool) (ImportResult, error) {
	classID = strings.TrimSpace(classID)
	if !validID(classID) {
		return ImportResult{}, errors.New("Class ID must be a canonical lowercase UUID.")
	}
	if strings.TrimSpace(pin) == "" || len(pin) > 256 || !utf8.ValidString(pin) || strings.ContainsFunc(pin, unicode.IsControl) {
		return ImportResult{}, errors.New("Enter the PIN students will log in with: at most 256 characters, without line breaks.")
	}
	ctx, base, token, err := s.beginAdmin()
	if err != nil {
		return ImportResult{}, err
	}
	s.mu.Lock()
	pending, forClass, source := s.pendingExam, s.pendingExamClass, s.pendingExamSource
	if forClass == classID {
		s.pendingExam, s.pendingExamClass, s.pendingExamSource = nil, "", nil // used up here; a preview made for another class is left for that class
	}
	s.mu.Unlock()
	defer s.endAction()
	if pending == nil || forClass != classID {
		return ImportResult{}, errors.New("Choose an exam file for this class first.")
	}
	exam := *pending
	exam.SourceMarkdown = string(source)
	exam.PIN, exam.Navigation = pin, examdoc.FreeNavigation
	if forwardOnly {
		exam.Navigation = examdoc.ForwardOnly
	}
	document, err := json.Marshal(exam)
	if err != nil {
		return ImportResult{}, errors.New("The exam could not be prepared for the server.")
	}
	var created struct {
		ID string `json:"exam_id"`
	}
	if err := adminSend(ctx, s.client, base, token, http.MethodPost, "/admin/class/"+classID+"/exam", "application/json", document, &created); err != nil {
		if definite(err) {
			if refused(err, http.StatusNotFound) {
				return ImportResult{}, errors.New("The class is no longer on the server. Look at the class list.")
			}
			return ImportResult{}, err
		}
		return ImportResult{}, errors.New(err.Error() + " Look at the exam list before importing again.")
	}
	if !validID(created.ID) {
		return ImportResult{}, errors.New("The server's answer was invalid. Look at the exam list before importing again.")
	}
	return ImportResult{ExamID: created.ID}, nil
}

// ChooseClassFile lets the proctor pick the roster CSV of a class to add and shows what the server makes of it, storing
// nothing. The file is kept until CreateClass or DiscardClassFile.
func (s *Service) ChooseClassFile() (ClassPreview, error) {
	path, err := s.choose(FileDialog{Title: "Choose the class roster file", FilterName: "Roster files (CSV)", Pattern: "*.csv"})
	if err != nil {
		return ClassPreview{Problems: []string{}, Warnings: []RosterWarning{}}, err
	}
	if path == "" {
		return ClassPreview{Cancelled: true, Problems: []string{}, Warnings: []RosterWarning{}}, nil
	}
	base := filepath.Base(path)
	preview := ClassPreview{File: base, SuggestedName: strings.TrimSuffix(base, filepath.Ext(base)), Problems: []string{}, Warnings: []RosterWarning{}}
	data, err := readLocalFile(path, maxRosterFile)
	if err != nil {
		return preview, err
	}
	s.mu.Lock()
	s.pendingClass = nil
	s.mu.Unlock()
	ctx, server, token, _, err := s.adminConnection()
	if err != nil {
		return preview, err
	}
	var report struct {
		Students     int             `json:"students"`
		WarningCount int             `json:"warning_count"`
		Warnings     []RosterWarning `json:"warnings"`
		Applied      bool            `json:"applied"`
	}
	if err := adminSend(ctx, s.client, server, token, http.MethodPost, "/admin/classes?dry_run=true", "text/csv; charset=utf-8", data, &report); err != nil {
		if found := problems(err); found != nil {
			preview.Problems = found
			return preview, nil
		}
		return preview, err
	}
	if report.Students <= 0 || report.WarningCount < 0 || report.Applied || report.Warnings == nil {
		return preview, errors.New("The server's check of the roster was invalid; nothing was stored.")
	}
	preview.Valid, preview.Students, preview.WarningCount, preview.Warnings = true, report.Students, report.WarningCount, report.Warnings
	s.mu.Lock()
	s.pendingClass = &pendingFile{name: preview.File, data: data}
	s.mu.Unlock()
	return preview, nil
}

// DiscardClassFile forgets the file shown in the new-class preview.
func (s *Service) DiscardClassFile() {
	s.mu.Lock()
	s.pendingClass = nil
	s.mu.Unlock()
}

// CreateClass stores the previewed roster as a class called name. When the server refuses (a name already taken, say) the
// file is kept so the name can be fixed; after an unclear outcome it is dropped, so the same class is never created twice
// by accident.
func (s *Service) CreateClass(name string) (ClassEntry, error) {
	name = strings.TrimSpace(name)
	if problem := classNameProblem(name); problem != "" {
		return ClassEntry{}, errors.New(problem)
	}
	ctx, server, token, err := s.beginAdmin()
	if err != nil {
		return ClassEntry{}, err
	}
	defer s.endAction()
	s.mu.Lock()
	pending := s.pendingClass
	s.mu.Unlock()
	if pending == nil {
		return ClassEntry{}, errors.New("Choose the roster file first.")
	}
	var created struct {
		Applied bool        `json:"applied"`
		Class   *ClassEntry `json:"class"`
	}
	err = adminSend(ctx, s.client, server, token, http.MethodPost, "/admin/classes?name="+url.QueryEscape(name), "text/csv; charset=utf-8", pending.data, &created)
	if err != nil && definite(err) {
		return ClassEntry{}, err // nothing was stored; the file stays for another try
	}
	s.mu.Lock()
	s.pendingClass = nil
	s.mu.Unlock()
	if err != nil {
		return ClassEntry{}, errors.New(err.Error() + " Look at the class list before adding the class again.")
	}
	if !created.Applied || created.Class == nil || !validID(created.Class.ID) || created.Class.Name == "" {
		return ClassEntry{}, errors.New("The server's answer was invalid. Look at the class list before adding the class again.")
	}
	return *created.Class, nil
}

// ClassRoster reads the roster of one class from the server: account number and full name of each student.
func (s *Service) ClassRoster(classID string) (ClassRoster, error) {
	classID = strings.TrimSpace(classID)
	if !validID(classID) {
		return ClassRoster{}, errors.New("Class ID must be a canonical lowercase UUID.")
	}
	ctx, server, token, _, err := s.adminConnection()
	if err != nil {
		return ClassRoster{}, err
	}
	var roster ClassRoster
	if err := getJSON(ctx, s.client, server, token, http.MethodGet, "/admin/class/"+classID+"/students", nil, &roster); err != nil {
		return ClassRoster{}, err
	}
	if err := validateClassRoster(roster, classID); err != nil {
		return ClassRoster{}, err
	}
	return roster, nil
}

// RenameClass gives a class a new name, which the server checks like the name of a new class.
func (s *Service) RenameClass(classID, name string) (ClassEntry, error) {
	classID, name = strings.TrimSpace(classID), strings.TrimSpace(name)
	if !validID(classID) {
		return ClassEntry{}, errors.New("Class ID must be a canonical lowercase UUID.")
	}
	if problem := classNameProblem(name); problem != "" {
		return ClassEntry{}, errors.New(problem)
	}
	ctx, server, token, err := s.beginAdmin()
	if err != nil {
		return ClassEntry{}, err
	}
	defer s.endAction()
	body, err := json.Marshal(map[string]string{"name": name})
	if err != nil {
		return ClassEntry{}, errors.New("Cannot encode request.")
	}
	var renamed ClassEntry
	if err := adminSend(ctx, s.client, server, token, http.MethodPatch, "/admin/class/"+classID, "application/json", body, &renamed); err != nil {
		if definite(err) {
			return ClassEntry{}, err
		}
		return ClassEntry{}, errors.New(err.Error() + " Look at the class list before renaming again.")
	}
	if renamed.ID != classID || renamed.Name != name {
		return ClassEntry{}, errors.New("The server's answer was invalid. Look at the class list to see the name.")
	}
	return renamed, nil
}

// classNameProblem says what is wrong with a class name, or "" when it is fine.
func classNameProblem(name string) string {
	switch {
	case name == "":
		return "Enter a name for the class."
	case utf8.RuneCountInString(name) > 120 || !utf8.ValidString(name) || strings.ContainsFunc(name, unicode.IsControl):
		return "The class name must be plain text of at most 120 characters."
	}
	return ""
}

// DeleteClass removes a class and its roster. Sessions and answers stay. The server refuses a class that has exams: an exam
// stays in its class for good, and the ones students have taken cannot be deleted.
func (s *Service) DeleteClass(classID string, confirmed bool) (DeletedClass, error) {
	classID = strings.TrimSpace(classID)
	if !confirmed {
		return DeletedClass{}, errors.New("Explicit confirmation is required; nothing was sent.")
	}
	if !validID(classID) {
		return DeletedClass{}, errors.New("Class ID must be a canonical lowercase UUID.")
	}
	ctx, server, token, err := s.beginAdmin()
	if err != nil {
		return DeletedClass{}, err
	}
	defer s.endAction()
	var deleted DeletedClass
	if err := adminSend(ctx, s.client, server, token, http.MethodDelete, "/admin/class/"+classID, "application/json", nil, &deleted); err != nil {
		if definite(err) {
			if refused(err, http.StatusConflict) {
				return DeletedClass{}, errors.New("The class still has exams. Delete its exams first. Exams students have taken cannot be deleted.")
			}
			return DeletedClass{}, err
		}
		return DeletedClass{}, errors.New(err.Error() + " Look at the class list before deleting again.")
	}
	if deleted.ID != classID || deleted.Students < 0 || deleted.Pruned < 0 {
		return DeletedClass{}, errors.New("The server's answer was invalid. Look at the class list to see whether the class is gone.")
	}
	return deleted, nil
}

// ChooseRosterFile lets the proctor pick a roster CSV for an existing class and compares it with that class's roster,
// changing nothing. The file is kept until ReplaceClassRoster or DiscardRosterFile.
func (s *Service) ChooseRosterFile(classID string) (RosterPreview, error) {
	classID = strings.TrimSpace(classID)
	empty := RosterPreview{Problems: []string{}, RosterChange: RosterChange{Warnings: []RosterWarning{}}}
	if !validID(classID) {
		return empty, errors.New("Class ID must be a canonical lowercase UUID.")
	}
	path, err := s.choose(FileDialog{Title: "Choose the new roster file", FilterName: "Roster files (CSV)", Pattern: "*.csv"})
	if err != nil {
		return empty, err
	}
	if path == "" {
		empty.Cancelled = true
		return empty, nil
	}
	preview := empty
	preview.File = filepath.Base(path)
	data, err := readLocalFile(path, maxRosterFile)
	if err != nil {
		return preview, err
	}
	s.mu.Lock()
	s.pendingRoster, s.pendingRosterClass = nil, ""
	s.mu.Unlock()
	ctx, server, token, _, err := s.adminConnection()
	if err != nil {
		return preview, err
	}
	var change RosterChange
	if err := adminSend(ctx, s.client, server, token, http.MethodPut, "/admin/class/"+classID+"/roster?dry_run=true", "text/csv; charset=utf-8", data, &change); err != nil {
		if found := problems(err); found != nil {
			preview.Problems = found
			return preview, nil
		}
		return preview, err
	}
	if change.Students <= 0 || change.Added < 0 || change.Removed < 0 || change.WarningCount < 0 || change.Applied || change.Warnings == nil {
		return preview, errors.New("The server's check of the roster was invalid; nothing was changed.")
	}
	preview.Valid, preview.RosterChange = true, change
	s.mu.Lock()
	s.pendingRoster, s.pendingRosterClass = &pendingFile{name: preview.File, data: data}, classID
	s.mu.Unlock()
	return preview, nil
}

// DiscardRosterFile forgets the file shown in the roster preview.
func (s *Service) DiscardRosterFile() {
	s.mu.Lock()
	s.pendingRoster, s.pendingRosterClass = nil, ""
	s.mu.Unlock()
}

// ReplaceClassRoster makes the class hold exactly the students of the previewed file. Students missing from it can no longer
// log in through this class; sessions they already have keep working. When the server refuses, the file stays for another
// try; after an unclear outcome it is dropped, so the same roster is never applied twice by accident.
func (s *Service) ReplaceClassRoster(classID string, confirmed bool) (RosterChange, error) {
	classID = strings.TrimSpace(classID)
	if !confirmed {
		return RosterChange{}, errors.New("Explicit confirmation is required; nothing was sent.")
	}
	if !validID(classID) {
		return RosterChange{}, errors.New("Class ID must be a canonical lowercase UUID.")
	}
	ctx, server, token, err := s.beginAdmin()
	if err != nil {
		return RosterChange{}, err
	}
	defer s.endAction()
	s.mu.Lock()
	pending, forClass := s.pendingRoster, s.pendingRosterClass
	s.mu.Unlock()
	if pending == nil || forClass != classID {
		return RosterChange{}, errors.New("Choose the roster file for this class first.")
	}
	var change RosterChange
	err = adminSend(ctx, s.client, server, token, http.MethodPut, "/admin/class/"+classID+"/roster", "text/csv; charset=utf-8", pending.data, &change)
	if err != nil && definite(err) {
		return RosterChange{}, err
	}
	s.mu.Lock()
	s.pendingRoster, s.pendingRosterClass = nil, ""
	s.mu.Unlock()
	if err != nil {
		return RosterChange{}, errors.New(err.Error() + " Open the class and look at its roster before replacing it again.")
	}
	if !change.Applied || change.Students <= 0 {
		return RosterChange{}, errors.New("The server's answer was invalid. Open the class and look at its roster before replacing it again.")
	}
	return change, nil
}

// Publish admits the students of the exam's class to examID and monitors the exam. expectedExam is the exam the proctor was
// looking at as published ("" for none), so a stale decision changes nothing. allowActive confirms replacing an exam whose
// students are still working.
func (s *Service) Publish(examID, expectedExam string, allowActive, confirmed bool) (PublishOutcome, error) {
	examID, expectedExam = strings.TrimSpace(examID), strings.TrimSpace(expectedExam)
	if !confirmed {
		return PublishOutcome{}, errors.New("Explicit confirmation is required; nothing was sent.")
	}
	if !validID(examID) || expectedExam != "" && !validID(expectedExam) {
		return PublishOutcome{}, errors.New("Choose an exam.")
	}
	outcome, err := s.changeAdmission("/admin/exam/"+examID+"/publish", examID, expectedExam, allowActive)
	if err != nil {
		return PublishOutcome{}, err
	}
	s.mu.Lock()
	if !s.stopped && s.view.Connected {
		s.selectLocked(examID)
	}
	s.mu.Unlock()
	s.signal()
	return outcome, nil
}

// Unpublish closes admission. Students already in the exam keep their sessions.
func (s *Service) Unpublish(expectedExam string, allowActive, confirmed bool) (PublishOutcome, error) {
	expectedExam = strings.TrimSpace(expectedExam)
	if !confirmed {
		return PublishOutcome{}, errors.New("Explicit confirmation is required; nothing was sent.")
	}
	if !validID(expectedExam) {
		return PublishOutcome{}, errors.New("There is no published exam to close.")
	}
	return s.changeAdmission("/admin/unpublish", "", expectedExam, allowActive)
}

func (s *Service) changeAdmission(path, wantExam, expectedExam string, allowActive bool) (PublishOutcome, error) {
	ctx, base, token, err := s.beginAdmin()
	if err != nil {
		return PublishOutcome{}, err
	}
	defer s.endAction()
	body, err := json.Marshal(struct {
		ExpectedCurrent string `json:"expected_current"`
		AllowActive     bool   `json:"allow_active"`
	}{expectedExam, allowActive})
	if err != nil {
		return PublishOutcome{}, errors.New("Cannot encode request.")
	}
	var result struct {
		CurrentExamID   string `json:"current_exam_id"`
		CurrentClassID  string `json:"current_class_id"`
		ReplacedExamID  string `json:"replaced_exam_id"`
		ReplacedClassID string `json:"replaced_class_id"`
		ActiveSessions  int    `json:"active_sessions"`
	}
	if err := adminSend(ctx, s.client, base, token, http.MethodPost, path, "application/json", body, &result); err != nil {
		return PublishOutcome{}, err
	}
	if result.CurrentExamID != wantExam || (wantExam == "") != (result.CurrentClassID == "") || result.CurrentClassID != "" && !validID(result.CurrentClassID) || result.ActiveSessions < 0 {
		return PublishOutcome{}, errors.New("The server's answer was invalid. Refresh to see which exam is published.")
	}
	return PublishOutcome{ExamID: result.CurrentExamID, ClassID: result.CurrentClassID, ReplacedExamID: result.ReplacedExamID, ReplacedClassID: result.ReplacedClassID, ActiveSessions: result.ActiveSessions}, nil
}

// DeleteExam removes an exam that is not published and that no student has taken. The server refuses any other exam, so the
// answers and history of a taken exam are never lost.
func (s *Service) DeleteExam(examID string, confirmed bool) (DeletedExam, error) {
	examID = strings.TrimSpace(examID)
	if !confirmed {
		return DeletedExam{}, errors.New("Explicit confirmation is required; nothing was sent.")
	}
	if !validID(examID) {
		return DeletedExam{}, errors.New("Invalid exam selection; nothing was sent.")
	}
	s.mu.Lock()
	listed := false
	for _, exam := range s.view.Catalog.Exams {
		listed = listed || exam.ID == examID
	}
	s.mu.Unlock()
	if !listed {
		return DeletedExam{}, errors.New("That exam is not on the server's list; nothing was sent. Look at the exam list again.")
	}
	ctx, base, token, err := s.beginAdmin()
	if err != nil {
		return DeletedExam{}, err
	}
	defer s.endAction()
	var deleted DeletedExam
	if err := adminSend(ctx, s.client, base, token, http.MethodDelete, "/admin/exam/"+examID, "application/json", nil, &deleted); err != nil {
		if definite(err) {
			if refused(err, http.StatusConflict) {
				return DeletedExam{}, errors.New("The exam is published or students have taken it, so it cannot be deleted.")
			}
			return DeletedExam{}, err
		}
		return DeletedExam{}, errors.New(err.Error() + " Look at the exam list before deleting again.")
	}
	if deleted.ID != examID {
		return DeletedExam{}, errors.New("The server's answer was invalid. Look at the exam list to see whether the exam is gone.")
	}
	// An exam that was monitored while it admitted nobody can be deleted too; there is nothing left to monitor.
	s.mu.Lock()
	if !s.stopped && s.view.Connected && s.view.ExamID == examID {
		s.selectLocked("")
	}
	s.mu.Unlock()
	return deleted, nil
}

// refused says whether the server answered the request with this status.
func refused(err error, status int) bool {
	var reject *rejection
	return errors.As(err, &reject) && reject.Status == status
}

// Quit closes the application when the operator asks for it. Nothing on the server is changed.
func (s *Service) Quit() {
	if s.opts.Quit != nil {
		s.opts.Quit()
	}
}
