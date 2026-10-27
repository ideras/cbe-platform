package service

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"exam-platform/internal/clock"
	"exam-platform/internal/domain"
	"exam-platform/shared/calc"
)

var (
	ErrInvalidInput   = errors.New("invalid request")
	ErrAnswerTooLarge = errors.New("answer size limit exceeded")
	ErrSessionLocked  = errors.New("session is locked")
	// ErrSessionOnHold means the exam is locked: the student may not see or answer it until a proctor unlocks it.
	ErrSessionOnHold = errors.New("the exam is locked until your instructor unlocks it")
)

type ExamRepository interface {
	// CreateExam stores the exam in exam.ClassID, which must be a stored class (domain.ErrNotFound otherwise).
	CreateExam(context.Context, domain.Exam) error
	// DeleteExam removes an exam that is not published and that nobody has taken (domain.ErrExamInUse otherwise).
	DeleteExam(context.Context, string) (domain.DeletedExam, error)
	// ExamDetail reads an exam with its class name and stored Markdown source (domain.ErrNotFound otherwise).
	ExamDetail(context.Context, string) (domain.ExamDetail, error)
}

type Exams struct {
	repository ExamRepository
	clock      clock.Clock
	// grace is where the default grace period of new exams comes from: the proctor's settings.
	grace GraceProvider
}

// GraceProvider is where the default grace period of new exams comes from: the proctor's settings.
type GraceProvider interface {
	// DefaultGraceSeconds is the grace period an exam gets when its file names none.
	DefaultGraceSeconds(context.Context) (int64, error)
}

// DefaultMinVolumePercent applies to exams created without an explicit volume.
const DefaultMinVolumePercent = 80

// MaxSourceMarkdownBytes bounds the Markdown source kept with an exam; it is the proctor's exam file limit.
const MaxSourceMarkdownBytes = 4 << 20

type CreateExamInput struct {
	Title           string `json:"title"`
	PIN             string `json:"pin"`
	DurationSeconds int64  `json:"duration_seconds"`
	GraceSeconds    *int64 `json:"grace_seconds,omitempty"`
	// MinVolumePercent (0-100) is the lowest system volume the student application keeps during the exam; 0 leaves the student's volume alone.
	MinVolumePercent *int `json:"min_volume_percent,omitempty"`
	// Navigation is free (the default) or forward_only.
	Navigation domain.Navigation `json:"navigation,omitempty"`
	// SourceMarkdown is the exam file's Markdown, stored as opaque text so the proctor can download filled-in copies of the
	// exam. It is optional; an import without it leaves the exam's questions out of downloaded submissions.
	SourceMarkdown *string `json:"source_markdown,omitempty"`
	// Tools is what the exam file's calculator key declared, already compiled by shared/examdoc (D8). The server
	// validates it with shared/calc; a config it does not understand is refused, never stored.
	Tools    *calc.Tools        `json:"tools,omitempty"`
	Content  domain.ExamContent `json:"content"`
	OpensAt  *time.Time         `json:"opens_at,omitempty"`
	ClosesAt *time.Time         `json:"closes_at,omitempty"`
}

// NewExams builds the exam service; new exams without a grace period of their own take grace's default.
func NewExams(repository ExamRepository, clock clock.Clock, grace GraceProvider) *Exams {
	return &Exams{repository: repository, clock: clock, grace: grace}
}

// ExamPreview is the validated shape of an exam document. It never carries the PIN or the exam's text.
type ExamPreview struct {
	Title            string            `json:"title"`
	ExerciseCount    int               `json:"exercise_count"`
	SlotCount        int               `json:"slot_count"`
	Navigation       domain.Navigation `json:"navigation"`
	DurationSeconds  int64             `json:"duration_seconds"`
	GraceSeconds     int64             `json:"grace_seconds"`
	MinVolumePercent int               `json:"min_volume_percent"`
	OpensAt          *time.Time        `json:"opens_at"`
	ClosesAt         *time.Time        `json:"closes_at"`
}

