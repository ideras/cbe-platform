package examdoc

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"exam-platform/shared/calc"
)

// Kinds of exam document. The document of a question exam carries no kind at all, so the documents of existing exams stay
// exactly as they were: a document without a kind is a question exam. A workspace exam says "workspace".
const (
	KindQuestion  = "question"
	KindWorkspace = "workspace"
)

// Limits of a workspace package (plan section 5.2). They apply to the starter project and, the same, to the grading folder.
const (
	// MaxBundleBytes is the most file content one bundle may hold.
	MaxBundleBytes = 20 << 20
	// MaxBundleFiles is the most files (regular files and symbolic links) one bundle may hold.
	MaxBundleFiles = 2000
)

// Defaults of a workspace exam that the front matter does not set. They are the plan's default limits (Appendix A); the server
// has its own maximums, which it applies when the exam is imported.
const (
	DefaultCPU      = 1.0
	DefaultMemoryMB = 1024
	DefaultDiskMB   = 100
	// MinSnapshotSeconds and MaxSnapshotSeconds bound the snapshot key (decision P0-19). The server may narrow them.
	MinSnapshotSeconds = 10
	MaxSnapshotSeconds = 600
)

// defaultIgnore is what a workspace exam leaves out of a submission when its front matter has no ignore key.
var defaultIgnore = []string{"build/**", "**/*.o", "**/*.out", "**/__pycache__/**", ".cache/**"}

// DefaultIgnore is a copy of the default ignore list.
func DefaultIgnore() []string { return slices.Clone(defaultIgnore) }

// WorkspaceExam is a compiled workspace exam: the document the proctor application sends to import one, next to the two bundles
// that CompileWorkspaceFS returns. Nothing from the grading folder is in it, only the hash of the grading bundle. The PIN is never
// part of the package; the proctor adds it when importing, as for a question exam.
type WorkspaceExam struct {
	Kind             string     `json:"kind"`
	Title            string     `json:"title"`
	PIN              string     `json:"pin,omitempty"`
	DurationSeconds  int64      `json:"duration_seconds"`
	GraceSeconds     *int64     `json:"grace_seconds,omitempty"`
	MinVolumePercent *int       `json:"min_volume_percent,omitempty"`
	OpensAt          *time.Time `json:"opens_at,omitempty"`
	ClosesAt         *time.Time `json:"closes_at,omitempty"`
	// Tools is what the calculator key declared, exactly as in a question exam.
	Tools     *calc.Tools       `json:"tools,omitempty"`
	Workspace WorkspaceSettings `json:"workspace"`
	Content   WorkspaceContent  `json:"content"`
	// StarterBundle and GradingBundle are the content hashes ("sha256:" and 64 hexadecimal digits) of the two bundles. The
	// grading bundle is left out when the package has no grading folder.
	StarterBundle string `json:"starter_bundle"`
	GradingBundle string `json:"grading_bundle,omitempty"`
}

// WorkspaceSettings is what the workspace.md front matter says about the container and the project.
type WorkspaceSettings struct {
	// Image names a course image, such as cbe/cpp20. The server checks that it has the image.
	Image    string `json:"image"`
	Terminal bool   `json:"terminal"`
	// Run and Check are the commands behind the Run and Check buttons, run in /work; a button is hidden when its command is empty.
	Run   string `json:"run,omitempty"`
	Check string `json:"check,omitempty"`
	// Submit lists the globs of what is submitted; nil means everything in /work except Ignore.
	Submit []string `json:"submit,omitempty"`
	// Ignore lists the globs left out of a submission. It is never nil: an exam may ignore nothing.
	Ignore    []string  `json:"ignore"`
	Resources Resources `json:"resources"`
	// SnapshotSeconds is how often the server snapshots the workspace while files change; nil means the server's default (D8).
	SnapshotSeconds *int64 `json:"snapshot_seconds,omitempty"`
}

// Resources are the limits of the student's container.
type Resources struct {
	CPU      float64 `json:"cpu"`
	MemoryMB int     `json:"memory_mb"`
	DiskMB   int     `json:"disk_mb"`
}

// WorkspaceContent is what students read: the instructions and the TODO list.
type WorkspaceContent struct {
	// Instructions is an HTML fragment of the body of workspace.md without its TODO items.
	Instructions string `json:"instructions,omitempty"`
	Todos        []Todo `json:"todos"`
}

// Todo is one task of the list. The student ticks it by hand, so progress is self-reported.
type Todo struct {
	ID string `json:"id"`
	// TextHTML is an inline HTML fragment: the text of the item rendered from Markdown.
	TextHTML string   `json:"text_html"`
	Points   *float64 `json:"points,omitempty"`
}

// Points is the sum of the TODO items' points, and false when an item has none.
func (c WorkspaceContent) Points() (float64, bool) {
	total := 0.0
	for _, todo := range c.Todos {
		if todo.Points == nil {
			return 0, false
		}
		total += *todo.Points
	}
	return total, len(c.Todos) > 0
}

// Bundle is a deterministic tar archive of one folder of the package: the same folder always gives the same bytes and so the
// same Hash.
type Bundle struct {
	// Data is the tar archive. Its entries are sorted by name, with fixed permissions, owner and times.
	Data []byte
	// Hash is "sha256:" followed by the hexadecimal SHA-256 of Data.
	Hash string
	// Files counts the regular files and symbolic links; folders are not counted. Size is the bytes of file content.
	Files int
	Size  int64
}

// WorkspaceBundles are the two archives of a package. Grading is nil when the package has no grading folder.
type WorkspaceBundles struct {
	Starter Bundle
	Grading *Bundle
}

// DocumentKind reads the kind of a compiled exam document: KindWorkspace, or KindQuestion, which is also what a document that
// says nothing about its kind is. Any other kind is an error, so a document from a newer release is never taken for a question exam.
func DocumentKind(document []byte) (string, error) {
	var header struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(document, &header); err != nil {
		return "", fmt.Errorf("the exam document is not a JSON object: %w", err)
	}
	switch header.Kind {
	case "", KindQuestion:
		return KindQuestion, nil
	case KindWorkspace:
		return KindWorkspace, nil
	}
	return "", fmt.Errorf("unknown exam kind %q", header.Kind)
}
