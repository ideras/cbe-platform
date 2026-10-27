package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"time"

	"exam-platform/internal/domain"
	"exam-platform/internal/service"
	"exam-platform/shared/calc"
)

type ExamAPI interface {
	Create(ctx context.Context, classID string, input service.CreateExamInput) (string, error)
	Preview(ctx context.Context, input service.CreateExamInput) (service.ExamPreview, error)
	Delete(ctx context.Context, examID string) (service.DeletedExam, error)
	Detail(ctx context.Context, examID string) (service.ExamDetail, error)
}

type Options struct {
	AdminToken     string
	Exams          ExamAPI
	Publisher      PublishAPI
	Classes        ClassAPI
	Proctor        ProctorAPI
	Screenshots    ScreenshotAPI
	Rules          RulesAPI
	MaxJSONBytes   int64
	MaxRosterBytes int64
}

type savedResponse struct {
	Sequence int64     `json:"version_seq"`
	SavedAt  time.Time `json:"saved_at"`
}
type examResponse struct {
	ID              string             `json:"exam_id"`
	Title           string             `json:"title"`
	Navigation      domain.Navigation  `json:"navigation"`
	Content         domain.ExamContent `json:"content"`
	CurrentExercise int                `json:"current_exercise"`
	Answers         domain.Answers     `json:"answers"`
	Latest          *savedResponse     `json:"latest_version"`
	// Tools is the exam's effective tools configuration; omitted when the exam has no calculator.
	Tools *calc.Tools `json:"tools,omitempty"`
}
type submitResponse struct {
	Status         domain.SessionStatus `json:"status"`
	SubmittedAt    time.Time            `json:"submitted_at"`
	FinalVersionID int64                `json:"final_version_id"`
	Sequence       int64                `json:"version_seq"`
}
type answerRequest struct {
	Answers map[string]*string `json:"answers"`
}
type advanceRequest struct {
	Answers map[string]*string `json:"answers"`
	// From is the exercise the student is leaving; it must be the one the server has them on.
	From *int `json:"from_exercise"`
}
type advanceResponse struct {
	CurrentExercise int       `json:"current_exercise"`
	Sequence        int64     `json:"version_seq"`
	SavedAt         time.Time `json:"saved_at"`
}

func decodeBody[T any](w http.ResponseWriter, r *http.Request, maxBytes int64) (*T, bool) {
	return decodeJSON[T](w, r, maxBytes, false, "")
}

// decodeJSON reads one JSON object. With detailed set (administrator documents only) a decoding failure says what the
// decoder found, such as an unknown field name, so an authoring tool can show it. A body over maxBytes is answered with
// tooLarge as its problem when that is set, so a document with a known cause of size can name it.
func decodeJSON[T any](w http.ResponseWriter, r *http.Request, maxBytes int64, detailed bool, tooLarge string) (*T, bool) {
	fail := func(err error) {
		var limit *http.MaxBytesError
		if tooLarge != "" && errors.As(err, &limit) {
			writeJSON(w, http.StatusRequestEntityTooLarge, problemResponse{Error: "request body too large", Problems: []string{tooLarge}})
			return
		}
		if detailed && !errors.As(err, &limit) {
			message := err.Error()
			// Name the field from the error itself: the struct name it carries depends on the Go version and is not wanted here.
			var mismatch *json.UnmarshalTypeError
			if errors.As(err, &mismatch) && mismatch.Field != "" && mismatch.Type != nil {
				message = "json: cannot unmarshal " + mismatch.Value + " into field " + mismatch.Field + " of type " + mismatch.Type.String()
			}
			writeJSON(w, http.StatusBadRequest, problemResponse{Error: "invalid JSON request", Problems: []string{message}})
			return
		}
		bodyError(w, err)
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return nil, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var input *T
	if err := decoder.Decode(&input); err != nil {
		fail(err)
		return nil, false
	}
	if input == nil {
		writeError(w, http.StatusBadRequest, "invalid JSON request")
		return nil, false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			err = errors.New("unexpected data after the JSON document")
		}
		fail(err)
		return nil, false
	}
	return input, true
}

// dryRun reads the optional dry_run=true|false query of an administrator write.
func dryRun(w http.ResponseWriter, r *http.Request) (bool, bool) {
	query, ok := proctorQuery(w, r, "dry_run")
	if !ok {
		return false, false
	}
	switch query.Get("dry_run") {
	case "", "false":
		return false, true
	case "true":
		return true, true
	}
	writeError(w, http.StatusBadRequest, "invalid query")
	return false, false
}

