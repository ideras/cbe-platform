package examdoc

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// bundleTime is the modification time of every entry of a bundle. A fixed time keeps the archive the same whatever day the
// package was compiled, and it is a plausible date for the tools that read the archive.
var bundleTime = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

const (
	maxNameBytes = 255
	maxPathBytes = 4096
)

// bundleEntry is one folder, file or symbolic link of a bundle.
type bundleEntry struct {
	name string // slash-separated, relative to the bundled folder
	kind byte   // tar.TypeDir, tar.TypeReg or tar.TypeSymlink
	exec bool
	data []byte
	link string
}

// buildBundle packs the folder dir of pkg into a deterministic tar archive. Folders, regular files and symbolic links go in, with
// owner 0, fixed times, permissions 0755 for folders and executable files and 0644 for the rest, and entries sorted by name. A
// file is executable when its permission bits say so on the author's computer, or when it starts with #! or is an ELF program,
// so the same package gives the same archive on a computer that has no executable bit. Everything it cannot take it reports
// through problem, with the path from the package root, and it goes on to find the rest; ok is false then.
func buildBundle(pkg fs.FS, dir string, problem func(format string, args ...any)) (Bundle, bool) {
	var entries []bundleEntry
	var files int
	var size int64
	ok := true
	fail := func(format string, args ...any) {
		ok = false
		problem(format, args...)
	}
	tooMany, tooBig := false, false
	walkErr := fs.WalkDir(pkg, dir, func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			fail("%s: %v", name, err)
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if name == dir {
			return nil
		}
		relative := strings.TrimPrefix(name, dir+"/")
		if reason := nameProblem(relative); reason != "" {
			fail("%s: %s", name, reason)
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		kind := d.Type()
		switch {
		case kind.IsDir():
			entries = append(entries, bundleEntry{name: relative, kind: tar.TypeDir})
			return nil
		case kind&fs.ModeSymlink != 0:
			files++
			target, err := fs.ReadLink(pkg, name)
			if err != nil {
				fail("%s: the symbolic link cannot be read: %v", name, err)
				return nil
			}
			if reason := linkProblem(relative, target); reason != "" {
				fail("%s: the symbolic link %s", name, reason)
				return nil
			}
			entries = append(entries, bundleEntry{name: relative, kind: tar.TypeSymlink, link: target})
		case kind.IsRegular():
			files++
			info, err := d.Info()
			if err != nil {
				fail("%s: %v", name, err)
				return nil
			}
			if size += info.Size(); size > MaxBundleBytes && !tooBig {
				tooBig = true
				fail("%s/ holds more than %d MiB of files; the limit is %d MiB", dir, MaxBundleBytes>>20, MaxBundleBytes>>20)
			}
			if files > MaxBundleFiles && !tooMany {
				tooMany = true
				fail("%s/ holds more than %d files; the limit is %d", dir, MaxBundleFiles, MaxBundleFiles)
			}
			if tooBig || tooMany {
				return nil
			}
			data, err := fs.ReadFile(pkg, name)
			if err != nil {
				fail("%s: the file cannot be read: %v", name, err)
				return nil
			}
			entries = append(entries, bundleEntry{name: relative, kind: tar.TypeReg, data: data, exec: info.Mode().Perm()&0o111 != 0 || isProgram(data)})
		default:
			fail("%s: it is %s; a package may hold only folders, regular files and symbolic links", name, describeIrregular(kind))
		}
		return nil
	})
	if walkErr != nil {
		fail("%s/: %v", dir, walkErr)
	}
	if files > MaxBundleFiles && !tooMany {
		fail("%s/ holds more than %d files; the limit is %d", dir, MaxBundleFiles, MaxBundleFiles)
	}
	if !ok {
		return Bundle{}, false
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	size = 0
	for _, entry := range entries {
		header := &tar.Header{Name: entry.name, ModTime: bundleTime, Format: tar.FormatPAX}
		switch entry.kind {
		case tar.TypeDir:
			header.Typeflag, header.Name, header.Mode = tar.TypeDir, entry.name+"/", 0o755
		case tar.TypeSymlink:
			header.Typeflag, header.Linkname, header.Mode = tar.TypeSymlink, entry.link, 0o777
		default:
			header.Typeflag, header.Size, header.Mode = tar.TypeReg, int64(len(entry.data)), 0o644
			if entry.exec {
				header.Mode = 0o755
			}
			size += int64(len(entry.data))
		}
		if err := writer.WriteHeader(header); err != nil {
			fail("%s/%s: %v", dir, entry.name, err)
			return Bundle{}, false
		}
		if entry.kind == tar.TypeReg {
			if _, err := writer.Write(entry.data); err != nil {
				fail("%s/%s: %v", dir, entry.name, err)
				return Bundle{}, false
			}
		}
	}
	if err := writer.Close(); err != nil {
		fail("%s/: %v", dir, err)
		return Bundle{}, false
	}
	sum := sha256.Sum256(archive.Bytes())
	return Bundle{Data: archive.Bytes(), Hash: "sha256:" + hex.EncodeToString(sum[:]), Files: files, Size: size}, true
}

// isProgram says whether data starts like something the system can run: a script with a #! line or an ELF program.
func isProgram(data []byte) bool {
	return bytes.HasPrefix(data, []byte("#!")) || bytes.HasPrefix(data, []byte("\x7fELF"))
}

// nameProblem says why a path under a bundled folder cannot go in a bundle, or returns "".
func nameProblem(relative string) string {
	if len(relative) > maxPathBytes {
		return fmt.Sprintf("the path is longer than %d bytes", maxPathBytes)
	}
	for _, part := range strings.Split(relative, "/") {
		switch {
		case len(part) > maxNameBytes:
			return fmt.Sprintf("a name in the path is longer than %d bytes", maxNameBytes)
		case !utf8.ValidString(part):
			return "the name is not valid UTF-8 text"
		case strings.ContainsFunc(part, func(r rune) bool { return r < ' ' || r == 0x7f }):
			return "the name has control characters"
		}
	}
	return ""
}

// linkProblem says why a symbolic link at relative (under the bundled folder) cannot go in a bundle, or returns "". The link must
// stay inside the folder: its target is not absolute, and its .. segments, which may only come first, do not climb above the
// folder. A .. after a name could pass through another link and so cannot be checked, so it is refused.
func linkProblem(relative, target string) string {
	switch {
	case target == "":
		return "has an empty target"
	case !utf8.ValidString(target) || strings.ContainsFunc(target, func(r rune) bool { return r < ' ' || r == 0x7f }):
		return "has a target with characters a path cannot have"
	case strings.Contains(target, `\`):
		return `has a target with \; write the path with /`
	case strings.HasPrefix(target, "/"):
		return fmt.Sprintf("points to the absolute path %q, outside the folder", target)
	}
	depth := strings.Count(relative, "/")
	named := false
	for _, part := range strings.Split(target, "/") {
		switch part {
		case "", ".":
		case "..":
			if named {
				return fmt.Sprintf("has the target %q, which uses .. after a name; use .. only at the start of a target", target)
			}
			if depth--; depth < 0 {
				return fmt.Sprintf("points to %q, outside the folder", target)
			}
		default:
			named = true
		}
	}
	return ""
}

func describeIrregular(mode fs.FileMode) string {
	switch {
	case mode&fs.ModeDevice != 0:
		return "a device file"
	case mode&fs.ModeNamedPipe != 0:
		return "a named pipe"
	case mode&fs.ModeSocket != 0:
		return "a socket"
	}
	return "not a regular file"
}
