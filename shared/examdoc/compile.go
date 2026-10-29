package examdoc

import (
	"bytes"
	"fmt"
	"io/fs"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"exam-platform/shared/examimage"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

const (
	maxPoints       = 10000
	maxProblems     = 50
	maxLines        = 100
	defaultTextRows = 6
)

// Problems lists everything wrong with an exam file, each naming the line it is on.
type Problems []string

func (p Problems) Error() string { return strings.Join(p, "\n") }

var (
	namePattern     = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	languagePattern = regexp.MustCompile(`^[a-z0-9+#-]{1,32}$`)
	letters         = "abcdefghijklmnopqrstuvwxyz"
)

// Compile turns an exam written in Markdown into the exam document the server takes. It returns Problems when the file
// cannot be compiled. An exam with pictures cannot be compiled this way, because nothing says where to find them: use
// CompileDir or CompileFS.
func Compile(source []byte) (Exam, error) {
	return CompileFS(source, nil)
}

// CompileDir is Compile for an exam file that sits in dir: its pictures are the files next to it, in dir and the folders under
// it. A picture cannot reach outside dir, not even through a symbolic link.
func CompileDir(source []byte, dir string) (Exam, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return Exam{}, Problems{"the folder of the exam file cannot be opened, so its pictures cannot be read: " + err.Error()}
	}
	defer root.Close()
	return CompileFS(source, root.FS())
}

// CompileFS is Compile with the pictures of the exam taken from assets, whose root is the folder of the exam file. A nil assets
// means the exam may not have pictures.
func CompileFS(source []byte, assets fs.FS) (Exam, error) {
	result, err := analyze(source, assets, false)
	return result.exam, err
}

// analyzed is a compiled exam together with the positions of its answer fields in the body, which Fill needs.
type analyzed struct {
	exam   Exam
	ranges []answerRange
	body   []byte
}

// analyze compiles source. skipImages leaves the pictures alone: they are not read or checked and the HTML has none of them.
// Fill needs that, because it runs on the Markdown the server stored, long after the exam's folder may be gone, and wants only
// the positions of the answer fields.
func analyze(source []byte, assets fs.FS, skipImages bool) (analyzed, error) {
	var problems Problems
	report := func(line int, format string, args ...any) {
		if len(problems) == maxProblems {
			problems = append(problems, "more problems were found; fix these first")
		}
		if len(problems) < maxProblems {
			problems = append(problems, fmt.Sprintf("line %d: ", line)+fmt.Sprintf(format, args...))
		}
	}
	source = bytes.TrimPrefix(source, []byte("\xef\xbb\xbf"))
	if !utf8.Valid(source) {
		return analyzed{}, Problems{"the file is not UTF-8 text"}
	}
	front, body, frontLines, err := splitFrontMatter(source)
	if err != nil {
		return analyzed{}, Problems{err.Error()}
	}
	settings := parseFrontMatter(front, report)
	c := &compiler{body: body, frontLines: frontLines, report: report, slotIDs: map[string]bool{}, ids: map[string]bool{},
		assets: assets, skipImages: skipImages, pictures: map[*ast.Image]string{}}
	settings.exam.Content = c.content()
	if total, ok := settings.exam.Content.Points(); ok && settings.points != nil && !samePoints(total, *settings.points) {
		report(1, "the exercises add up to %s points but the front matter says %s", formatPoints(total), formatPoints(*settings.points))
	}
	if len(problems) > 0 {
		return analyzed{}, problems
	}
	return analyzed{exam: settings.exam, ranges: c.ranges, body: body}, nil
}

type compiler struct {
	body       []byte
	frontLines int
	report     func(int, string, ...any)
	markdown   goldmark.Markdown
	// ids holds exercise and item IDs, slotIDs the full slot IDs; both must be unique in the exam.
	ids, slotIDs map[string]bool
	// ranges records where every answer field sits in the body, so Fill can replace it.
	ranges []answerRange
	// assets holds the files the exam's pictures name; skipImages says to ignore the pictures.
	assets     fs.FS
	skipImages bool
	// pictures holds the <img> element written for each picture met so far, which the renderer puts in place of the node.
	// pictureBytes is how much the files add up to; overTotal says that was already reported.
	pictures     map[*ast.Image]string
	pictureBytes int
	overTotal    bool
}

