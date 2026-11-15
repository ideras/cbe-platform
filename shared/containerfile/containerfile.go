// Package containerfile reads the Containerfile of a workspace exam's `image/` folder and says what is wrong with it for this platform
// (decisions P7-3 to P7-5): the compiler checks it when a package is built, the exam server when one is imported, and the runner when
// the image is built, all with this one code, so they agree.
//
// It is not a full Dockerfile parser. It reads what the rules need (instructions, their flags, the arguments of FROM and RUN) and leaves
// everything else to the builder, which says what it does not understand when the build runs.
package containerfile

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// MaxBytes is the largest Containerfile that is read.
const MaxBytes = 256 << 10

// Instruction is one instruction of a Containerfile: the lines it was written on, with its continuations joined.
type Instruction struct {
	// Line and EndLine are the first and last line of the instruction, counted from 1.
	Line, EndLine int
	// Command is the instruction's name in capitals: FROM, RUN, COPY...
	Command string
	// Flags are the options before the arguments (`--network=host`), as written.
	Flags []string
	// Args is the rest, on one line (a heredoc's body, when there is one, follows after a line break).
	Args string
}

var commands = map[string]bool{"FROM": true, "RUN": true, "CMD": true, "LABEL": true, "MAINTAINER": true, "EXPOSE": true, "ENV": true, "ADD": true,
	"COPY": true, "ENTRYPOINT": true, "VOLUME": true, "USER": true, "WORKDIR": true, "ARG": true, "ONBUILD": true, "STOPSIGNAL": true,
	"HEALTHCHECK": true, "SHELL": true}

// flagged are the instructions whose leading --options are read apart from their arguments.
var flagged = map[string]bool{"FROM": true, "RUN": true, "COPY": true, "ADD": true, "HEALTHCHECK": true}

var heredoc = regexp.MustCompile(`<<(-?)(?:"([A-Za-z0-9_]+)"|'([A-Za-z0-9_]+)'|([A-Za-z_][A-Za-z0-9_]*))`)

// Parse reads the instructions of a Containerfile. It fails only on text that cannot be an instruction at all.
func Parse(text string) ([]Instruction, error) {
	if len(text) > MaxBytes {
		return nil, fmt.Errorf("the Containerfile is larger than %d KiB", MaxBytes>>10)
	}
	if strings.ContainsRune(text, 0) {
		return nil, fmt.Errorf("the Containerfile has a null byte: it is not text")
	}
	text = strings.TrimPrefix(text, "\xef\xbb\xbf")
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var out []Instruction
	escape := byte('\\')
	// Parser directives are the comments at the top of the file (`# escape=`).
	i := 0
	for ; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(trimmed, "#") {
			break
		}
		name, value, ok := strings.Cut(strings.TrimSpace(strings.TrimPrefix(trimmed, "#")), "=")
		if ok && strings.EqualFold(strings.TrimSpace(name), "escape") {
			if v := strings.TrimSpace(value); v != "\\" {
				return nil, fmt.Errorf("line %d: only the backslash can end a line that goes on (# escape=%s is not supported)", i+1, v)
			}
		}
	}
	for i = 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		start := i
		joined := trimmed
		// A line that ends with the escape character goes on in the next one; comments and empty lines between are skipped.
		for strings.HasSuffix(joined, string(escape)) && i+1 < len(lines) {
			joined = strings.TrimSpace(strings.TrimSuffix(joined, string(escape)))
			for i+1 < len(lines) {
				next := strings.TrimSpace(lines[i+1])
				i++
				if next == "" || strings.HasPrefix(next, "#") {
					continue
				}
				joined += " " + next
				break
			}
		}
		joined = strings.TrimSuffix(strings.TrimSpace(joined), string(escape))
		name, rest, _ := strings.Cut(joined, " ")
		if name = strings.ToUpper(strings.TrimSpace(name)); name == "" {
			return nil, fmt.Errorf("line %d: not an instruction", start+1)
		}
		in := Instruction{Line: start + 1, EndLine: i + 1, Command: name}
		rest = strings.TrimSpace(rest)
		if flagged[name] {
			for strings.HasPrefix(rest, "--") {
				flag, tail, _ := strings.Cut(rest, " ")
				in.Flags = append(in.Flags, flag)
				rest = strings.TrimSpace(tail)
			}
		}
		// A heredoc: the lines up to the word that ends it are the instruction's body.
		if name == "RUN" || name == "COPY" || name == "ADD" {
			for _, m := range heredoc.FindAllStringSubmatch(rest, -1) {
				word := m[2] + m[3] + m[4]
				body := []string{}
				closed := false
				for i+1 < len(lines) {
					i++
					line := lines[i]
					if m[1] == "-" {
						line = strings.TrimLeft(line, "\t")
					}
					if line == word {
						closed = true
						break
					}
					body = append(body, lines[i])
				}
				if !closed {
					return nil, fmt.Errorf("line %d: the here-document %s is never closed", start+1, word)
				}
				rest += "\n" + strings.Join(body, "\n")
				in.EndLine = i + 1
			}
		}
		in.Args = rest
		out = append(out, in)
	}
	return out, nil
}

