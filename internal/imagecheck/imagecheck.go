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
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
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
		// Every path the runtime is given is absolute: where the program runs from is nobody's business (and another account may not be able to enter it).
		cmd.Dir = "/"
		cmd.Stdout, cmd.Stderr = stdout, stderr
		return cmd.Run()
	}
}

// Options say what to check and how.
type Options struct {
	// Dir is a package folder (its image/ folder is checked), an image folder, or the Containerfile itself.
	Dir string
	// Agent is the static cbe-agent the runner puts in every image it finishes (the installer puts it in INSTALL_DIR/bin). AgentFrom says where it was
	// found, for the output ("agent_binary in /opt/cbe-portal/etc/runner.json").
	Agent, AgentFrom string
	// Release is the release the agent should be ("v0.19.0") and ReleaseOf whose release that is ("the server installed here"): an agent that says it
	// is another is a warning. Empty or "dev": no comparison.
	Release, ReleaseOf string
	// Epoch is the date the images, and every file in them, are stamped with (podman build --timestamp, and SOURCE_DATE_EPOCH when the Containerfile
	// declares that ARG), so that a build of the same files gives the same image. 0 is DefaultEpoch.
	Epoch int64
	// Reproducible builds everything twice from nothing and compares the two images: Result.Irreproducible says how they differ.
	Reproducible bool
	// Config holds the decisions of the server the exam is for (custom_base, base_registries): nil is no decision, every base allowed.
	Config *runner.Config
	// Podman is how the container runtime is run.
	Podman Podman
	// CABundle is the certificate bundle lent to the build as ca.crt, as the server does (empty: none).
	CABundle string
	// Rebuild builds without the cache. Keep leaves the two images in the storage, to look inside.
	Rebuild, Keep bool
	// Name keeps the image that is accepted, under the names Name:<its id> and Name:latest (`cbe-portalctl image install`): Name is a repository such as
	// localhost/cbe-course/nn-2026. Labels are put on the finished image. Without a Name, nothing is kept unless Keep is set.
	Name   string
	Labels map[string]string
	// Readable makes the folders of the build, and the files in them, readable by other accounts: the runtime that builds is another account's
	// (the runner's) when the server's administrator installs an image.
	Readable bool
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
	// Image is the finished image (kept only with Options.Keep or Options.Name: with a Name, its Name:latest), ImageID its ID and Tags the names it was given.
	Image   string
	ImageID string
	Tags    []string
	// Irreproducible says how two builds from nothing differed (Options.Reproducible): empty when they gave the same image, or were not compared.
	Irreproducible string
}

// DefaultEpoch is the date the images are stamped with when nothing else is said: 2000-01-01, as the course images always were.
const DefaultEpoch = 946684800