// section is the run of top-level blocks under one heading, or before the first exercise heading.
type section struct {
	heading *ast.Heading
	nodes   []ast.Node
}

func (c *compiler) content() Content {
	c.markdown = goldmark.New(
		goldmark.WithExtensions(extension.NewTable(extension.WithTableCellAlignMethod(extension.TableCellAlignAttribute)), extension.Strikethrough),
		goldmark.WithParserOptions(parser.WithHeadingAttribute(), parser.WithInlineParsers(util.Prioritized(blankParser{}, 50))),
		goldmark.WithRendererOptions(renderer.WithNodeRenderers(util.Prioritized(slotRenderer{}, 50), util.Prioritized(pictureRenderer{c.pictures}, 50))),
	)
	document := c.markdown.Parser().Parse(text.NewReader(c.body))
	instructions := section{}
	var exercises []section
	var items [][]section
	for node := document.FirstChild(); node != nil; node = node.NextSibling() {
		heading, _ := node.(*ast.Heading)
		switch {
		case heading != nil && heading.Level == 1:
			c.report(c.line(node), "use ## for an exercise and ### for an item; the exam title belongs in the front matter")
		case heading != nil && heading.Level == 2:
			exercises = append(exercises, section{heading: heading})
			items = append(items, nil)
		case heading != nil && heading.Level == 3 && len(exercises) > 0:
			last := len(items) - 1
			items[last] = append(items[last], section{heading: heading})
		case len(exercises) == 0:
			instructions.nodes = append(instructions.nodes, node)
		case len(items[len(items)-1]) > 0:
			list := items[len(items)-1]
			list[len(list)-1].nodes = append(list[len(list)-1].nodes, node)
		default:
			exercises[len(exercises)-1].nodes = append(exercises[len(exercises)-1].nodes, node)
		}
	}
	var content Content
	if slots := c.collect(instructions, ""); len(slots) > 0 {
		c.report(slots[0].line, "answer fields cannot be in the general instructions; put them under an exercise (##) or item (###)")
	}
	content.Instructions = c.render(instructions)
	if len(exercises) == 0 {
		c.report(c.frontLines+1, "the exam has no exercises; start each one with a ## heading")
	}
	for i, ex := range exercises {
		exercise := Exercise{}
		exercise.ID, exercise.Title, exercise.Points = c.heading(ex.heading, strconv.Itoa(i+1))
		exercise.Slots = c.slots(c.collect(ex, exercise.ID))
		exercise.Body = c.render(ex)
		total, complete, count := 0.0, true, len(exercise.Slots)
		for j, it := range items[i] {
			item := Item{}
			item.ID, item.Title, item.Points = c.heading(it.heading, exercise.ID+label(j))
			item.Slots = c.slots(c.collect(it, item.ID))
			item.Body = c.render(it)
			if len(item.Slots) == 0 {
				c.report(c.line(it.heading), "item %q has no answer field; add a {{blank}} or an ```answer block", item.Title)
			}
			if item.Points == nil {
				complete = false
			} else {
				total += *item.Points
			}
			count += len(item.Slots)
			exercise.Items = append(exercise.Items, item)
		}
		if count == 0 && len(exercise.Items) == 0 {
			c.report(c.line(ex.heading), "exercise %q has no answer field; add a {{blank}} or an ```answer block", exercise.Title)
		}
		if exercise.Points != nil && complete && len(exercise.Items) > 0 && !samePoints(total, *exercise.Points) {
			c.report(c.line(ex.heading), "the items of exercise %q add up to %s points, not %s", exercise.Title, formatPoints(total), formatPoints(*exercise.Points))
		}
		content.Exercises = append(content.Exercises, exercise)
	}
	return content
}

