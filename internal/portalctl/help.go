package portalctl

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"exam-platform/internal/portalctl/conf"
)

// helpWidth is the width the help is filled to: a terminal of 80 columns shows it without breaking a line.
const helpWidth = 80

// commandHelp is the help of one command: how it is called, its line in the list of commands, and what it does.
type commandHelp struct {
	// Name is what is typed: "configure", "image check".
	Name string
	// Args are its options and operands, for the usage line.
	Args string
	// Summary is its line in the list of commands.
	Summary string
	// Text is what it does, in paragraphs separated by a blank line. A line that starts with two spaces (an example) is written as it is.
	Text string
	// Examples are command lines, written as they are after the options.
	Examples []string
}

// helpGroups are the commands, in the order the overview lists them.
var helpGroups = []struct {
	Title    string
	Commands []commandHelp
}{
	{"Set up the machine", []commandHelp{
		{
			Name:    "configure",
			Args:    "[--config FILE] [--yes] [--dry-run] [--interactive]",
			Summary: "Make this machine the server a configuration describes",
			Text: `Make this machine the exam server the configuration describes: the accounts, the folders, the XFS volume for workspace exams, and the services. It checks the machine first, as doctor does, then lists every step and asks before it changes anything.

With no --config it uses the answers the last run saved in INSTALL_DIR/etc/cbe-portal.conf. On a machine that was never configured it asks the basic questions instead (at a terminal).

It is safe to run again: a step that is done is left as it is, and a service that runs is not restarted (restart is where that is decided). The answers are saved for the next run.

Run it as root, unless with --dry-run.`,
			Examples: []string{
				"cbe-portalctl sample-config > cbe.conf",
				"sudo cbe-portalctl configure --config cbe.conf --dry-run",
				"sudo cbe-portalctl configure --config cbe.conf",
			},
		},
		{
			Name:    "doctor",
			Args:    "[--config FILE]",
			Summary: "Check that this machine can be configured, changing nothing",
			Text: `Check that this machine can be configured as the configuration says, and change nothing: systemd, the programs, room for the data, and for workspace exams Podman and its version, newuidmap, control groups v2 and the XFS volume. Each check says ok, warn or FAIL.

With no --config it checks for the answers the last run saved, or for the defaults.

Exit status 0: nothing failed, and configure can go ahead.`,
		},
		{
			Name:    "sample-config",
			Summary: "Print a configuration file that explains every setting",
			Text:    `Print a configuration file with every setting, its default and what it is for. Save it, change what this server needs, and give it to configure.`,
			Examples: []string{
				"cbe-portalctl sample-config > cbe.conf",
				"sudo cbe-portalctl configure --config cbe.conf",
			},
		},
		{
			Name:    "unconfigure",
			Args:    "[--remove-data] [--yes] [--dry-run]",
			Summary: "Stop and remove the services; the data stays unless asked",
			Text: `Stop the services and remove what configure made for them: the units, and the runner's limits and delegation. The data stays: the database, the screenshots, the students' folders, the volume and the accounts. The answers of the last configure are kept beside the data, in DATA_DIR/cbe-portal.conf.last.

With --remove-data all of it is deleted as well: everything in DATA_DIR, the volume file configure made and its line in /etc/fstab, and the accounts. Exams, answers and screenshots are gone for good, so it asks you to type remove-data first (at a terminal), unless --yes.

Run it as root, unless with --dry-run.`,
		},
	}},
	{"Run the server", []commandHelp{
		{
			Name:    "status",
			Summary: "Show the services, the students' volume and the workspaces",
			Text: `Show where the program and the data are, the address the server listens on, the state of each service and whether it starts at boot, and, on a server with workspace exams, the students' volume and how many student workspaces are running.

The runner's service and its containers belong to the runner's account, and only root can ask about them: run it with sudo to see them. Without root they are shown as unknown, never as stopped.

Exit status 0: everything is running. 1: something is not running, or could not be checked.

The logs:
  sudo journalctl -u cbe-server.service
  sudo journalctl -u cbe-quota-helper.service
  sudo journalctl _SYSTEMD_USER_UNIT=cbe-runner.service`,
			Examples: []string{"sudo cbe-portalctl status"},
		},
		{
			Name:    "restart",
			Args:    "[--force]",
			Summary: "Restart the services, so they run what is installed now",
			Text: `Restart the services, the quota helper and the runner first and then the server, so that they run what is installed and written now: a new program after an upgrade, a changed unit, new settings.

The students' containers keep running and their files are safe, but every student's connection is cut for a moment (the application reconnects). So the restart is refused while student workspaces are running, or when they cannot be counted, unless --force.

Run it as root.`,
		},
	}},
	{"Workspace images", []commandHelp{
		{
			Name:    "image check",
			Args:    "[--agent FILE] [--runner-config FILE] [--podman COMMAND] [--rebuild] [--keep] DIR",
			Summary: "Check an exam's own base image before the server builds it",
			Text: `Check an exam's own base image on this computer before the server builds it. It builds the package's image/Containerfile, finishes it as the server does (the student account, the agent, no way to gain privileges), and runs the agent's self test in a container made as a student's is.

DIR is the exam package's folder, its image/ folder, or the Containerfile. It needs Podman for the user who runs it (no root) and the agent. When INSTALL_DIR/etc/runner.json is there, this server's settings apply too: whether an exam may bring its own base (custom_base) and from which registries (base_registries).

Exit status 0: the server will accept the image. 1: it will not (the line NOT ACCEPTED says why), or the check could not be made.`,
			Examples: []string{
				"cbe-portalctl image check lab-python/",
				"cbe-portalctl image check lab-python/ --agent ./cbe-agent --rebuild",
			},
		},
		{
			Name:    "image install",
			Args:    "NAME DIR [--agent FILE] [--replace] [--restart]",
			Summary: "Build a class image and approve it on this server",
			Text: `Install a class image on this server: build DIR once, as image check does but in the runner's storage, and approve it as NAME in the runner's settings, so that every exam of the class can say "image: NAME".

NAME is lower-case words joined by / . _ or - (cbe/cpp20, nn-2026), with no tag. DIR is a folder with the image's Containerfile, or the Containerfile, as for image check.

A new NAME is used once the runner restarts: run cbe-portalctl restart, or add --restart, which does it now unless students are working. Installing a NAME again needs --replace and no restart: the workspaces created from then on use the new image.

Run it as root.`,
			Examples: []string{"sudo cbe-portalctl image install cbe/cpp20 images/cpp20/ --restart"},
		},
		{
			Name:    "image list",
			Summary: "List the images the runner approves, and whether each is there",
			Text: `List the course images the runner approves, each with the image it names and whether the container runtime has it. Run it as root.

Exit status 1: an approved image is missing.`,
		},
	}},
	{"", []commandHelp{
		{
			Name:    "help",
			Args:    "[COMMAND]",
			Summary: "Show this help, or what a command does and its options",
			Text:    `Show the commands, or what one does and its options. "cbe-portalctl COMMAND --help" says the same.`,
		},
		{
			Name:    "version",
			Summary: "Print the release of cbe-portalctl",
			Text:    `Print the release of cbe-portalctl.`,
		},
	}},
}