// Check does it. An error is something that stopped the check (no Podman, no agent, no folder); an image that is not accepted is a Result with a Problem.
func Check(ctx context.Context, o Options) (Result, error) {
	out := o.Out
	if out == nil {
		out = io.Discard
	}
	say := func(format string, args ...any) { fmt.Fprintf(out, format+"\n", args...) }

	folder, file, err := Locate(o.Dir)
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
	if problems := append(rules.Problems, copiesTheAgent(string(text))...); len(problems) > 0 {
		for _, problem := range problems {
			say("  Containerfile %s", problem)
		}
		return fail(result, "the Containerfile is not accepted: "+strings.Join(problems, "; ")), nil
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

	// 3. The agent, which goes in the image last: looked at before anything is built.
	agent, err := ReadAgent(ctx, o.Agent, runtime.GOARCH)
	if err != nil {
		return Result{}, fmt.Errorf("the agent to put in the image: %w", err)
	}
	warn := func(format string, args ...any) {
		text := fmt.Sprintf(format, args...)
		result.Warnings = append(result.Warnings, text)
		say("  warning: %s", text)
	}
	agentVersion, from := agent.Version, ""
	if agentVersion == "" {
		agentVersion = "version not known"
	}
	if o.AgentFrom != "" {
		from = " (" + o.AgentFrom + ")"
	}
	say("  agent       ok: %s%s, %s, linux/%s, static, sha256 %s", o.Agent, from, agentVersion, agent.Arch, agent.Sum[:12])
	if agent.VersionProblem != "" {
		warn("the agent's version could not be asked (%s): the image is not labelled with it", agent.VersionProblem)
	}
	if agent.Version != "" && o.Release != "" && o.Release != "dev" && agent.Version != o.Release {
		warn("the agent is %s and %s is %s: a runner talks to the agents of its own release", agent.Version, o.ReleaseOf, o.Release)
	}
	if o.Podman == nil {
		o.Podman = Command("podman")
	}
	var version bytes.Buffer
	if err := o.Podman(ctx, []string{"version", "--format", "{{.Client.Version}}"}, &version, &version); err != nil {
		return Result{}, fmt.Errorf("the container runtime does not answer (is podman installed, and does `podman info` work for this user?): %v %s", err, strings.TrimSpace(version.String()))
	}

	// 4. The author's build and the runner's, in a folder of their own.
	work, err := os.MkdirTemp("", "cbe-image-check-")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(work)
	if o.Readable {
		if err := os.Chmod(work, 0o755); err != nil {
			return Result{}, err
		}
	}
	id := make([]byte, 5)
	if _, err := rand.Read(id); err != nil {
		return Result{}, err
	}
	authors := "localhost/cbe-check/" + hex.EncodeToString(id) + ":author"
	finished := "localhost/cbe-check/" + hex.EncodeToString(id) + ":finished"
	kept := ""
	defer func() {
		if o.Keep {
			return
		}
		var ignored bytes.Buffer
		if kept != "" {
			// The image has its names now: only the check's are taken off. (rmi --force would take with it the images that are made on the author's image: this one.)
			_ = o.Podman(context.Background(), []string{"untag", kept, finished}, &ignored, &ignored)
			_ = o.Podman(context.Background(), []string{"untag", authors, authors}, &ignored, &ignored)
			return
		}
		_ = o.Podman(context.Background(), []string{"rmi", "--force", finished, authors}, &ignored, &ignored)
	}()

	context1 := filepath.Join(work, "author")
	if err := copyTree(folder, context1, o.Readable); err != nil {
		return Result{}, err
	}
	if o.CABundle != "" {
		if ca, err := os.ReadFile(o.CABundle); err == nil && len(ca) > 0 {
			if err := os.WriteFile(filepath.Join(context1, "ca.crt"), ca, 0o644); err != nil {
				return Result{}, err
			}
		}
	}
	context2 := filepath.Join(work, "finish")
	if err := os.MkdirAll(context2, 0o755); err != nil {
		return Result{}, err
	}
	for _, f := range runner.FinalizeFiles(authors, agent.Data) {
		if err := os.WriteFile(filepath.Join(context2, f.Name), f.Data, fs.FileMode(f.Mode)); err != nil {
			return Result{}, err
		}
		_ = os.Chmod(filepath.Join(context2, f.Name), fs.FileMode(f.Mode))
	}

	// Every file of both builds, and the images themselves, have the same date, so that the same files give the same image.
	epoch := o.Epoch
	if epoch == 0 {
		epoch = DefaultEpoch
	}
	stamp := strconv.FormatInt(epoch, 10)
	authorBuild := []string{"build", "--format", "docker", "--timestamp", stamp}
	if declares(string(text), "SOURCE_DATE_EPOCH") {
		authorBuild = append(authorBuild, "--build-arg", "SOURCE_DATE_EPOCH="+stamp)
	}
	finishing := []string{"build", "--format", "docker", "--timestamp", stamp}
	labels := map[string]string{}
	if agent.Version != "" {
		labels[LabelAgentVersion] = agent.Version
	}
	for key, value := range o.Labels {
		labels[key] = value
	}
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		finishing = append(finishing, "--label", key+"="+labels[key])
	}
	// buildBoth makes the author's image and finishes it, and says what failed. fromNothing builds the author's image without the cache (--rebuild),
	// and both builds when the finishing layer must be made again too (--reproducible).
	buildBoth := func(fromNothing, finishFromNothing bool) string {
		say("\nBuilding the Containerfile (the base is pulled the first time: this can take minutes)\n")
		build := append(append([]string{}, authorBuild...), "--tag", authors)
		if fromNothing {
			build = append(build, "--no-cache")
		}
		if err := o.Podman(ctx, append(build, context1), out, out); err != nil {
			return "the build of the Containerfile failed: " + err.Error() + " (the output above says which step)"
		}
		say("\nFinishing the image: the student account, no way up, the agent\n")
		finish := append(append([]string{}, finishing...), "--tag", finished)
		if finishFromNothing {
			finish = append(finish, "--no-cache")
		}
		if err := o.Podman(ctx, append(finish, context2), out, out); err != nil {
			return "the runner could not finish the image (the base has to be a Linux image with /bin/sh): " + err.Error()
		}
		return ""
	}
	if problem := buildBoth(o.Rebuild || o.Reproducible, o.Reproducible); problem != "" {
		return fail(result, problem), nil
	}
	if o.Keep {
		result.Image = finished
	}

	// 5. What the finished image is.
	var inspected bytes.Buffer
	if err := o.Podman(ctx, []string{"image", "inspect", finished}, &inspected, io.Discard); err != nil {
		return Result{}, fmt.Errorf("the image that was built cannot be read: %w", err)
	}
	info, err := imageInfo(inspected.Bytes())
	if err != nil {
		return Result{}, err
	}
	if problem := runner.ImageConfigProblem(info, agent.Sum); problem != "" {
		return fail(result, problem), nil
	}
	say("  image       ok (user 1000:1000, the agent, no volume)")

	// 6. The agent's self test, in a container made as a student's is (internal/runner/policy.go, hardened).
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
		return fail(result, "the agent in the image did not run: "+detail+". The image must be a Linux image the agent, a static program, can run in: it needs /proc"), nil
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

	// 7. The same files, built again from nothing, must give the same image.
	if o.Reproducible {
		first := append([]byte{}, inspected.Bytes()...)
		say("\nBuilding everything again from nothing, to compare the two images\n")
		var ignored bytes.Buffer
		_ = o.Podman(ctx, []string{"rmi", "--force", finished, authors}, &ignored, &ignored)
		if problem := buildBoth(true, true); problem != "" {
			return fail(result, "the second build from nothing: "+problem), nil
		}
		var again bytes.Buffer
		if err := o.Podman(ctx, []string{"image", "inspect", finished}, &again, io.Discard); err != nil {
			return Result{}, fmt.Errorf("the image that was built again cannot be read: %w", err)
		}
		second, err := imageInfo(again.Bytes())
		if err != nil {
			return Result{}, err
		}
		say("")
		if second.ID == info.ID {
			say("  reproduced  ok: two builds from nothing gave the same image (%s)", shortID(info.ID))
		} else {
			result.Irreproducible = differences(first, again.Bytes())
			say("  reproduced  NO: %s", result.Irreproducible)
		}
		info = second
	}

	if o.Name != "" {
		names := []string{o.Name + ":" + shortID(info.ID), o.Name + ":latest"}
		if err := o.Podman(ctx, append([]string{"tag", finished}, names...), out, out); err != nil {
			return Result{}, fmt.Errorf("naming the image: %w", err)
		}
		kept = names[0]
		result.Image, result.ImageID, result.Tags = names[1], info.ID, names
		say("  named       %s", strings.Join(names, ", "))
	}
	return result, nil
}

