package runner

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// grading.run (Appendix E, Phase 6): a fresh container from the exam's image, made from the same hardening as a student's (no network, a
// read-only root, no capabilities, the same limits), with the submission copied to /work and the exam's grading bundle mounted
// read-only at /grading, which runs /grading/grade and is thrown away when it ends. The runner returns what it printed and how it ended; what
// the output means is for the exam server.

// Where the grading bundle is inside a grading container, and the labels that tell a grading container from a student's. A grading
// container has no cbe.managed label: the runner does not take it for a student's session when it looks for those.
const (
	GradingPath  = "/grading"
	LabelRole    = "cbe.role"
	RoleGrading  = "grading"
	LabelGrading = "cbe.grading"
	// MaxSubmissionBytes is the most a submission may hold for a grading container (the files of one student; the exam's /work quota is the
	// real limit). DefaultGradeSeconds is the plan's timeout when a request names none.
	MaxSubmissionBytes  = 64 << 20
	DefaultGradeSeconds = 300
	// maxGradingIDBytes keeps "grade-" and the ID within the quota helper's 64 bytes.
	maxGradingIDBytes = 48
)

// GradeRequest is the JSON part of grading.run: which run (an ID the exam server chose, unique), of which exam, in which approved image,
// for how long at most, and with which limits.
type GradeRequest struct {
	Grading        string    `json:"grading"`
	Exam           string    `json:"exam"`
	Image          string    `json:"image"`
	TimeoutSeconds int       `json:"timeout_seconds"`
	Resources      Resources `json:"resources"`
}

// GradeResult is what a grading run returns: how it ended, and what it printed. A run that failed, timed out or was killed for its memory is
// a result, not an error: the exam server decides what that means for the student.
type GradeResult struct {
	Grading         string `json:"grading"`
	ExitCode        int    `json:"exit_code"`
	TimedOut        bool   `json:"timed_out"`
	OOMKilled       bool   `json:"oom_killed"`
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	StdoutTruncated bool   `json:"stdout_truncated,omitempty"`
	StderrTruncated bool   `json:"stderr_truncated,omitempty"`
	DurationMS      int64  `json:"duration_ms"`
	// Skipped names the entries of the submission the container was not given (a link out of /work, a name it cannot hold), at most 20.
	Skipped []string `json:"skipped,omitempty"`
}

// GradeContainerName is the name of a grading container.
func GradeContainerName(exam, grading string) string { return "cbe-grade-" + exam + "-" + grading }

// gradeWorkID names the /work folder of a grading run for the quota helper; it cannot be a session's (those are IDs, this has a prefix).
func gradeWorkID(grading string) string { return "grade-" + grading }

// CheckGrade validates a grading request and returns the limits the container gets, the image's reference and the time the run may take.
func (c Config) CheckGrade(req GradeRequest) (Resources, string, time.Duration, error) {
	if len(req.Grading) > maxGradingIDBytes {
		return Resources{}, "", 0, badRequest("grading must be an ID of at most %d bytes", maxGradingIDBytes)
	}
	resources, reference, err := c.Check(CreateRequest{Session: req.Grading, Exam: req.Exam, Image: req.Image, Resources: req.Resources})
	if err != nil {
		return Resources{}, "", 0, err
	}
	seconds := req.TimeoutSeconds
	if seconds == 0 {
		seconds = DefaultGradeSeconds
	}
	if seconds < 1 || seconds > c.GradeMaxSeconds {
		return Resources{}, "", 0, badRequest("timeout_seconds must be 1 to %d", c.GradeMaxSeconds)
	}
	return resources, reference, time.Duration(seconds) * time.Second, nil
}

// BuildGradingSpec is the only place a grading container's options are decided: the student's own hardening (hardened), the two mounts
// (the run's /work, and the grading bundle read-only), and /grading/grade as the program, in place of the agent.
func BuildGradingSpec(c Config, req GradeRequest, resources Resources, reference string) ContainerSpec {
	spec := hardened(c, resources, reference)
	spec.Name = GradeContainerName(req.Exam, req.Grading)
	spec.Labels = map[string]string{LabelRole: RoleGrading, LabelGrading: req.Grading, LabelExam: req.Exam, LabelImage: req.Image}
	spec.Binds = []Bind{
		{Source: filepath.Join(c.WorkRoot, gradeWorkID(req.Grading)), Target: WorkPath},
		{Source: filepath.Join(c.GradeRoot, req.Grading, "grading"), Target: GradingPath, ReadOnly: true},
	}
	spec.Entrypoint = []string{GradingPath + "/grade"}
	spec.Cmd = []string{}
	spec.Env = []string{"HOME=" + WorkPath, "CBE_GRADING=1"}
	return spec
}