func helpFor(name string) (commandHelp, bool) {
	for _, group := range helpGroups {
		for _, h := range group.Commands {
			if h.Name == name {
				return h, true
			}
		}
	}
	return commandHelp{}, false
}

// commandNames are the commands, as they are typed, for "did you mean".
func commandNames() []string {
	var names []string
	for _, group := range helpGroups {
		for _, h := range group.Commands {
			names = append(names, h.Name)
		}
	}
	return names
}

// usage is how a command is called, after label ("Usage: "), in lines of at most helpWidth columns: the lines after the first start under
// its first option, and an option in brackets is never broken.
func usage(label string, h commandHelp) string {
	first := label + "cbe-portalctl " + h.Name
	var tokens []string
	for rest := strings.TrimSpace(h.Args); rest != ""; rest = strings.TrimSpace(rest) {
		end := strings.IndexByte(rest, ' ')
		if rest[0] == '[' {
			end = strings.IndexByte(rest, ']') + 1
		}
		if end <= 0 {
			end = len(rest)
		}
		tokens = append(tokens, rest[:end])
		rest = rest[end:]
	}
	var b strings.Builder
	// A line always holds at least one token after the command or the indent, so a token too long for any line still has one.
	line, indent := first, strings.Repeat(" ", len(first))
	for _, token := range tokens {
		if len(line)+1+len(token) > helpWidth {
			b.WriteString(line + "\n")
			line = indent
		}
		line += " " + token
	}
	b.WriteString(line + "\n")
	return b.String()
}

