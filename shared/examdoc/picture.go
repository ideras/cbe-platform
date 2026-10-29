package examdoc

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	stdhtml "html"
	"io"
	"io/fs"
	"net/url"
	"path"
	"regexp"
	"strings"

	"exam-platform/shared/examimage"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

var (
	// schemePattern finds a URL scheme such as https: or data:. A file name in an exam may not start with one.
	schemePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)
	// drivePattern finds a Windows absolute path such as C:\ or C:/, which schemePattern would also take for a scheme.
	drivePattern = regexp.MustCompile(`^[A-Za-z]:[\\/]`)
)

// picture reads the ![alt](file) image n, finds the file next to the exam, checks it and remembers the <img> the renderer will
// write for it. Whatever is wrong is reported with the line of the image; a picture is never silently left out.
func (c *compiler) picture(n *ast.Image) {
	if c.skipImages {
		return
	}
	line := c.line(n)
	name := string(n.Destination)
	problem := func(format string, args ...any) {
		c.report(line, "image %q: %s", abbreviatedName(name), fmt.Sprintf(format, args...))
	}
	alt := altText(n, c.body)
	hasBlank := false
	ast.Walk(n, func(child ast.Node, entering bool) (ast.WalkStatus, error) {
		_, blank := child.(*blankNode)
		hasBlank = hasBlank || blank
		return ast.WalkContinue, nil
	})
	if hasBlank {
		problem("answer fields cannot be in the alternative text of an image")
	}
	if err := examimage.CheckAlt(alt); err != nil {
		problem("%v", err)
	}
	file, err := pictureName(n.Destination)
	if err != nil {
		problem("%v", err)
		return
	}
	name = file
	if c.assets == nil {
		problem("this exam has pictures, so it must be compiled from its file (examc exam.md, or Import exam… in the proctor application) to find them next to it")
		return
	}
	data, err := readPicture(c.assets, file)
	if err != nil {
		problem("%v", err)
		return
	}
	mime, err := examimage.Validate(data)
	if err != nil {
		problem("%v", err)
		return
	}
	if !examimage.ExtensionMatches(file, mime) {
		problem("the file is %s but its name says otherwise; give it the matching extension (.png, .jpg, .gif or .svg)", strings.TrimPrefix(mime, "image/"))
		return
	}
	if c.pictureBytes += len(data); c.pictureBytes > examimage.MaxTotalBytes {
		if !c.overTotal {
			c.overTotal = true
			problem("the pictures of the exam add up to more than %d KiB with this one; use fewer or smaller pictures", examimage.MaxTotalBytes>>10)
		}
		return
	}
	c.pictures[n] = examimage.HTML(mime, data, alt)
}

// pictureName turns the destination written in the Markdown into a slash-separated path inside the exam's folder, or says why
// the destination is not allowed.
func pictureName(destination []byte) (string, error) {
	written := string(util.UnescapePunctuations(util.ResolveNumericReferences(util.ResolveEntityNames(destination))))
	name, err := url.PathUnescape(written)
	switch {
	case err != nil:
		return "", errors.New("the file name has a % that is not followed by two hexadecimal digits")
	case strings.TrimSpace(name) == "":
		return "", errors.New("the image has no file name")
	case strings.IndexFunc(name, func(r rune) bool { return r < ' ' || r == 0x7f }) >= 0:
		return "", errors.New("the file name has control characters")
	case drivePattern.MatchString(name) || strings.HasPrefix(name, "/"):
		return "", errors.New("absolute paths are not allowed; write the path relative to the exam file, such as img/figure.png")
	case schemePattern.MatchString(name):
		return "", errors.New("only files next to the exam can be used; web addresses, data: and file: URLs are not allowed")
	case strings.Contains(name, `\`):
		return "", errors.New(`write the path with / instead of \`)
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return "", errors.New(`".." is not allowed: images must be in the exam's folder or a folder under it`)
		}
	}
	name = path.Clean(name)
	if !fs.ValidPath(name) || name == "." {
		return "", errors.New("the file name is not valid")
	}
	return name, nil
}

// readPicture reads the file for a picture, never more than the largest picture plus one byte.
func readPicture(assets fs.FS, name string) ([]byte, error) {
	// Open could block forever on a pipe, so look at what the file is first.
	info, err := fs.Stat(assets, name)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, errors.New("the file does not exist next to the exam; the path is relative to the exam file")
	case err != nil:
		return nil, fmt.Errorf("the file cannot be read: %v", err)
	case info.IsDir():
		return nil, errors.New("that is a folder, not an image file")
	case !info.Mode().IsRegular():
		return nil, errors.New("that is not a regular file")
	}
	file, err := assets.Open(name)
	if err != nil {
		return nil, fmt.Errorf("the file cannot be read: %v", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, examimage.MaxImageBytes+1))
	if err != nil {
		return nil, fmt.Errorf("the file cannot be read: %v", err)
	}
	return data, nil
}

// altText is the plain text of an image's description, with line breaks and runs of spaces collapsed to single spaces.
func altText(image *ast.Image, source []byte) string {
	var out bytes.Buffer
	writer := bufio.NewWriter(&out)
	ast.Walk(image, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *ast.Text:
			value := n.Segment.Value(source)
			if n.IsRaw() {
				_, _ = writer.Write(util.EscapeHTML(value))
			} else {
				html.DefaultWriter.Write(writer, value)
			}
			if n.SoftLineBreak() || n.HardLineBreak() {
				_ = writer.WriteByte(' ')
			}
		case *ast.String:
			_, _ = writer.Write(util.EscapeHTML(n.Value))
		}
		return ast.WalkContinue, nil
	})
	_ = writer.Flush()
	// The writer resolves backslash escapes and entities and escapes HTML; undo only the escaping, the picture writes its own.
	return strings.Join(strings.Fields(stdhtml.UnescapeString(out.String())), " ")
}

func abbreviatedName(name string) string {
	if runes := []rune(name); len(runes) > 80 {
		return string(runes[:80]) + "…"
	}
	return name
}

// pictureRenderer writes the <img> that picture prepared for each image; an image that was rejected or skipped writes nothing.
type pictureRenderer struct {
	pictures map[*ast.Image]string
}

func (r pictureRenderer) RegisterFuncs(registerer renderer.NodeRendererFuncRegisterer) {
	registerer.Register(ast.KindImage, func(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			_, _ = w.WriteString(r.pictures[node.(*ast.Image)])
		}
		return ast.WalkSkipChildren, nil
	})
}
