package examdoc

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

const (
	maxWorkspaceSource = 4 << 20
	maxTodos           = 100
	maxCommandBytes    = 1000
	maxImageNameBytes  = 128
)

var (
	// imagePattern is a course image name such as cbe/cpp20: lowercase words joined by / and . _ -, with no tag or digest.
	imagePattern = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)*(?:/[a-z0-9]+(?:[._-][a-z0-9]+)*)*$`)
	// todoPattern is a TODO item at the start of a line: a GFM task-list item, or the bare [ ] the plan's example uses.
	todoPattern = regexp.MustCompile(`^(?:[-*+][ \t]+)?\[[ xX]\](?:[ \t]+(.*))?$`)
	// misplacedTodoPattern finds a TODO item that is not at the start of its line: indented, in a quote or numbered.
	misplacedTodoPattern = regexp.MustCompile(`^[ \t>]*(?:(?:[-*+]|[0-9]{1,9}[.)])[ \t]+)?\[[ xX]\](?:[ \t]|$)`)
	// attributesPattern finds the {#id points=N} at the end of a TODO item.
	attributesPattern = regexp.MustCompile(`[ \t]*(\{[^{}]*\})[ \t]*$`)
	fenceOpenPattern  = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})")
	cpuPattern        = regexp.MustCompile(`^[0-9]+(?:\.[0-9]{1,2})?$`)
	sizePattern       = regexp.MustCompile(`^([0-9]{1,6})([mMgG])$`)
)

// workspaceFiles are the entries a package folder may hold.
var workspaceFiles = map[string]bool{"workspace.md": true, "project": true, "grading": true, "image": true}

// CompileWorkspaceDir compiles the workspace package in dir: the folder that holds workspace.md. Nothing in the package can
// reach outside dir, not even through a symbolic link. It returns Problems when the package cannot be compiled.
func CompileWorkspaceDir(dir string) (WorkspaceExam, WorkspaceBundles, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return WorkspaceExam{}, WorkspaceBundles{}, Problems{"the package folder cannot be opened: " + err.Error()}
	}
	defer root.Close()
	return CompileWorkspaceFS(root.FS())
}

// CompileWorkspaceFS compiles a workspace package whose root is pkg (section 5 of the workspace plan):
//
//	workspace.md   settings, instructions and the TODO list (required)
//	project/       the starter files, copied to /work (required, may be empty)
//	grading/       hidden tests, never sent to students (optional; it must hold an executable file called grade)
//
// The document it returns holds the hashes of the two bundles and nothing of the grading folder. The bundles hold the bytes.
// Everything wrong with the package comes back together as Problems, each naming its line or its path.
func CompileWorkspaceFS(pkg fs.FS) (WorkspaceExam, WorkspaceBundles, error) {
	var problems Problems
	add := func(format string, args ...any) {
		if len(problems) == maxProblems {
			problems = append(problems, "more problems were found; fix these first")
		}
		if len(problems) < maxProblems {
			problems = append(problems, fmt.Sprintf(format, args...))
		}
	}
	report := func(line int, format string, args ...any) { add("line %d: "+format, append([]any{line}, args...)...) }

	var exam WorkspaceExam
	var bundles WorkspaceBundles
	entries, err := fs.ReadDir(pkg, ".")
	if err != nil {
		return exam, bundles, Problems{"the package folder cannot be read: " + err.Error()}
	}
	kinds := map[string]fs.FileMode{}
	var strangers []string
	for _, entry := range entries {
		name := entry.Name()
		kinds[name] = entry.Type()
		if !workspaceFiles[name] && !strings.HasPrefix(name, ".") && name != "Thumbs.db" && name != "desktop.ini" {
			strangers = append(strangers, name)
		}
	}
	pictureRoots := map[string]bool{}

	// workspace.md
	switch mode, found := kinds["workspace.md"]; {
	case !found:
		add("workspace.md is missing: it holds the settings, the instructions and the TODO list")
	case !mode.IsRegular():
		add("workspace.md must be a regular file")
	default:
		source, err := readSource(pkg, "workspace.md")
		if err != nil {
			add("workspace.md: %v", err)
			break
		}
		pictureRoots = compileWorkspaceMarkdown(source, pkg, &exam, report, add)
	}
	// Anything else in the package must be a picture the instructions use, next to workspace.md or in a folder under the package.
	for _, name := range strangers {
		if !pictureRoots[name] {
			add("%s: a workspace package holds only workspace.md, project/, grading/, image/ and the pictures the instructions use (names that start with . are ignored); move it out of the package", name)
		}
	}

	// image/ (Phase 7)
	if _, found := kinds["image"]; found {
		add("image/: per-exam image customization is not supported yet; remove the folder and use a course image")
	}

	// project/ and grading/
	switch mode, found := kinds["project"]; {
	case !found:
		add("project/ is missing: it holds the starter files, and it may be empty")
	case !mode.IsDir():
		add("project must be a folder")
	default:
		if bundle, ok := buildBundle(pkg, "project", add); ok {
			bundles.Starter = bundle
			exam.StarterBundle = bundle.Hash
		}
	}
	if mode, found := kinds["grading"]; found {
		if !mode.IsDir() {
			add("grading must be a folder")
		} else if bundle, ok := buildBundle(pkg, "grading", add); ok {
			if reason := gradeProblem(pkg); reason != "" {
				add("grading/grade: %s", reason)
			} else {
				bundles.Grading = &bundle
				exam.GradingBundle = bundle.Hash
			}
		}
	}

	if len(problems) > 0 {
		return WorkspaceExam{}, WorkspaceBundles{}, problems
	}
	exam.Kind = KindWorkspace
	return exam, bundles, nil
}

// readSource reads workspace.md, never more than the limit plus one byte.
func readSource(pkg fs.FS, name string) ([]byte, error) {
	file, err := pkg.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxWorkspaceSource+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxWorkspaceSource {
		return nil, fmt.Errorf("the file is larger than %d MiB", maxWorkspaceSource>>20)
	}
	return data, nil
}

// gradeProblem says what is wrong with grading/grade: it must be a regular file the system can run.
func gradeProblem(pkg fs.FS) string {
	info, err := fs.Lstat(pkg, "grading/grade")
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "missing: the grading folder must hold an executable file called grade that prints the results as JSON"
	case err != nil:
		return err.Error()
	case !info.Mode().IsRegular():
		return "must be a regular file, not a folder or a link"
	}
	data, err := fs.ReadFile(pkg, "grading/grade")
	if err != nil {
		return err.Error()
	}
	if info.Mode().Perm()&0o111 == 0 && !isProgram(data) {
		return "is not executable: begin the script with a #! line, or make it an ELF program, or set its executable bit"
	}
	return ""
}

// compileWorkspaceMarkdown reads the front matter and the body of workspace.md into exam. It returns the first name of every
// picture path the instructions use, so the caller can tell the pictures from what does not belong in the package.
func compileWorkspaceMarkdown(source []byte, pkg fs.FS, exam *WorkspaceExam, report func(int, string, ...any), add func(string, ...any)) map[string]bool {
	source = bytes.TrimPrefix(source, []byte("\xef\xbb\xbf"))
	if !utf8.Valid(source) {
		add("workspace.md: the file is not UTF-8 text")
		return nil
	}
	front, body, frontLines, err := splitFrontMatter(source)
	if err != nil {
		add("workspace.md: %v", err)
		return nil
	}
	settings, workspace := parseWorkspaceFrontMatter(front, report)
	c := &compiler{frontLines: frontLines, report: report, ids: map[string]bool{}, slotIDs: map[string]bool{}, pictures: map[*ast.Image]string{}, assets: visibleAssets{pkg}, pictureRoots: map[string]bool{}}
	c.markdown = c.newMarkdown(false)

	remaining, items := extractTodos(body, frontLines, report)
	c.body = remaining
	content := WorkspaceContent{Instructions: c.workspaceBody()}
	for i, item := range items {
		content.Todos = append(content.Todos, c.todo(item, i+1))
	}
	switch {
	case len(items) == 0:
		report(frontLines+1, "the exam has no TODO items; list the tasks as [ ] items, one per line")
	case len(items) > maxTodos:
		report(items[maxTodos].line, "the exam has more than %d TODO items", maxTodos)
	}
	if total, ok := content.Points(); ok && settings.points != nil && !samePoints(total, *settings.points) {
		report(1, "the TODO items add up to %s points but the front matter says %s", formatPoints(total), formatPoints(*settings.points))
	}

	exam.Title = strings.TrimSpace(settings.exam.Title)
	exam.DurationSeconds = settings.exam.DurationSeconds
	exam.GraceSeconds = settings.exam.GraceSeconds
	exam.MinVolumePercent = settings.exam.MinVolumePercent
	exam.OpensAt, exam.ClosesAt = settings.exam.OpensAt, settings.exam.ClosesAt
	exam.Tools = settings.exam.Tools
	exam.Workspace = workspace
	exam.Content = content
	return c.pictureRoots
}

// visibleAssets is the folder a workspace exam's pictures are read from: the package, except the grading folder, whose files
// no student may see, not even as a picture in the instructions.
type visibleAssets struct{ pkg fs.FS }

var errGradingHidden = errors.New("the grading folder is never part of the exam text")

func (v visibleAssets) Open(name string) (fs.File, error) {
	if name == "grading" || strings.HasPrefix(name, "grading/") {
		return nil, &fs.PathError{Op: "open", Path: name, Err: errGradingHidden}
	}
	return v.pkg.Open(name)
}

// parseWorkspaceFrontMatter reads the front matter of workspace.md. The keys it shares with question exams are read by the
// same code; the rest are the workspace's own.
func parseWorkspaceFrontMatter(lines []string, report func(int, string, ...any)) (settings, WorkspaceSettings) {
	var s settings
	workspace := WorkspaceSettings{Terminal: true, Ignore: DefaultIgnore(), Resources: Resources{CPU: DefaultCPU, MemoryMB: DefaultMemoryMB, DiskMB: DefaultDiskMB}}
	seen := readFrontMatter(lines, report, func(line int, key, value string) {
		if s.common(line, key, value, report) {
			return
		}
		switch key {
		case "image":
			switch {
			case value == "":
				report(line, "image needs the name of a course image, such as image: cbe/cpp20")
			case len(value) > maxImageNameBytes || !imagePattern.MatchString(value):
				report(line, "image must be the name of a course image, such as cbe/cpp20: lowercase words joined by / . _ or -, with no tag or digest (the server pins the version)")
			default:
				workspace.Image = value
			}
		case "terminal":
			switch value {
			case "on":
				workspace.Terminal = true
			case "off":
				workspace.Terminal = false
			default:
				report(line, "terminal must be on or off")
			}
		case "run", "check":
			if reason := commandProblem(value); reason != "" {
				report(line, "%s %s", key, reason)
				return
			}
			if key == "run" {
				workspace.Run = value
			} else {
				workspace.Check = value
			}
		case "submit", "ignore":
			globs, ok := globList(key, value, report, line)
			if !ok {
				return
			}
			if key == "submit" {
				workspace.Submit = globs
			} else {
				workspace.Ignore = globs
			}
		case "resources":
			if resources, ok := parseResources(value, report, line); ok {
				workspace.Resources = resources
			}
		case "snapshot":
			seconds, ok := wholeSeconds(value)
			if !ok || seconds < MinSnapshotSeconds || seconds > MaxSnapshotSeconds {
				report(line, "snapshot must be a length of time from %ds to %dm, such as snapshot: 30s", MinSnapshotSeconds, MaxSnapshotSeconds/60)
				return
			}
			workspace.SnapshotSeconds = &seconds
		case "navigation":
			report(line, "navigation is not allowed in a workspace exam: it is one page")
		default:
			report(line, "unknown front matter key %q", key)
		}
	})
	s.requireBasics(seen, report)
	if !seen["image"] {
		report(1, "the front matter must set an image, such as image: cbe/cpp20")
	}
	return s, workspace
}

// commandProblem says what is wrong with the command behind a Run or Check button, or returns "".
func commandProblem(command string) string {
	switch {
	case strings.TrimSpace(command) == "":
		return "needs a command, such as make run"
	case len(command) > maxCommandBytes:
		return fmt.Sprintf("must be at most %d bytes", maxCommandBytes)
	case !utf8.ValidString(command) || strings.ContainsFunc(command, func(r rune) bool { return r < ' ' || r == 0x7f }):
		return "must be one line of plain text"
	}
	return ""
}

// globList reads the value of submit or ignore: one glob, or a bracketed list of them.
func globList(key, value string, report func(int, string, ...any), line int) ([]string, bool) {
	globs, ok := listValue(value)
	if !ok {
		report(line, "%s=%s is not a list such as %s: [src/**, Makefile]", key, value, key)
		return nil, false
	}
	if len(globs) == 0 && key == "submit" {
		report(line, "submit is an empty list; leave it out to submit everything except what ignore names")
		return nil, false
	}
	if len(globs) > maxGlobs {
		report(line, "%s has more than %d patterns", key, maxGlobs)
		return nil, false
	}
	good := true
	for _, glob := range globs {
		if reason := validGlob(glob); reason != "" {
			report(line, "%s pattern %q %s", key, glob, reason)
			good = false
		}
	}
	if globs == nil {
		globs = []string{}
	}
	return globs, good
}

// parseResources reads resources: cpu=1, memory=1g, disk=100m, the same setting=value pairs as the calculator key.
func parseResources(value string, report func(int, string, ...any), line int) (Resources, bool) {
	resources := Resources{CPU: DefaultCPU, MemoryMB: DefaultMemoryMB, DiskMB: DefaultDiskMB}
	good := true
	seen := map[string]bool{}
	problem := func(format string, args ...any) {
		good = false
		report(line, "resources: "+format, args...)
	}
	if strings.TrimSpace(value) == "" {
		problem("needs settings such as resources: cpu=1, memory=1g, disk=100m")
		return resources, false
	}
	for _, pair := range splitSettings(value) {
		setting, rest, found := strings.Cut(pair, "=")
		setting, rest = strings.TrimSpace(setting), strings.TrimSpace(rest)
		if !found || setting == "" || rest == "" {
			problem("%q is not a setting=value pair such as cpu=1", strings.TrimSpace(pair))
			continue
		}
		if seen[setting] {
			problem("%s is set twice", setting)
			continue
		}
		seen[setting] = true
		switch setting {
		case "cpu":
			cpu, err := strconv.ParseFloat(rest, 64)
			if !cpuPattern.MatchString(rest) || err != nil || cpu < 0.1 || cpu > 64 {
				problem("cpu must be a number of cores from 0.1 to 64 with at most two decimals, such as cpu=1 or cpu=0.5")
				continue
			}
			resources.CPU = cpu
		case "memory", "disk":
			megabytes, ok := parseSize(rest)
			lowest, highest := 64, 64<<10
			if setting == "disk" {
				lowest, highest = 1, 100<<10
			}
			if !ok || megabytes < lowest || megabytes > highest {
				problem("%s must be a size in m or g from %s to %s, such as %s=%s", setting, sizeText(lowest), sizeText(highest), setting, map[string]string{"memory": "1g", "disk": "100m"}[setting])
				continue
			}
			if setting == "memory" {
				resources.MemoryMB = megabytes
			} else {
				resources.DiskMB = megabytes
			}
		default:
			problem("unknown setting %q; the settings are cpu, memory and disk", setting)
		}
	}
	return resources, good
}

// parseSize reads 512m or 1g as megabytes (1 g is 1024 m).
func parseSize(value string) (int, bool) {
	match := sizePattern.FindStringSubmatch(value)
	if match == nil {
		return 0, false
	}
	number, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, false
	}
	if strings.EqualFold(match[2], "g") {
		number <<= 10
	}
	return number, true
}

func sizeText(megabytes int) string {
	if megabytes%1024 == 0 {
		return fmt.Sprintf("%dg", megabytes>>10)
	}
	return fmt.Sprintf("%dm", megabytes)
}

// todoSource is a TODO item found in the body: the text after its checkbox, and the line it is on.
type todoSource struct {
	line int
	text string
}

// extractTodos takes the TODO items out of the body. A TODO item is a task-list item at the start of a line, with or without its
// leading dash: "- [ ] text" or "[ ] text". It becomes an empty line in the body, so every other line keeps its number and the
// instructions come out without the list. Lines inside a fenced code block are left alone. An item that does not start its line
// (indented, in a quote or numbered) is reported, so it is never silently shown as plain text.
func extractTodos(body []byte, frontLines int, report func(int, string, ...any)) ([]byte, []todoSource) {
	var items []todoSource
	lines := bytes.Split(body, []byte("\n"))
	fence, fenceLength := byte(0), 0
	for i, raw := range lines {
		line := strings.TrimRight(string(raw), "\r")
		number := frontLines + i + 1
		if fence != 0 {
			// A closing fence has at most three spaces before it and nothing but the fence character after them.
			trimmed := strings.TrimSpace(line)
			if len(line)-len(strings.TrimLeft(line, " ")) <= 3 && len(trimmed) >= fenceLength && strings.Trim(trimmed, string(fence)) == "" {
				fence = 0
			}
			continue
		}
		if match := fenceOpenPattern.FindStringSubmatch(line); match != nil {
			fence, fenceLength = match[1][0], len(match[1])
			continue
		}
		if match := todoPattern.FindStringSubmatch(line); match != nil {
			items = append(items, todoSource{line: number, text: strings.TrimSpace(match[1])})
			lines[i] = nil
			continue
		}
		if misplacedTodoPattern.MatchString(line) {
			report(number, "a TODO item must start its line, as [ ] text or - [ ] text, outside lists and quotes")
		}
	}
	return bytes.Join(lines, []byte("\n")), items
}

// workspaceBody renders the body, without its TODO items, as the instructions, and reports what a workspace exam may not hold:
// links, HTML, answer fields, answer blocks and a # heading.
func (c *compiler) workspaceBody() string {
	document := c.markdown.Parser().Parse(text.NewReader(c.body))
	var nodes []ast.Node
	for node := document.FirstChild(); node != nil; node = node.NextSibling() {
		nodes = append(nodes, node)
	}
	for _, node := range nodes {
		ast.Walk(node, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
			if !entering {
				return ast.WalkContinue, nil
			}
			c.forbid(n)
			switch n := n.(type) {
			case *ast.Image:
				c.workspacePicture(n)
			case *ast.FencedCodeBlock:
				if string(n.Language(c.body)) == "answer" {
					c.report(c.line(n), "answer blocks are not allowed in a workspace exam; the student's work is the files in /work")
				}
			case *ast.Heading:
				if n.Level == 1 {
					c.report(c.line(n), "use ## and ### for headings; the exam title belongs in the front matter")
				}
				ast.Walk(n, func(child ast.Node, entering bool) (ast.WalkStatus, error) {
					if _, ok := child.(*ast.Image); ok && entering {
						c.report(c.line(n), "images cannot be in headings: a title is plain text; put the image in the text below")
					}
					return ast.WalkContinue, nil
				})
			}
			return ast.WalkContinue, nil
		})
	}
	return c.render(section{nodes: nodes})
}

// forbid reports a link, raw HTML or an answer field met in n.
func (c *compiler) forbid(n ast.Node) {
	switch n := n.(type) {
	case *ast.Link, *ast.AutoLink:
		c.report(c.line(n), "links are not allowed: following one would leave the exam window")
	case *ast.RawHTML, *ast.HTMLBlock:
		c.report(c.line(n), "HTML is not allowed in an exam file; use Markdown")
	case *blankNode:
		c.report(lineOf(c.body, n.offset, c.frontLines), "answer fields ({{name}}) are not allowed in a workspace exam; they belong to question exams")
	}
}

// workspacePicture reads a picture of the instructions from next to workspace.md. A picture cannot come from the grading folder.
func (c *compiler) workspacePicture(n *ast.Image) {
	if name, err := pictureName(n.Destination); err == nil {
		c.pictureRoots[strings.SplitN(name, "/", 2)[0]] = true
		if name == "grading" || strings.HasPrefix(name, "grading/") {
			c.report(c.line(n), "image %q: pictures cannot come from the grading folder, whose files students never see", abbreviatedName(name))
			return
		}
	}
	c.picture(n)
}

// todo reads one TODO item: the {#id points=N} at the end of its line, then its text as inline Markdown.
func (c *compiler) todo(item todoSource, position int) Todo {
	todo := Todo{ID: "t" + strconv.Itoa(position)}
	body := item.text
	if match := attributesPattern.FindStringSubmatchIndex(body); match != nil && !strings.HasSuffix(body[:match[0]], "{") {
		attributes := body[match[2]:match[3]]
		body = body[:match[0]]
		reader := text.NewReader([]byte(attributes))
		parsed, ok := parser.ParseAttributes(reader)
		reader.SkipSpaces()
		switch {
		case ok && len(parsed) == 0 && reader.Peek() == text.EOF:
			// "{}" is text, not attributes: leave it where it is.
			body = item.text
		case !ok || reader.Peek() != text.EOF:
			c.report(item.line, "the attributes at the end of the TODO item could not be read; write them like {#t1 points=10}")
		}
		for _, attribute := range parsed {
			switch name := string(attribute.Name); name {
			case "id":
				value, _ := attribute.Value.([]byte)
				todo.ID = string(value)
			case "points":
				value, isNumber := attribute.Value.(float64)
				if !isNumber || !(value > 0) || value > maxPoints {
					c.report(item.line, "points must be a positive number, such as points=10")
					continue
				}
				todo.Points = &value
			default:
				c.report(item.line, "unknown TODO attribute %q; only #id and points are allowed", name)
			}
		}
	}
	if !namePattern.MatchString(todo.ID) {
		c.report(item.line, "TODO id %q may only use letters, digits, _ and -, at most 64 characters", todo.ID)
	} else if c.ids[todo.ID] {
		c.report(item.line, "TODO id %q is already used by another item", todo.ID)
	}
	c.ids[todo.ID] = true
	todo.TextHTML = c.todoText(strings.TrimSpace(body), item.line)
	return todo
}

// todoText renders the text of a TODO item as an inline HTML fragment. The item is one line of Markdown text: no pictures,
// links, HTML or answer fields, and no block such as a heading, a list or a quote.
func (c *compiler) todoText(source string, line int) string {
	if source == "" {
		c.report(line, "the TODO item has no text")
		return ""
	}
	sub := &compiler{body: []byte(source), frontLines: line - 1, report: c.report, markdown: c.markdown, pictures: map[*ast.Image]string{}, ids: map[string]bool{}, slotIDs: map[string]bool{}}
	document := c.markdown.Parser().Parse(text.NewReader(sub.body))
	first := document.FirstChild()
	if _, isParagraph := first.(*ast.Paragraph); !isParagraph || first.NextSibling() != nil {
		c.report(line, "a TODO item is one line of text without a heading, list, quote or code block")
		return ""
	}
	ast.Walk(document, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		sub.forbid(n)
		if _, isImage := n.(*ast.Image); isImage {
			c.report(line, "images cannot be in a TODO item; put them in the instructions")
		}
		return ast.WalkContinue, nil
	})
	var out bytes.Buffer
	if err := c.markdown.Renderer().Render(&out, sub.body, document); err != nil {
		c.report(line, "the TODO item could not be rendered: %v", err)
		return ""
	}
	html := strings.TrimSpace(out.String())
	html = strings.TrimSuffix(strings.TrimPrefix(html, "<p>"), "</p>")
	if len(html) > maxTodoBytes {
		c.report(line, "the TODO item is longer than %d bytes", maxTodoBytes)
	}
	return html
}

const maxTodoBytes = 2048
