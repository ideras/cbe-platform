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

// image is the `image` command (decisions D-CB-5 and D-CB-7).
func image(args []string, env Env) int {
	if len(args) == 0 {
		fmt.Fprint(env.Stderr, "cbe-portalctl image: name what to do: check, install or list\n\n")
		printImageHelp(env.Stderr)
		return 2
	}
	switch args[0] {
	case "-h", "--help", "-help":
		printImageHelp(env.Stdout)
		return 0
	case "check":
		return imageCheck(args[1:], env)
	case "install":
		return imageInstall(args[1:], env)
	case "list":
		return imageList(args[1:], env)
	}
	return unknownCommand(env, "image "+args[0], []string{"image check", "image install", "image list"}, "image")
}

func imageCheck(args []string, env Env) int {
	fs := newFlags("image check", env)
	install := installDir(env)
	agent := fs.String("agent", filepath.Join(install, "bin", "cbe-agent"), "the static cbe-agent `FILE` to put in the image")
	runnerConfig := fs.String("runner-config", "", "the runner's settings `FILE`, for custom_base and base_registries (default: INSTALL_DIR/etc/runner.json, when it is there)")
	podman := fs.String("podman", "podman", "the container runtime's `COMMAND`, with its options (for example \"podman --root /tmp/storage\")")
	rebuild := fs.Bool("rebuild", false, "build without the cache")
	keep := fs.Bool("keep", false, "keep the images that were built, to look inside")
	// The folder may come before or after the options.
	rest, code, ok := parseArgs(fs, args, env)
	if !ok {
		return code
	}
	if len(rest) != 1 {
		return usageError(env, "image check", "it takes one DIR: the package folder, its image/ folder or the Containerfile")
	}

	options := imagecheck.Options{Dir: rest[0], Agent: *agent, Podman: imagecheck.Command(strings.Fields(*podman)...), Rebuild: *rebuild, Keep: *keep, Out: env.Stdout}
	if env.Podman != nil {
		options.Podman = env.Podman
	}
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
