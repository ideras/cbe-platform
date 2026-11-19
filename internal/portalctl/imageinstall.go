package portalctl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"

	"exam-platform/internal/imagecheck"
	"exam-platform/internal/runner"
)

// courseRepository is where the images this command installs are kept in the runner's storage: localhost/cbe-course/<name>:<id> and :latest.
const courseRepository = "localhost/cbe-course/"

// courseLabel is the label an installed image carries: the name it was installed as.
const courseLabel = "io.cbe.course"

// runnerPodman is the container runtime of the runner account, which is where the server's images are kept.
func runnerPodman(env Env, l Layout) (imagecheck.Podman, error) {
	if env.Podman != nil {
		return env.Podman, nil
	}
	u, ok := env.Sys.Lookup(l.RunnerUser)
	if !ok {
		return nil, fmt.Errorf("the account %s does not exist: this server has not been configured for workspace exams", l.RunnerUser)
	}
	return imagecheck.Command("runuser", "-u", l.RunnerUser, "--", "env", fmt.Sprintf("XDG_RUNTIME_DIR=/run/user/%d", u.UID),
		fmt.Sprintf("DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/%d/bus", u.UID), "HOME="+u.Home, "podman"), nil
}

// readRunnerSettings reads the runner's settings as they are written (every key kept, for writing back) and as the runner reads them.
func readRunnerSettings(env Env, l Layout) (map[string]any, runner.Config, error) {
	data, err := env.Sys.ReadFile(l.RunnerConfig())
	if err != nil {
		return nil, runner.Config{}, fmt.Errorf("the runner's settings are not there (%s): run cbe-portalctl configure first", l.RunnerConfig())
	}
	settings := map[string]any{}
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, runner.Config{}, fmt.Errorf("%s is not JSON (%v): put it right first", l.RunnerConfig(), err)
	}
	cfg, err := runner.ParseConfig(data, l.RunnerConfig())
	if err != nil {
		return nil, runner.Config{}, err
	}
	return settings, cfg, nil
}

// serverForImages is what `image install` and `image list` need: the saved configuration of a server that serves workspace exams, and root.
func serverForImages(env Env) (*Ctx, error) {
	c, err := savedLayout(env)
	if err != nil {
		return nil, err
	}
	if !env.Sys.IsRoot() {
		return nil, fmt.Errorf("run it as root (sudo): the images are kept in the storage of the account %s", c.L.RunnerUser)
	}
	if !c.L.Workspace {
		return nil, fmt.Errorf("this server does not serve workspace exams (WORKSPACE_ENABLED=no): there is no runner to keep images for")
	}
	return c, nil
}

