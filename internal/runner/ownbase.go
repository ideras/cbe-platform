package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"exam-platform/shared/containerfile"
)

// An exam with a base of its own (decision D-CB-1): its image/Containerfile starts FROM any image, and what comes out of it is finished by the runner
// (finalize.go: the student account, no way up, the agent, the entrypoint) and checked (the image's configuration, and `cbe-agent -selftest` in a
// container made as a student's is) before it is kept. The author's file is built as it is; the runner's layer is built after it, on top of it.

// ownBuild is what BuildImageLive has made by the time it knows the exam has a base of its own.
type ownBuild struct {
	log     *logBuffer
	result  *BuildResult
	finish  func(code, message string) (BuildResult, error)
	text    string
	clean   []byte
	rawSum  []byte
	seconds int
}

// SelfTestReport is what `cbe-agent -selftest` prints (internal/agent/selftest.go).
type SelfTestReport struct {
	Version string `json:"version"`
	OK      bool   `json:"ok"`
	Checks  []struct {
		Name   string `json:"name"`
		OK     bool   `json:"ok"`
		Detail string `json:"detail"`
	} `json:"checks"`
}

// Failed lists what did not pass, as a sentence.
func (r SelfTestReport) Failed() string {
	var out []string
	for _, c := range r.Checks {
		if !c.OK {
			out = append(out, c.Name+": "+c.Detail)
		}
	}
	return strings.Join(out, "; ")
}