// heading reads an exercise or item heading: its text is the title and {#id points=N} sets the rest.
func (c *compiler) heading(heading *ast.Heading, fallbackID string) (string, string, *float64) {
	line := c.line(heading)
	id := fallbackID
	var points *float64
	for _, attribute := range heading.Attributes() {
		switch name := string(attribute.Name); name {
		case "id":
			value, _ := attribute.Value.([]byte)
			id = string(value)
		case "points":
			value, ok := attribute.Value.(float64)
			if !ok || !(value > 0) || value > maxPoints {
				c.report(line, "points must be a positive number, such as points=10")
				continue
			}
			points = &value
		default:
			c.report(line, "unknown heading attribute %q; only #id and points are allowed", name)
		}
	}
	title := strings.TrimSpace(plainText(heading, c.body))
	switch {
	case title == "":
		c.report(line, "the heading needs a title")
	case strings.HasSuffix(title, "}") && strings.Contains(title, "{"):
		c.report(line, "the attributes at the end of the heading could not be read; write them like {#1a points=10}")
	}
	if !namePattern.MatchString(id) {
		c.report(line, "id %q may only use letters, digits, _ and -, at most 64 characters", id)
	} else if c.ids[id] {
		c.report(line, "id %q is already used by another exercise or item", id)
	}
	c.ids[id] = true
	return id, title, points
}

// found is an answer field met in a section, in document order.
type found struct {
	slot Slot
	line int
}

// collect turns the answer blocks of a section into slot nodes, names every slot with the section's ID, and reports what an
// exam may not contain. The slots come back in reading order.
func (c *compiler) collect(s section, prefix string) []found {
	var answers []*ast.FencedCodeBlock
	for _, node := range s.nodes {
		ast.Walk(node, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
			if !entering {
				return ast.WalkContinue, nil
			}
			switch n := n.(type) {
			case *ast.FencedCodeBlock:
				if string(n.Language(c.body)) == "answer" {
					answers = append(answers, n)
				}
			case *ast.Image:
				c.picture(n)
			case *ast.Link, *ast.AutoLink:
				c.report(c.line(n), "links are not allowed: following one would leave the exam window")
			case *ast.RawHTML, *ast.HTMLBlock:
				c.report(c.line(n), "HTML is not allowed in an exam file; use Markdown")
			case *ast.Heading:
				if n.Level <= 3 && n.Parent() != nil && n.Parent().Kind() != ast.KindDocument {
					c.report(c.line(n), "## and ### headings must start at the beginning of a line, outside lists and quotes")
				}
			}
			return ast.WalkContinue, nil
		})
	}
	for _, block := range answers {
		slot, ok := c.answer(block, prefix)
		start, end := answerBlockRange(c.body, block)
		answer := &answerNode{slot: slot, ok: ok, line: c.line(block), start: start, end: end}
		block.Parent().ReplaceChild(block.Parent(), block, answer)
		for i, node := range s.nodes {
			if node == ast.Node(block) {
				s.nodes[i] = answer
			}
		}
	}
	var slots []found
	for _, node := range s.nodes {
		ast.Walk(node, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
			if !entering {
				return ast.WalkContinue, nil
			}
			switch n := n.(type) {
			case *blankNode:
				line := lineOf(c.body, n.offset, c.frontLines)
				if n.problem != "" {
					c.report(line, "%s", n.problem)
					break
				}
				n.id = prefix + "." + n.name
				slot := Slot{ID: n.id, Kind: Blank}
				slots = append(slots, found{slot, line})
				c.ranges = append(c.ranges, answerRange{start: n.offset, end: n.endOffset, slot: slot, blank: true, inTable: inTableCell(n)})
			case *answerNode:
				if n.ok {
					slots = append(slots, found{n.slot, n.line})
					c.ranges = append(c.ranges, answerRange{start: n.start, end: n.end, slot: n.slot})
				}
			}
			return ast.WalkContinue, nil
		})
	}
	if s.heading != nil {
		ast.Walk(s.heading, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
			if blank, ok := n.(*blankNode); ok && entering {
				c.report(c.line(s.heading), "answer fields cannot be in headings; put {{%s}} in the text below", blank.name)
			}
			if _, ok := n.(*ast.Image); ok && entering && !c.skipImages {
				c.report(c.line(s.heading), "images cannot be in headings: a title is plain text; put the image in the text below")
			}
			return ast.WalkContinue, nil
		})
	}
	return slots
}