// Preview validates an exam document as Create would, without storing it. The PIN may be left out: the proctor checks the
// exam file before choosing the PIN.
func (s *Exams) Preview(ctx context.Context, input CreateExamInput) (ExamPreview, error) {
	exam, err := s.build(ctx, input, input.PIN == "")
	if err != nil {
		return ExamPreview{}, err
	}
	slots := len(exam.Content.SlotExercises())
	return ExamPreview{Title: exam.Title, ExerciseCount: len(exam.Content.Exercises), SlotCount: slots, Navigation: exam.Navigation, DurationSeconds: exam.DurationSeconds,
		GraceSeconds: exam.GraceSeconds, MinVolumePercent: exam.MinVolumePercent, OpensAt: exam.OpensAt, ClosesAt: exam.ClosesAt}, nil
}

// Create imports an exam into a class, where it stays. A class that does not exist is domain.ErrNotFound.
func (s *Exams) Create(ctx context.Context, classID string, input CreateExamInput) (string, error) {
	if !validProctorID(classID) {
		return "", ErrInvalidInput
	}
	exam, err := s.build(ctx, input, false)
	if err != nil {
		return "", err
	}
	id, err := randomUUID()
	if err != nil {
		return "", err
	}
	exam.ID, exam.ClassID, exam.CreatedAt = id, classID, s.clock.Now().UTC()
	if err := s.repository.CreateExam(ctx, exam); err != nil {
		return "", err
	}
	return id, nil
}

// ExamDetail is what the proctor reads to render students' submissions: the exam's content and, when it was stored, the
// Markdown source. It never carries the PIN.
type ExamDetail struct {
	ExamID     string            `json:"exam_id"`
	Title      string            `json:"title"`
	ClassID    string            `json:"class_id"`
	ClassName  string            `json:"class_name"`
	Navigation domain.Navigation `json:"navigation"`
	// Tools is the exam's effective tools configuration; omitted when the exam has no calculator.
	Tools          *calc.Tools        `json:"tools,omitempty"`
	Content        domain.ExamContent `json:"content"`
	SourceMarkdown *string            `json:"source_markdown"`
}

// Detail reads one exam for the proctor; an unknown exam is domain.ErrNotFound.
func (s *Exams) Detail(ctx context.Context, examID string) (ExamDetail, error) {
	if !validProctorID(examID) {
		return ExamDetail{}, ErrInvalidInput
	}
	detail, err := s.repository.ExamDetail(ctx, examID)
	if err != nil {
		return ExamDetail{}, err
	}
	entry := ExamDetail{ExamID: detail.ID, Title: detail.Title, ClassID: detail.ClassID, ClassName: detail.ClassName, Navigation: detail.Navigation, Content: detail.Content, SourceMarkdown: detail.SourceMarkdown}
	if detail.Tools != nil && detail.Tools.On() {
		effective := detail.Tools.Effective()
		entry.Tools = &effective
	}
	return entry, nil
}

// DeletedExam is the exam that was removed.
type DeletedExam struct {
	ID    string `json:"exam_id"`
	Title string `json:"title"`
}

// Delete removes an exam that is not published and that no student has taken; any other exam is domain.ErrExamInUse.
func (s *Exams) Delete(ctx context.Context, examID string) (DeletedExam, error) {
	if !validProctorID(examID) {
		return DeletedExam{}, ErrInvalidInput
	}
	deleted, err := s.repository.DeleteExam(ctx, examID)
	if err != nil {
		return DeletedExam{}, err
	}
	return DeletedExam{ID: deleted.ID, Title: deleted.Title}, nil
}

