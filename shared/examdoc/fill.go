package examdoc

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strings"

	goldmarkast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension/ast"
)

// answerRange is where one answer field sits in the body, and what kind of field it is.
type answerRange struct {
	start, end int
	slot       Slot
	// blank is true for a {{name}} field; otherwise this is an ```answer block.
	blank bool
	// inTable says the blank sits in a table cell, where GFM needs | escaped even inside code.
	inTable bool
}

// Fill returns a student's file: the exam's Markdown with every answer field replaced by the student's answer, and the
// original front matter replaced by header. header is the front matter block itself, without a trailing blank line; Fill
// adds the line break before the body.
//
// A source that no longer compiles is an error. An answer whose slot is not in the exam is ignored, and a slot without an
// answer counts as empty. Use FillChecked when the stored exam's slot list is available, so a compiler change that renamed
// slots is caught instead of silently leaving fields unanswered.
func Fill(source []byte, answers map[string]string, header string) ([]byte, error) {
	return fill(source, answers, nil, header)
}

// FillChecked is Fill that also refuses a source whose slot IDs differ from the stored exam's, in either direction.
func FillChecked(source []byte, answers map[string]string, slots []string, header string) ([]byte, error) {
	return fill(source, answers, slots, header)
}

func fill(source []byte, answers map[string]string, expected []string, header string) ([]byte, error) {
	// The pictures are left out: the exam's folder may be gone, and they change no answer field.
	result, err := analyze(source, nil, true)
	if err != nil {
		return nil, err
	}
	compiled := map[string]bool{}
	for _, id := range result.exam.Content.SlotIDs() {
		compiled[id] = true
	}
	if expected != nil {
		wanted := map[string]bool{}
		for _, id := range expected {
			wanted[id] = true
			if !compiled[id] {
				return nil, fmt.Errorf("the exam's Markdown no longer has answer field %q", id)
			}
		}
		for id := range compiled {
			if !wanted[id] {
				return nil, fmt.Errorf("the exam's Markdown has answer field %q, which the stored exam does not", id)
			}
		}
	}
	// An answer whose slot is not in the exam is ignored: the stored content (FillChecked) is what decides which slots exist.
	body := append([]byte(nil), result.body...)
	ranges := append([]answerRange(nil), result.ranges...)
	// Replace from the end so the offsets of the ranges still to come stay valid.
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].start > ranges[j].start })
	previous := len(body) + 1
	for _, span := range ranges {
		if span.start < 0 || span.end < span.start || span.end > len(body) || span.end > previous {
			return nil, errors.New("the answer fields of the exam overlap; nothing was filled in")
		}
		previous = span.start
		answer := answers[span.slot.ID]
		var replacement string
		if span.blank {
			replacement = fillBlank(answer, span.inTable)
			// A backtick right next to the span would join its fence into one longer run and break both code spans, as with
			// two blanks written back to back. A space keeps them apart.
			if span.start > 0 && body[span.start-1] == '`' {
				replacement = " " + replacement
			}
			if span.end < len(body) && body[span.end] == '`' {
				replacement += " "
			}
		} else {
			replacement = fillAnswerBlock(span.slot, answer, indentAt(body, span.start))
		}
		filled := make([]byte, 0, len(body)-(span.end-span.start)+len(replacement))
		filled = append(filled, body[:span.start]...)
		filled = append(filled, replacement...)
		filled = append(filled, body[span.end:]...)
		body = filled
	}
	var out bytes.Buffer
	if strings.TrimSpace(header) != "" {
		out.WriteString(strings.TrimRight(header, "\r\n"))
		out.WriteByte('\n')
	}
	out.Write(body)
	return out.Bytes(), nil
}

// fillBlank renders a {{name}} answer as an inline code span, so nothing the student typed is read as Markdown or HTML.
func fillBlank(answer string, inTable bool) string {
	if strings.TrimSpace(answer) == "" {
		return "*(no answer)*"
	}
	// A blank is one line of Markdown; keep the whole answer on it.
	text := strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		return r
	}, answer)
	if inTable {
		// GFM ends a cell at | even inside a code span.
		text = strings.ReplaceAll(text, "|", `\|`)
	}
	fence := strings.Repeat("`", maxBacktickRun(text)+1)
	if strings.HasPrefix(text, "`") || strings.HasSuffix(text, "`") {
		text = " " + text + " "
	}
	return fence + text + fence
}

