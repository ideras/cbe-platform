package portalctl

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"exam-platform/internal/imagecheck"
	"exam-platform/internal/imagetest"
	"exam-platform/internal/runner"
)

// imageTest is `image test NAME [DIR]` (decision D-CB-10): it tries an installed image in a student's workspace, through the runner.
func imageTest(args []string, env Env) int {
	fs := newFlags("image test", env)
	rest, code, ok := parseArgs(fs, args, env)
	if !ok {
		return code
	}
	if len(rest) < 1 || len(rest) > 2 {
		return usageError(env, "image test", "it takes a NAME, and the image's DIR for the course's own checks")
	}
	name := rest[0]
	if !runner.ValidImageName(name) {
		fmt.Fprintf(env.Stderr, "error: %q is not the name of a course image: lower case words joined by / . _ or - (cbe/cpp20, nn-2026), with no tag\n", name)
		return 2
	}
	var checks []imagetest.Check
	if len(rest) == 2 {
		folder, _, err := imagecheck.Locate(rest[1])
		if err != nil {
			fmt.Fprintf(env.Stderr, "error: %v\n", err)
			return 1
		}
		found := false
		if checks, found, err = imagetest.ReadChecks(folder); err != nil {
			fmt.Fprintf(env.Stderr, "error: %v\n", err)
			return 1
		}
		if !found {
			fmt.Fprintf(env.Stderr, "error: %s has no file %s, the course's own checks: leave DIR out to run only the checks every image must pass\n", folder, imagetest.ChecksFile)
			return 1
		}
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return testImage(ctx, env, cfg, name, checks)
}

// testImage runs the test of an installed image, for image test and image install --test.
func testImage(ctx context.Context, env Env, cfg runner.Config, name string, checks []imagetest.Check) int {
	r, err := runnerClient(env, cfg)
	if err != nil {
		fmt.Fprintf(env.Stderr, "error: %v\n", err)
		return 1
	}
	result, err := imagetest.Run(ctx, imagetest.Options{Image: name, Runner: r, Checks: checks, RunRoot: cfg.RunRoot, Out: env.Stdout})
	if err != nil {
		fmt.Fprintf(env.Stderr, "error: the test of %s: %v\n", name, err)
		return 1
	}
	if !result.OK() {
		fmt.Fprintf(env.Stdout, "\nNOT PASSED: %s failed %d check(s): %s\n", name, result.Failed, strings.Join(result.Failures, "; "))
		return 1
	}
	fmt.Fprintf(env.Stdout, "\nOK: %s works in a student's workspace (%d checks).\n", name, result.Passed)
	return 0
}

// runnerClient is the runner, as the exam server talks to it: its socket and its secret, which only root and the server's account can read.
func runnerClient(env Env, cfg runner.Config) (imagetest.Runner, error) {
	if env.Runner != nil {
		return env.Runner, nil
	}
	secret, err := env.Sys.ReadFile(cfg.SecretFile)
	if err != nil {
		return nil, fmt.Errorf("the runner's secret (%s): %v", cfg.SecretFile, err)
	}
	return runner.NewClient(cfg.Socket, strings.TrimSpace(string(secret))), nil
}