func (s *Service) buildOwnBase(ctx context.Context, req BuildRequest, b *ownBuild) (BuildResult, error) {
	log, result, finish := b.log, b.result, b.finish
	if !s.cfg.CustomBase {
		return finish(FailPolicy, "this server does not build images on a base of the exam's own (custom_base is off in its runner settings): the exam must name one of the approved course images")
	}
	check := containerfile.Check(b.text, "")
	if !req.NoWarnings {
		for _, warning := range check.Warnings {
			fmt.Fprintf(log, "warning: Containerfile %s\n", warning)
		}
		result.Warnings = check.Warnings
	}
	if len(check.Problems) > 0 {
		for _, problem := range check.Problems {
			fmt.Fprintf(log, "Containerfile %s\n", problem)
		}
		return finish(FailContainerfile, "the Containerfile is not accepted: "+strings.Join(check.Problems, "; "))
	}
	pulled := containerfile.References(b.text)
	if len(pulled) > 0 {
		result.BaseName = pulled[0].Text
		fmt.Fprintf(log, "Base image: %s (the exam's own: the runner adds the agent and the student account after the Containerfile)\n", result.BaseName)
	}
	if len(s.cfg.BaseRegistries) > 0 {
		for _, ref := range pulled {
			if !s.cfg.AllowsRegistry(ref.Registry) {
				return finish(FailPolicy, fmt.Sprintf("FROM %s (line %d): this server does not take images from %s; it allows %s", ref.Text, ref.Line, ref.Registry, strings.Join(s.cfg.BaseRegistries, ", ")))
			}
		}
	}
	if s.cfg.AgentBinary == "" {
		return finish(FailPolicy, "this server has no agent to put in the image: agent_binary is not set in its runner settings (the installer sets it)")
	}
	agent, err := os.ReadFile(s.cfg.AgentBinary)
	if err != nil {
		return finish(FailPolicy, fmt.Sprintf("this server cannot read the agent to put in the image (%s): %v", s.cfg.AgentBinary, err))
	}
	agentSum := sha256.Sum256(agent)
	agentHex := hex.EncodeToString(agentSum[:])
	fmt.Fprintf(log, "Build context: %d bytes, sha256:%s\nAgent: sha256:%s\n", len(b.clean), hex.EncodeToString(b.rawSum), agentHex)

	// The same files with the same agent and the same finishing give the same image: the tag has the exam and a hash of them in it. (A base named by
	// a tag with no digest may be another image tomorrow: a rebuild is asked for, and the warning above says so.)
	sum := sha256.Sum256([]byte("cbe-exam-image/2\nown-base\n" + finalizeVersion + "\n" + agentHex + "\n" + hex.EncodeToString(b.rawSum)))
	result.Hash = hex.EncodeToString(sum[:8])
	result.Tag = BuiltReference(req.Exam, result.Hash)
	if existing, err := s.rt.ImageInspect(ctx, result.Tag); err == nil && !req.Rebuild && existing.Labels[LabelBuildExam] == req.Exam && existing.Labels[LabelBuildHash] == result.Hash {
		result.Cached, result.ImageID = true, existing.ID
		fmt.Fprintf(log, "\nThe image %s is built already from these files with this agent: nothing was built.\n", result.Tag)
		return b.finish("", "")
	}

	authors := pendingRepo + req.Exam + ":" + result.Hash + "-author"
	pending := pendingRepo + req.Exam + ":" + result.Hash
	cleanup := func() {
		bg, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = s.rt.ImageRemove(bg, pending)
		_ = s.rt.ImageRemove(bg, authors)
	}
	cleanup()
	defer cleanup()

	// The author's Containerfile, as it is written, with the host's certificate bundle lent as ca.crt.
	packed, err := repack(b.clean, b.text)
	if err != nil {
		return BuildResult{}, err
	}
	fmt.Fprintf(log, "\nBuilding the exam's image from its Containerfile\n\n")
	building, stop := context.WithTimeout(ctx, time.Duration(b.seconds)*buildSecond)
	err = s.rt.ImageBuild(building, ImageBuildSpec{Tag: authors, NoCache: req.Rebuild, MemoryBytes: int64(s.cfg.BuildMemoryMB) << 20}, packed, log)
	stop()
	if failed := s.ownBuildEnded(ctx, err, pulled, b.seconds, finish); failed != nil {
		return failed.result, failed.err
	}
	if len(pulled) > 0 {
		if info, err := s.rt.ImageInspect(ctx, pulled[0].Text); err == nil {
			result.BaseDigest = baseDigest(pulled[0].Text, info)
			fmt.Fprintf(log, "\nThe first base image is %s, digest %s\n", pulled[0].Text, result.BaseDigest)
		}
	}

	// The runner's layer: the student account, no way up, the agent, the way the image is started.
	fmt.Fprintf(log, "\nFinishing the image: the student account, no way up, the agent\n\n")
	last, err := finalizeContext(authors, agent, agentHex)
	if err != nil {
		return BuildResult{}, err
	}
	building, stop = context.WithTimeout(ctx, time.Duration(b.seconds)*buildSecond)
	err = s.rt.ImageBuild(building, ImageBuildSpec{Tag: pending, MemoryBytes: int64(s.cfg.BuildMemoryMB) << 20, Labels: map[string]string{
		LabelBuildExam: req.Exam, LabelBuildHash: result.Hash, LabelBuildBase: result.BaseDigest, LabelBuildImage: result.BaseName}}, last, log)
	stop()
	if failed := s.ownBuildEnded(ctx, err, nil, b.seconds, finish); failed != nil {
		if failed.result.Failure == FailBuild {
			failed.result, _ = finish(FailBuild, "the runner could not finish the image (the base has to be a Linux image with /bin/sh): "+failed.result.Message)
		}
		return failed.result, failed.err
	}

	built, err := s.rt.ImageInspect(ctx, pending)
	if errors.Is(err, ErrNotFound) {
		return finish(FailBuild, "the build ended without making an image")
	}
	if err != nil {
		return BuildResult{}, err
	}
	if message := s.verifyOwnBase(ctx, req, built, pending, agentHex, log); message != "" {
		return finish(FailVerify, message)
	}
	if err := s.rt.ImageTag(ctx, pending, result.Tag); err != nil {
		return BuildResult{}, fmt.Errorf("naming the image: %w", err)
	}
	result.ImageID = built.ID
	if existing, err := s.rt.ImageInspect(ctx, result.Tag); err == nil && existing.ID != built.ID {
		return BuildResult{}, fmt.Errorf("naming the image: %s is a different image", result.Tag)
	}
	fmt.Fprintf(log, "\nVerified: student 1000:1000, the agent, a shell, no way up. Tagged %s (image %s).\n", result.Tag, built.ID)
	return finish("", "")
}

// ownEnd is what ended a step of the build early: the result to return, or an error.
type ownEnd struct {
	result BuildResult
	err    error
}