// fillAnswerBlock renders an ```answer block holding the student's answer, with the original block's indentation so it stays
// where it was (an answer block may sit inside a list item).
func fillAnswerBlock(slot Slot, answer, indent string) string {
	language := "text"
	if slot.Kind == Code && slot.Language != "" {
		language = slot.Language
	}
	fence := strings.Repeat("`", max(3, maxBacktickRun(answer)+1))
	info := language + ` title="` + slot.ID + `"`
	var block strings.Builder
	writeBlock := func(content string) {
		block.WriteString(indent)
		block.WriteString(fence)
		block.WriteString(info)
		block.WriteByte('\n')
		if content != "" {
			for _, line := range answerLines(content) {
				if line != "" {
					block.WriteString(indent)
					block.WriteString(line)
				}
				block.WriteByte('\n')
			}
		}
		block.WriteString(indent)
		block.WriteString(fence)
	}
	trimmed := strings.TrimSpace(answer)
	switch {
	case trimmed == "":
		writeBlock("")
		block.WriteByte('\n')
		block.WriteString(indent)
		block.WriteString("*(no answer)*")
	case slot.Kind == Code && slot.Starter != "" && answer == slot.Starter:
		block.WriteString(indent)
		block.WriteString("*(starter text unchanged)*\n")
		writeBlock(answer)
	default:
		writeBlock(answer)
	}
	return block.String()
}

// answerLines splits an answer into lines, dropping the empty line a trailing newline would add.
func answerLines(answer string) []string {
	answer = strings.ReplaceAll(answer, "\r\n", "\n")
	lines := strings.Split(answer, "\n")
	if len(lines) > 1 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// maxBacktickRun is the longest run of consecutive backticks in text.
func maxBacktickRun(text string) int {
	longest, run := 0, 0
	for _, r := range text {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	return longest
}

// indentAt is the leading whitespace of the line that holds offset.
func indentAt(body []byte, offset int) string {
	start := offset
	for start > 0 && body[start-1] != '\n' {
		start--
	}
	end := start
	for end < len(body) && (body[end] == ' ' || body[end] == '\t') {
		end++
	}
	return string(body[start:end])
}

// inTableCell says whether a blank sits in a table cell, where GFM reads | as a cell separator even inside code spans.
func inTableCell(node goldmarkast.Node) bool {
	for n := node.Parent(); n != nil; n = n.Parent() {
		if _, ok := n.(*ast.TableCell); ok {
			return true
		}
	}
	return false
}

// answerBlockRange is where an ```answer block sits in the body: from the start of its opening fence line through the end of
// its closing fence line. goldmark does not keep the fence lines, so they are read from the source.
func answerBlockRange(body []byte, block *goldmarkast.FencedCodeBlock) (int, int) {
	pos := block.Pos()
	if pos < 0 || pos > len(body) {
		switch {
		case block.Info != nil:
			pos = block.Info.Segment.Start
		case block.Lines().Len() > 0:
			pos = block.Lines().At(0).Start
		default:
			return -1, -1
		}
	}
	start := pos
	for start > 0 && body[start-1] != '\n' {
		start--
	}
	lineEnd := start
	for lineEnd < len(body) && body[lineEnd] != '\n' {
		lineEnd++
	}
	opening := bytes.TrimLeft(body[start:lineEnd], " \t")
	if len(opening) == 0 || (opening[0] != '`' && opening[0] != '~') {
		return -1, -1
	}
	fenceChar := opening[0]
	fenceLength := 0
	for fenceLength < len(opening) && opening[fenceLength] == fenceChar {
		fenceLength++
	}
	for pos = lineEnd; ; {
		if pos < len(body) && body[pos] == '\n' {
			pos++
		}
		next := pos
		for next < len(body) && body[next] != '\n' {
			next++
		}
		if isClosingFence(body[pos:next], fenceChar, fenceLength) {
			return start, next
		}
		if next >= len(body) {
			return start, len(body)
		}
		pos = next
	}
}

// isClosingFence says whether line closes a fence of at least length of char.
func isClosingFence(line []byte, char byte, length int) bool {
	trimmed := bytes.TrimLeft(line, " \t")
	count := 0
	for count < len(trimmed) && trimmed[count] == char {
		count++
	}
	return count >= length && len(bytes.TrimSpace(trimmed[count:])) == 0
}
