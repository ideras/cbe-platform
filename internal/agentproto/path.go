package agentproto

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	// MaxPathBytes and MaxNameBytes are the longest path and the longest name in it, as the kernel has them.
	MaxPathBytes = 4096
	MaxNameBytes = 255
	// ReservedPrefix starts the names the agent keeps for itself, such as the temporary file of a write. No request may name one,
	// and listings leave them out.
	ReservedPrefix = ".cbe-"
)

// CheckPath says what is wrong with a path of a request, or returns nil. A path is written from /work down, with / between names.
// "" is /work itself. It may not be absolute or hold an empty, "." or ".." name, a NUL byte, a backslash, a control character or
// invalid UTF-8, and no name may start with ReservedPrefix. The exam server applies the same rule before a message reaches the
// agent, and the agent applies it again: neither trusts the other.
func CheckPath(path string) error {
	if path == "" {
		return nil
	}
	if len(path) > MaxPathBytes {
		return badPath("the path is longer than %d bytes", MaxPathBytes)
	}
	if strings.HasPrefix(path, "/") {
		return badPath("the path must be relative to /work, not absolute")
	}
	for _, name := range strings.Split(path, "/") {
		if err := CheckName(name); err != nil {
			return err
		}
	}
	return nil
}

// CheckName says what is wrong with one name in a path, or returns nil.
func CheckName(name string) error {
	switch {
	case name == "":
		return badPath("the path has an empty name (a doubled or a trailing /)")
	case name == "." || name == "..":
		return badPath("the path has a %q name", name)
	case len(name) > MaxNameBytes:
		return badPath("a name in the path is longer than %d bytes", MaxNameBytes)
	case !utf8.ValidString(name):
		return badPath("a name in the path is not valid UTF-8")
	case strings.HasPrefix(name, ReservedPrefix):
		return badPath("names that start with %s are kept for the system", ReservedPrefix)
	}
	for _, r := range name {
		switch {
		case r == 0:
			return badPath("the path has a NUL byte")
		case r == '\\':
			return badPath(`the path has a \; write it with /`)
		case r < ' ' || r == 0x7f:
			return badPath("a name in the path has a control character")
		}
	}
	return nil
}

// SplitPath splits a checked, non-empty path into the folder that holds it ("" for /work) and its last name.
func SplitPath(path string) (folder, name string) {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[:i], path[i+1:]
	}
	return "", path
}

func badPath(format string, args ...any) error {
	return &Error{Code: CodeBadPath, Message: fmt.Sprintf(format, args...)}
}