// answer reads an ```answer {#name lang=mips lines=12} block; what the block holds becomes the starter text.
func (c *compiler) answer(block *ast.FencedCodeBlock, prefix string) (Slot, bool) {
	line := c.line(block)
	slot := Slot{Kind: Text}
	name := "answer"
	info := bytes.TrimSpace(block.Info.Segment.Value(c.body))
	rest := bytes.TrimSpace(bytes.TrimPrefix(info, []byte("answer")))
	ok := true
	kind := ""
	if len(rest) > 0 {
		reader := text.NewReader(rest)
		attributes, parsed := parser.ParseAttributes(reader)
		reader.SkipSpaces()
		if !parsed || reader.Peek() != text.EOF {
			c.report(line, "the answer block options could not be read; write them like ```answer {#name lang=mips lines=12}")
			ok = false
		}
		for _, attribute := range attributes {
			switch key := string(attribute.Name); key {
			case "id":
				value, _ := attribute.Value.([]byte)
				name = string(value)
			case "kind":
				value, _ := attribute.Value.([]byte)
				kind = string(value)
				if kind != Text && kind != Code {
					c.report(line, "kind must be text or code")
					ok = false
				}
			case "lang":
				value, _ := attribute.Value.([]byte)
				slot.Language = string(value)
				if !languagePattern.MatchString(slot.Language) {
					c.report(line, "lang must be a short lowercase language name, such as mips or c")
					ok = false
				}
			case "lines":
				value, isNumber := attribute.Value.(float64)
				if !isNumber || value != math.Trunc(value) || value < 1 || value > maxLines {
					c.report(line, "lines must be a whole number from 1 to %d", maxLines)
					ok = false
					continue
				}
				slot.Lines = int(value)
			default:
				c.report(line, "unknown answer option %q; use #name, kind, lang or lines", key)
				ok = false
			}
		}
	}
	switch {
	case kind != "":
		slot.Kind = kind
	case slot.Language != "":
		slot.Kind = Code
	}
	if slot.Kind == Text && slot.Language != "" {
		c.report(line, "lang only applies to code answers")
		ok = false
	}
	if slot.Kind == Text && slot.Lines == 0 {
		slot.Lines = defaultTextRows
	}
	var starter strings.Builder
	for i := 0; i < block.Lines().Len(); i++ {
		segment := block.Lines().At(i)
		starter.Write(segment.Value(c.body))
	}
	slot.Starter = starter.String()
	if !namePattern.MatchString(name) {
		c.report(line, "answer name %q may only use letters, digits, _ and -, at most 64 characters", name)
		ok = false
	}
	slot.ID = prefix + "." + name
	return slot, ok
}

// slots checks that every slot of a section has its own ID.
func (c *compiler) slots(found []found) []Slot {
	var slots []Slot
	for _, f := range found {
		if c.slotIDs[f.slot.ID] {
			c.report(f.line, "answer field %q is already used; give each field its own name", f.slot.ID)
			continue
		}
		c.slotIDs[f.slot.ID] = true
		slots = append(slots, f.slot)
	}
	return slots
}

func (c *compiler) render(s section) string {
	document := ast.NewDocument()
	for _, node := range s.nodes {
		document.AppendChild(document, node)
	}
	var out bytes.Buffer
	if err := c.markdown.Renderer().Render(&out, c.body, document); err != nil {
		c.report(c.frontLines+1, "the exam could not be rendered: %v", err)
	}
	fragment := strings.TrimSpace(out.String())
	if len(fragment) > examimage.MaxFragmentBytes {
		line, name := c.frontLines+1, "the general instructions"
		if s.heading != nil {
			line, name = c.line(s.heading), fmt.Sprintf("%q", strings.TrimSpace(plainText(s.heading, c.body)))
		}
		c.report(line, "the text and pictures of %s take %d KiB, more than the %d KiB one part may hold (a picture takes a third more than its file); move pictures to another item or make them smaller",
			name, (len(fragment)+1023)>>10, examimage.MaxFragmentBytes>>10)
	}
	return fragment
}

