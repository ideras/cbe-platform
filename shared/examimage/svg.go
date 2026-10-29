package examimage

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

const (
	svgNamespace = "http://www.w3.org/2000/svg"
	// maxSVGDepth keeps a pathological file from nesting a webview's renderer into the ground; real drawings stay far below it.
	maxSVGDepth = 100
)

// forbiddenElements can run code, load something or animate. An SVG shown in an <img> cannot do any of that, so this is a second
// line of defense that also refuses files that are clearly not a plain figure.
var forbiddenElements = map[string]bool{
	"script": true, "foreignobject": true, "iframe": true, "object": true, "embed": true, "audio": true, "video": true,
	"animate": true, "animatemotion": true, "animatetransform": true, "set": true,
}

// cssURL finds url( in a style or attribute value; its argument may only be a #fragment.
var cssURL = regexp.MustCompile(`(?i)url\(\s*["']?\s*([^)"']*)`)

// validateSVG checks that data is a well-formed SVG document and holds nothing that could run or load. It accepts what
// Graphviz and Inkscape write: an XML declaration, comments, a plain DOCTYPE, and namespaces and attributes of other tools.
func validateSVG(data []byte) error {
	decoder := xml.NewDecoder(bytes.NewReader(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))))
	var style strings.Builder
	depth, roots, styles := 0, 0, 0
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("the SVG is not well-formed UTF-8 XML: %v", err)
		}
		switch token := token.(type) {
		case xml.StartElement:
			depth++
			if depth == 1 {
				if roots++; roots > 1 {
					return errors.New("the SVG has more than one root element")
				}
				if token.Name.Local != "svg" || token.Name.Space != svgNamespace {
					return fmt.Errorf("the root element must be <svg xmlns=%q>", svgNamespace)
				}
			}
			if depth > maxSVGDepth {
				return fmt.Errorf("the SVG nests elements more than %d levels deep", maxSVGDepth)
			}
			if forbiddenElements[strings.ToLower(token.Name.Local)] {
				return fmt.Errorf("the SVG has a <%s> element, which is not allowed", token.Name.Local)
			}
			if strings.EqualFold(token.Name.Local, "style") {
				styles++
			}
			for _, attribute := range token.Attr {
				if err := checkAttribute(attribute); err != nil {
					return err
				}
			}
		case xml.EndElement:
			depth--
			if strings.EqualFold(token.Name.Local, "style") {
				if styles--; styles == 0 {
					if err := checkCSS(style.String(), "a <style> element"); err != nil {
						return err
					}
					style.Reset()
				}
			}
		case xml.CharData:
			if styles > 0 {
				style.Write(token)
			} else if depth == 0 && len(bytes.TrimSpace(token)) > 0 {
				return errors.New("the SVG has text outside its root element")
			}
		case xml.Directive:
			if err := checkDirective(token); err != nil {
				return err
			}
		case xml.ProcInst:
			if strings.EqualFold(token.Target, "xml-stylesheet") {
				return errors.New("the SVG links a style sheet, which is not allowed")
			}
		}
	}
	if roots == 0 {
		return errors.New("the SVG has no <svg> root element")
	}
	return nil
}

func checkAttribute(attribute xml.Attr) error {
	name := attribute.Name.Local
	switch {
	case strings.HasPrefix(strings.ToLower(name), "on"):
		return fmt.Errorf("the SVG has an event handler (%s), which is not allowed", name)
	case strings.EqualFold(name, "href") && !strings.HasPrefix(strings.TrimSpace(attribute.Value), "#"):
		return fmt.Errorf("the SVG has a reference to something outside itself (%s=%q); only #references to its own elements are allowed, so a picture embedded in an SVG must be exported as its own PNG", name, abbreviate(attribute.Value))
	}
	return checkCSS(attribute.Value, "the "+name+" attribute")
}

// checkCSS refuses @import and any url() that does not point at a #fragment of the same document.
func checkCSS(css, where string) error {
	if strings.Contains(strings.ToLower(css), "@import") {
		return fmt.Errorf("%s of the SVG uses @import, which is not allowed", where)
	}
	for _, match := range cssURL.FindAllStringSubmatch(css, -1) {
		if !strings.HasPrefix(strings.TrimSpace(match[1]), "#") {
			return fmt.Errorf("%s of the SVG uses url(%s), which is not allowed; only url(#id) is", where, abbreviate(match[1]))
		}
	}
	return nil
}

// checkDirective accepts a plain <!DOCTYPE svg PUBLIC …> and refuses an internal subset, where entities are declared.
func checkDirective(directive xml.Directive) error {
	quote := byte(0)
	for _, c := range directive {
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '[':
			return errors.New("the SVG declares entities or other definitions in its DOCTYPE, which is not allowed")
		}
	}
	if bytes.Contains(bytes.ToUpper(directive), []byte("<!ENTITY")) {
		return errors.New("the SVG declares entities, which is not allowed")
	}
	return nil
}

func abbreviate(value string) string {
	value = strings.TrimSpace(value)
	if runes := []rune(value); len(runes) > 40 {
		return string(runes[:40]) + "…"
	}
	return value
}
