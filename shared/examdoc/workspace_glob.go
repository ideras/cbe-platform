package examdoc

import (
	"path"
	"strings"
	"unicode/utf8"
)

const (
	maxGlobs     = 50
	maxGlobBytes = 200
	maxGlobStars = 2
)

// validGlob says what is wrong with a submit or ignore pattern, or returns "" when it is fine. Patterns are matched against
// the slash-separated paths of the files under /work, written from /work down, such as src/*.cpp, **/*.o or build/**. Within a
// segment a star matches any run of characters except a slash, a question mark matches one character, and square brackets
// match one character of a class, all as in path.Match. A segment that is just two stars stands for any number of folders.
func validGlob(pattern string) string {
	switch {
	case pattern == "":
		return "is empty"
	case len(pattern) > maxGlobBytes:
		return "is longer than 200 bytes"
	case !utf8.ValidString(pattern) || strings.ContainsFunc(pattern, func(r rune) bool { return r < ' ' || r == 0x7f }):
		return "has characters a path cannot have"
	case strings.Contains(pattern, `\`):
		return `uses \; write the path with /`
	case strings.HasPrefix(pattern, "/"):
		return "starts with /; write the path from the project folder, such as src/**"
	case strings.HasSuffix(pattern, "/"):
		return "ends with /; name files, or use folder/** for everything below a folder"
	}
	stars := 0
	for _, segment := range strings.Split(pattern, "/") {
		switch {
		case segment == "" || segment == "." || segment == "..":
			return `has an empty, "." or ".." segment`
		case segment == "**":
			stars++
		case strings.Contains(segment, "**"):
			return "uses ** inside a segment; ** must be a whole segment, as in src/**"
		default:
			if _, err := path.Match(segment, ""); err != nil {
				return "is not a valid pattern: " + err.Error()
			}
		}
	}
	if stars > maxGlobStars {
		return "uses ** more than twice"
	}
	return ""
}

// GlobMatch reports whether the file at name, a slash-separated path under /work, matches pattern. The pattern must have passed
// the compiler. A ** segment matches any number of whole folders, none included, except as the last segment, where it matches
// the one or more names below a folder: so build/** matches build/a.o and build/x/y, but not a file called build, and **/*.o
// matches a.o and src/a.o.
func GlobMatch(pattern, name string) bool {
	return matchSegments(strings.Split(pattern, "/"), strings.Split(name, "/"))
}

func matchSegments(pattern, name []string) bool {
	for len(pattern) > 0 {
		if pattern[0] == "**" {
			rest := pattern[1:]
			if len(rest) == 0 {
				return len(name) > 0
			}
			for skip := 0; skip <= len(name); skip++ {
				if matchSegments(rest, name[skip:]) {
					return true
				}
			}
			return false
		}
		if len(name) == 0 {
			return false
		}
		if ok, err := path.Match(pattern[0], name[0]); err != nil || !ok {
			return false
		}
		pattern, name = pattern[1:], name[1:]
	}
	return len(name) == 0
}
