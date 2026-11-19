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
		fmt.Fprint(env.Stderr, "cbe-portalctl image: name what to do: check, install, test or list\n\n")
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
	case "test":
		return imageTest(args[1:], env)
	case "list":
		return imageList(args[1:], env)
	}
	return unknownCommand(env, "image "+args[0], []string{"image check", "image install", "image test", "image list"}, "image")
}

func imageCheck(args []string, env Env) int {
	fs := newFlags("image check", env)
	install := installDir(env)
	agent := fs.String("agent", "", "the static cbe-agent `FILE` to put in the image (default: the server's agent_binary when a server is installed here, else the cbe-agent next to this program)")
	runnerConfig := fs.String("runner-config", "", "the runner's settings `FILE`, for custom_base and base_registries (default: INSTALL_DIR/etc/runner.json, when it is there)")
	podman := fs.String("podman", "podman", "the container runtime's `COMMAND`, with its options (for example \"podman --root /tmp/storage\")")
	rebuild := fs.Bool("rebuild", false, "build without the cache")
	reproducible := fs.Bool("reproducible", false, "build everything twice from nothing and say whether the two images are the same (it takes twice as long)")
	keep := fs.Bool("keep", false, "keep the images that were built, to look inside")
	// The folder may come before or after the options.
	rest, code, ok := parseArgs(fs, args, env)
	if !ok {
		return code
	}
	if len(rest) != 1 {
		return usageError(env, "image check", "it takes one DIR: the package folder, its image/ folder or the Containerfile")
	}
	// What is checked comes before what it is checked with: a folder that is not there is said first.
	if _, _, err := imagecheck.Locate(rest[0]); err != nil {
		fmt.Fprintf(env.Stderr, "error: %v\n", err)
		return 1
	}

	options := imagecheck.Options{Dir: rest[0], Podman: imagecheck.Command(strings.Fields(*podman)...), Rebuild: *rebuild, Reproducible: *reproducible, Keep: *keep,
		Epoch: epochFromEnvironment(), Out: env.Stdout}
	if env.Podman != nil {
		options.Podman = env.Podman
	}
	if _, err := os.Stat(runner.HostCABundle()); err == nil {
		options.CABundle = runner.HostCABundle()
	}
	// A server is installed here when its runner's settings are: they decide, unless others are named, and its agent is the one images get.
	saved := filepath.Join(install, "etc", "runner.json")
	configPath := *runnerConfig
	if configPath == "" && fileExists(saved) {
		configPath = saved
	}
	if configPath != "" {
		cfg, err := runner.LoadConfig(configPath)
		if err != nil {
			fmt.Fprintf(env.Stderr, "error: the runner's settings %s: %v\n", configPath, err)
			return 1
		}
		options.Config = &cfg
	}
	serverAgent := ""
	if configPath == saved && options.Config != nil {
		serverAgent = options.Config.AgentBinary
	} else if fileExists(saved) {
		if cfg, err := runner.LoadConfig(saved); err == nil {
			serverAgent = cfg.AgentBinary
		}
	}
	file, from, err := agentFor(env, *agent, serverAgent, saved)
	if err != nil {
		fmt.Fprintf(env.Stderr, "error: %v\n", err)
		return 1
	}
	options.Agent, options.AgentFrom = file, from
	options.Release, options.ReleaseOf = releaseHere(env, install)

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
	if result.Irreproducible != "" {
		fmt.Fprintf(env.Stdout, "\nNOT REPRODUCIBLE: %s.\nThe image keeps the contract, and the server would accept it, but the same files built again do not give the same image.\n", result.Irreproducible)
		return 1
	}
	fmt.Fprintf(env.Stdout, "\nOK: the image keeps the contract of a workspace image, and the server will accept it.\n")
	if *reproducible {
		fmt.Fprintf(env.Stdout, "It is reproducible: the same files built again from nothing give the same image.\n")
	}
	if result.Image != "" {
		fmt.Fprintf(env.Stdout, "Kept: %s (podman run --rm -it --entrypoint /bin/sh %s)\n", result.Image, result.Image)
	}
	return 0
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