// printOverview writes the list of commands.
func printOverview(w io.Writer) {
	fmt.Fprint(w, "cbe-portalctl sets up and runs a CBE Portal exam server on this Linux machine.\n\nUsage: cbe-portalctl COMMAND [OPTIONS]\n")
	listCommands(w, "")
	fmt.Fprint(w, "\n")
	fill(w, `Run as root (sudo): configure, unconfigure, restart, image install and image list. doctor and image check need no root, and neither do configure and unconfigure with --dry-run. status runs as anyone, but only root sees the runner and the student workspaces.`, "", "")
	fmt.Fprint(w, "\n")
	fill(w, `Exit status: 0 done; 1 it failed, or something is not working; 2 a wrong command, option or configuration.`, "", "")
	fmt.Fprint(w, "\n")
	fill(w, `"cbe-portalctl help COMMAND" or "cbe-portalctl COMMAND --help" says what a command does and lists its options.`, "", "")
}

// listCommands writes the commands whose names start with prefix, under their groups.
func listCommands(w io.Writer, prefix string) {
	// help is shown with its operand, so that it is plain that it takes a command.
	shown := func(h commandHelp) string {
		if h.Name == "help" {
			return h.Name + " " + h.Args
		}
		return h.Name
	}
	width := 0
	for _, group := range helpGroups {
		for _, h := range group.Commands {
			width = max(width, len(shown(h)))
		}
	}
	for _, group := range helpGroups {
		var lines []string
		for _, h := range group.Commands {
			if strings.HasPrefix(h.Name, prefix) {
				lines = append(lines, fmt.Sprintf("  %-*s  %s", width, shown(h), h.Summary))
			}
		}
		if len(lines) == 0 {
			continue
		}
		fmt.Fprint(w, "\n")
		if group.Title != "" {
			fmt.Fprintf(w, "%s:\n", group.Title)
		}
		fmt.Fprintln(w, strings.Join(lines, "\n"))
	}
}

// printImageHelp writes the help of the image command itself: its subcommands.
func printImageHelp(w io.Writer) {
	fmt.Fprint(w, "Usage: cbe-portalctl image COMMAND [OPTIONS]\n")
	listCommands(w, "image ")
	fmt.Fprint(w, "\n")
	fill(w, `"cbe-portalctl help image COMMAND" says what a command does and lists its options.`, "", "")
}

// printHelp writes a command's help: how it is called, what it does, its options and examples.
func printHelp(w io.Writer, h commandHelp, fs *flag.FlagSet) {
	fmt.Fprint(w, usage("Usage: ", h)+"\n")
	paragraphs(w, h.Text, "")
	type option struct {
		flag  string
		words []string
	}
	var options []option
	fs.VisitAll(func(f *flag.Flag) {
		value, text := flag.UnquoteUsage(f)
		if isBool(f) {
			value = ""
		}
		words := strings.Fields(text)
		if f.DefValue != "" && f.DefValue != "false" {
			words = append(words, "(default: "+f.DefValue+")") // one word: a default is not broken between two lines
		}
		options = append(options, option{strings.TrimSpace("--" + f.Name + " " + value), words})
	})
	if len(options) > 0 {
		fmt.Fprint(w, "\nOptions:\n")
		width := 0
		for _, o := range options {
			width = max(width, len(o.flag))
		}
		for _, o := range options {
			first := fmt.Sprintf("  %-*s  ", width, o.flag)
			fillWords(w, o.words, first, strings.Repeat(" ", len(first)))
		}
	}
	if len(h.Examples) > 0 {
		fmt.Fprint(w, "\nExamples:\n")
		for _, example := range h.Examples {
			fmt.Fprintf(w, "  %s\n", example)
		}
	}
}

func isBool(f *flag.Flag) bool {
	b, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && b.IsBoolFlag()
}

// paragraphs writes text filled to the width: paragraphs are separated by a blank line, and a line that starts with two spaces is written
// as it is.
func paragraphs(w io.Writer, text, indent string) {
	var words []string
	flush := func() {
		if len(words) > 0 {
			fill(w, strings.Join(words, " "), indent, indent)
			words = nil
		}
	}
	for _, line := range strings.Split(text, "\n") {
		switch {
		case strings.TrimSpace(line) == "":
			flush()
			fmt.Fprint(w, "\n")
		case strings.HasPrefix(line, "  "):
			flush()
			fmt.Fprintf(w, "%s%s\n", indent, line)
		default:
			words = append(words, strings.Fields(line)...)
		}
	}
	flush()
}