// build validates input and returns every problem found, naming fields, exercises and slots but never echoing the PIN.
// withoutPIN skips the PIN, for a preview.
func (s *Exams) build(ctx context.Context, input CreateExamInput, withoutPIN bool) (domain.Exam, error) {
	var problems ValidationError
	var grace int64
	if input.GraceSeconds != nil {
		grace = *input.GraceSeconds
	} else {
		var err error
		if grace, err = s.grace.DefaultGraceSeconds(ctx); err != nil {
			return domain.Exam{}, err
		}
	}
	volume := DefaultMinVolumePercent
	if input.MinVolumePercent != nil {
		volume = *input.MinVolumePercent
	}
	navigation := input.Navigation
	if navigation == "" {
		navigation = domain.FreeNavigation
	}
	exam := domain.Exam{Title: strings.TrimSpace(input.Title), PIN: input.PIN, DurationSeconds: input.DurationSeconds,
		GraceSeconds: grace, MinVolumePercent: volume, Navigation: navigation, Content: input.Content, OpensAt: input.OpensAt, ClosesAt: input.ClosesAt}
	if input.Tools != nil {
		if found := input.Tools.Validate(); len(found) > 0 {
			for _, problem := range found {
				problems.add("%s", problem)
			}
		} else {
			exam.Tools = input.Tools
		}
	}
	if !validText(exam.Title, 512) {
		problems.add("title is required and must be at most 512 bytes of text")
	}
	if !withoutPIN && !validText(exam.PIN, 256) {
		problems.add("pin is required and must be at most 256 bytes of text")
	}
	const maxSeconds = math.MaxInt64 / int64(time.Second)
	switch {
	case exam.DurationSeconds <= 0:
		problems.add("duration_seconds must be a positive whole number of seconds")
	case exam.DurationSeconds > maxSeconds:
		problems.add("duration_seconds is too large")
	}
	switch {
	case exam.GraceSeconds <= 0:
		problems.add("grace_seconds must be a positive whole number of seconds")
	case exam.GraceSeconds > maxSeconds:
		problems.add("grace_seconds is too large")
	case exam.DurationSeconds > 0 && exam.DurationSeconds <= maxSeconds && exam.GraceSeconds > maxSeconds-exam.DurationSeconds:
		problems.add("duration_seconds plus grace_seconds is too large")
	}
	if volume < 0 || volume > 100 {
		problems.add("min_volume_percent must be between 0 and 100")
	}
	if exam.OpensAt != nil && exam.ClosesAt != nil && !exam.OpensAt.Before(*exam.ClosesAt) {
		problems.add("opens_at must be earlier than closes_at")
	}
	if navigation != domain.FreeNavigation && navigation != domain.ForwardOnly {
		problems.add("navigation must be free or forward_only")
	}
	if input.SourceMarkdown != nil && *input.SourceMarkdown != "" {
		switch {
		case !utf8.ValidString(*input.SourceMarkdown):
			problems.add("source_markdown must be UTF-8 text")
		case len(*input.SourceMarkdown) > MaxSourceMarkdownBytes:
			problems.add("source_markdown must be at most %d MiB", MaxSourceMarkdownBytes>>20)
		default:
			exam.SourceMarkdown = input.SourceMarkdown
		}
	}
	validateContent(exam.Content, &problems)
	if len(problems.Problems) > 0 {
		return domain.Exam{}, &problems
	}
	return exam, nil
}

func validText(value string, maxBytes int) bool {
	return utf8.ValidString(value) && strings.TrimSpace(value) != "" && len(value) <= maxBytes
}

type AnswerLimits struct{ PerAnswerBytes, TotalBytes int64 }

func DefaultAnswerLimits() AnswerLimits {
	return AnswerLimits{PerAnswerBytes: 64 << 10, TotalBytes: 512 << 10}
}

func (l AnswerLimits) Validate() error {
	if l.PerAnswerBytes <= 0 || l.TotalBytes < l.PerAnswerBytes {
		return errors.New("answer limits must be positive and total must cover one answer")
	}
	return nil
}

func (s *Sessions) validateAnswers(exam domain.Exam, answers domain.Answers) error {
	if answers == nil {
		return ErrInvalidInput
	}
	slots := exam.Content.SlotExercises()
	var total int64
	for id, text := range answers {
		if _, ok := slots[id]; !ok || !utf8.ValidString(text) {
			return ErrInvalidInput
		}
		size := int64(len(text))
		if size > s.limits.PerAnswerBytes || size > s.limits.TotalBytes-total {
			return ErrAnswerTooLarge
		}
		total += size
	}
	return nil
}
