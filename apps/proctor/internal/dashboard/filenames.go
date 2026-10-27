package dashboard

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// File and folder names for downloaded submissions. They are built from names people typed in a roster, so the rules are
// fixed and predictable: accents are dropped, runs of whitespace become _, and everything that is not an ASCII letter, a
// digit, _ - or . is removed. Names are safe on Windows, macOS and Linux, where file names also ignore case.

// maxFileNameBytes bounds a student's file name, without its .md extension.
const maxFileNameBytes = 100

// nameReplacements are letters that have no decomposition to an ASCII letter.
var nameReplacements = map[rune]string{
	'ß': "ss", 'ẞ': "SS",
	'æ': "ae", 'Æ': "AE",
	'ø': "o", 'Ø': "O",
	'đ': "d", 'Đ': "D",
	'ł': "l", 'Ł': "L",
	'œ': "oe", 'Œ': "OE",
}

// downloadStudent is one person of the class roster, or one who has a session but has since left it.
type downloadStudent struct {
	Account string
	Name    string
}

// normalizeFileName turns a student's name into a file name, as decided in docs/handoff-download-submissions.md. An empty
// result becomes the account number, so a student never ends up with a file called just ".md".
func normalizeFileName(name, account string) string {
	if normalized := normalizeName(name); normalized != "" {
		return normalized
	}
	if normalized := finishName(safeSuffix(account)); normalized != "" {
		return normalized
	}
	return "student"
}

// normalizeName applies the normalization rules to a name, returning "" when nothing is left.
func normalizeName(name string) string {
	var out strings.Builder
	space := false
	for _, r := range norm.NFD.String(name) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		if replacement, ok := nameReplacements[r]; ok {
			out.WriteString(replacement)
			continue
		}
		if unicode.IsSpace(r) {
			if !space {
				out.WriteByte('_')
				space = true
			}
			continue
		}
		space = false
		if r < unicode.MaxASCII && (asciiLetter(r) || asciiDigit(r) || r == '_' || r == '-' || r == '.') {
			out.WriteRune(r)
		}
	}
	return finishName(out.String())
}

// nameSortKey orders names the way a person reads a list: accents and case do not count, so "Álvarez" sits among the As.
func nameSortKey(name string) string {
	var out strings.Builder
	for _, r := range norm.NFD.String(name) {
		if !unicode.Is(unicode.Mn, r) {
			out.WriteRune(unicode.ToLower(r))
		}
	}
	return out.String()
}

// finishName collapses repeated _, trims _ - . from both ends and caps the length.
func finishName(value string) string {
	for strings.Contains(value, "__") {
		value = strings.ReplaceAll(value, "__", "_")
	}
	value = strings.Trim(value, "_-.")
	if len(value) > maxFileNameBytes {
		value = value[:maxFileNameBytes]
	}
	return value
}

// safeSuffix keeps an account number usable inside a file name.
func safeSuffix(value string) string {
	var out strings.Builder
	for _, r := range value {
		if r < unicode.MaxASCII && (asciiLetter(r) || asciiDigit(r) || r == '_' || r == '-' || r == '.') {
			out.WriteRune(r)
		}
	}
	return out.String()
}

func asciiLetter(r rune) bool { return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' }
func asciiDigit(r rune) bool  { return r >= '0' && r <= '9' }

// studentFileNames gives every student the name of their Markdown file. Names that collide ignoring case (Windows and macOS
// file systems do) all get _<account_number>, not just the second one, so a name does not depend on the order of the list.
func studentFileNames(students []downloadStudent) map[string]string {
	accounts := make([]string, 0, len(students))
	base := map[string]string{}
	for _, student := range students {
		if _, seen := base[student.Account]; seen {
			continue
		}
		accounts = append(accounts, student.Account)
		base[student.Account] = normalizeFileName(student.Name, student.Account)
	}
	groups := map[string][]string{}
	for _, account := range accounts {
		key := strings.ToLower(base[account])
		groups[key] = append(groups[key], account)
	}
	names := make(map[string]string, len(accounts))
	for _, account := range accounts {
		name := base[account]
		if len(groups[strings.ToLower(name)]) > 1 {
			suffix := safeSuffix(account)
			if suffix == "" {
				suffix = "student"
			}
			name += "_" + suffix
		}
		names[account] = name + ".md"
	}
	return names
}

// downloadFolderName builds the folder of one download: the class and the exam, normalized like a file name.
func downloadFolderName(className, examTitle string) string {
	class := normalizeName(className)
	exam := normalizeName(examTitle)
	if class == "" {
		class = "class"
	}
	if exam == "" {
		exam = "exam"
	}
	return class + "-" + exam
}