// fill writes text in lines of at most helpWidth columns: the first starts with first, the others with rest. A word longer than a line has
// a line of its own.
func fill(w io.Writer, text, first, rest string) { fillWords(w, strings.Fields(text), first, rest) }

func fillWords(w io.Writer, words []string, first, rest string) {
	line := first
	empty := true
	for _, word := range words {
		if !empty && len(line)+1+len(word) > helpWidth {
			fmt.Fprintln(w, line)
			line, empty = rest, true
		}
		if empty {
			line += word
			empty = false
		} else {
			line += " " + word
		}
	}
	fmt.Fprintln(w, line)
}

// parseArgs parses a command's arguments: its options, and its operands wherever they are among the options. With -h or --help it writes
// the command's help on standard output and says to stop with status 0; with an option it does not know, it says what is wrong and how the
// command is called on standard error, and says to stop with status 2.
func parseArgs(fs *flag.FlagSet, args []string, env Env) (operands []string, code int, ok bool) {
	name := strings.TrimPrefix(fs.Name(), "cbe-portalctl ")
	// The flag package's own messages and usage are replaced by what follows, in the words of the rest of the help.
	fs.Usage = func() {}
	fs.SetOutput(io.Discard)
	for {
		err := fs.Parse(args)
		if errors.Is(err, flag.ErrHelp) {
			if h, found := helpFor(name); found {
				printHelp(env.Stdout, h, fs)
			}
			return nil, 0, false
		}
		if err != nil {
			return nil, usageError(env, name, flagProblem(err)), false
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return operands, 0, true
		}
		// The flag package takes a "--" and stops there: what follows it is operands, even what starts with a dash.
		if parsed := len(args) - len(rest); parsed > 0 && args[parsed-1] == "--" {
			return append(operands, rest...), 0, true
		}
		operands = append(operands, rest[0])
		args = rest[1:]
	}
}

// flagProblem says what the flag package found wrong, with the options written as the help writes them (--name).
func flagProblem(err error) string {
	text := err.Error()
	if option, found := strings.CutPrefix(text, "flag provided but not defined: "); found {
		return "--" + strings.TrimLeft(option, "-") + " is not an option"
	}
	if option, found := strings.CutPrefix(text, "flag needs an argument: "); found {
		return "--" + strings.TrimLeft(option, "-") + " needs a value"
	}
	return text
}

// noOperands parses the arguments of a command that takes options only.
func noOperands(fs *flag.FlagSet, args []string, env Env) (code int, ok bool) {
	rest, code, ok := parseArgs(fs, args, env)
	if !ok {
		return code, false
	}
	if len(rest) > 0 {
		return usageError(env, strings.TrimPrefix(fs.Name(), "cbe-portalctl "), fmt.Sprintf("unexpected %q", rest[0])), false
	}
	return 0, true
}

// usageError says what is wrong with how a command was called, and how it is called; it returns the exit status 2.
func usageError(env Env, name, problem string) int {
	if problem != "" {
		fmt.Fprintf(env.Stderr, "cbe-portalctl %s: %s\n", name, problem)
	}
	if h, found := helpFor(name); found {
		fmt.Fprint(env.Stderr, usage("usage: ", h))
	}
	fmt.Fprintf(env.Stderr, "Run \"cbe-portalctl help %s\" to see what it does and its options.\n", name)
	return 2
}

// unknownCommand says that a word is not a command, which one it may have been meant to be, and where the commands are listed (helpTopic:
// "" for all of them, "image" for the image commands).
func unknownCommand(env Env, word string, among []string, helpTopic string) int {
	fmt.Fprintf(env.Stderr, "cbe-portalctl: %q is not a command", word)
	if best := conf.Closest(word, among); best != "" {
		fmt.Fprintf(env.Stderr, " (did you mean %s?)", best)
	}
	fmt.Fprintf(env.Stderr, "\nRun %q to see the commands.\n", strings.TrimSpace("cbe-portalctl help "+helpTopic))
	return 2
}
