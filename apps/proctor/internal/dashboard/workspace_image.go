package dashboard

import (
	"bytes"
	"encoding/json"
	"errors"
	"html"
	"mime/multipart"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"exam-platform/shared/clientwire"
	"exam-platform/shared/examdoc"
)

// Importing a workspace package and building its own image (workspace plan, Phase 7). The proctor chooses the package's workspace.md, sees what
// the compiler made of it, chooses the PIN and imports it; if the package has an image/ folder, the image is built before the exam is
// published (a proctor's action, with its log), and the server refuses to publish the exam until it is.

// ImageBuild is a build of an exam's own image as the server reports it. Log is only there when a build is read by itself or as the latest.
type ImageBuild struct {
	ID         int64      `json:"build_id"`
	State      string     `json:"state"`
	Failure    string     `json:"failure"`
	Message    string     `json:"message"`
	Cached     bool       `json:"cached"`
	Rebuild    bool       `json:"rebuild"`
	Hash       string     `json:"hash"`
	BaseName   string     `json:"base_name"`
	BaseDigest string     `json:"base_digest"`
	Warnings   []string   `json:"warnings"`
	DurationMS int64      `json:"duration_ms"`
	QueuedAt   time.Time  `json:"queued_at"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	Log        string     `json:"log"`
	// A build that is waiting or going has its log read from an offset (decision P7-10): Live says so, LogFrom is where Log starts (0 when the offset asked
	// for was not one the server had) and LogNext the offset to ask for next. Step, Steps and StepText say where the build is, and LastOutputAt when it last
	// printed anything.
	Live         bool       `json:"live"`
	LogFrom      int64      `json:"log_from"`
	LogNext      int64      `json:"log_next"`
	Step         int        `json:"step"`
	Steps        int        `json:"steps"`
	StepText     string     `json:"step_text"`
	LastOutputAt *time.Time `json:"last_output_at"`
}

// ExamImage is the state of a workspace exam's own image: none (the exam has no image folder), not_built, queued, building, ready, failed or
// missing (the runner lost it). Ready says the exam can be published as far as its image goes. Present is only there when the server asked
// the runner. Build is the latest build, and Builds the exam's builds, newest first, without their logs.
type ExamImage struct {
	ExamID  string       `json:"exam_id"`
	State   string       `json:"state"`
	Image   string       `json:"image"`
	Present *bool        `json:"present"`
	Ready   bool         `json:"ready"`
	Build   *ImageBuild  `json:"build"`
	Builds  []ImageBuild `json:"builds"`
}

func validImageState(state string) bool {
	switch state {
	case "none", "not_built", "queued", "building", "ready", "failed", "missing":
		return true
	}
	return false
}

var buildHashFormat = regexp.MustCompile(`^[0-9a-f]{16}$`)

func validBuild(b ImageBuild) bool {
	if b.LogFrom < 0 || b.LogNext < b.LogFrom || b.Step < 0 || b.Steps < b.Step || len(b.StepText) > 1024 || !utf8.ValidString(b.StepText) || b.Live && b.State != "queued" && b.State != "running" {
		return false
	}
	if b.ID <= 0 || b.QueuedAt.IsZero() || b.DurationMS < 0 || len(b.Warnings) > 200 || len(b.Log) > 2<<20 || len(b.Message) > 8<<10 || len(b.Failure) > 32 ||
		len(b.BaseName) > 128 || len(b.BaseDigest) > 128 || !utf8.ValidString(b.Log) || !utf8.ValidString(b.Message) || b.Hash != "" && !buildHashFormat.MatchString(b.Hash) {
		return false
	}
	switch b.State {
	case "queued", "running", "ok", "failed":
	default:
		return false
	}
	for _, w := range b.Warnings {
		if len(w) > 2048 || !utf8.ValidString(w) {
			return false
		}
	}
	return b.State != "ok" || b.Hash != ""
}

func validImage(image *ExamImage) bool {
	if image == nil {
		return true
	}
	if !validImageState(image.State) || len(image.Image) > 128 || len(image.Builds) > 100 || (image.Build != nil && !validBuild(*image.Build)) {
		return false
	}
	for _, b := range image.Builds {
		if !validBuild(b) {
			return false
		}
	}
	// A state that says there is a build to read has one, and one that says there is none does not pretend to.
	switch image.State {
	case "none", "not_built":
		return image.Build == nil || image.State == "not_built"
	case "queued", "building", "ready", "failed":
		return image.Build != nil
	}
	return true
}

// ExamImage reads the state of an exam's own image; with withLog the latest build comes with its log, and, when that build is going, only the part of it
// from the offset logFrom on (the LogNext of the last look; 0 for all of it), so a look every second transfers what is new. The server asks the runner
// whether it still has the image, which is why this is not part of the polling.
func (s *Service) ExamImage(examID string, withLog bool, logFrom int64) (ExamImage, error) {
	ctx, base, token, link, err := s.linkState()
	if err != nil {
		return ExamImage{}, err
	}
	if !validID(examID) || logFrom < 0 {
		return ExamImage{}, errors.New("Choose an exam.")
	}
	path := "/admin/workspace/exam/" + url.PathEscape(examID) + "/image"
	if withLog {
		path += "?log=1&log_from=" + strconv.FormatInt(logFrom, 10)
	}
	var out ExamImage
	if err := getJSON(ctx, s.client, base, token, http.MethodGet, path, nil, &out); err != nil {
		return ExamImage{}, err
	}
	if err := s.stillLinked(link); err != nil {
		return ExamImage{}, err
	}
	if out.ExamID != "" && out.ExamID != examID || !validImage(&out) {
		return ExamImage{}, errors.New("Invalid image response.")
	}
	out.ExamID = examID
	return out, nil
}

// BuildExamImage asks the server to build the exam's image. rebuild builds again without the runner's cache. The build goes on in the
// background and takes seconds to minutes; ExamImage shows it go.
func (s *Service) BuildExamImage(examID string, rebuild bool) (ExamImage, error) {
	ctx, base, token, err := s.beginAdmin()
	if err != nil {
		return ExamImage{}, err
	}
	defer s.endAction()
	if !validID(examID) {
		return ExamImage{}, errors.New("Choose an exam.")
	}
	body, err := json.Marshal(map[string]bool{"rebuild": rebuild})
	if err != nil {
		return ExamImage{}, errors.New("Cannot encode request.")
	}
	var out ExamImage
	if err := adminSend(ctx, s.client, base, token, http.MethodPost, "/admin/workspace/exam/"+url.PathEscape(examID)+"/image/build", "application/json", body, &out); err != nil {
		return ExamImage{}, err
	}
	if out.ExamID != "" && out.ExamID != examID || !validImage(&out) || out.State == "none" {
		return ExamImage{}, errors.New("The server's answer was invalid. Look at the exam's image to see whether it is being built.")
	}
	out.ExamID = examID
	return out, nil
}

// ExamImageBuild reads one build of an exam's image with its log: what the builder printed, and what verification said. It is text, never markup.
func (s *Service) ExamImageBuild(examID string, buildID int64) (ImageBuild, error) {
	ctx, base, token, link, err := s.linkState()
	if err != nil {
		return ImageBuild{}, err
	}
	if !validID(examID) || buildID <= 0 {
		return ImageBuild{}, errors.New("Invalid build selection.")
	}
	var out ImageBuild
	if err := getJSON(ctx, s.client, base, token, http.MethodGet, "/admin/workspace/exam/"+url.PathEscape(examID)+"/image/build/"+strconv.FormatInt(buildID, 10), nil, &out); err != nil {
		return ImageBuild{}, err
	}
	if err := s.stillLinked(link); err != nil {
		return ImageBuild{}, err
	}
	if out.ID != buildID || !validBuild(out) {
		return ImageBuild{}, errors.New("Invalid build response.")
	}
	return out, nil
}

// pendingWorkspace is a compiled package that a preview showed, with its bundles, for the class it will be imported into.
type pendingWorkspace struct {
	exam    examdoc.WorkspaceExam
	bundles examdoc.WorkspaceBundles
}

// WorkspacePreviewTodo is a TODO item as the preview shows it: plain text, since the preview does not render the exam's markup.
type WorkspacePreviewTodo struct {
	ID     string   `json:"id"`
	Text   string   `json:"text"`
	Points *float64 `json:"points"`
}

// WorkspacePreview is what compiling a chosen workspace package made of it. Nothing is stored on the server until ImportWorkspaceExam.
type WorkspacePreview struct {
	Cancelled       bool                   `json:"cancelled"`
	Folder          string                 `json:"folder"`
	Valid           bool                   `json:"valid"`
	Problems        []string               `json:"problems"`
	Title           string                 `json:"title"`
	DurationSeconds int64                  `json:"duration_seconds"`
	Image           string                 `json:"image"`
	Terminal        bool                   `json:"terminal"`
	Run             string                 `json:"run"`
	Check           string                 `json:"check"`
	CPU             float64                `json:"cpu"`
	MemoryMB        int                    `json:"memory_mb"`
	DiskMB          int                    `json:"disk_mb"`
	Todos           []WorkspacePreviewTodo `json:"todos"`
	Points          *float64               `json:"points"`
	StarterFiles    int                    `json:"starter_files"`
	GradingFiles    int                    `json:"grading_files"`
	HasGrading      bool                   `json:"has_grading"`
	// OwnImage says the package has an image folder: the exam's own image, built from Image before the exam is published. ImageFiles counts
	// the files of its build context, and ImageWarnings are what the Containerfile's checker would only say.
	OwnImage      bool     `json:"own_image"`
	ImageFiles    int      `json:"image_files"`
	ImageWarnings []string `json:"image_warnings"`
}

var tagPattern = regexp.MustCompile(`<[^>]*>`)

// plainText is the text of an HTML fragment of the exam, for the preview.
func plainText(fragment string) string {
	return strings.TrimSpace(html.UnescapeString(tagPattern.ReplaceAllString(fragment, "")))
}

// ChooseWorkspaceExam lets the proctor pick the workspace.md of a package, compiles the package (its folder is the one that holds the file) and shows
// what it made of it, storing nothing. The package is kept for that class until ImportWorkspaceExam or DiscardWorkspaceExam.
func (s *Service) ChooseWorkspaceExam(classID string) (WorkspacePreview, error) {
	empty := WorkspacePreview{Problems: []string{}, Todos: []WorkspacePreviewTodo{}, ImageWarnings: []string{}}
	classID = strings.TrimSpace(classID)
	if !validID(classID) {
		return empty, errors.New("Class ID must be a canonical lowercase UUID.")
	}
	path, err := s.choose(FileDialog{Title: "Choose the workspace.md of the package", FilterName: "Workspace exam (workspace.md)", Pattern: "workspace.md"})
	if err != nil {
		return empty, err
	}
	if path == "" {
		empty.Cancelled = true
		return empty, nil
	}
	dir := filepath.Dir(path)
	preview := empty
	preview.Folder = filepath.Base(dir)
	s.mu.Lock()
	s.pendingWorkspace, s.pendingWorkspaceClass = nil, ""
	s.mu.Unlock()
	exam, bundles, err := examdoc.CompileWorkspaceDir(dir)
	var compileProblems examdoc.Problems
	if errors.As(err, &compileProblems) {
		preview.Problems = compileProblems
		return preview, nil
	}
	if err != nil {
		return preview, err
	}
	w := exam.Workspace
	preview.Valid, preview.Title, preview.DurationSeconds, preview.Image, preview.Terminal = true, exam.Title, exam.DurationSeconds, w.Image, w.Terminal
	preview.Run, preview.Check, preview.CPU, preview.MemoryMB, preview.DiskMB = w.Run, w.Check, w.Resources.CPU, w.Resources.MemoryMB, w.Resources.DiskMB
	for _, todo := range exam.Content.Todos {
		preview.Todos = append(preview.Todos, WorkspacePreviewTodo{ID: todo.ID, Text: plainText(todo.TextHTML), Points: todo.Points})
	}
	if total, ok := exam.Content.Points(); ok {
		preview.Points = &total
	}
	preview.StarterFiles = bundles.Starter.Files
	if bundles.Grading != nil {
		preview.HasGrading, preview.GradingFiles = true, bundles.Grading.Files
	}
	if bundles.Image != nil {
		preview.OwnImage, preview.ImageFiles = true, bundles.Image.Files
		preview.ImageWarnings = append(preview.ImageWarnings, bundles.ImageWarnings...)
	}
	s.mu.Lock()
	s.pendingWorkspace, s.pendingWorkspaceClass = &pendingWorkspace{exam: exam, bundles: bundles}, classID
	s.mu.Unlock()
	return preview, nil
}

// DiscardWorkspaceExam forgets the package shown in the workspace preview.
func (s *Service) DiscardWorkspaceExam() {
	s.mu.Lock()
	s.pendingWorkspace, s.pendingWorkspaceClass = nil, ""
	s.mu.Unlock()
}

// ImportWorkspaceExam stores the previewed package in the class it was previewed for, with the PIN students will log in with, and does not
// publish it. The package is used up whatever the server answers, so an unclear failure can never import the same exam twice by accident. The
// server checks everything again, and a refusal lists every problem it found.
func (s *Service) ImportWorkspaceExam(classID, pin string) (ImportResult, error) {
	classID = strings.TrimSpace(classID)
	if !validID(classID) {
		return ImportResult{}, errors.New("Class ID must be a canonical lowercase UUID.")
	}
	if strings.TrimSpace(pin) == "" || len(pin) > 256 || !utf8.ValidString(pin) || strings.ContainsFunc(pin, func(r rune) bool { return r < ' ' || r == 0x7f }) {
		return ImportResult{}, errors.New("Enter the PIN students will log in with: at most 256 characters, without line breaks.")
	}
	ctx, base, token, err := s.beginAdmin()
	if err != nil {
		return ImportResult{}, err
	}
	s.mu.Lock()
	pending, forClass := s.pendingWorkspace, s.pendingWorkspaceClass
	if forClass == classID {
		s.pendingWorkspace, s.pendingWorkspaceClass = nil, ""
	}
	s.mu.Unlock()
	defer s.endAction()
	if pending == nil || forClass != classID {
		return ImportResult{}, errors.New("Choose a workspace package for this class first.")
	}
	exam := pending.exam
	exam.PIN = pin
	document, err := json.Marshal(exam)
	if err != nil {
		return ImportResult{}, errors.New("The exam could not be prepared for the server.")
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	parts := []struct {
		name string
		data []byte
	}{{"document", document}, {"starter", pending.bundles.Starter.Data}}
	if pending.bundles.Grading != nil {
		parts = append(parts, struct {
			name string
			data []byte
		}{"grading", pending.bundles.Grading.Data})
	}
	if pending.bundles.Image != nil {
		parts = append(parts, struct {
			name string
			data []byte
		}{"image", pending.bundles.Image.Data})
	}
	for _, part := range parts {
		field, err := form.CreateFormField(part.name)
		if err != nil {
			return ImportResult{}, errors.New("The exam could not be prepared for the server.")
		}
		if _, err := field.Write(part.data); err != nil {
			return ImportResult{}, errors.New("The exam could not be prepared for the server.")
		}
	}
	if err := form.Close(); err != nil {
		return ImportResult{}, errors.New("The exam could not be prepared for the server.")
	}
	var created struct {
		ID string `json:"exam_id"`
	}
	// A package can be tens of megabytes, which the usual few seconds of a request are too short for.
	client := clientwire.NewHTTPClient(5 * time.Minute)
	if err := adminSend(ctx, client, base, token, http.MethodPost, "/admin/class/"+classID+"/workspace-exam", form.FormDataContentType(), body.Bytes(), &created); err != nil {
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
	s.signal()
	return ImportResult{ExamID: created.ID}, nil
}
