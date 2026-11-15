package runner

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"exam-platform/shared/containerfile"
)

// image.build (Appendix E, Phase 7, decisions P7-3 to P7-9): an exam's own image, built from a Containerfile and its context on top of an
// approved course image. The build has network (D11) and runs as the Containerfile says; what comes out is checked before it is kept, and the
// containers made from it are made exactly as those of the course image are, with no network.

// Labels the runner puts on an image it built, and the places an exam's image is found by.
const (
	LabelBuildExam  = "io.cbe.exam"
	LabelBuildHash  = "io.cbe.build.hash"
	LabelBuildBase  = "io.cbe.base.digest"
	LabelBuildImage = "io.cbe.base.image"
	// LabelAgent is the label of the course image that holds the SHA-256 of the agent in it (images/README.md).
	LabelAgent = "io.cbe.agent.sha256"

	RoleVerify = "verify"
	// AgentPath is where every course image has the agent.
	AgentPath = "/usr/local/bin/cbe-agent"
	// DefaultBuildSeconds is how long a build may take when its request names no time; the runner's build_max_seconds is the most.
	maxBuildContextBytes = 22 << 20
	maxBuildContextFiles = 2000
	// maxLogBytes is the most of a build's log that is returned: the start, and the end (where the error is), with a note in between.
	maxLogBytes     = 1 << 20
	logHeadBytes    = 256 << 10
	verifyTimeout   = 60 * time.Second
	builtRepository = "localhost/cbe-exam/"
	pendingRepo     = "localhost/cbe-pending/"
)

// buildSecond is the unit of a build's timeout; a variable so that a test need not wait ten seconds to see one.
var buildSecond = time.Second

// hostCABundle is the certificate bundle the build lends to the Containerfile as ca.crt; a variable so that a test can point it elsewhere.
var hostCABundle = "/etc/ssl/certs/ca-certificates.crt"

var (
	buildHashPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)
	// examTagPattern is what an exam's ID must be for its name to be part of an image's: image names are lowercase.
	examTagPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
	// BuiltName matches the name image.inspect takes for an exam's built image: cbe-exam/<exam>:<build>.
	builtNamePattern = regexp.MustCompile(`^cbe-exam/([a-z0-9][a-z0-9_-]{0,63}):([0-9a-f]{16})$`)
)

// BuiltReference is the reference an exam's built image has in the runtime.
func BuiltReference(exam, build string) string { return builtRepository + exam + ":" + build }

// BuiltName is the name of an exam's built image as image.inspect takes it.
func BuiltName(exam, build string) string { return "cbe-exam/" + exam + ":" + build }