// gradeNote is what the runner writes down about a run in progress, so that a runner that was restarted can remove what the run left.
type gradeNote struct {
	Grading string `json:"grading"`
	Exam    string `json:"exam"`
}

func (s *Service) gradeNotes() string { return filepath.Join(s.cfg.StateRoot, "grading") }
func (s *Service) gradeNotePath(grading string) string {
	return filepath.Join(s.gradeNotes(), grading+".json")
}

// Grade is grading.run. At most GradeParallel runs go at once; the others wait for a place. It returns when the run is over and
// everything it made is removed, whatever the way it ended.
func (s *Service) Grade(ctx context.Context, req GradeRequest, submission, bundle io.Reader) (GradeResult, error) {
	resources, reference, timeout, err := s.cfg.CheckGrade(req)
	if err != nil {
		return GradeResult{}, err
	}
	select {
	case s.gradeSlots <- struct{}{}:
		defer func() { <-s.gradeSlots }()
	case <-ctx.Done():
		return GradeResult{}, ctx.Err()
	}
	defer s.lock(gradeWorkID(req.Grading))()
	started := time.Now()

	// What goes in is checked before anything is made. The submission is the student's: what a container cannot be given is left out and
	// named. The bundle is the exam's: it is refused if it is not what the compiler makes, and it must have a grade that can be run.
	var work, grading bytes.Buffer
	_, skipped, err := sanitize(io.LimitReader(submission, MaxSubmissionBytes+MaxSeedFiles*1024+1<<20), &work, sanitizeLimits{bytes: MaxSubmissionBytes, files: s.cfg.Inodes, what: "submission"}, true)
	if err != nil {
		return GradeResult{}, err
	}
	if _, _, err := sanitize(io.LimitReader(bundle, maxSeedBody), &grading, sanitizeLimits{bytes: MaxSeedBytes, files: MaxSeedFiles, what: "grading bundle"}, false); err != nil {
		return GradeResult{}, err
	}
	if !hasGrade(grading.Bytes()) {
		return GradeResult{}, badRequest("the grading bundle has no file called grade that can be run")
	}

	name := GradeContainerName(req.Exam, req.Grading)
	if err := os.MkdirAll(s.gradeNotes(), 0o700); err != nil {
		return GradeResult{}, err
	}
	note, _ := json.Marshal(gradeNote{Grading: req.Grading, Exam: req.Exam})
	if err := os.WriteFile(s.gradeNotePath(req.Grading), note, 0o600); err != nil {
		return GradeResult{}, err
	}
	// Whatever happens next, what the run made is removed, with a context of its own: the request's may be the reason it is ending.
	defer s.removeGrading(req.Grading, req.Exam)

	dir := filepath.Join(s.cfg.GradeRoot, req.Grading)
	if err := os.MkdirAll(filepath.Join(dir, "grading"), 0o755); err != nil {
		return GradeResult{}, err
	}
	if err := extractBundle(&grading, filepath.Join(dir, "grading")); err != nil {
		return GradeResult{}, err
	}
	if _, err := s.rt.ImageInspect(ctx, reference); err != nil {
		if errors.Is(err, ErrNotFound) {
			return GradeResult{}, fmt.Errorf("%w: the runtime does not have image %s", ErrNotFound, reference)
		}
		return GradeResult{}, err
	}
	if _, err := s.quota.Create(gradeWorkID(req.Grading), resources.DiskMB, s.cfg.Inodes); err != nil {
		return GradeResult{}, fmt.Errorf("the /work folder of the run: %w", err)
	}
	if _, err := s.rt.Create(ctx, BuildGradingSpec(s.cfg, req, resources, reference)); err != nil {
		return GradeResult{}, fmt.Errorf("the grading container: %w", err)
	}
	if err := s.rt.CopyTo(ctx, name, WorkPath, &work); err != nil {
		return GradeResult{}, fmt.Errorf("putting the submission in /work: %w", err)
	}
	// What it prints is read from the moment it starts, to the end of its output.
	collector, err := s.rt.Attach(ctx, name, int64(s.cfg.GradeOutputBytes))
	if err != nil {
		return GradeResult{}, fmt.Errorf("attaching to the grading container: %w", err)
	}
	defer collector.Close()
	if err := s.rt.Start(ctx, name); err != nil {
		return GradeResult{}, fmt.Errorf("starting the grading container: %w", err)
	}

	result := GradeResult{Grading: req.Grading, Skipped: skipped}
	running, stop := context.WithTimeout(ctx, timeout)
	code, waitErr := s.rt.Wait(running, name)
	stop()
	switch {
	case waitErr == nil:
		result.ExitCode = code
	case ctx.Err() != nil:
		return GradeResult{}, ctx.Err()
	case errors.Is(waitErr, context.DeadlineExceeded):
		// Time is up: the container is stopped (killed after a second), and what it had printed is what is returned.
		result.TimedOut = true
		bg, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		_ = s.rt.Stop(bg, name, time.Second)
		cancel()
	default:
		return GradeResult{}, fmt.Errorf("waiting for the grading container: %w", waitErr)
	}
	bg, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if found, err := s.rt.Inspect(bg, name); err == nil {
		result.OOMKilled = found.OOMKilled
		if result.TimedOut {
			result.ExitCode = found.ExitCode
		}
	}
	// The container has ended, so its output ends with it. A stream that does not end in a little while (the runtime lost the end of it) gives
	// what was read, and the run is judged by that.
	reading, stopReading := context.WithTimeout(bg, 20*time.Second)
	out, err := collector.Collect(reading)
	stopReading()
	if err != nil {
		s.logger.Warn("what a grading container printed was not read to its end", "grading", req.Grading, "error", err)
	}
	result.Stdout, result.Stderr = strings.ToValidUTF8(string(out.Stdout), "�"), strings.ToValidUTF8(string(out.Stderr), "�")
	result.StdoutTruncated, result.StderrTruncated = out.StdoutTruncated, out.StderrTruncated
	result.DurationMS = time.Since(started).Milliseconds()
	s.logger.Info("grading run ended", "grading", req.Grading, "exam", req.Exam, "exit_code", result.ExitCode, "timed_out", result.TimedOut, "oom", result.OOMKilled, "ms", result.DurationMS)
	return result, nil
}

