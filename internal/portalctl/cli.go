package portalctl

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"exam-platform/internal/portalctl/conf"
	"exam-platform/internal/portalctl/sys"
)

// Env is what the command runs in: the machine, the terminal, and where the program is.
type Env struct {
	Sys    sys.System
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	// Interactive says that a person is at the terminal and can be asked.
	Interactive bool
	// Exe is the path of this program: INSTALL_DIR is where its bin folder is.
	Exe     string
	Version string

	// reader is what is read from Stdin, made once: a second reader on the same input would not have what the first has already taken in.
	reader *bufio.Reader
}

const usage = `cbe-portalctl configures a CBE Portal server on this machine.

  cbe-portalctl configure   [--config FILE] [--yes] [--dry-run] [--interactive]
        Make this machine the server the configuration describes: accounts, folders, the XFS volume for workspace exams, the services.
        With no --config it uses the answers saved by the last run, or asks (at a terminal). Safe to run again: what is done is left.
  cbe-portalctl doctor      [--config FILE]     Check that this machine can be configured; change nothing.
  cbe-portalctl status                          The services, and where the server is.
  cbe-portalctl restart     [--force]           Restart the services (refused while students are working, unless --force).
  cbe-portalctl unconfigure [--remove-data] [--yes] [--dry-run]
        Stop and remove the services. The data (database, screenshots, the students' folders, the volume) stays,
        unless --remove-data says that all of it is to be deleted too.
  cbe-portalctl sample-config                   Print a documented configuration file.
  cbe-portalctl version
`

// Main runs a command and returns the exit status: 0 done, 1 something stopped it, 2 the command or the configuration is wrong.
func Main(args []string, env Env) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprint(env.Stdout, usage)
		return 0
	}
	command, rest := args[0], args[1:]
	env.reader = bufio.NewReader(env.Stdin)
	switch command {
	case "version":
		fmt.Fprintf(env.Stdout, "cbe-portalctl %s\n", env.Version)
		return 0
	case "sample-config":
		fmt.Fprint(env.Stdout, conf.Sample())
		return 0
	case "configure":
		return configure(rest, env)
	case "doctor":
		return doctor(rest, env)
	case "status":
		return status(rest, env)
	case "restart":
		return restart(rest, env)
	case "unconfigure":
		return unconfigure(rest, env)
	}
	fmt.Fprintf(env.Stderr, "cbe-portalctl: %q is not a command\n\n%s", command, usage)
	return 2
}