// BuildRequest is image.build: whose image, from which approved course image, and whether to build again what is already built.
type BuildRequest struct {
	Exam           string `json:"exam"`
	Image          string `json:"image"`
	Rebuild        bool   `json:"rebuild,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

// The ways a build can fail, in BuildResult.Failure.
const (
	FailContainerfile = "containerfile"
	FailBase          = "base"
	FailBuild         = "build"
	FailTimeout       = "timeout"
	FailVerify        = "verify"
)

// BuildResult is what image.build returns, whatever way the build ended: a Containerfile that is refused, a step that fails or a build that
// takes too long are results, and the exam server decides what they mean for the exam.
type BuildResult struct {
	Exam string `json:"exam"`
	OK   bool   `json:"ok"`
	// Failure is one of the Fail… codes when OK is false, and Message says it in a sentence the proctor can read.
	Failure string `json:"failure,omitempty"`
	Message string `json:"message,omitempty"`
	// Cached says the image was there already, built from the same files on the same base, and nothing was built.
	Cached bool   `json:"cached,omitempty"`
	Hash   string `json:"hash,omitempty"`
	Tag    string `json:"tag,omitempty"`
	// ImageID is the runtime's ID of the image; BaseName is the course image and BaseDigest the digest of it that was built on.
	ImageID    string   `json:"image_id,omitempty"`
	BaseName   string   `json:"base_name"`
	BaseDigest string   `json:"base_digest,omitempty"`
	Log        string   `json:"log"`
	Warnings   []string `json:"warnings,omitempty"`
	DurationMS int64    `json:"duration_ms"`
}

// logBuffer keeps the first logHeadBytes of what is written to it and the last of the rest, so that a log of any length fits in maxLogBytes
// with the start of the build and its end, which is where the error is. The end is a ring, so a write costs what it is long.
type logBuffer struct {
	head []byte
	ring []byte
	// pos is where the next byte of the ring goes, and written how many bytes went to it in all.
	pos     int
	written int64
}

func newLogBuffer() *logBuffer { return &logBuffer{ring: make([]byte, maxLogBytes-logHeadBytes)} }

func (l *logBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if room := logHeadBytes - len(l.head); room > 0 {
		take := min(room, len(p))
		l.head = append(l.head, p[:take]...)
		p = p[take:]
	}
	l.written += int64(len(p))
	if len(p) >= len(l.ring) {
		copy(l.ring, p[len(p)-len(l.ring):])
		l.pos = 0
		return n, nil
	}
	first := copy(l.ring[l.pos:], p)
	copy(l.ring, p[first:])
	l.pos = (l.pos + len(p)) % len(l.ring)
	return n, nil
}

func (l *logBuffer) String() string {
	text := string(l.head)
	switch {
	case l.written <= int64(len(l.ring)):
		text += string(l.ring[:l.written])
	default:
		text += fmt.Sprintf("\n[… %d bytes of the log are not kept: the start and the end of it are …]\n", l.written-int64(len(l.ring)))
		text += string(l.ring[l.pos:]) + string(l.ring[:l.pos])
	}
	return strings.ToValidUTF8(text, "�")
}

// repository returns a reference without its tag or digest.
func repository(ref string) string {
	if at := strings.Index(ref, "@"); at >= 0 {
		ref = ref[:at]
	}
	if colon := strings.LastIndex(ref, ":"); colon > strings.LastIndex(ref, "/") {
		ref = ref[:colon]
	}
	return ref
}

// baseDigest is the digest the runtime knows an image by: the one of its repository, else the first it has, else (an image made some other
// way) its own ID.
func baseDigest(ref string, info ImageInfo) string {
	repo := repository(ref)
	for _, d := range info.RepoDigests {
		if name, digest, ok := strings.Cut(d, "@"); ok && name == repo {
			return digest
		}
	}
	for _, d := range info.RepoDigests {
		if _, digest, ok := strings.Cut(d, "@"); ok {
			return digest
		}
	}
	return info.ID
}

// checkImage is what a container or a grading run needs to know about the image it will be made from: that the runtime has it, and that,
// when it is an exam's built image, it is the image built for that exam and not another's.
func (s *Service) checkImage(ctx context.Context, exam, build, reference string) error {
	info, err := s.rt.ImageInspect(ctx, reference)
	if errors.Is(err, ErrNotFound) {
		return fmt.Errorf("%w: the runtime does not have image %s", ErrNotFound, reference)
	}
	if err != nil {
		return err
	}
	if build != "" && (info.Labels[LabelBuildExam] != exam || info.Labels[LabelBuildHash] != build) {
		return fmt.Errorf("%w: image %s was not built for this exam", ErrNotApproved, reference)
	}
	return nil
}

// BuildImage is image.build. One build runs at a time; others wait. It returns when the build is over and everything it made but the image
// is removed, whatever way it ended.
func (s *Service) BuildImage(ctx context.Context, req BuildRequest, buildContext io.Reader) (BuildResult, error) {
	reference, approved := s.cfg.Images[req.Image]
	switch {
	case !examTagPattern.MatchString(req.Exam):
		return BuildResult{}, badRequest("the exam of a build must be lowercase letters, digits, _ and -, at most 64")
	case !approved:
		return BuildResult{}, fmt.Errorf("%w: %q", ErrNotApproved, req.Image)
	}
	seconds := req.TimeoutSeconds
	if seconds == 0 {
		seconds = s.cfg.BuildMaxSeconds
	}
	if seconds < 10 || seconds > s.cfg.BuildMaxSeconds {
		return BuildResult{}, badRequest("timeout_seconds must be 10 to %d", s.cfg.BuildMaxSeconds)
	}
	// What goes in is read and checked before anything is made. The context is the exam author's: it is refused if it is not what the
	// compiler makes, and it must have a Containerfile.
	raw, err := io.ReadAll(io.LimitReader(buildContext, maxBuildContextBytes+1))
	if err != nil {
		return BuildResult{}, err
	}
	if len(raw) > maxBuildContextBytes {
		return BuildResult{}, badRequest("the build context holds more than %d MiB", maxBuildContextBytes>>20)
	}
	var clean bytes.Buffer
	if _, _, err := sanitize(bytes.NewReader(raw), &clean, sanitizeLimits{bytes: maxBuildContextBytes, files: maxBuildContextFiles, what: "build context"}, false); err != nil {
		return BuildResult{}, err
	}
	text, found := contextFile(clean.Bytes(), "Containerfile")
	if !found {
		return BuildResult{}, badRequest("the build context has no file called Containerfile")
	}
	rawSum := sha256.Sum256(raw)

	select {
	case s.buildSlot <- struct{}{}:
		defer func() { <-s.buildSlot }()
	case <-ctx.Done():
		return BuildResult{}, ctx.Err()
	}
	started := time.Now()
	result := BuildResult{Exam: req.Exam, BaseName: req.Image}
	log := newLogBuffer()
	finish := func(code, message string) (BuildResult, error) {
		result.OK = code == ""
		result.Failure, result.Message = code, message
		if code != "" {
			fmt.Fprintf(log, "\n%s\n", message)
		}
		result.Log = log.String()
		result.DurationMS = time.Since(started).Milliseconds()
		s.logger.Info("image build ended", "exam", req.Exam, "ok", result.OK, "failure", code, "cached", result.Cached, "hash", result.Hash, "ms", result.DurationMS)
		return result, nil
	}

	// The course image, and the digest the build is pinned to.
	base, err := s.rt.ImageInspect(ctx, reference)
	if errors.Is(err, ErrNotFound) {
		return finish(FailBase, fmt.Sprintf("this server does not have the course image %s: ask whoever runs it to build it", req.Image))
	}
	if err != nil {
		return BuildResult{}, err
	}
	digest := baseDigest(reference, base)
	result.BaseDigest = digest
	pinnedRef := repository(reference) + "@" + digest
	check := containerfile.Check(string(text), req.Image)
	pinnedBy := "pinned by this build to the image the server has"
	if check.Digest != "" {
		pinnedBy = "pinned by the Containerfile"
	}
	fmt.Fprintf(log, "Base image: %s is %s\n            digest %s (%s)\n", req.Image, reference, digest, pinnedBy)
	fmt.Fprintf(log, "Build context: %d bytes, sha256:%s\n", len(raw), hex.EncodeToString(rawSum[:]))
	for _, warning := range check.Warnings {
		fmt.Fprintf(log, "warning: Containerfile %s\n", warning)
	}
	result.Warnings = check.Warnings
	if len(check.Problems) > 0 {
		for _, problem := range check.Problems {
			fmt.Fprintf(log, "Containerfile %s\n", problem)
		}
		return finish(FailContainerfile, "the Containerfile is not accepted: "+strings.Join(check.Problems, "; "))
	}
	if check.Digest != "" && check.Digest != digest {
		return finish(FailBase, fmt.Sprintf("the Containerfile builds on %s@%s, but the %s of this server is %s: write the FROM without a digest to take this server's image, or the digest of this one", req.Image, check.Digest, req.Image, digest))
	}

	// The same files on the same base give the same image: the tag has the exam and a hash of both in it.
	sum := sha256.Sum256([]byte("cbe-exam-image/1\n" + digest + "\n" + hex.EncodeToString(rawSum[:])))
	result.Hash = hex.EncodeToString(sum[:8])
	result.Tag = BuiltReference(req.Exam, result.Hash)
	if existing, err := s.rt.ImageInspect(ctx, result.Tag); err == nil && !req.Rebuild && existing.Labels[LabelBuildExam] == req.Exam && existing.Labels[LabelBuildHash] == result.Hash {
		result.Cached, result.ImageID = true, existing.ID
		fmt.Fprintf(log, "\nThe image %s is built already from these files on this base: nothing was built.\n", result.Tag)
		return finish("", "")
	}

	// The build: the context with the Containerfile pinned to the digest, and the host's certificate bundle lent as ca.crt (the course image
	// has none: a step that reaches an HTTPS archive mounts it, as images/build.sh does, and it is not in the image).
	pinned, err := containerfile.Pin(string(text), req.Image, pinnedRef)
	if err != nil {
		return finish(FailContainerfile, "the Containerfile is not accepted: "+err.Error())
	}
	packed, err := repack(clean.Bytes(), pinned)
	if err != nil {
		return BuildResult{}, err
	}
	pending := pendingRepo + req.Exam + ":" + result.Hash
	_ = s.rt.ImageRemove(ctx, pending)
	defer func() {
		bg, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = s.rt.ImageRemove(bg, pending)
	}()
	fmt.Fprintf(log, "\nBuilding %s\n\n", result.Tag)
	building, stop := context.WithTimeout(ctx, time.Duration(seconds)*buildSecond)
	err = s.rt.ImageBuild(building, ImageBuildSpec{Tag: pending, NoCache: req.Rebuild, MemoryBytes: int64(s.cfg.BuildMemoryMB) << 20, Labels: map[string]string{
		LabelBuildExam: req.Exam, LabelBuildHash: result.Hash, LabelBuildBase: digest, LabelBuildImage: req.Image}}, packed, log)
	stop()
	switch {
	case err == nil:
	case ctx.Err() != nil:
		return BuildResult{}, ctx.Err()
	case errors.Is(err, context.DeadlineExceeded):
		return finish(FailTimeout, fmt.Sprintf("the build did not end in %d seconds and was stopped", seconds))
	case errors.Is(err, ErrBuildFailed):
		return finish(FailBuild, strings.TrimSpace(strings.TrimPrefix(err.Error(), ErrBuildFailed.Error()+": ")))
	default:
		return BuildResult{}, err
	}

	// What the build made is checked before it is kept.
	built, err := s.rt.ImageInspect(ctx, pending)
	if errors.Is(err, ErrNotFound) {
		return finish(FailBuild, "the build ended without making an image")
	}
	if err != nil {
		return BuildResult{}, err
	}
	if message := s.verifyImage(ctx, req, base, built, pending); message != "" {
		return finish(FailVerify, message)
	}
	if err := s.rt.ImageTag(ctx, pending, result.Tag); err != nil {
		return BuildResult{}, fmt.Errorf("naming the image: %w", err)
	}
	result.ImageID = built.ID
	if existing, err := s.rt.ImageInspect(ctx, result.Tag); err == nil && existing.ID != built.ID {
		return BuildResult{}, fmt.Errorf("naming the image: %s is a different image", result.Tag)
	}
	fmt.Fprintf(log, "\nVerified: made from %s, user %s, the agent is the course image's. Tagged %s (image %s).\n", req.Image, built.User, result.Tag, built.ID)
	return finish("", "")
}

// verifyImage says what is wrong with a built image, or "" when it is what the course image's contract needs (decision P7-8): it is made from
// the course image, its user, entrypoint, command and working folder are the course image's, and the agent in it is the course image's agent.
func (s *Service) verifyImage(ctx context.Context, req BuildRequest, base, built ImageInfo, ref string) string {
	if len(base.Layers) == 0 || len(built.Layers) < len(base.Layers) || !slices.Equal(built.Layers[:len(base.Layers)], base.Layers) {
		return fmt.Sprintf("the image is not made from the course image %s: its first layers are not that image's (a build must start from %s and add to it)", req.Image, req.Image)
	}
	if built.User != base.User {
		return fmt.Sprintf("the build changed the user of the image to %q, and it must stay %q: end the Containerfile with USER %s", built.User, base.User, base.User)
	}
	if !slices.Equal(built.Entrypoint, base.Entrypoint) {
		return fmt.Sprintf("the build changed the entrypoint of the image to %q, and it must stay %q: the agent is what runs in a student's container", built.Entrypoint, base.Entrypoint)
	}
	if !slices.Equal(built.Cmd, base.Cmd) {
		return fmt.Sprintf("the build changed the command of the image to %q, and it must stay %q", built.Cmd, base.Cmd)
	}
	if built.WorkDir != base.WorkDir {
		return fmt.Sprintf("the build changed the working folder of the image to %q, and it must stay %q", built.WorkDir, base.WorkDir)
	}
	want := base.Labels[LabelAgent]
	if want == "" {
		return "" // a course image without the agent's label is not one this check can say more about
	}
	if built.Labels[LabelAgent] != want {
		return fmt.Sprintf("the build changed the label %s, which says which agent the image holds", LabelAgent)
	}
	got, err := s.agentHash(ctx, req.Exam, built, ref)
	if err != nil {
		return "the agent in the image could not be checked: " + err.Error()
	}
	if got != want {
		return fmt.Sprintf("the file %s of the image is not the course image's agent (sha256 %s, and it must be %s): a build must not replace it", AgentPath, got, want)
	}
	return ""
}

// agentHash hashes the agent in an image, in a container made as a student's is (no network, a read-only root, no capabilities) that runs
// sha256sum instead of the agent, and is removed after.
func (s *Service) agentHash(ctx context.Context, exam string, image ImageInfo, ref string) (string, error) {
	spec := hardened(s.cfg, Resources{CPU: 1, MemoryMB: 128, DiskMB: 1}, ref)
	id := strings.TrimPrefix(image.ID, "sha256:")
	spec.Name = "cbe-verify-" + exam + "-" + id[:min(12, len(id))]
	spec.Labels = map[string]string{LabelRole: RoleVerify, LabelExam: exam}
	spec.Entrypoint = []string{"/usr/bin/sha256sum"}
	spec.Cmd = []string{AgentPath}
	bg := func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(context.Background(), 30*time.Second)
	}
	_ = s.rt.Remove(ctx, spec.Name)
	if _, err := s.rt.Create(ctx, spec); err != nil {
		return "", fmt.Errorf("the checking container: %w", err)
	}
	defer func() {
		c, cancel := bg()
		defer cancel()
		_ = s.rt.Remove(c, spec.Name)
	}()
	collector, err := s.rt.Attach(ctx, spec.Name, 4<<10)
	if err != nil {
		return "", err
	}
	defer collector.Close()
	if err := s.rt.Start(ctx, spec.Name); err != nil {
		return "", err
	}
	running, stop := context.WithTimeout(ctx, verifyTimeout)
	code, err := s.rt.Wait(running, spec.Name)
	stop()
	if err != nil {
		c, cancel := bg()
		_ = s.rt.Stop(c, spec.Name, time.Second)
		cancel()
		return "", fmt.Errorf("it did not end: %w", err)
	}
	reading, stopReading := context.WithTimeout(ctx, 20*time.Second)
	out, _ := collector.Collect(reading)
	stopReading()
	if code != 0 {
		return "", fmt.Errorf("sha256sum ended with code %d: %s", code, strings.TrimSpace(string(out.Stderr)))
	}
	fields := strings.Fields(string(out.Stdout))
	if len(fields) == 0 || len(fields[0]) != 64 {
		return "", fmt.Errorf("sha256sum printed %q", strings.TrimSpace(string(out.Stdout)))
	}
	return fields[0], nil
}

// contextFile returns the contents of a regular file at the top of a tar archive.
func contextFile(archive []byte, name string) ([]byte, bool) {
	reader := tar.NewReader(bytes.NewReader(archive))
	for {
		header, err := reader.Next()
		if err != nil {
			return nil, false
		}
		if header.Typeflag == tar.TypeReg && strings.TrimPrefix(header.Name, "./") == name {
			data, err := io.ReadAll(io.LimitReader(reader, containerfile.MaxBytes+1))
			return data, err == nil
		}
	}
}

// repack is the build context a build is given: the exam's, with the Containerfile replaced by the pinned one and the host's certificate
// bundle added as ca.crt (when the host has one).
func repack(archive []byte, pinned string) (io.Reader, error) {
	var out bytes.Buffer
	writer := tar.NewWriter(&out)
	reader := tar.NewReader(bytes.NewReader(archive))
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if header.Typeflag == tar.TypeReg && header.Name == "Containerfile" {
			header.Size = int64(len(pinned))
			if err := writer.WriteHeader(header); err != nil {
				return nil, err
			}
			if _, err := io.WriteString(writer, pinned); err != nil {
				return nil, err
			}
			continue
		}
		if err := writer.WriteHeader(header); err != nil {
			return nil, err
		}
		if _, err := io.Copy(writer, reader); err != nil {
			return nil, err
		}
	}
	if ca, err := os.ReadFile(hostCABundle); err == nil && len(ca) > 0 {
		if err := writer.WriteHeader(&tar.Header{Name: "ca.crt", Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(ca)), ModTime: time.Unix(0, 0)}); err != nil {
			return nil, err
		}
		if _, err := writer.Write(ca); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return &out, nil
}

// sweepVerifiers removes the checking containers a runner that stopped left.
func (s *Service) sweepVerifiers(ctx context.Context) int {
	list, err := s.rt.List(ctx, LabelRole, RoleVerify)
	if err != nil {
		return 0
	}
	removed := 0
	for _, c := range list {
		if err := s.rt.Remove(ctx, c.Name); err == nil || errors.Is(err, ErrNotFound) {
			removed++
		}
	}
	return removed
}

// imageStatusOfBuilt is image.inspect for the name of an exam's built image: it is "approved" when the runtime has an image of that name
// that was built for that exam, with that hash.
func (s *Service) imageStatusOfBuilt(ctx context.Context, name string, match []string) (ImageStatus, error) {
	status := ImageStatus{Name: name}
	exam, build := match[1], match[2]
	info, err := s.rt.ImageInspect(ctx, BuiltReference(exam, build))
	if errors.Is(err, ErrNotFound) {
		return status, nil
	}
	if err != nil {
		return status, err
	}
	status.Present, status.ID = true, info.ID
	status.Approved = info.Labels[LabelBuildExam] == exam && info.Labels[LabelBuildHash] == build
	return status, nil
}