func imageInstall(args []string, env Env) int {
	fs := newFlags("image install", env)
	agent := fs.String("agent", "", "the static cbe-agent `FILE` to put in the image (default: the server's agent_binary, else the cbe-agent next to this program)")
	replace := fs.Bool("replace", false, "build NAME again although it is installed, and use the new image from now on")
	restart := fs.Bool("restart", false, "when the new name needs it, restart the services as cbe-portalctl restart does (refused while students are working)")
	rest, code, ok := parseArgs(fs, args, env)
	if !ok {
		return code
	}
	if len(rest) != 2 {
		return usageError(env, "image install", "it takes a NAME and a DIR")
	}
	name, dir := rest[0], rest[1]
	if !runner.ValidImageName(name) {
		fmt.Fprintf(env.Stderr, "error: %q is not the name of a course image: lower case words joined by / . _ or - (cbe/cpp20, nn-2026), with no tag\n", name)
		return 2
	}
	c, err := serverForImages(env)
	if err != nil {
		fmt.Fprintf(env.Stderr, "error: %v\n", err)
		return 1
	}
	l := c.L
	settings, cfg, err := readRunnerSettings(env, l)
	if err != nil {
		fmt.Fprintf(env.Stderr, "error: %v\n", err)
		return 1
	}
	images, _ := settings["images"].(map[string]any)
	if images == nil {
		images = map[string]any{}
	}
	reference := courseRepository + name + ":latest"
	previous, installed := images[name].(string)
	if installed && !*replace {
		fmt.Fprintf(env.Stderr, "error: %s is approved already (%s): --replace builds it again, and the workspaces created from then on use the new image\n", name, previous)
		return 1
	}
	podman, err := runnerPodman(env, l)
	if err != nil {
		fmt.Fprintf(env.Stderr, "error: %v\n", err)
		return 1
	}
	// Building an image for a class is the administrator's own act: custom_base is about what an exam brings, so it does not apply; base_registries does.
	cfg.CustomBase = true
	if _, _, err := imagecheck.Locate(dir); err != nil {
		fmt.Fprintf(env.Stderr, "error: %v\n", err)
		return 1
	}
	file, from, err := agentFor(env, *agent, cfg.AgentBinary, l.RunnerConfig())
	if err != nil {
		fmt.Fprintf(env.Stderr, "error: %v\n", err)
		return 1
	}
	options := imagecheck.Options{
		Dir: dir, Agent: file, AgentFrom: from, Config: &cfg, Podman: podman, Out: env.Stdout, Readable: true, Epoch: epochFromEnvironment(),
		Name: courseRepository + name, Labels: map[string]string{courseLabel: name},
	}
	options.Release, options.ReleaseOf = releaseHere(env, l.Install)
	if _, err := os.Stat(runner.HostCABundle()); err == nil {
		options.CABundle = runner.HostCABundle()
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	result, err := imagecheck.Check(ctx, options)
	if err != nil {
		fmt.Fprintf(env.Stderr, "error: %v\n", err)
		return 1
	}
	if !result.OK {
		fmt.Fprintf(env.Stdout, "\nNOT INSTALLED: %s\n", result.Problem)
		return 1
	}

	changed := !installed || previous != reference
	if changed {
		images[name] = reference
		settings["images"] = images
		out, err := json.MarshalIndent(settings, "", "  ")
		if err != nil {
			fmt.Fprintf(env.Stderr, "error: %v\n", err)
			return 1
		}
		if err := env.Sys.WriteFile(l.RunnerConfig(), append(out, '\n'), 0o644); err != nil {
			fmt.Fprintf(env.Stderr, "error: the image is built (%s), and the runner's settings could not be written: %v\n", reference, err)
			return 1
		}
	}
	fmt.Fprintf(env.Stdout, "\nInstalled %s: %s (image %s).\n", name, reference, result.ImageID)
	if installed && previous != reference {
		fmt.Fprintf(env.Stdout, "It was %s before.\n", previous)
	}
	if !changed {
		fmt.Fprintf(env.Stdout, "The runner uses the new image for the workspaces created from now on; no restart is needed. Students already working keep the container they have.\n")
		return 0
	}
	if !*restart {
		fmt.Fprintf(env.Stdout, "The runner reads its settings when it starts: run `cbe-portalctl restart` when no class is working (or say --restart next time), and exams can say `image: %s`.\n", name)
		return 0
	}
	if err := Restart(c, false); err != nil {
		fmt.Fprintf(env.Stderr, "error: the image is installed, and the runner was not restarted: %v\n(run `cbe-portalctl restart` when it can be)\n", err)
		return 1
	}
	fmt.Fprintf(env.Stdout, "The runner was restarted: exams can say `image: %s`.\n", name)
	return 0
}

func imageList(args []string, env Env) int {
	if code, ok := noOperands(newFlags("image list", env), args, env); !ok {
		return code
	}
	c, err := serverForImages(env)
	if err != nil {
		fmt.Fprintf(env.Stderr, "error: %v\n", err)
		return 1
	}
	_, cfg, err := readRunnerSettings(env, c.L)
	if err != nil {
		fmt.Fprintf(env.Stderr, "error: %v\n", err)
		return 1
	}
	podman, err := runnerPodman(env, c.L)
	if err != nil {
		fmt.Fprintf(env.Stderr, "error: %v\n", err)
		return 1
	}
	if len(cfg.Images) == 0 {
		fmt.Fprintf(env.Stdout, "The runner approves no course image: exams bring their own (`image install NAME DIR` installs one for a class).\n")
		return 0
	}
	names := make([]string, 0, len(cfg.Images))
	for name := range cfg.Images {
		names = append(names, name)
	}
	sort.Strings(names)
	width := 0
	for _, name := range names {
		width = max(width, len(name))
	}
	missing := 0
	for _, name := range names {
		reference := cfg.Images[name]
		var out bytes.Buffer
		state := "present"
		err := podman(context.Background(), []string{"image", "inspect", "--format", `{{.Id}} {{index .Labels "` + courseLabel + `"}}`, reference}, &out, &bytes.Buffer{})
		fields := strings.Fields(out.String())
		switch {
		case err != nil || len(fields) == 0:
			state, missing = "MISSING: the container runtime does not have it", missing+1
		case len(fields) > 1 && fields[1] == name:
			state = "present, installed by image install (" + shortID(fields[0]) + ")"
		default:
			state = "present (" + shortID(fields[0]) + ")"
		}
		fmt.Fprintf(env.Stdout, "%-*s  %s  %s\n", width, name, reference, state)
	}
	if missing > 0 {
		return 1
	}
	return 0
}

func shortID(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