// ownBuildEnded says what a build step's end means: nil when it succeeded; otherwise the way the whole build ends.
func (s *Service) ownBuildEnded(ctx context.Context, err error, pulled []containerfile.Reference, seconds int, finish func(string, string) (BuildResult, error)) *ownEnd {
	end := func(code, message string) *ownEnd {
		r, e := finish(code, message)
		return &ownEnd{r, e}
	}
	switch {
	case err == nil:
		return nil
	case ctx.Err() != nil:
		s.removeOwnWorkingContainers(pulled)
		return &ownEnd{BuildResult{}, ctx.Err()}
	case errors.Is(err, context.DeadlineExceeded):
		s.removeOwnWorkingContainers(pulled)
		return end(FailTimeout, fmt.Sprintf("the build did not end in %d seconds and was stopped", seconds))
	case errors.Is(err, ErrBuildFailed):
		return end(FailBuild, strings.TrimSpace(strings.TrimPrefix(err.Error(), ErrBuildFailed.Error()+": ")))
	}
	return &ownEnd{BuildResult{}, err}
}

// removeOwnWorkingContainers removes what a cancelled build left (the runtime keeps one working container for each image a build starts from).
func (s *Service) removeOwnWorkingContainers(pulled []containerfile.Reference) {
	seen := map[string]bool{}
	for _, ref := range pulled {
		if !seen[ref.Text] {
			seen[ref.Text] = true
			s.removeWorkingContainers(ref.Text, len(pulled))
		}
	}
	s.removeWorkingContainers(pendingRepo+"x", 2)
}

// AllowsRegistry says whether the administrator allows images from a registry (no list: every registry).
func (c Config) AllowsRegistry(registry string) bool {
	if len(c.BaseRegistries) == 0 {
		return true
	}
	for _, allowed := range c.BaseRegistries {
		if strings.EqualFold(allowed, registry) {
			return true
		}
	}
	return false
}

// ImageConfigProblem says what is wrong with the configuration of an image the runner has finished (the user, the entrypoint, the working folder, the
// agent's label, the volumes), or "" when it is a workspace image's. agentHex is the SHA-256 of the agent that was put in it.
func ImageConfigProblem(built ImageInfo, agentHex string) string {
	switch {
	case built.User != "1000:1000" && built.User != "1000":
		return fmt.Sprintf("the user of the image is %q, and it must be 1000:1000", built.User)
	case len(built.Entrypoint) != 1 || built.Entrypoint[0] != AgentPath:
		return fmt.Sprintf("the entrypoint of the image is %q, and it must be the agent, %s", built.Entrypoint, AgentPath)
	case built.WorkDir != WorkPath:
		return fmt.Sprintf("the working folder of the image is %q, and it must be %s", built.WorkDir, WorkPath)
	case built.Labels[LabelAgent] != agentHex:
		return fmt.Sprintf("the label %s does not say which agent the image holds", LabelAgent)
	case len(built.Volumes) > 0:
		return fmt.Sprintf("the image declares the volume(s) %s (VOLUME in the Containerfile of the base, as the postgres image does): a container made from it would get a writable folder outside the student's, with no quota. Start from an image that declares no volume (for the database: a plain ubuntu image with the server installed from its packages)", strings.Join(built.Volumes, ", "))
	}
	return ""
}

// verifyOwnBase says what is wrong with the image the runner finished, or "" when it is a workspace image: its configuration is what the runner's layer
// wrote, and the agent in it, run as a student's container is run, finds nothing wrong (cbe-agent -selftest).
func (s *Service) verifyOwnBase(ctx context.Context, req BuildRequest, built ImageInfo, ref, agentHex string, log *logBuffer) string {
	if problem := ImageConfigProblem(built, agentHex); problem != "" {
		return problem
	}
	out, code, err := s.runChecker(ctx, req.Exam, built, ref, []string{AgentPath}, []string{"-selftest"})
	if err != nil {
		return "the image could not be checked: " + err.Error()
	}
	var report SelfTestReport
	if jsonErr := json.Unmarshal(out.Stdout, &report); jsonErr != nil {
		return fmt.Sprintf("the agent in the image did not run (status %d): %s%s. The image must be a Linux image the agent, a static program, can run in: not a different architecture, and with /proc", code, strings.TrimSpace(string(out.Stderr)), strings.TrimSpace(string(out.Stdout)))
	}
	for _, c := range report.Checks {
		mark := "ok  "
		if !c.OK {
			mark = "FAIL"
		}
		fmt.Fprintf(log, "  self test  %s  %s\n", mark, c.Name)
	}
	if !report.OK {
		return "the image does not keep the contract of a workspace image: " + report.Failed()
	}
	return ""
}
