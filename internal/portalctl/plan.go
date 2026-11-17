package portalctl

import (
	"fmt"
	"io"
	"strings"

	"exam-platform/internal/portalctl/conf"
	"exam-platform/internal/portalctl/sys"
)

// Step is one thing the configuration needs done on the machine.
type Step struct {
	Name string
	// Check says whether the step is already done, and why (or what is to be done). It changes nothing; where it needs something an earlier
	// step has not made yet (an account), the step is simply not done. An error is something that stops the configuration: a folder that
	// is somebody else's, a program that is not there.
	Check func(c *Ctx) (done bool, note string, err error)
	// Apply does it. The step is checked again afterwards.
	Apply func(c *Ctx) error
}

// Ctx is what a step works with.
type Ctx struct {
	Sys sys.System
	Cfg *conf.Values
	L   Layout
	Out io.Writer
	// Changed says what this run has changed, for the steps after it ("units", "user-manager").
	Changed map[string]bool
}

// NewCtx is a context for a configuration on a machine.
func NewCtx(machine sys.System, cfg *conf.Values, out io.Writer) *Ctx {
	return &Ctx{Sys: machine, Cfg: cfg, L: NewLayout(cfg), Out: out, Changed: map[string]bool{}}
}

// Options are how a plan is run.
type Options struct {
	// DryRun checks every step and changes nothing.
	DryRun bool
}

// Result is what running a plan came to.
type Result struct {
	Done, Todo, Problems int
}

// Run goes through the steps in order. A step that is done is left alone. In a dry run every step is checked and what is to be done is
// said; otherwise the first step that cannot be done stops the plan, because the steps after it depend on it.
func Run(c *Ctx, steps []Step, opts Options) (Result, error) {
	var result Result
	for _, step := range steps {
		done, note, err := step.Check(c)
		switch {
		case err != nil:
			result.Problems++
			fmt.Fprintf(c.Out, "  PROBLEM  %s: %v\n", step.Name, err)
			if !opts.DryRun {
				return result, fmt.Errorf("%s: %w", step.Name, err)
			}
		case done:
			result.Done++
			fmt.Fprintf(c.Out, "  ok       %s%s\n", step.Name, suffix(note))
		case opts.DryRun:
			result.Todo++
			fmt.Fprintf(c.Out, "  todo     %s%s\n", step.Name, suffix(note))
		default:
			fmt.Fprintf(c.Out, "  doing    %s%s\n", step.Name, suffix(note))
			if err := step.Apply(c); err != nil {
				result.Problems++
				fmt.Fprintf(c.Out, "  FAILED   %s: %v\n", step.Name, err)
				return result, fmt.Errorf("%s: %w", step.Name, err)
			}
			if again, why, err := step.Check(c); err != nil || !again {
				if err == nil {
					err = fmt.Errorf("it was done, and is still not as it should be (%s)", why)
				}
				result.Problems++
				fmt.Fprintf(c.Out, "  FAILED   %s: %v\n", step.Name, err)
				return result, fmt.Errorf("%s: %w", step.Name, err)
			}
			result.Todo++
		}
	}
	return result, nil
}

func suffix(note string) string {
	if note == "" {
		return ""
	}
	return " (" + note + ")"
}

// Lines is the text of a file as lines, without the empty ones and the comments (for the files of the machine that are read, not run).
func lines(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			out = append(out, line)
		}
	}
	return out
}
