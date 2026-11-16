package containerfile

import (
	"fmt"
	"strings"
)

// unpinned returns a warning for every package install in a RUN that names a package without a version (decision P7-5). It reads shell
// text loosely: the commands of the line split at && || ; | and line breaks, and each is checked for the installers it knows.
func unpinned(script string, snapshot bool) []string {
	var found []string
	for _, command := range splitCommands(script) {
		words := strings.Fields(command)
		for len(words) > 0 && (words[0] == "sudo" || words[0] == "env" || strings.Contains(words[0], "=") && !strings.HasPrefix(words[0], "-")) {
			words = words[1:]
		}
		if len(words) < 2 {
			continue
		}
		tool, rest := words[0], words[1:]
		var names []string
		var how string
		switch {
		case tool == "apt-get" || tool == "apt" || tool == "aptitude":
			if snapshot {
				continue
			}
			names, how = packages(rest, "install", map[string]bool{"-o": true, "-t": true, "-c": true, "--target-release": true},
				func(p string) bool { return strings.Contains(p, "=") }), "name=version"
		case tool == "apk":
			names, how = packages(rest, "add", map[string]bool{"-t": true, "--virtual": true, "-X": true, "--repository": true},
				func(p string) bool { return strings.Contains(p, "=") }), "name=version"
		case tool == "pip" || tool == "pip3" || strings.HasPrefix(tool, "python") && len(rest) >= 3 && rest[0] == "-m" && rest[1] == "pip":
			if strings.HasPrefix(tool, "python") {
				rest = rest[2:]
			}
			names, how = packages(rest, "install", map[string]bool{"-r": true, "-c": true, "-e": true, "-i": true, "-f": true, "-t": true, "--index-url": true,
				"--extra-index-url": true, "--find-links": true, "--target": true, "--constraint": true, "--requirement": true, "--editable": true},
				func(p string) bool {
					return strings.Contains(p, "==") || strings.Contains(p, "@") || strings.Contains(p, "://") || strings.HasPrefix(p, ".") || strings.HasPrefix(p, "/")
				}), "name==version"
		case tool == "npm" || tool == "yarn" || tool == "pnpm":
			verb := "install"
			if len(rest) > 0 && (rest[0] == "i" || rest[0] == "add") {
				verb = rest[0]
			}
			names, how = packages(rest, verb, map[string]bool{"--prefix": true, "--registry": true},
				func(p string) bool {
					return strings.LastIndex(p, "@") > 0 || strings.Contains(p, "://") || strings.HasPrefix(p, ".") || strings.HasPrefix(p, "/")
				}), "name@version"
		case tool == "gem":
			if len(rest) > 0 && rest[0] == "install" && !hasFlag(rest, "-v", "--version") {
				names, how = packages(rest, "install", map[string]bool{"-i": true, "--install-dir": true, "-s": true, "--source": true},
					func(p string) bool { return strings.Contains(p, ":") }), "-v version"
			}
		}
		if len(names) > 0 {
			found = append(found, fmt.Sprintf("%s installs %s without a version (write %s): a rebuild may not give the same image", tool, strings.Join(limit(names, 6), ", "), how))
		}
	}
	return found
}

func limit(names []string, n int) []string {
	if len(names) > n {
		return append(append([]string{}, names[:n]...), fmt.Sprintf("and %d more", len(names)-n))
	}
	return names
}

func hasFlag(words []string, flags ...string) bool {
	for _, w := range words {
		for _, f := range flags {
			if w == f || strings.HasPrefix(w, f+"=") {
				return true
			}
		}
	}
	return false
}

// packages returns the packages named after the verb that are not pinned. takesValue are the options whose next word is theirs.
func packages(words []string, verb string, takesValue map[string]bool, pinned func(string) bool) []string {
	seen := false
	var names []string
	skip := false
	for _, w := range words {
		switch {
		case skip:
			skip = false
		case !seen:
			if w == verb {
				seen = true
			}
		case strings.HasPrefix(w, "-"):
			if takesValue[w] {
				skip = true
			}
		case strings.HasPrefix(w, "$") || strings.HasPrefix(w, "\"$") || strings.HasPrefix(w, "`") || strings.HasPrefix(w, "\\"):
			// a shell variable or substitution: it is not known what it holds
		default:
			if w = strings.Trim(w, `"'`); w != "" && !pinned(w) {
				names = append(names, w)
			}
		}
	}
	return names
}

// splitCommands splits shell text at && || ; | and line breaks.
func splitCommands(script string) []string {
	replacer := strings.NewReplacer("&&", "\n", "||", "\n", ";", "\n", "|", "\n")
	var out []string
	for _, line := range strings.Split(replacer.Replace(script), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}