// removeGrading removes everything a grading run made: its container, its /work folder, its copy of the bundle and its note.
func (s *Service) removeGrading(grading, exam string) {
	bg, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.rt.Remove(bg, GradeContainerName(exam, grading)); err != nil && !errors.Is(err, ErrNotFound) {
		s.logger.Warn("a grading container could not be removed", "grading", grading, "error", err)
	}
	if err := s.quota.Remove(gradeWorkID(grading)); err != nil {
		s.logger.Warn("the /work folder of a grading run could not be removed", "grading", grading, "error", err)
	}
	_ = os.RemoveAll(filepath.Join(s.cfg.GradeRoot, grading))
	_ = os.Remove(s.gradeNotePath(grading))
}

// sweepGradings removes what runs that were going when the runner stopped left: their containers (by label, and by the notes), their
// folders and their copies of the bundle. No run can be going when the runner starts, so all of it goes.
func (s *Service) sweepGradings(ctx context.Context) int {
	removed := 0
	if list, err := s.rt.List(ctx, LabelRole, RoleGrading); err == nil {
		for _, c := range list {
			if err := s.rt.Remove(ctx, c.Name); err == nil || errors.Is(err, ErrNotFound) {
				removed++
			}
		}
	}
	if notes, err := os.ReadDir(s.gradeNotes()); err == nil {
		for _, entry := range notes {
			data, err := os.ReadFile(filepath.Join(s.gradeNotes(), entry.Name()))
			var note gradeNote
			if err == nil && json.Unmarshal(data, &note) == nil && ValidID(note.Grading) && ValidID(note.Exam) {
				_ = s.rt.Remove(ctx, GradeContainerName(note.Exam, note.Grading))
				_ = s.quota.Remove(gradeWorkID(note.Grading))
			}
			_ = os.Remove(filepath.Join(s.gradeNotes(), entry.Name()))
		}
	}
	if entries, err := os.ReadDir(s.cfg.GradeRoot); err == nil {
		for _, entry := range entries {
			_ = os.RemoveAll(filepath.Join(s.cfg.GradeRoot, entry.Name()))
		}
	}
	return removed
}

// hasGrade says whether a sanitized bundle has a regular file called grade that can be run.
func hasGrade(archive []byte) bool {
	reader := tar.NewReader(bytes.NewReader(archive))
	for {
		header, err := reader.Next()
		if err != nil {
			return false
		}
		if strings.TrimSuffix(header.Name, "/") == "grade" {
			return header.Typeflag == tar.TypeReg && header.Mode&0o111 != 0
		}
	}
}

// extractBundle writes a sanitized bundle into dir, which only the runner can enter. Names are relative and have no "..", links cannot
// leave the folder (sanitize saw to both), and modes are the ones sanitize chose, 0755 and 0644: the student user in the container is
// not the owner and needs to read and run them.
func extractBundle(r io.Reader, dir string) error {
	reader := tar.NewReader(r)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		target := filepath.Join(dir, filepath.FromSlash(strings.TrimSuffix(header.Name, "/")))
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			if err := os.Chmod(target, 0o755); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(header.Linkname, target); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
			if err != nil {
				return err
			}
			_, copyErr := io.CopyN(file, reader, header.Size)
			closeErr := file.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
			if err := os.Chmod(target, os.FileMode(header.Mode&0o777)); err != nil {
				return err
			}
		}
	}
}