func fail(r Result, problem string) Result {
	r.OK, r.Problem = false, problem
	return r
}

func shortID(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// copiesTheAgent finds the lines that COPY or ADD a file called cbe-agent from the build's folder. There is none there: the runner adds the agent to
// every image after the Containerfile's last line (internal/runner/finalize.go), whether the server builds it or cbe-portalctl does (decision D-CB-9).
func copiesTheAgent(text string) []string {
	instructions, err := containerfile.Parse(text)
	if err != nil {
		return nil // the rules have said what is wrong
	}
	var problems []string
	for _, in := range instructions {
		if in.Command != "COPY" && in.Command != "ADD" || slices.ContainsFunc(in.Flags, func(flag string) bool { return flag == "--from" || strings.HasPrefix(flag, "--from=") }) {
			continue
		}
		words := copyWords(in.Args)
		for _, word := range words[:max(len(words)-1, 0)] { // the last word is where it goes
			if path.Base(word) == "cbe-agent" {
				problems = append(problems, fmt.Sprintf("line %d: %s %s: there is no cbe-agent in the build's folder: the runner adds the agent to every image after the last line of the Containerfile (%s), so take this line out", in.Line, in.Command, word, runner.AgentPath))
				break
			}
		}
	}
	return problems
}

// copyWords are the sources and the destination of a COPY or ADD, written as words or as a JSON list.
func copyWords(args string) []string {
	line, _, _ := strings.Cut(args, "\n") // a heredoc's body follows the first line
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "[") {
		var words []string
		if json.Unmarshal([]byte(line), &words) == nil {
			return words
		}
	}
	return strings.Fields(line)
}

