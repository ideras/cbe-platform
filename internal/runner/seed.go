package runner

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// Limits of a starter bundle (plan 5.2), which the runner enforces again whatever the exam server says.
const (
	MaxSeedBytes = 20 << 20
	MaxSeedFiles = 2000
	maxNameBytes = 255
	maxPathBytes = 4096
)

// SanitizeSeed copies a starter bundle (a tar archive) to w, making it what the container may be given. It refuses what a bundle
// from the compiler never holds, and puts right what the container needs: only folders, regular files and symbolic links go through;
// no name is absolute, empty-named, or has "." or "..", no name has a control character or a backslash; no link is absolute or climbs
// out of /work, and none uses ".." after a name; the bundle has at most 2,000 files and 20 MiB; and every entry is owned by the student
// (uid and gid 1000), which is why the bundle is rewritten rather than passed on. Modification times are kept. It returns how many
// files the bundle had.
func SanitizeSeed(r io.Reader, w io.Writer) (int, error) {
	files, _, err := sanitize(r, w, sanitizeLimits{bytes: MaxSeedBytes, files: MaxSeedFiles, what: "starter bundle"}, false)
	return files, err
}

// sanitizeLimits are how much a bundle may hold, and what to call it in a message.
type sanitizeLimits struct {
	bytes int64
	files int
	what  string
}

// maxSkippedNames is how many of the entries a lenient pass left out are named in what it returns.
const maxSkippedNames = 20

// sanitize is SanitizeSeed with limits of its own. A lenient pass is for what a student made: an entry it cannot give to a container (a
// link that leaves /work, a name with a control character, a device file) is left out and named, instead of making the whole archive
// unusable; the size and the file count are still limits. A strict pass refuses the archive at the first such entry.
func sanitize(r io.Reader, w io.Writer, limits sanitizeLimits, lenient bool) (int, []string, error) {
	in := tar.NewReader(r)
	out := tar.NewWriter(w)
	files := 0
	var total int64
	var skipped []string
	left := func(name string) {
		if len(skipped) < maxSkippedNames {
			skipped = append(skipped, name)
		}
	}
	for {
		header, err := in.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return files, skipped, fmt.Errorf("%w: the %s is not a tar archive: %v", ErrBadRequest, limits.what, err)
		}
		name := strings.TrimSuffix(header.Name, "/")
		if err := checkSeedName(name); err != nil {
			if lenient {
				left(name)
				continue
			}
			return files, skipped, err
		}
		clean := &tar.Header{Name: name, ModTime: header.ModTime.UTC().Truncate(1e9), Uid: StudentUID, Gid: StudentGID, Format: tar.FormatPAX}
		switch header.Typeflag {
		case tar.TypeDir:
			clean.Typeflag, clean.Name, clean.Mode = tar.TypeDir, name+"/", 0o755
		case tar.TypeReg:
			files++
			if total += header.Size; total > limits.bytes || header.Size < 0 {
				return files, skipped, badRequest("the %s holds more than %d MiB", limits.what, limits.bytes>>20)
			}
			clean.Typeflag, clean.Size, clean.Mode = tar.TypeReg, header.Size, 0o644
			if header.Mode&0o111 != 0 {
				clean.Mode = 0o755
			}
		case tar.TypeSymlink:
			if err := checkSeedLink(name, header.Linkname); err != nil {
				if lenient {
					left(name)
					continue
				}
				return files, skipped, err
			}
			files++
			clean.Typeflag, clean.Linkname, clean.Mode = tar.TypeSymlink, header.Linkname, 0o777
		default:
			if lenient {
				left(name)
				continue
			}
			return files, skipped, badRequest("%s: a %s may hold only folders, files and links (this is type %q)", name, limits.what, string(header.Typeflag))
		}
		if files > limits.files {
			return files, skipped, badRequest("the %s holds more than %d files", limits.what, limits.files)
		}
		if err := out.WriteHeader(clean); err != nil {
			return files, skipped, err
		}
		if clean.Typeflag == tar.TypeReg {
			if n, err := io.CopyN(out, in, header.Size); err != nil || n != header.Size {
				return files, skipped, badRequest("%s: the %s ends in the middle of a file", name, limits.what)
			}
		}
	}
	return files, skipped, out.Close()
}

func checkSeedName(name string) error {
	switch {
	case name == "":
		return badRequest("an entry of the starter bundle has no name")
	case len(name) > maxPathBytes:
		return badRequest("a name in the starter bundle is longer than %d bytes", maxPathBytes)
	case strings.HasPrefix(name, "/"):
		return badRequest("%s: names in the starter bundle are relative", name)
	case !utf8.ValidString(name):
		return badRequest("a name in the starter bundle is not UTF-8")
	}
	for _, part := range strings.Split(name, "/") {
		switch {
		case part == "" || part == "." || part == "..":
			return badRequest("%s: a name in the starter bundle has an empty, . or .. part", name)
		case len(part) > maxNameBytes:
			return badRequest("a name in the starter bundle is longer than %d bytes", maxNameBytes)
		case strings.HasPrefix(part, ".cbe-"):
			return badRequest("%s: names that start with .cbe- are kept for the system", name)
		}
		for _, r := range part {
			if r < ' ' || r == 0x7f || r == '\\' {
				return badRequest("%s: a name in the starter bundle has a control character or a backslash", name)
			}
		}
	}
	return nil
}

func checkSeedLink(name, target string) error {
	if target == "" || strings.HasPrefix(target, "/") || strings.ContainsAny(target, "\\\x00") || !utf8.ValidString(target) {
		return badRequest("%s: the link target %q is not allowed", name, target)
	}
	depth := strings.Count(name, "/")
	named := false
	for _, part := range strings.Split(target, "/") {
		switch part {
		case "", ".":
		case "..":
			if named {
				return badRequest("%s: the link target %q uses .. after a name", name, target)
			}
			if depth--; depth < 0 {
				return badRequest("%s: the link target %q leaves /work", name, target)
			}
		default:
			named = true
		}
	}
	return nil
}

// CheckGrading checks a grading bundle, which stays on the exam server and is only ever given to a grading container: it must pass
// the same rules as a starter bundle, and hold a file called grade that can be run (plan, 5.3).
func CheckGrading(r io.Reader) error {
	var kept bytes.Buffer
	if _, err := SanitizeSeed(io.TeeReader(r, &kept), io.Discard); err != nil {
		return err
	}
	reader := tar.NewReader(&kept)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return badRequest("the grading bundle has no file called grade")
		}
		if err != nil {
			return badRequest("the grading bundle is not a tar archive")
		}
		if strings.TrimSuffix(header.Name, "/") == "grade" {
			if header.Typeflag != tar.TypeReg || header.Mode&0o111 == 0 {
				return badRequest("grade in the grading bundle must be a regular file that can be run")
			}
			return nil
		}
	}
}