func newFlags(name string, env Env) *flag.FlagSet {
	fs := flag.NewFlagSet("cbe-portalctl "+name, flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	return fs
}

// installDir is where the program is installed: the folder above the bin folder this program is in.
func installDir(env Env) string {
	if dir := path.Dir(env.Exe); path.Base(dir) == "bin" {
		return path.Dir(dir)
	}
	return "/opt/cbe-portal"
}

// load reads the configuration: the file named, or the answers saved by the last run, or nothing (every key at its default).
func load(env Env, file string) (*conf.Values, string, error) {
	install := installDir(env)
	source := file
	if source == "" {
		if saved := path.Join(install, "etc", "cbe-portal.conf"); env.Sys.Stat(saved).Exists {
			source = saved
		}
	}
	v := conf.New()
	if source != "" {
		data, err := env.Sys.ReadFile(source)
		if err != nil {
			return nil, source, err
		}
		if v, err = conf.Parse(strings.NewReader(string(data)), source); err != nil {
			return nil, source, err
		}
	}
	if !v.IsSet("INSTALL_DIR") {
		if err := v.Set("INSTALL_DIR", install); err != nil {
			return nil, source, err
		}
	} else if v.Get("INSTALL_DIR") != install {
		return nil, source, fmt.Errorf("INSTALL_DIR is %s in %s, and this program is installed in %s: the installer puts the files in the same place the configuration says, or INSTALL_DIR is left out", v.Get("INSTALL_DIR"), source, install)
	}
	return v, source, nil
}

func report(env Env, p conf.Problems, source string) bool {
	for _, warning := range p.Warnings {
		fmt.Fprintf(env.Stderr, "warning: %s\n", warning)
	}
	for _, problem := range p.Errors {
		fmt.Fprintf(env.Stderr, "error: %s\n", problem)
	}
	if !p.OK() {
		fmt.Fprintf(env.Stderr, "the configuration%s is not valid\n", of(source))
	}
	return p.OK()
}

func of(source string) string {
	if source == "" {
		return ""
	}
	return " (" + source + ")"
}

func configure(args []string, env Env) int {
	fs := newFlags("configure", env)
	file := fs.String("config", "", "the configuration file")
	yes := fs.Bool("yes", false, "do not ask before changing the machine")
	dry := fs.Bool("dry-run", false, "say what would be done, and change nothing")
	interactive := fs.Bool("interactive", false, "ask about the basic settings, starting from the file or the defaults")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	v, source, err := load(env, *file)
	if err != nil {
		fmt.Fprintf(env.Stderr, "error: %v\n", err)
		return 2
	}
	asking := *interactive || (source == "" && env.Interactive)
	if asking && !env.Interactive {
		fmt.Fprintln(env.Stderr, "error: --interactive needs a terminal")
		return 2
	}
	if source == "" && !asking {
		fmt.Fprintln(env.Stderr, "error: there is no configuration: pass --config FILE (cbe-portalctl sample-config prints one), or run this at a terminal to be asked")
		return 2
	}
	if asking {
		if err := ask(env, v); err != nil {
			fmt.Fprintf(env.Stderr, "error: %v\n", err)
			return 2
		}
	}
	if !report(env, v.Validate(), source) {
		return 2
	}
	if !*dry && !env.Sys.IsRoot() {
		fmt.Fprintln(env.Stderr, "error: configuring changes the machine: run it as root (sudo), or add --dry-run to see what it would do")
		return 1
	}

	ctx := NewCtx(env.Sys, v, env.Stdout)
	l := ctx.L
	fmt.Fprintf(env.Stdout, "cbe-portalctl %s: a server for %s, data in %s\n\nThis machine:\n", env.Version, map[bool]string{true: "question and workspace exams", false: "question exams"}[l.Workspace], l.Data)
	findings := Doctor(ctx)
	for _, f := range findings {
		fmt.Fprintln(env.Stdout, f)
	}
	if Failed(findings) {
		fmt.Fprintln(env.Stderr, "\nerror: this machine cannot be configured yet: put right what failed above, and run this again")
		return 1
	}

	fmt.Fprintln(env.Stdout, "\nWhat is to be done:")
	plan := ConfigurePlan(l)
	preview, err := Run(ctx, plan, Options{DryRun: true})
	if err != nil || preview.Problems > 0 {
		fmt.Fprintln(env.Stderr, "\nerror: it cannot be done as it is: see the problems above")
		return 1
	}
	if *dry {
		fmt.Fprintf(env.Stdout, "\n%d step(s) to do, %d already done. Nothing was changed.\n", preview.Todo, preview.Done)
		return 0
	}
	if preview.Todo > 0 && !*yes {
		if !env.Interactive {
			fmt.Fprintln(env.Stderr, "\nerror: changing the machine needs a yes: add --yes (or run this at a terminal)")
			return 1
		}
		if !confirm(env, fmt.Sprintf("\n%d step(s) will change this machine. Go ahead?", preview.Todo)) {
			fmt.Fprintln(env.Stdout, "Nothing was changed.")
			return 1
		}
	}
	if preview.Todo > 0 {
		fmt.Fprintln(env.Stdout, "\nConfiguring:")
		if _, err := Run(ctx, plan, Options{}); err != nil {
			fmt.Fprintf(env.Stderr, "\nerror: %v\nWhat was done stays done: put right the cause and run this again, and it goes on from there.\n", err)
			return 1
		}
	} else {
		fmt.Fprintln(env.Stdout, "\nNothing to do: this machine is configured as asked.")
	}
	if err := saveConfig(env, ctx, v); err != nil {
		fmt.Fprintf(env.Stderr, "warning: the answers could not be saved in %s: %v\n", l.SavedConfig(), err)
	} else {
		fmt.Fprintf(env.Stdout, "The answers are saved in %s.\n", l.SavedConfig())
	}
	fmt.Fprint(env.Stdout, l.Next())
	return 0
}

func saveConfig(env Env, ctx *Ctx, v *conf.Values) error {
	l := ctx.L
	if err := env.Sys.MkdirAll(path.Dir(l.SavedConfig()), 0o755); err != nil {
		return err
	}
	return env.Sys.WriteFile(l.SavedConfig(), []byte(v.Render()), 0o644)
}

func doctor(args []string, env Env) int {
	fs := newFlags("doctor", env)
	file := fs.String("config", "", "the configuration file")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	v, source, err := load(env, *file)
	if err != nil {
		fmt.Fprintf(env.Stderr, "error: %v\n", err)
		return 2
	}
	if !report(env, v.Validate(), source) {
		return 2
	}
	findings := Doctor(NewCtx(env.Sys, v, env.Stdout))
	for _, f := range findings {
		fmt.Fprintln(env.Stdout, f)
	}
	if Failed(findings) {
		return 1
	}
	return 0
}

// savedLayout reads the answers the last configure saved.
func savedLayout(env Env) (*Ctx, error) {
	v, source, err := load(env, "")
	if err != nil {
		return nil, err
	}
	if source == "" {
		return nil, errors.New("this machine has not been configured by cbe-portalctl: there are no saved answers (" + path.Join(installDir(env), "etc", "cbe-portal.conf") + ")")
	}
	return NewCtx(env.Sys, v, env.Stdout), nil
}

func status(args []string, env Env) int {
	ctx, err := savedLayout(env)
	if err != nil {
		fmt.Fprintf(env.Stderr, "error: %v\n", err)
		return 1
	}
	l := ctx.L
	fmt.Fprintf(env.Stdout, "cbe-portal %s\n  program   %s\n  data      %s\n  server    http://%s\n", env.Version, l.Install, l.Data, l.Bind)
	down := false
	for _, s := range Status(ctx) {
		state := "stopped"
		if s.Active {
			state = "running"
		} else {
			down = true
		}
		fmt.Fprintf(env.Stdout, "  %-44s %s%s\n", s.Unit, state, map[bool]string{true: ", at boot", false: ""}[s.Enabled])
	}
	if l.Workspace {
		if ok, why := volumeReady(ctx, l.WorkDir); ok {
			fmt.Fprintf(env.Stdout, "  students' volume                             %s: xfs, project quotas\n", l.WorkDir)
		} else {
			fmt.Fprintf(env.Stdout, "  students' volume                             NOT READY: %s\n", why)
			down = true
		}
		fmt.Fprintf(env.Stdout, "  student workspaces running                   %d\n", RunningContainers(ctx))
	}
	if down {
		return 1
	}
	return 0
}

func restart(args []string, env Env) int {
	fs := newFlags("restart", env)
	force := fs.Bool("force", false, "restart even while students are working")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	ctx, err := savedLayout(env)
	if err != nil {
		fmt.Fprintf(env.Stderr, "error: %v\n", err)
		return 1
	}
	if !env.Sys.IsRoot() {
		fmt.Fprintln(env.Stderr, "error: run it as root (sudo)")
		return 1
	}
	if err := Restart(ctx, *force); err != nil {
		fmt.Fprintf(env.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Fprintln(env.Stdout, "The services were restarted.")
	return 0
}

func unconfigure(args []string, env Env) int {
	fs := newFlags("unconfigure", env)
	removeData := fs.Bool("remove-data", false, "also delete the data: the database, the screenshots, the students' folders, the volume and the accounts")
	yes := fs.Bool("yes", false, "do not ask")
	dry := fs.Bool("dry-run", false, "say what would be done, and change nothing")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	ctx, err := savedLayout(env)
	if err != nil {
		fmt.Fprintf(env.Stderr, "error: %v\n", err)
		return 1
	}
	if !*dry && !env.Sys.IsRoot() {
		fmt.Fprintln(env.Stderr, "error: run it as root (sudo), or add --dry-run")
		return 1
	}
	if *removeData {
		fmt.Fprintf(env.Stdout, "ALL OF THE DATA OF THIS SERVER IS TO BE DELETED: everything in %s", ctx.L.Data)
		if ctx.L.Workspace && ctx.L.CreateVolume {
			fmt.Fprintf(env.Stdout, ", the volume file %s", ctx.L.VolumeFile)
		}
		fmt.Fprintln(env.Stdout, ", and the accounts. Exams, answers and screenshots are gone for good.")
	} else {
		fmt.Fprintln(env.Stdout, "The services are removed. The data stays: database, screenshots, the students' folders, the volume and the accounts.")
	}
	plan := UnconfigurePlan(ctx.L, *removeData)
	preview, err := Run(ctx, plan, Options{DryRun: true})
	if err != nil || preview.Problems > 0 {
		fmt.Fprintln(env.Stderr, "error: it cannot be done as it is: see the problems above")
		return 1
	}
	if *dry {
		fmt.Fprintf(env.Stdout, "%d step(s) to do. Nothing was changed.\n", preview.Todo)
		return 0
	}
	if *removeData && !*yes {
		if !env.Interactive || !typed(env, "Type  remove-data  to go on: ", "remove-data") {
			fmt.Fprintln(env.Stderr, "Nothing was changed (deleting the data needs --yes, or typing remove-data at a terminal).")
			return 1
		}
	}
	if _, err := Run(ctx, plan, Options{}); err != nil {
		fmt.Fprintf(env.Stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

// ----- asking

func ask(env Env, v *conf.Values) error {
	in := env.reader
	fmt.Fprintln(env.Stdout, "Answer each question, or press Enter to take what is in the brackets.")
	if !v.IsSet("WORKSPACE_ENABLED") {
		if _, found := env.Sys.LookPath("podman"); found {
			_ = v.Set("WORKSPACE_ENABLED", "yes")
		}
	}
	for _, spec := range conf.Specs {
		if !spec.Basic || !wanted(spec, v) {
			continue
		}
		if spec.Name == "CREATE_XFS_VOLUME" && !v.IsSet(spec.Name) {
			if ok, _ := volumeReady(NewCtx(env.Sys, v, io.Discard), v.Get("WORK_DIR")); !ok {
				_ = v.Set(spec.Name, "yes") // nothing is mounted there: the volume is the thing to make
			}
		}
		for attempt := 0; ; attempt++ {
			fmt.Fprintf(env.Stdout, "\n%s\n%s [%s]: ", spec.Help, spec.Name, shown(v, spec))
			line, err := in.ReadString('\n')
			if err != nil && line == "" {
				return fmt.Errorf("no answer to %s", spec.Name)
			}
			line = strings.TrimSpace(line)
			if line == "" {
				break
			}
			if err := v.Set(spec.Name, line); err != nil {
				fmt.Fprintf(env.Stdout, "  %v\n", err)
				if attempt >= 4 {
					return fmt.Errorf("%s: too many wrong answers", spec.Name)
				}
				continue
			}
			break
		}
	}
	return nil
}

func shown(v *conf.Values, spec conf.Spec) string { return v.Get(spec.Name) }

// wanted says whether a key is asked about now: the workspace keys only for a server with workspaces, and the volume's file and size only
// when the volume is to be made.
func wanted(spec conf.Spec, v *conf.Values) bool {
	if spec.Workspace && !v.Bool("WORKSPACE_ENABLED") {
		return false
	}
	if spec.Name == "XFS_VOLUME_FILE" || spec.Name == "XFS_VOLUME_SIZE" {
		return v.Bool("CREATE_XFS_VOLUME") && v.Get("XFS_DEVICE") == ""
	}
	return true
}

func confirm(env Env, question string) bool {
	fmt.Fprintf(env.Stdout, "%s [y/N] ", question)
	line, _ := env.reader.ReadString('\n')
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}

func typed(env Env, prompt, word string) bool {
	fmt.Fprint(env.Stdout, prompt)
	line, _ := env.reader.ReadString('\n')
	return strings.TrimSpace(line) == word
}

// RealEnv is the environment of the program on a real machine.
func RealEnv(version string) Env {
	exe, _ := os.Executable()
	interactive := false
	if info, err := os.Stdin.Stat(); err == nil {
		interactive = info.Mode()&os.ModeCharDevice != 0
	}
	return Env{Sys: sys.OS{}, Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, Interactive: interactive, Exe: exe, Version: version}
}