// declares says whether the Containerfile has the ARG name.
func declares(text, name string) bool {
	instructions, err := containerfile.Parse(text)
	if err != nil {
		return false
	}
	for _, in := range instructions {
		if in.Command != "ARG" {
			continue
		}
		for _, word := range strings.Fields(in.Args) {
			if key, _, _ := strings.Cut(word, "="); key == name {
				return true
			}
		}
	}
	return false
}

// layer is a layer of an image and the step of the Containerfile that made it.
type layer struct{ digest, step string }

// layersOf reads the layers of an image from `podman image inspect`, each with the step that made it (the history's entries that made a layer are in
// the order of the layers).
func layersOf(inspected []byte) ([]layer, error) {
	var images []struct {
		RootFS struct {
			Layers []string `json:"Layers"`
		} `json:"RootFS"`
		History []struct {
			CreatedBy  string `json:"created_by"`
			EmptyLayer bool   `json:"empty_layer"`
		} `json:"History"`
	}
	if err := json.Unmarshal(inspected, &images); err != nil || len(images) != 1 {
		return nil, fmt.Errorf("the image's layers cannot be read: %v", err)
	}
	var steps []string
	for _, h := range images[0].History {
		if !h.EmptyLayer {
			steps = append(steps, h.CreatedBy)
		}
	}
	layers := make([]layer, len(images[0].RootFS.Layers))
	for i, digest := range images[0].RootFS.Layers {
		layers[i] = layer{digest: digest, step: "a step the image does not name"}
		if len(steps) == len(layers) {
			layers[i].step = stepText(steps[i])
		}
	}
	return layers, nil
}

// stepText is a step of an image's history as it is shown: the Containerfile's words, on one line, not too long.
func stepText(createdBy string) string {
	text := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(createdBy), "/bin/sh -c"))
	text = strings.TrimSpace(strings.TrimPrefix(text, "#(nop)"))
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > 70 {
		text = text[:70] + "…"
	}
	return text
}

// differences says where two images built from the same files differ: the layers that are not the same, with the step that made each.
func differences(first, second []byte) string {
	a, errA := layersOf(first)
	b, errB := layersOf(second)
	switch {
	case errA != nil || errB != nil:
		return "the two builds from nothing gave different images (their layers could not be read to say where)"
	case len(a) != len(b):
		return fmt.Sprintf("the two builds from nothing gave images of %d and %d layers", len(a), len(b))
	}
	var differ []string
	for i := range a {
		if a[i].digest != b[i].digest {
			differ = append(differ, fmt.Sprintf("layer %d of %d (%s)", i+1, len(a), a[i].step))
		}
	}
	if len(differ) == 0 {
		return "the two builds from nothing have the same layers and different configurations (a date or a label that changes with every build)"
	}
	return "the two builds from nothing differ in " + strings.Join(differ, ", ") + ": something in that step changes with every build (a date, a log, a download that is not pinned)"
}

func registriesText(c *runner.Config) string {
	if len(c.BaseRegistries) == 0 {
		return "any"
	}
	return strings.Join(c.BaseRegistries, ", ")
}

// Locate finds the folder of the build context and its Containerfile: a package folder (image/Containerfile), an image folder, or the file itself.
func Locate(dir string) (folder, file string, err error) {
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
func copyTree(from, to string, readable bool) error {
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
			perm := info.Mode().Perm()
			if readable {
				perm |= 0o044
			}
			return os.WriteFile(target, data, perm)
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