// line is the file line a node starts on, found through the nearest node that knows its position.
func (c *compiler) line(node ast.Node) int {
	for n := node; n != nil; n = n.Parent() {
		if position := n.Pos(); position >= 0 {
			return lineOf(c.body, position, c.frontLines)
		}
	}
	return c.frontLines + 1
}

func plainText(node ast.Node, source []byte) string {
	var out strings.Builder
	ast.Walk(node, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *ast.Text:
			out.Write(n.Segment.Value(source))
			if n.SoftLineBreak() || n.HardLineBreak() {
				out.WriteByte(' ')
			}
		case *ast.String:
			out.Write(n.Value)
		}
		return ast.WalkContinue, nil
	})
	return out.String()
}

// label is the letter of the i-th item: a, b, ..., z, then 27, 28, ...
func label(i int) string {
	if i < len(letters) {
		return letters[i : i+1]
	}
	return strconv.Itoa(i + 1)
}

func samePoints(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func formatPoints(points float64) string { return strconv.FormatFloat(points, 'f', -1, 64) }

var (
	kindBlank  = ast.NewNodeKind("ExamBlank")
	kindAnswer = ast.NewNodeKind("ExamAnswer")
)

// blankNode is a {{name}} field in the text. A malformed one carries the problem to report instead of a name.
type blankNode struct {
	ast.BaseInline
	name, id, problem string
	offset, endOffset int
}

func (n *blankNode) Kind() ast.NodeKind { return kindBlank }
func (n *blankNode) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{"name": n.name}, nil)
}

// answerNode replaces an ```answer block once its slot is read; ok is false when the block's options were wrong.
type answerNode struct {
	ast.BaseBlock
	slot       Slot
	ok         bool
	line       int
	start, end int
}

func (n *answerNode) Kind() ast.NodeKind { return kindAnswer }
func (n *answerNode) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{"id": n.slot.ID}, nil)
}

type blankParser struct{}

func (blankParser) Trigger() []byte { return []byte{'{'} }

func (blankParser) Parse(_ ast.Node, block text.Reader, _ parser.Context) ast.Node {
	line, segment := block.PeekLine()
	if !bytes.HasPrefix(line, []byte("{{")) {
		return nil
	}
	node := &blankNode{offset: segment.Start}
	node.SetPos(segment.Start)
	end := bytes.Index(line, []byte("}}"))
	if end < 0 {
		node.problem = "{{ opens an answer field that is not closed by }} on the same line"
		block.Advance(2)
		return node
	}
	node.name = strings.TrimSpace(string(line[2:end]))
	if !namePattern.MatchString(node.name) {
		node.problem = fmt.Sprintf("answer field name %q may only use letters, digits, _ and -, at most 64 characters", node.name)
	}
	node.endOffset = segment.Start + end + 2
	block.Advance(end + 2)
	return node
}

type slotRenderer struct{}

func (slotRenderer) RegisterFuncs(registerer renderer.NodeRendererFuncRegisterer) {
	registerer.Register(kindBlank, func(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering && node.(*blankNode).id != "" {
			_, _ = w.WriteString(`<span class="slot" data-slot="`)
			_, _ = w.Write(util.EscapeHTML([]byte(node.(*blankNode).id)))
			_, _ = w.WriteString(`"></span>`)
		}
		return ast.WalkSkipChildren, nil
	})
	registerer.Register(kindAnswer, func(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			_, _ = w.WriteString(`<div class="slot" data-slot="`)
			_, _ = w.Write(util.EscapeHTML([]byte(node.(*answerNode).slot.ID)))
			_, _ = w.WriteString("\"></div>\n")
		}
		return ast.WalkSkipChildren, nil
	})
}
