package workspace

import (
	"html"
	"strconv"
	"strings"
)

// markdownOf turns the HTML of an exam's instructions and TODO items, which the compiler wrote (paragraphs, emphasis, code, lists,
// headings, tables, quotes), back into Markdown for the report. It reads only what the compiler writes; anything else loses its tags
// and keeps its text.
func markdownOf(source string) string {
	c := converter{}
	c.run(source)
	return strings.TrimSpace(c.blocks()) + "\n"
}

type converter struct {
	out      strings.Builder
	line     strings.Builder
	lists    []list
	pre      bool
	language string
	quote    int
	table    [][]string
	row      []string
	cell     strings.Builder
	inCell   bool
	header   bool
}

type list struct {
	ordered bool
	number  int
}

func (c *converter) blocks() string {
	c.flushLine()
	return c.out.String()
}

func (c *converter) write(text string) {
	if c.inCell {
		c.cell.WriteString(text)
		return
	}
	c.line.WriteString(text)
}

func (c *converter) flushLine() {
	text := c.line.String()
	c.line.Reset()
	if strings.TrimSpace(text) == "" {
		return
	}
	prefix := strings.Repeat("> ", c.quote)
	for _, line := range strings.Split(strings.TrimRight(text, "\n "), "\n") {
		c.out.WriteString(prefix + line + "\n")
	}
}

func (c *converter) blank() {
	c.flushLine()
	if s := c.out.String(); s != "" && !strings.HasSuffix(s, "\n\n") {
		c.out.WriteString("\n")
	}
}

func (c *converter) run(source string) {
	for len(source) > 0 {
		open := strings.IndexByte(source, '<')
		if open < 0 {
			c.text(source)
			return
		}
		c.text(source[:open])
		end := strings.IndexByte(source[open:], '>')
		if end < 0 {
			c.text(source[open:])
			return
		}
		c.tag(source[open+1 : open+end])
		source = source[open+end+1:]
	}
}

func (c *converter) text(raw string) {
	if raw == "" {
		return
	}
	text := html.UnescapeString(raw)
	if !c.pre {
		text = strings.Join(strings.Fields(text), " ")
		if text == "" {
			if strings.ContainsAny(raw, " \n\t") {
				c.write(" ")
			}
			return
		}
		if strings.ContainsAny(raw[:1], " \n\t") {
			text = " " + text
		}
		if strings.ContainsAny(raw[len(raw)-1:], " \n\t") {
			text += " "
		}
	}
	c.write(text)
}

func (c *converter) tag(raw string) {
	closing := strings.HasPrefix(raw, "/")
	raw = strings.TrimPrefix(strings.TrimSuffix(raw, "/"), "/")
	name, attributes, _ := strings.Cut(strings.TrimSpace(raw), " ")
	name = strings.ToLower(name)
	switch name {
	case "p", "div":
		c.blank()
	case "br":
		c.write("  \n")
	case "hr":
		c.blank()
		c.out.WriteString("---\n\n")
	case "strong", "b":
		c.write("**")
	case "em", "i":
		c.write("*")
	case "code":
		if !c.pre {
			c.write("`")
		} else if !closing && strings.Contains(attributes, "language-") {
			rest := attributes[strings.Index(attributes, "language-")+len("language-"):]
			c.language = strings.Trim(strings.FieldsFunc(rest, func(r rune) bool { return r == '"' || r == '\'' || r == ' ' })[0], `"'`)
		}
	case "pre":
		if closing {
			c.pre = false
			code := strings.TrimRight(c.line.String(), "\n")
			c.line.Reset()
			prefix := strings.Repeat("> ", c.quote)
			c.out.WriteString(prefix + "```" + c.language + "\n")
			for _, line := range strings.Split(code, "\n") {
				c.out.WriteString(prefix + line + "\n")
			}
			c.out.WriteString(prefix + "```\n\n")
			c.language = ""
		} else {
			c.blank()
			c.pre = true
		}
	case "ul", "ol":
		if closing {
			c.flushLine()
			if len(c.lists) > 0 {
				c.lists = c.lists[:len(c.lists)-1]
			}
			if len(c.lists) == 0 {
				c.blank()
			}
		} else {
			c.flushLine()
			c.lists = append(c.lists, list{ordered: name == "ol"})
		}
	case "li":
		c.flushLine()
		if !closing && len(c.lists) > 0 {
			top := &c.lists[len(c.lists)-1]
			marker := "- "
			if top.ordered {
				top.number++
				marker = strconv.Itoa(top.number) + ". "
			}
			c.line.WriteString(strings.Repeat("  ", len(c.lists)-1) + marker)
		}
	case "h1", "h2", "h3", "h4", "h5", "h6":
		c.blank()
		if !closing {
			c.line.WriteString(strings.Repeat("#", int(name[1]-'0')) + " ")
		} else {
			c.blank()
		}
	case "blockquote":
		c.blank()
		if closing && c.quote > 0 {
			c.quote--
		} else if !closing {
			c.quote++
		}
	case "table":
		if closing {
			c.flushTable()
		} else {
			c.blank()
			c.table = nil
		}
	case "tr":
		if closing {
			c.table = append(c.table, c.row)
		} else {
			c.row = nil
		}
	case "th", "td":
		if closing {
			c.row = append(c.row, strings.TrimSpace(strings.ReplaceAll(c.cell.String(), "|", `\|`)))
			c.inCell = false
		} else {
			c.cell.Reset()
			c.inCell = true
			c.header = name == "th"
		}
	case "img":
		if i := strings.Index(attributes, `alt="`); i >= 0 {
			alt := attributes[i+5:]
			c.write("[picture: " + html.UnescapeString(alt[:strings.IndexByte(alt, '"')]) + "]")
		}
	}
}

func (c *converter) flushTable() {
	if len(c.table) == 0 {
		return
	}
	width := 0
	for _, row := range c.table {
		width = max(width, len(row))
	}
	for i, row := range c.table {
		for len(row) < width {
			row = append(row, "")
		}
		c.out.WriteString("| " + strings.Join(row, " | ") + " |\n")
		if i == 0 {
			c.out.WriteString("|" + strings.Repeat(" --- |", width) + "\n")
		}
	}
	c.out.WriteString("\n")
	c.table = nil
}
