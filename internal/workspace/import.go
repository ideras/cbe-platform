package workspace

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"exam-platform/internal/runner"
	"exam-platform/shared/examimage"
)

// ValidationError lists everything wrong with an imported package, as the proctor sees it.
type ValidationError struct{ Problems []string }

func (e *ValidationError) Error() string { return strings.Join(e.Problems, "; ") }

func (e *ValidationError) add(format string, args ...any) {
	if len(e.Problems) < 50 {
		e.Problems = append(e.Problems, fmt.Sprintf(format, args...))
	}
}

// Limits of an import, enforced here whatever the compiler checked: the package comes from the proctor's computer, and the compiler
// that built it may be older or different.
const (
	MaxDocumentBytes   = 1 << 20
	MaxBundleBytes     = 22 << 20
	maxInstructions    = 256 << 10
	maxTodoBytes       = 2048
	maxTodos           = 100
	maxCommandBytes    = 1000
	maxGlobs           = 50
	maxGlobBytes       = 200
	DefaultMinVolume   = 80
	minSnapshotSeconds = 10
	maxSnapshotSeconds = 600
)

var (
	idPattern    = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	imagePattern = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)*(?:/[a-z0-9]+(?:[._-][a-z0-9]+)*)*$`)
	hashFormat   = regexp.MustCompile(`^sha256:([0-9a-f]{64})$`)
)

// Importer takes workspace packages in: it checks everything, keeps the two bundles by their hashes, and stores the exam in its class.
type Importer struct {
	Repo   Repository
	Runner interface {
		ImageInspect(ctx context.Context, name string) (runner.ImageStatus, error)
	}
	Blobs Blobs
	// Max are the largest limits an exam may ask for: the runner's own maximums, which it enforces again when it makes the container.
	Max Resources
	// DefaultGrace is the grace period of an exam that names none: the proctor's setting.
	DefaultGrace func(context.Context) (int64, error)
	Now          func() time.Time
}

func newID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6], b[8] = b[6]&0x0f|0x40, b[8]&0x3f|0x80
	h := hex.EncodeToString(b[:])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:], nil
}

func validText(value string, limit int) bool {
	return utf8.ValidString(value) && strings.TrimSpace(value) != "" && len(value) <= limit
}

func plainLine(value string) bool {
	return utf8.ValidString(value) && !strings.ContainsFunc(value, func(r rune) bool { return r < ' ' || r == 0x7f })
}

// Import checks a package (the compiled document and its two bundles) and stores it as a new exam of the class. The document is the
// compiler's, with the PIN the proctor chose. It returns the new exam's ID, or a *ValidationError.
func (im *Importer) Import(ctx context.Context, classID string, documentJSON, starter, grading []byte) (string, error) {
	problems := &ValidationError{}
	if len(documentJSON) == 0 || len(documentJSON) > MaxDocumentBytes {
		problems.add("the document must be 1 to %d bytes", MaxDocumentBytes)
		return "", problems
	}
	var doc Document
	decoder := json.NewDecoder(bytes.NewReader(documentJSON))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&doc); err != nil || decoder.More() {
		problems.add("the document is not a workspace exam document: %v", err)
		return "", problems
	}
	if doc.Kind != "workspace" {
		problems.add(`kind must be "workspace"`)
	}
	if !validText(doc.Title, 512) {
		problems.add("title is required and must be at most 512 bytes of text")
	}
	if !validText(doc.PIN, 256) {
		problems.add("pin is required and must be at most 256 bytes of text")
	}
	if doc.DurationSeconds <= 0 || doc.DurationSeconds > 1<<31 {
		problems.add("duration_seconds must be a positive whole number of seconds")
	}
	grace := int64(0)
	switch {
	case doc.GraceSeconds != nil && (*doc.GraceSeconds <= 0 || *doc.GraceSeconds > 1<<31):
		problems.add("grace_seconds must be a positive whole number of seconds")
	case doc.GraceSeconds != nil:
		grace = *doc.GraceSeconds
	default:
		var err error
		if grace, err = im.DefaultGrace(ctx); err != nil {
			return "", err
		}
	}
	volume := DefaultMinVolume
	if doc.MinVolumePercent != nil {
		if volume = *doc.MinVolumePercent; volume < 0 || volume > 100 {
			problems.add("min_volume_percent must be between 0 and 100")
		}
	}
	if doc.OpensAt != nil && doc.ClosesAt != nil && !doc.OpensAt.Before(*doc.ClosesAt) {
		problems.add("opens_at must be earlier than closes_at")
	}
	if doc.Tools != nil {
		for _, problem := range doc.Tools.Validate() {
			problems.add("%s", problem)
		}
	}
	im.checkWorkspace(ctx, doc.Workspace, problems)
	im.checkContent(doc.Content, problems)
	starterHash := im.checkBundle("starter", starter, doc.StarterBundle, true, problems)
	gradingHash := ""
	switch {
	case len(grading) > 0:
		gradingHash = im.checkBundle("grading", grading, doc.GradingBundle, false, problems)
		if err := runner.CheckGrading(bytes.NewReader(grading)); err != nil {
			problems.add("grading bundle: %v", strings.TrimPrefix(err.Error(), "bad request: "))
		}
	case doc.GradingBundle != "":
		problems.add("the document names a grading bundle but none came with it")
	}
	if len(problems.Problems) > 0 {
		return "", problems
	}
	if _, err := im.Blobs.PutBytes(starter); err != nil {
		return "", err
	}
	if len(grading) > 0 {
		if _, err := im.Blobs.PutBytes(grading); err != nil {
			return "", err
		}
	}
	id, err := newID()
	if err != nil {
		return "", err
	}
	stored := doc
	stored.PIN = ""
	err = im.Repo.CreateExam(ctx, ExamRecord{ExamID: id, ClassID: classID, Title: strings.TrimSpace(doc.Title), PIN: doc.PIN, DurationSeconds: doc.DurationSeconds, GraceSeconds: grace,
		MinVolumePercent: volume, OpensAt: doc.OpensAt, ClosesAt: doc.ClosesAt, Tools: doc.Tools, Document: stored, StarterHash: starterHash, GradingHash: gradingHash, CreatedAt: im.Now().UTC()})
	if err != nil {
		return "", err
	}
	return id, nil
}

func (im *Importer) checkWorkspace(ctx context.Context, w Settings, problems *ValidationError) {
	if len(w.Image) > 128 || !imagePattern.MatchString(w.Image) {
		problems.add("workspace.image must be the name of a course image, such as cbe/cpp20")
	} else if status, err := im.Runner.ImageInspect(ctx, w.Image); err != nil {
		problems.add("the runner could not be asked about the image %s: %v", w.Image, err)
	} else if !status.Approved || !status.Present {
		problems.add("this server does not have the course image %s (approved: %v, present: %v)", w.Image, status.Approved, status.Present)
	}
	r := w.Resources
	switch {
	case r.CPU < 0.1 || r.CPU > im.Max.CPU:
		problems.add("workspace.resources.cpu must be 0.1 to %g on this server", im.Max.CPU)
	case r.MemoryMB < 64 || r.MemoryMB > im.Max.MemoryMB:
		problems.add("workspace.resources.memory_mb must be 64 to %d on this server", im.Max.MemoryMB)
	case r.DiskMB < 1 || r.DiskMB > im.Max.DiskMB:
		problems.add("workspace.resources.disk_mb must be 1 to %d on this server", im.Max.DiskMB)
	}
	for name, command := range map[string]string{"run": w.Run, "check": w.Check} {
		if len(command) > maxCommandBytes || !plainLine(command) {
			problems.add("workspace.%s must be one line of plain text of at most %d bytes", name, maxCommandBytes)
		}
	}
	for name, globs := range map[string][]string{"submit": w.Submit, "ignore": w.Ignore} {
		if len(globs) > maxGlobs {
			problems.add("workspace.%s has more than %d patterns", name, maxGlobs)
		}
		for _, glob := range globs {
			if glob == "" || len(glob) > maxGlobBytes || !plainLine(glob) || strings.HasPrefix(glob, "/") || strings.Contains(glob, `\`) || hasDotDot(glob) {
				problems.add("workspace.%s has a pattern that is not a path from /work down: %q", name, truncate(glob, 40))
			}
		}
	}
	if w.SnapshotSeconds != nil && (*w.SnapshotSeconds < minSnapshotSeconds || *w.SnapshotSeconds > maxSnapshotSeconds) {
		problems.add("workspace.snapshot_seconds must be %d to %d", minSnapshotSeconds, maxSnapshotSeconds)
	}
}

func hasDotDot(glob string) bool {
	for _, part := range strings.Split(glob, "/") {
		if part == ".." || part == "." || part == "" {
			return true
		}
	}
	return false
}

func (im *Importer) checkContent(c Content, problems *ValidationError) {
	if len(c.Instructions) > maxInstructions || !utf8.ValidString(c.Instructions) {
		problems.add("content.instructions must be UTF-8 text of at most %d bytes", maxInstructions)
	} else if _, err := examimage.CheckFragment(c.Instructions); err != nil {
		problems.add("content.instructions: %v", err)
	}
	if len(c.Todos) == 0 || len(c.Todos) > maxTodos {
		problems.add("content.todos must hold 1 to %d items", maxTodos)
	}
	seen := map[string]bool{}
	for i, todo := range c.Todos {
		label := fmt.Sprintf("content.todos[%d]", i)
		switch {
		case !idPattern.MatchString(todo.ID):
			problems.add("%s: the id must be letters, digits, _ and -, at most 64", label)
		case seen[todo.ID]:
			problems.add("%s: the id %q is used twice", label, todo.ID)
		}
		seen[todo.ID] = true
		if todo.TextHTML == "" || len(todo.TextHTML) > maxTodoBytes || !utf8.ValidString(todo.TextHTML) {
			problems.add("%s: text_html must be 1 to %d bytes of text", label, maxTodoBytes)
		} else if _, err := examimage.CheckFragment(todo.TextHTML); err != nil {
			problems.add("%s: %v", label, err)
		}
		if todo.Points != nil && (!(*todo.Points > 0) || *todo.Points > 10000) {
			problems.add("%s: points must be a positive number", label)
		}
	}
}

// checkBundle checks one bundle against the hash the document gives it, and returns the hash in hex.
func (im *Importer) checkBundle(name string, data []byte, declared string, required bool, problems *ValidationError) string {
	match := hashFormat.FindStringSubmatch(declared)
	switch {
	case match == nil:
		problems.add("%s_bundle must be sha256: and 64 hexadecimal digits", name)
		return ""
	case len(data) > MaxBundleBytes:
		problems.add("the %s bundle is larger than %d MiB", name, MaxBundleBytes>>20)
		return ""
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != match[1] {
		problems.add("the %s bundle does not match the hash in the document", name)
		return ""
	}
	if required {
		if _, err := runner.SanitizeSeed(bytes.NewReader(data), io.Discard); err != nil {
			problems.add("the %s bundle: %v", name, strings.TrimPrefix(err.Error(), "bad request: "))
		}
	}
	return match[1]
}
