// Package imagecheck is the validator of an exam's own base image (decision D-CB-5): it takes the image/ folder of a package on an author's computer and does
// what the runner does when the exam is published, with that computer's Podman: the Containerfile's rules, the author's build, the runner's finishing layer
// (the student account, the agent, no way up), the image's configuration and the agent's self test in a container run as a student's is. What it says is what
// the runner will say, without waiting for the server and without an exam to import.
package imagecheck

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"exam-platform/internal/runner"
	"exam-platform/shared/containerfile"
)

// Podman runs the container runtime's command with arguments (without its name: `build`, `image inspect`...), writing what it prints to stdout and stderr.
type Podman func(ctx context.Context, args []string, stdout, stderr io.Writer) error

// Command is a Podman that runs a program: ["podman"], or ["podman", "--root", "/tmp/x"] for a storage of its own.
func Command(prefix ...string) Podman {
	return func(ctx context.Context, args []string, stdout, stderr io.Writer) error {
		cmd := exec.CommandContext(ctx, prefix[0], append(append([]string{}, prefix[1:]...), args...)...)
		cmd.Stdout, cmd.Stderr = stdout, stderr
		return cmd.Run()
	}
}

// Options say what to check and how.
type Options struct {
	// Dir is a package folder (its image/ folder is checked), an image folder, or the Containerfile itself.
	Dir string
	// Agent is the static cbe-agent the runner puts in every image it finishes (the installer puts it in INSTALL_DIR/bin).
	Agent string
	// Config holds the decisions of the server the exam is for (custom_base, base_registries): nil is no decision, every base allowed.
	Config *runner.Config
	// Podman is how the container runtime is run.
	Podman Podman
	// CABundle is the certificate bundle lent to the build as ca.crt, as the server does (empty: none).
	CABundle string
	// Rebuild builds without the cache. Keep leaves the two images in the storage, to look inside.
	Rebuild, Keep bool
	// Out is where the progress goes, with what the builds print.
	Out io.Writer
}

// SelfCheck is one thing the agent's self test looked at.
type SelfCheck struct {
	Name   string
	OK     bool
	Detail string
}

// Result is what a check came to. Problem is why it failed (empty when it passed), and Checks are the agent's self test.
type Result struct {
	OK       bool
	Problem  string
	Warnings []string
	Checks   []SelfCheck
	// Image is the finished image (kept only with Options.Keep).
	Image string
}

