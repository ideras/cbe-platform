// Package examimage decides which pictures an exam may carry and how they are written into the exam's HTML.
//
// The exam compiler (shared/examdoc) embeds each picture as a data: URI in an <img> element, and the server checks that what
// it is sent has that shape and nothing else. Both use this package, so what the compiler writes and what the server accepts
// cannot drift apart. The student application's sanitizer (shared/exam/sanitize.js) repeats the shape check in JavaScript.
package examimage

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"path"
	"strings"
	"unicode/utf8"
)

// Limits of the pictures of an exam. One body is at most 256 KiB and base64 adds a third, so these keep the compiled JSON
// well below the server's body, content and request caps. If they ever grow, those caps must grow with them.
const (
	// MaxImageBytes is the largest picture file.
	MaxImageBytes = 128 << 10
	// MaxTotalBytes is the most all the picture files of one exam may add up to.
	MaxTotalBytes = 512 << 10
	// MaxDimension is the largest width or height of a PNG, JPEG or GIF, in pixels.
	MaxDimension = 4000
	// MaxPixels is the most pixels a PNG, JPEG or GIF may have. Together with MaxDimension it bounds the memory a webview
	// needs to show the picture without anyone decoding the pixels here.
	MaxPixels = 12_000_000
	// MaxAltRunes is the longest alternative text.
	MaxAltRunes = 500
	// MaxFragmentBytes is the most HTML one body or the general instructions may hold; pictures count at a third more than
	// their files. The server enforces it as maxFragmentBytes, which is this constant; the compiler checks it too so the
	// author gets a message that names the line.
	MaxFragmentBytes = 256 << 10
)

// The kinds of picture an exam may carry.
const (
	PNG  = "image/png"
	JPEG = "image/jpeg"
	GIF  = "image/gif"
	SVG  = "image/svg+xml"
)

// Formats names the allowed pictures for messages.
const Formats = "PNG, JPEG, GIF or SVG"

// Validate decides what kind of picture data is from its content and returns its MIME type, or says what is wrong with it.
// It never decodes pixels. The file name plays no part here; see ExtensionMatches.
func Validate(data []byte) (string, error) {
	switch {
	case len(data) == 0:
		return "", fmt.Errorf("the file is empty")
	case len(data) > MaxImageBytes:
		return "", fmt.Errorf("the file is larger than %d KiB", MaxImageBytes>>10)
	}
	switch mime := sniff(data); mime {
	case PNG, JPEG, GIF:
		return mime, validateRaster(data, mime)
	case SVG:
		return mime, validateSVG(data)
	}
	return "", fmt.Errorf("the content is not a %s image", Formats)
}

// sniff tells the kind of picture by its content: the signature of a raster format, or XML text for an SVG.
func sniff(data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		return PNG
	case bytes.HasPrefix(data, []byte("\xff\xd8\xff")):
		return JPEG
	case bytes.HasPrefix(data, []byte("GIF87a")), bytes.HasPrefix(data, []byte("GIF89a")):
		return GIF
	}
	if bytes.HasPrefix(bytes.TrimLeft(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")), " \t\r\n"), []byte("<")) {
		return SVG
	}
	return ""
}

func validateRaster(data []byte, mime string) error {
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("the %s image is damaged: %v", strings.ToUpper(strings.TrimPrefix(mime, "image/")), err)
	}
	if "image/"+format != mime {
		return fmt.Errorf("the content is %s but its signature says %s", format, mime)
	}
	if config.Width < 1 || config.Height < 1 || config.Width > MaxDimension || config.Height > MaxDimension {
		return fmt.Errorf("the image is %d x %d pixels; each side must be between 1 and %d", config.Width, config.Height, MaxDimension)
	}
	if config.Width*config.Height > MaxPixels {
		return fmt.Errorf("the image is %d x %d pixels; it may have at most %d million pixels", config.Width, config.Height, MaxPixels/1_000_000)
	}
	return nil
}

// ExtensionMatches says whether the file name's extension is the usual one for the MIME type, ignoring case.
func ExtensionMatches(name, mime string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".png":
		return mime == PNG
	case ".jpg", ".jpeg":
		return mime == JPEG
	case ".gif":
		return mime == GIF
	case ".svg":
		return mime == SVG
	}
	return false
}

// CheckAlt reports what is wrong with an image's alternative text, which is the accessible description of the figure and what
// the student sees if it cannot be shown.
func CheckAlt(alt string) error {
	switch {
	case strings.TrimSpace(alt) == "":
		return fmt.Errorf("the image needs alternative text: write what it shows between the brackets, as in ![Lexer automaton](dfa.svg)")
	case utf8.RuneCountInString(alt) > MaxAltRunes:
		return fmt.Errorf("the alternative text is longer than %d characters", MaxAltRunes)
	}
	return nil
}

// DataURI is the data: URI that holds data.
func DataURI(mime string, data []byte) string {
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}

var altEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")

// HTML is the one form in which a picture appears in an exam's HTML: a fixed attribute order and nothing else, so the server
// can recognize it exactly. The caller has validated data and alt.
func HTML(mime string, data []byte, alt string) string {
	return `<img src="` + DataURI(mime, data) + `" alt="` + altEscaper.Replace(alt) + `">`
}
