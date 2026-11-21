package dashboard

import (
	"crypto/rand"
	"errors"
	"path/filepath"
	"strings"

	"exam-platform/shared/examdoc"
)

const workspaceManifest = "workspace.md"

// ExamImportSource describes a native selection without exposing its local path.
type ExamImportSource struct {
	Cancelled bool   `json:"cancelled"`
	ID        string `json:"id"`
	File      string `json:"file"`
	Kind      string `json:"kind"`
}

type ExamImportPreview struct {
	Traditional *ImportPreview    `json:"traditional,omitempty"`
	Workspace   *WorkspacePreview `json:"workspace,omitempty"`
}

type traditionalImport struct {
	exam   examdoc.Exam
	source []byte
}

// All draft fields are guarded by Service.mu. Only the current draft can be prepared or consumed.
type examImportDraft struct {
	id, classID string
	picking     bool
	path        string // nonempty only between picking and starting validation
	traditional *traditionalImport
	workspace   *pendingWorkspace
}

// ChooseExamSource picks one Markdown file. Detection is filename-only: a malformed workspace.md is still a workspace candidate.
func (s *Service) ChooseExamSource(classID string) (ExamImportSource, error) {
	classID = strings.TrimSpace(classID)
	if !validID(classID) {
		return ExamImportSource{}, errors.New("Class ID must be a canonical lowercase UUID.")
	}
	s.mu.Lock()
	if s.picking || (s.examImport != nil && s.examImport.picking) {
		s.mu.Unlock()
		return ExamImportSource{}, errors.New("A file dialog is already open.")
	}
	draft := &examImportDraft{id: rand.Text(), classID: classID, picking: true}
	s.examImport = draft // even a cancelled picker replaces the previous preview
	s.mu.Unlock()

	path, err := s.choose(FileDialog{Title: "Choose an exam file", FilterName: "Exam files (Markdown)", Pattern: "*.md"})
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.examImport != draft {
		return ExamImportSource{Cancelled: true}, nil
	}
	if err != nil || path == "" {
		s.examImport = nil
		return ExamImportSource{Cancelled: path == "" && err == nil}, err
	}
	file := filepath.Base(path)
	if !strings.EqualFold(filepath.Ext(file), ".md") {
		s.examImport = nil
		return ExamImportSource{}, errors.New("Choose a Markdown exam file (.md).")
	}
	kind := examdoc.KindQuestion
	if file == workspaceManifest {
		kind = examdoc.KindWorkspace
	}
	draft.path, draft.picking = path, false
	return ExamImportSource{ID: draft.id, File: file, Kind: kind}, nil
}

// PrepareExamImport validates the chosen file as the explicitly confirmed kind. It never stores the exam on the server.
func (s *Service) PrepareExamImport(classID, selectionID, kind string) (ExamImportPreview, error) {
	s.mu.Lock()
	draft := s.examImport
	if draft == nil || draft.id != selectionID || draft.classID != strings.TrimSpace(classID) || draft.path == "" || !s.view.Connected {
		s.mu.Unlock()
		return ExamImportPreview{}, errors.New("Choose an exam file for this class first.")
	}
	path := draft.path
	draft.path = "" // one validation per selection; changing the source requires a new pick
	classID = draft.classID
	ctx, base, token := s.connection, s.view.Server, s.token
	s.mu.Unlock()

	var result ExamImportPreview
	var traditional *traditionalImport
	var workspace *pendingWorkspace
	var err error
	switch kind {
	case examdoc.KindQuestion:
		var preview ImportPreview
		preview, traditional, err = s.prepareTraditionalExam(ctx, base, token, classID, path)
		result.Traditional = &preview
	case examdoc.KindWorkspace:
		var preview WorkspacePreview
		preview, workspace, err = prepareWorkspaceExam(path)
		result.Workspace = &preview
	default:
		err = errors.New("Choose Workspace Exam or Traditional Exam.")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.examImport != draft {
		return ExamImportPreview{}, errors.New("That exam selection is no longer current.")
	}
	if traditional == nil && workspace == nil {
		s.examImport = nil
	} else {
		draft.traditional, draft.workspace = traditional, workspace
	}
	return result, err
}

// DiscardExamImport cancels this exact selection or preview, without touching a newer one.
func (s *Service) DiscardExamImport(classID, selectionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if draft := s.examImport; draft != nil && draft.classID == strings.TrimSpace(classID) && draft.id == selectionID {
		s.examImport = nil
	}
}