// Result is what Check found: Problems stop the package from being imported or built, Warnings are said and nothing more. Each begins
// with "line N: ".
type Result struct {
	Problems []string
	Warnings []string
	// Digest is the digest the file writes after the course image (`sha256:` and 64 hex), or "" when it writes none.
	Digest string
	// Stages is the number of stages (FROM lines) of the file.
	Stages int
}

var (
	digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	stagePattern  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]*$`)
)

// Check says what is wrong with a Containerfile for an exam whose course image is image (`cbe/cpp20`), and what to warn about.
func Check(text, image string) Result {
	var r Result
	problem := func(line int, format string, args ...any) {
		if len(r.Problems) < 50 {
			r.Problems = append(r.Problems, fmt.Sprintf("line %d: ", line)+fmt.Sprintf(format, args...))
		}
	}
	warn := func(line int, format string, args ...any) {
		if len(r.Warnings) < 50 {
			r.Warnings = append(r.Warnings, fmt.Sprintf("line %d: ", line)+fmt.Sprintf(format, args...))
		}
	}
	instructions, err := Parse(text)
	if err != nil {
		r.Problems = []string{err.Error()}
		return r
	}
	snapshot := strings.Contains(text, "snapshot.ubuntu.com") || strings.Contains(text, "snapshot.debian.org")
	stages := []string{}
	seenDigest := map[string]bool{}
	for _, in := range instructions {
		if !commands[in.Command] {
			problem(in.Line, "%s is not a Containerfile instruction", truncate(in.Command, 30))
			continue
		}
		if r.Stages == 0 && in.Command != "FROM" && in.Command != "ARG" {
			problem(in.Line, "%s comes before the first FROM: a Containerfile starts with FROM %s", in.Command, image)
			continue
		}
		switch in.Command {
		case "FROM":
			name := checkFrom(in, image, stages, problem)
			r.Stages++
			stages = append(stages, name)
			if digest := fromDigest(in, image); digest != "" {
				seenDigest[digest] = true
				if r.Digest == "" {
					r.Digest = digest
				}
			}
		case "VOLUME":
			problem(in.Line, "VOLUME is not allowed: a volume would be writable in a container whose files are meant to be read-only; students write in /work")
		case "ONBUILD":
			problem(in.Line, "ONBUILD is not allowed")
		case "RUN":
			checkRun(in, stages, snapshot, problem, warn)
		case "COPY", "ADD":
			checkCopy(in, stages, problem, warn)
		}
	}
	if r.Stages == 0 && len(r.Problems) == 0 {
		problem(1, "a Containerfile starts with FROM %s", image)
	}
	if len(seenDigest) > 1 {
		problem(1, "the FROM lines write different digests of %s: they must all be the one the build pins", image)
	}
	return r
}

// fromImage returns the image and the stage name of a FROM instruction.
func fromParts(in Instruction) (ref, as string, ok bool) {
	fields := strings.Fields(in.Args)
	switch {
	case len(fields) == 1:
		return fields[0], "", true
	case len(fields) == 3 && strings.EqualFold(fields[1], "AS"):
		return fields[0], fields[2], true
	}
	return "", "", false
}

func fromDigest(in Instruction, image string) string {
	ref, _, ok := fromParts(in)
	if !ok {
		return ""
	}
	if name, digest, found := strings.Cut(ref, "@"); found && name == image && digestPattern.MatchString(digest) {
		return digest
	}
	return ""
}

func checkFrom(in Instruction, image string, stages []string, problem func(int, string, ...any)) string {
	if len(in.Flags) > 0 {
		problem(in.Line, "FROM takes no options here (%s)", truncate(in.Flags[0], 30))
	}
	ref, as, ok := fromParts(in)
	if !ok {
		problem(in.Line, "FROM must be written FROM %s or FROM %s AS name", image, image)
		return ""
	}
	if as != "" && !stagePattern.MatchString(as) {
		problem(in.Line, "the stage name %q must be letters, digits, . _ or -, starting with a letter", truncate(as, 30))
	}
	name := strings.ToLower(as)
	for _, earlier := range stages {
		if earlier != "" && strings.EqualFold(earlier, ref) {
			return name
		}
	}
	base, digest, hasDigest := strings.Cut(ref, "@")
	switch {
	case strings.Contains(ref, "$"):
		problem(in.Line, "FROM cannot use a variable: it names the course image %s as it is", image)
	case base == image && !hasDigest:
	case base == image && digestPattern.MatchString(digest):
	case base == image:
		problem(in.Line, "the digest after %s must be sha256: and 64 hexadecimal digits", image)
	case strings.HasPrefix(base, image+":"):
		problem(in.Line, "write the course image without a tag: FROM %s (the build pins the version of the image the server has)", image)
	default:
		problem(in.Line, "FROM %s: the only image a workspace exam's Containerfile may start from is the exam's own course image, %s, or an earlier stage of the same file", truncate(ref, 60), image)
	}
	return name
}

// option returns the value of --name=value among flags.
func option(flags []string, name string) (string, bool) {
	for _, flag := range flags {
		if key, value, ok := strings.Cut(flag, "="); ok && key == "--"+name {
			return value, true
		}
		if flag == "--"+name {
			return "", true
		}
	}
	return "", false
}

func isStage(ref string, stages []string) bool {
	if index := strings.TrimSpace(ref); index != "" && strings.Trim(index, "0123456789") == "" {
		var n int
		fmt.Sscanf(index, "%d", &n)
		return n < len(stages)
	}
	for _, name := range stages {
		if name != "" && strings.EqualFold(name, ref) {
			return true
		}
	}
	return false
}

func checkRun(in Instruction, stages []string, snapshot bool, problem func(int, string, ...any), warn func(int, string, ...any)) {
	for _, flag := range in.Flags {
		key, value, _ := strings.Cut(flag, "=")
		switch key {
		case "--network":
			if value != "default" && value != "none" {
				problem(in.Line, "RUN --network=%s is not allowed: only default (the build's own network) and none; host would let the build reach the services of the machine", truncate(value, 30))
			}
		case "--security":
			problem(in.Line, "RUN --security is not allowed")
		case "--mount":
			for _, part := range strings.Split(value, ",") {
				if k, v, ok := strings.Cut(part, "="); ok && k == "from" && !isStage(v, stages) {
					problem(in.Line, "RUN --mount …,from=%s: it can only name an earlier stage of this file", truncate(v, 40))
				}
			}
		}
	}
	args := in.Args
	if strings.HasPrefix(args, "[") {
		var words []string
		if json.Unmarshal([]byte(args), &words) == nil {
			args = strings.Join(words, " ")
		}
	}
	for _, finding := range unpinned(args, snapshot) {
		warn(in.Line, "%s", finding)
	}
}

func checkCopy(in Instruction, stages []string, problem func(int, string, ...any), warn func(int, string, ...any)) {
	if from, ok := option(in.Flags, "from"); ok && !isStage(from, stages) {
		problem(in.Line, "%s --from=%s: it can only name an earlier stage of this file, not another image", in.Command, truncate(from, 40))
	}
	if in.Command != "ADD" {
		return
	}
	if _, checked := option(in.Flags, "checksum"); checked {
		return
	}
	for _, word := range strings.Fields(in.Args) {
		if strings.Contains(word, "://") || strings.HasPrefix(word, "git@") {
			warn(in.Line, "ADD %s downloads something with no checksum: add --checksum=sha256:… so a rebuild gets the same file", truncate(word, 60))
			return
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// Pin rewrites every FROM that names the course image to ref, the image's full reference with its digest, and returns the new text. It
// is for a Containerfile that Check accepted; the line numbers do not change.
func Pin(text, image, ref string) (string, error) {
	instructions, err := Parse(text)
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.ReplaceAll(strings.TrimPrefix(text, "\xef\xbb\xbf"), "\r\n", "\n"), "\n")
	for _, in := range instructions {
		if in.Command != "FROM" {
			continue
		}
		name, as, ok := fromParts(in)
		if !ok {
			continue
		}
		if base, _, _ := strings.Cut(name, "@"); base != image {
			continue
		}
		replacement := "FROM " + ref
		if as != "" {
			replacement += " AS " + as
		}
		// The instruction's other lines (a FROM that was continued) become empty ones, so the numbering stays.
		lines[in.Line-1] = replacement
		for l := in.Line; l < in.EndLine; l++ {
			lines[l] = ""
		}
	}
	return strings.Join(lines, "\n"), nil
}