// Check does it. An error is something that stopped the check (no Podman, no agent, no folder); an image that is not accepted is a Result with a Problem.
func Check(ctx context.Context, o Options) (Result, error) {
	out := o.Out
	if out == nil {
		out = io.Discard
	}
	say := func(format string, args ...any) { fmt.Fprintf(out, format+"\n", args...) }

	folder, file, err := locate(o.Dir)
	if err != nil {
		return Result{}, err
	}
	text, err := os.ReadFile(file)
	if err != nil {
		return Result{}, err
	}
	say("Checking %s", file)

	// 1. The Containerfile's rules, which need no runtime.
	var result Result
	rules := containerfile.Check(string(text), "")
	result.Warnings = rules.Warnings
	for _, warning := range rules.Warnings {
		say("  warning: Containerfile %s", warning)
	}
	if len(rules.Problems) > 0 {
		for _, problem := range rules.Problems {
			say("  Containerfile %s", problem)
		}
		return fail(result, "the Containerfile is not accepted: "+strings.Join(rules.Problems, "; ")), nil
	}
	say("  rules       ok")

	// 2. The administrator's decisions, when this computer knows them.
	if o.Config != nil {
		if !o.Config.CustomBase {
			return fail(result, "this server does not build images on a base of the exam's own (custom_base is off in its runner settings): the exam must name one of the approved course images"), nil
		}
		for _, ref := range containerfile.References(string(text)) {
			if !o.Config.AllowsRegistry(ref.Registry) {
				return fail(result, fmt.Sprintf("FROM %s (line %d): this server does not take images from %s; it allows %s", ref.Text, ref.Line, ref.Registry, strings.Join(o.Config.BaseRegistries, ", "))), nil
			}
		}
		say("  server      ok (custom_base on, registries: %s)", registriesText(o.Config))
	}

	agent, err := os.ReadFile(o.Agent)
	if err != nil {
		return Result{}, fmt.Errorf("the agent to put in the image: %w (--agent names a static cbe-agent; the installer puts one in /opt/cbe-portal/bin)", err)
	}
	if o.Podman == nil {
		o.Podman = Command("podman")
	}
	var version bytes.Buffer
	if err := o.Podman(ctx, []string{"version", "--format", "{{.Client.Version}}"}, &version, &version); err != nil {
		return Result{}, fmt.Errorf("the container runtime does not answer (is podman installed, and does `podman info` work for this user?): %v %s", err, strings.TrimSpace(version.String()))
	}

	// 3. The author's build and the runner's, in a folder of their own.
	work, err := os.MkdirTemp("", "cbe-image-check-")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(work)
	id := make([]byte, 5)
	if _, err := rand.Read(id); err != nil {
		return Result{}, err
	}
	authors := "localhost/cbe-check/" + hex.EncodeToString(id) + ":author"
	finished := "localhost/cbe-check/" + hex.EncodeToString(id) + ":finished"
	defer func() {
		if !o.Keep {
			var ignored bytes.Buffer
			_ = o.Podman(context.Background(), []string{"rmi", "--force", finished, authors}, &ignored, &ignored)
		}
	}()

	context1 := filepath.Join(work, "author")
	if err := copyTree(folder, context1); err != nil {
		return Result{}, err
	}
	if o.CABundle != "" {
		if ca, err := os.ReadFile(o.CABundle); err == nil && len(ca) > 0 {
			if err := os.WriteFile(filepath.Join(context1, "ca.crt"), ca, 0o644); err != nil {
				return Result{}, err
			}
		}
	}
	say("\nBuilding the Containerfile (the base is pulled the first time: this can take minutes)\n")
	build := []string{"build", "--format", "docker", "--tag", authors}
	if o.Rebuild {
		build = append(build, "--no-cache")
	}
	if err := o.Podman(ctx, append(build, context1), out, out); err != nil {
		return fail(result, "the build of the Containerfile failed: "+err.Error()+" (the output above says which step)"), nil
	}

	context2 := filepath.Join(work, "finish")
	if err := os.MkdirAll(context2, 0o755); err != nil {
		return Result{}, err
	}
	for _, f := range runner.FinalizeFiles(authors, agent) {
		if err := os.WriteFile(filepath.Join(context2, f.Name), f.Data, fs.FileMode(f.Mode)); err != nil {
			return Result{}, err
		}
		_ = os.Chmod(filepath.Join(context2, f.Name), fs.FileMode(f.Mode))
	}
	say("\nFinishing the image: the student account, no way up, the agent\n")
	if err := o.Podman(ctx, []string{"build", "--format", "docker", "--tag", finished, context2}, out, out); err != nil {
		return fail(result, "the runner could not finish the image (the base has to be a Linux image with /bin/sh): "+err.Error()), nil
	}
	if o.Keep {
		result.Image = finished
	}

	// 4. What the finished image is.
	var inspected bytes.Buffer
	if err := o.Podman(ctx, []string{"image", "inspect", finished}, &inspected, io.Discard); err != nil {
		return Result{}, fmt.Errorf("the image that was built cannot be read: %w", err)
	}
	info, err := imageInfo(inspected.Bytes())
	if err != nil {
		return Result{}, err
	}
	if problem := runner.ImageConfigProblem(info, runner.AgentSum(agent)); problem != "" {
		return fail(result, problem), nil
	}
	say("  image       ok (user 1000:1000, the agent, no volume)")

	// 5. The agent's self test, in a container made as a student's is (internal/runner/policy.go, hardened).
	var report, stderr bytes.Buffer
	run := []string{"run", "--rm", "--user", "1000:1000", "--network", "none", "--read-only", "--cap-drop", "all", "--security-opt", "no-new-privileges",
		"--pids-limit", "256", "--memory", "256m", "--memory-swap", "256m", "--tmpfs", "/tmp:rw,noexec,nosuid,nodev,size=64m,mode=1777",
		"--workdir", runner.WorkPath, "--entrypoint", runner.AgentPath, finished, "-selftest"}
	runErr := o.Podman(ctx, run, &report, &stderr)
	var self runner.SelfTestReport
	if jsonErr := json.Unmarshal(report.Bytes(), &self); jsonErr != nil {
		detail := strings.TrimSpace(stderr.String() + report.String())
		if runErr != nil {
			detail = strings.TrimSpace(runErr.Error() + " " + detail)
		}
		return fail(result, "the agent in the image did not run: "+detail+". The image must be a Linux image the agent, a static program, can run in: the agent given with --agent must be built for this machine's architecture, and the image needs /proc"), nil
	}
	say("")
	for _, c := range self.Checks {
		mark := "ok  "
		if !c.OK {
			mark = "FAIL"
		}
		result.Checks = append(result.Checks, SelfCheck{Name: c.Name, OK: c.OK, Detail: c.Detail})
		line := fmt.Sprintf("  self test   %s  %s", mark, c.Name)
		if !c.OK {
			line += ": " + c.Detail
		}
		say("%s", line)
	}
	if !self.OK {
		return fail(result, "the image does not keep the contract of a workspace image: "+self.Failed()), nil
	}
	result.OK = true
	return result, nil
}