// createExamHandler imports the exam document of the request into the class named by the path, or with dry_run=true only
// checks it.
func createExamHandler(exams ExamAPI, logger *slog.Logger, limit int64) http.HandlerFunc {
	// An imported exam carries its compiled document and, on request, its Markdown source; both count against the limit.
	tooLarge := fmt.Sprintf("the exam document and its source_markdown must fit in %d bytes; raise -max-json-bytes or shorten the source", limit)
	return func(w http.ResponseWriter, r *http.Request) {
		preview, ok := dryRun(w, r)
		if !ok {
			return
		}
		input, ok := decodeJSON[service.CreateExamInput](w, r, limit, true, tooLarge)
		if !ok {
			return
		}
		if preview {
			result, err := exams.Preview(r.Context(), *input)
			if err != nil {
				serviceError(w, logger, "preview exam", err)
				return
			}
			writeJSON(w, http.StatusOK, result)
			return
		}
		id, err := exams.Create(r.Context(), r.PathValue("id"), *input)
		if err != nil {
			serviceError(w, logger, "create exam", err)
			return
		}
		writeJSON(w, http.StatusCreated, struct {
			ID string `json:"exam_id"`
		}{id})
	}
}

func examDetailHandler(exams ExamAPI, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := proctorQuery(w, r); !ok {
			return
		}
		detail, err := exams.Detail(r.Context(), r.PathValue("id"))
		if err != nil {
			serviceError(w, logger, "read exam", err)
			return
		}
		writeJSON(w, http.StatusOK, detail)
	}
}

func deleteExamHandler(exams ExamAPI, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := proctorQuery(w, r); !ok {
			return
		}
		deleted, err := exams.Delete(r.Context(), r.PathValue("id"))
		if err != nil {
			serviceError(w, logger, "delete exam", err)
			return
		}
		logger.Info("exam deleted", "exam_id", deleted.ID)
		writeJSON(w, http.StatusOK, deleted)
	}
}

func examHandler(sessions SessionAPI, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := PrincipalFromContext(r.Context())
		if !ok {
			serviceError(w, logger, "exam", service.ErrInvalidToken)
			return
		}
		result, err := sessions.GetExam(r.Context(), principal.Session.Token)
		if err != nil {
			serviceError(w, logger, "exam", err)
			return
		}
		response := examResponse{ID: result.ID, Title: result.Title, Navigation: result.Navigation, Content: result.Content, CurrentExercise: result.CurrentExercise, Answers: result.Answers, Tools: result.Tools}
		if result.Latest != nil {
			response.Latest = &savedResponse{Sequence: result.Latest.Sequence, SavedAt: result.Latest.SavedAt}
		}
		writeJSON(w, http.StatusOK, response)
	}
}

func answerHandler(sessions SessionAPI, logger *slog.Logger, limit int64, submit bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		input, ok := decodeBody[answerRequest](w, r, limit)
		if !ok {
			return
		}
		answers, ok := answerMap(w, input.Answers)
		if !ok {
			return
		}
		principal, ok := PrincipalFromContext(r.Context())
		if !ok {
			serviceError(w, logger, "save", service.ErrInvalidToken)
			return
		}
		if submit {
			result, err := sessions.Submit(r.Context(), principal.Session.Token, answers)
			if err != nil {
				serviceError(w, logger, "submit", err)
				return
			}
			writeJSON(w, http.StatusOK, submitResponse{Status: result.Status, SubmittedAt: result.SubmittedAt, FinalVersionID: result.FinalVersionID, Sequence: result.Sequence})
			return
		}
		result, err := sessions.Autosave(r.Context(), principal.Session.Token, answers)
		if err != nil {
			serviceError(w, logger, "autosave", err)
			return
		}
		writeJSON(w, http.StatusOK, savedResponse{Sequence: result.Sequence, SavedAt: result.SavedAt})
	}
}

// answerMap reads the answers of a request: an object whose every value is a string.
func answerMap(w http.ResponseWriter, input map[string]*string) (domain.Answers, bool) {
	if input == nil {
		writeError(w, http.StatusBadRequest, "answers must be an object of strings")
		return nil, false
	}
	answers := make(domain.Answers, len(input))
	for id, text := range input {
		if text == nil {
			writeError(w, http.StatusBadRequest, "answers must be an object of strings")
			return nil, false
		}
		answers[id] = *text
	}
	return answers, true
}

// advanceHandler saves a forward-only student's answers and moves them to the next exercise.
func advanceHandler(sessions SessionAPI, logger *slog.Logger, limit int64) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		input, ok := decodeBody[advanceRequest](w, r, limit)
		if !ok {
			return
		}
		if input.From == nil {
			writeError(w, http.StatusBadRequest, "from_exercise is required")
			return
		}
		answers, ok := answerMap(w, input.Answers)
		if !ok {
			return
		}
		principal, ok := PrincipalFromContext(r.Context())
		if !ok {
			serviceError(w, logger, "advance", service.ErrInvalidToken)
			return
		}
		result, err := sessions.Advance(r.Context(), principal.Session.Token, *input.From, answers)
		if err != nil {
			serviceError(w, logger, "advance", err)
			return
		}
		writeJSON(w, http.StatusOK, advanceResponse{CurrentExercise: result.CurrentExercise, Sequence: result.Sequence, SavedAt: result.SavedAt})
	}
}
