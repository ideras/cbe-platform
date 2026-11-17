package portalctl

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"exam-platform/internal/imagecheck"
	"exam-platform/internal/runner"
)

const imageUsage = `  cbe-portalctl image check [--agent FILE] [--runner-config FILE] [--podman COMMAND] [--rebuild] [--keep] DIR
        Check an exam's own base image on this computer, before the server does: build the package's image/Containerfile, finish it as the server does
        (the student account, the agent, no way up) and run the agent's self test in a container made as a student's is. DIR is the package folder, its
        image/ folder or the Containerfile. It needs Podman, and the agent (default: INSTALL_DIR/bin/cbe-agent); the settings of this server
        (INSTALL_DIR/etc/runner.json: custom_base, base_registries) are applied when the file is there. Exit status 0: the server will accept it.
`

// image is the `image` command: the checks an author can run on their own computer (decision D-CB-5).
func image(args []string, env Env) int {
	if len(args) == 0 || args[0] != "check" {
		fmt.Fprintf(env.Stderr, "usage:\n%s", imageUsage)
		return 2
	}
	fs := newFlags("image check", env)
	install := installDir(env)
	agent := fs.String("agent", filepath.Join(install, "bin", "cbe-agent"), "the static cbe-agent to put in the image")
	runnerConfig := fs.String("runner-config", "", "the runner's settings, for custom_base and base_registries (default: INSTALL_DIR/etc/runner.json, when it is there)")
	podman := fs.String("podman", "podman", "the container runtime's command, with its options (for example \"podman --root /tmp/storage\")")
	rebuild := fs.Bool("rebuild", false, "build without the cache")
	keep := fs.Bool("keep", false, "keep the images that were built, to look inside")
	// The folder may come before or after the options.
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	rest := fs.Args()
	if len(rest) > 0 {
		if err := fs.Parse(rest[1:]); err != nil {
			return 2
		}
		rest = append(rest[:1], fs.Args()...)
	}
	if len(rest) != 1 {
		fmt.Fprintf(env.Stderr, "usage:\n%s", imageUsage)
		return 2
	}

	options := imagecheck.Options{Dir: rest[0], Agent: *agent, Podman: imagecheck.Command(strings.Fields(*podman)...), Rebuild: *rebuild, Keep: *keep, Out: env.Stdout}
	if _, err := os.Stat(runner.HostCABundle()); err == nil {
		options.CABundle = runner.HostCABundle()
	}
	configPath := *runnerConfig
	if configPath == "" {
		if saved := filepath.Join(install, "etc", "runner.json"); fileExists(saved) {
			configPath = saved
		}
	}
	if configPath != "" {
		cfg, err := runner.LoadConfig(configPath)
		if err != nil {
			fmt.Fprintf(env.Stderr, "error: the runner's settings %s: %v\n", configPath, err)
			return 1
		}
		options.Config = &cfg
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	result, err := imagecheck.Check(ctx, options)
	if err != nil {
		fmt.Fprintf(env.Stderr, "error: %v\n", err)
		return 1
	}
	if !result.OK {
		fmt.Fprintf(env.Stdout, "\nNOT ACCEPTED: %s\n", result.Problem)
		return 1
	}
	fmt.Fprintf(env.Stdout, "\nOK: the image keeps the contract of a workspace image, and the server will accept it.\n")
	if result.Image != "" {
		fmt.Fprintf(env.Stdout, "Kept: %s (podman run --rm -it --entrypoint /bin/sh %s)\n", result.Image, result.Image)
	}
	return 0
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