func fail(r Result, problem string) Result {
	r.OK, r.Problem = false, problem
	return r
}

func registriesText(c *runner.Config) string {
	if len(c.BaseRegistries) == 0 {
		return "any"
	}
	return strings.Join(c.BaseRegistries, ", ")
}

// locate finds the folder of the build context and its Containerfile: a package folder (image/Containerfile), an image folder, or the file itself.
func locate(dir string) (folder, file string, err error) {
	if dir == "" {
		return "", "", errors.New("name the package folder, its image/ folder or the Containerfile")
	}
	info, err := os.Stat(dir)
	if err != nil {
		return "", "", err
	}
	if !info.IsDir() {
		if filepath.Base(dir) != "Containerfile" {
			return "", "", fmt.Errorf("%s is not a folder, and not a file called Containerfile", dir)
		}
		return filepath.Dir(dir), dir, nil
	}
	for _, candidate := range []string{filepath.Join(dir, "image"), dir} {
		if file := filepath.Join(candidate, "Containerfile"); fileExists(file) {
			return candidate, file, nil
		}
	}
	return "", "", fmt.Errorf("%s has no image/Containerfile and no Containerfile (a Containerfile is not called Dockerfile here)", dir)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// copyTree copies the regular files and folders of a build context. A link is refused, as the server's compiler refuses it: what the build sees must be
// what the package holds.
func copyTree(from, to string) error {
	return filepath.WalkDir(from, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		target := filepath.Join(to, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o755)
		case d.Type().IsRegular():
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			return os.WriteFile(target, data, info.Mode().Perm())
		}
		return fmt.Errorf("%s is not a regular file or folder: a build context holds no links", path)
	})
}

// imageInfo reads the configuration of an image from `podman image inspect`.
func imageInfo(inspected []byte) (runner.ImageInfo, error) {
	var images []struct {
		ID     string `json:"Id"`
		Config struct {
			User       string              `json:"User"`
			Entrypoint any                 `json:"Entrypoint"`
			WorkingDir string              `json:"WorkingDir"`
			Labels     map[string]string   `json:"Labels"`
			Volumes    map[string]struct{} `json:"Volumes"`
		} `json:"Config"`
	}
	if err := json.Unmarshal(inspected, &images); err != nil || len(images) != 1 {
		return runner.ImageInfo{}, fmt.Errorf("the image that was built cannot be read: %v", err)
	}
	c := images[0].Config
	info := runner.ImageInfo{ID: images[0].ID, User: c.User, WorkDir: c.WorkingDir, Labels: c.Labels}
	switch entry := c.Entrypoint.(type) {
	case string:
		info.Entrypoint = []string{entry}
	case []any:
		for _, part := range entry {
			info.Entrypoint = append(info.Entrypoint, fmt.Sprint(part))
		}
	}
	for volume := range c.Volumes {
		info.Volumes = append(info.Volumes, volume)
	}
	sort.Strings(info.Volumes)
	return info, nil
}
