package examdoc

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"exam-platform/shared/examimage"
)

func pngFile(t testing.TB, width, height int) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := png.Encode(&out, image.NewGray(image.Rect(0, 0, width, height))); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func jpegFile(t testing.TB) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := jpeg.Encode(&out, image.NewRGBA(image.Rect(0, 0, 8, 8)), nil); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func gifFile(t testing.TB) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := gif.Encode(&out, image.NewPaletted(image.Rect(0, 0, 4, 4), color.Palette{color.Black, color.White}), nil); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// pngHeader is a PNG that stops after its header and claims the given size, which is all the checks read.
func pngHeader(width, height uint32) []byte {
	var out bytes.Buffer
	out.WriteString("\x89PNG\r\n\x1a\n")
	header := make([]byte, 13)
	binary.BigEndian.PutUint32(header[0:], width)
	binary.BigEndian.PutUint32(header[4:], height)
	header[8], header[9] = 8, 6
	chunk := append([]byte("IHDR"), header...)
	_ = binary.Write(&out, binary.BigEndian, uint32(len(header)))
	out.Write(chunk)
	_ = binary.Write(&out, binary.BigEndian, crc32.ChecksumIEEE(chunk))
	return out.Bytes()
}

const svgFile = `<svg xmlns="http://www.w3.org/2000/svg" width="20" height="20"><circle cx="10" cy="10" r="5"/></svg>`

func compileWith(t *testing.T, files fstest.MapFS, body string) (Exam, error) {
	t.Helper()
	return CompileFS([]byte(header+body), files)
}

func problemsOf(t *testing.T, err error) string {
	t.Helper()
	var problems Problems
	if !errors.As(err, &problems) {
		t.Fatalf("err = %v, want problems", err)
	}
	return problems.Error()
}

func TestEveryFormatBecomesTheSameImgElement(t *testing.T) {
	files := fstest.MapFS{
		"a.png":      {Data: pngFile(t, 5, 5)},
		"b.JPG":      {Data: jpegFile(t)},
		"img/c.gif":  {Data: gifFile(t)},
		"img/d.svg":  {Data: []byte(svgFile)},
		"E.JPEG":     {Data: jpegFile(t)},
		"my fig.png": {Data: pngFile(t, 3, 3)},
	}
	for _, test := range []struct {
		markdown, file, mime, alt string
	}{
		{"![A square](a.png)", "a.png", examimage.PNG, "A square"},
		{"![Photo](b.JPG)", "b.JPG", examimage.JPEG, "Photo"},
		{"![Animated](img/c.gif)", "img/c.gif", examimage.GIF, "Animated"},
		{"![Automaton](img/d.svg)", "img/d.svg", examimage.SVG, "Automaton"},
		{"![Photo](E.JPEG)", "E.JPEG", examimage.JPEG, "Photo"},
		{"![Dot-slash](./a.png)", "a.png", examimage.PNG, "Dot-slash"},
		{"![Encoded](my%20fig.png)", "my fig.png", examimage.PNG, "Encoded"},
		{`![Pointy](<my fig.png>)`, "my fig.png", examimage.PNG, "Pointy"},
		{`![With title](a.png "ignored title")`, "a.png", examimage.PNG, "With title"},
		{`![Say "hi" & 1 &lt; 2 &gt; 0](a.png)`, "a.png", examimage.PNG, `Say "hi" & 1 < 2 > 0`},
		{"![A *very* `big` figure\nacross  lines](a.png)", "a.png", examimage.PNG, "A very big figure across lines"},
		{`![Escaped \* &copy; &amp;](a.png)`, "a.png", examimage.PNG, "Escaped * © &"},
	} {
		exam, err := compileWith(t, files, "## A\n\n"+test.markdown+"\n\n{{x}}\n")
		if err != nil {
			t.Errorf("%s: %v", test.markdown, err)
			continue
		}
		data := files[test.file].Data
		want := "<p>" + examimage.HTML(test.mime, data, test.alt) + "</p>\n<p><span class=\"slot\" data-slot=\"1.x\"></span></p>"
		if got := exam.Content.Exercises[0].Body; got != want {
			t.Errorf("%s:\n got %.200s\nwant %.200s", test.markdown, got, want)
		}
	}
}

func TestReferenceStyleImagesAndPlacesAnImageCanBe(t *testing.T) {
	files := fstest.MapFS{"a.png": {Data: pngFile(t, 5, 5)}, "b.svg": {Data: []byte(svgFile)}}
	exam, err := compileWith(t, files, "![General figure][fig]\n\n[fig]: a.png \"title\"\n\n"+
		"## One\n\nTwo here: ![first](a.png) and ![second](b.svg).\n\n"+
		"- a list item with ![in a list](a.png)\n\n> a quote with ![in a quote](a.png)\n\n"+
		"| Figure | Answer |\n| --- | --- |\n| ![in a cell](a.png) | {{cell}} |\n| ![with its own blank](b.svg) {{same}} | - |\n\n"+
		"### Part {points=1}\n\n![item figure](b.svg) {{part}}\n")
	if err != nil {
		t.Fatal(err)
	}
	content := exam.Content
	if got := strings.Count(content.Instructions, "<img "); got != 1 || !strings.Contains(content.Instructions, `alt="General figure"`) {
		t.Errorf("instructions = %.200s", content.Instructions)
	}
	if got := strings.Count(content.Exercises[0].Body, "<img "); got != 6 {
		t.Errorf("exercise body has %d images:\n%.600s", got, content.Exercises[0].Body)
	}
	if !strings.Contains(content.Exercises[0].Body, `alt="with its own blank"> <span class="slot" data-slot="1.same"></span></td>`) {
		t.Errorf("a picture and a blank in one cell:\n%s", content.Exercises[0].Body)
	}
	if !strings.Contains(content.Exercises[0].Body, `alt="in a cell"></td>`) || !strings.Contains(content.Exercises[0].Body, `<span class="slot" data-slot="1.cell"></span>`) {
		t.Errorf("the table cell lost its picture or its blank:\n%s", content.Exercises[0].Body)
	}
	if got := content.Exercises[0].Items[0].Body; strings.Count(got, "<img ") != 1 || !strings.Contains(got, `data-slot="1a.part"`) {
		t.Errorf("item body = %.200s", got)
	}
}

func TestTheSameFileUsedTwiceIsEmbeddedTwiceAndCountedTwice(t *testing.T) {
	data := pngFile(t, 5, 5)
	files := fstest.MapFS{"a.png": {Data: data}}
	exam, err := compileWith(t, files, "## A\n\n![one](a.png) ![two](a.png)\n\n{{x}}\n")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(exam.Content.Exercises[0].Body, examimage.DataURI(examimage.PNG, data)); got != 2 {
		t.Errorf("the file is embedded %d times, want 2", got)
	}
}

func TestPicturesAreCheckedAndReportedWithTheirLine(t *testing.T) {
	notPNG := append([]byte("\x00"), pngFile(t, 4, 4)[1:]...)
	files := fstest.MapFS{
		"ok.png":       {Data: pngFile(t, 5, 5)},
		"empty.png":    {Data: nil},
		"broken.png":   {Data: notPNG},
		"photo.jpg":    {Data: pngFile(t, 4, 4)},
		"page.svg":     {Data: []byte("<html><body>hello</body></html>")},
		"wide.png":     {Data: pngHeader(4001, 10)},
		"huge.png":     {Data: pngHeader(3500, 3500)},
		"big.png":      {Data: append(pngFile(t, 4, 4), make([]byte, examimage.MaxImageBytes)...)},
		"script.svg":   {Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)},
		"onload.svg":   {Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg" onload="x()"/>`)},
		"external.svg": {Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink"><image xlink:href="https://example.com/a.png"/></svg>`)},
		"entity.svg":   {Data: []byte("<!DOCTYPE svg [<!ENTITY a \"b\">]><svg xmlns=\"http://www.w3.org/2000/svg\"/>")},
		"folder":       {Mode: 0o755 | 1<<31},
		"fifo.png":     {Mode: 1 << 25},
		"notes.txt":    {Data: []byte("text")},
		"a.webp":       {Data: []byte("RIFF....WEBP")},
	}
	for name, test := range map[string]struct{ markdown, want string }{
		"missing":           {"![x](nothere.png)", `line 7: image "nothere.png": the file does not exist next to the exam`},
		"empty alt":         {"![](ok.png)", `line 7: image "ok.png": the image needs alternative text`},
		"blank alt":         {"![   ](ok.png)", "needs alternative text"},
		"long alt":          {"![" + strings.Repeat("a", 501) + "](ok.png)", "longer than 500 characters"},
		"blank in alt":      {"![{{b}}](ok.png)", "answer fields cannot be in the alternative text"},
		"empty file":        {"![x](empty.png)", "the file is empty"},
		"bad signature":     {"![x](broken.png)", "not a PNG, JPEG, GIF or SVG image"},
		"png as jpg":        {"![x](photo.jpg)", "the file is png but its name says otherwise"},
		"html as svg":       {"![x](page.svg)", "root element must be <svg"},
		"text file":         {"![x](notes.txt)", "not a PNG, JPEG, GIF or SVG image"},
		"webp":              {"![x](a.webp)", "not a PNG, JPEG, GIF or SVG image"},
		"too wide":          {"![x](wide.png)", "each side must be between 1 and 4000"},
		"too many pixels":   {"![x](huge.png)", "at most 12 million pixels"},
		"too big":           {"![x](big.png)", "larger than 128 KiB"},
		"script":            {"![x](script.svg)", "<script> element"},
		"onload":            {"![x](onload.svg)", "event handler (onload)"},
		"external href":     {"![x](external.svg)", "outside itself"},
		"entity":            {"![x](entity.svg)", "entities"},
		"folder":            {"![x](folder)", "that is a folder"},
		"pipe":              {"![x](fifo.png)", "not a regular file"},
		"http":              {"![x](http://example.com/a.png)", "web addresses, data: and file: URLs are not allowed"},
		"https upper":       {"![x](HTTPS://example.com/a.png)", "web addresses"},
		"data uri":          {"![x](data:image/png;base64,AAAA)", "web addresses, data: and file: URLs"},
		"file uri":          {"![x](file:///etc/passwd)", "web addresses"},
		"encoded scheme":    {"![x](http%3A//example.com/a.png)", "web addresses"},
		"protocol relative": {"![x](//example.com/a.png)", "absolute paths are not allowed"},
		"absolute":          {"![x](/etc/passwd)", "absolute paths are not allowed"},
		"windows absolute":  {`![x](<C:\Users\me\a.png>)`, "absolute paths are not allowed"},
		"parent":            {"![x](../a.png)", `".." is not allowed`},
		"parent inside":     {"![x](img/../../a.png)", `".." is not allowed`},
		"encoded parent":    {"![x](%2e%2e/a.png)", `".." is not allowed`},
		"backslash":         {`![x](<img\a.png>)`, "with / instead of"},
		"encoded backslash": {`![x](img%5Ca.png)`, "with / instead of"},
		"bad percent":       {"![x](a%zz.png)", "% that is not followed by two hexadecimal digits"},
		"control":           {"![x](a%00.png)", "control characters"},
		"no name":           {"![x]()", "the image has no file name"},
		"dot":               {"![x](.)", "the file name is not valid"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := compileWith(t, files, "## A\n\n"+test.markdown+" {{x}}\n")
			if got := problemsOf(t, err); !strings.Contains(got, test.want) {
				t.Fatalf("problems:\n%s\nwant %q", got, test.want)
			}
		})
	}
}

func TestPicturesCannotBeInHeadings(t *testing.T) {
	files := fstest.MapFS{"a.png": {Data: pngFile(t, 5, 5)}}
	for name, test := range map[string]struct{ source, line string }{
		"exercise": {"## Figure ![d](a.png)\n{{x}}\n", "line 5: "},
		"item":     {"## A\n### Figure ![d](a.png)\n{{x}}\n", "line 6: "},
	} {
		_, err := compileWith(t, files, test.source)
		if got := problemsOf(t, err); !strings.Contains(got, "images cannot be in headings") || !strings.Contains(got, test.line) {
			t.Errorf("%s: problems:\n%s", name, got)
		}
	}
}

func TestPictureTotalsAreLimited(t *testing.T) {
	// Eight copies of a 100 KiB picture are 800 KiB in total, but each of them is allowed on its own.
	filler := bytes.Repeat([]byte("<!-- padding padding padding padding padding padding -->\n"), 100<<10/57)
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg">` + string(filler) + `</svg>`)
	if len(svg) > examimage.MaxImageBytes || len(svg) < 90<<10 {
		t.Fatalf("test picture is %d bytes", len(svg))
	}
	files := fstest.MapFS{"big.svg": {Data: svg}}
	var body strings.Builder
	for i := range 8 {
		body.WriteString("## E" + string(rune('a'+i)) + "\n\n![figure](big.svg)\n\n{{x}}\n\n")
	}
	_, err := compileWith(t, files, body.String())
	got := problemsOf(t, err)
	if !strings.Contains(got, "add up to more than 512 KiB") || strings.Count(got, "add up to") != 1 {
		t.Errorf("problems:\n%s", got)
	}
	// Five copies fit.
	body.Reset()
	for i := range 5 {
		body.WriteString("## E" + string(rune('a'+i)) + "\n\n![figure](big.svg)\n\n{{x}}\n\n")
	}
	if _, err := compileWith(t, files, body.String()); err != nil {
		t.Errorf("five pictures of %d bytes: %v", len(svg), err)
	}
}

func TestOnePartCannotHoldMoreThanTheFragmentLimit(t *testing.T) {
	filler := bytes.Repeat([]byte("<!-- padding padding padding padding padding padding -->\n"), 120<<10/57)
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg">` + string(filler) + `</svg>`)
	files := fstest.MapFS{"big.svg": {Data: svg}}
	// Two pictures of 120 KiB are 320 KiB of base64 in one body, over the 256 KiB a part may hold.
	_, err := compileWith(t, files, "## A\n\n![one](big.svg)\n\n![two](big.svg)\n\n{{x}}\n")
	if got := problemsOf(t, err); !strings.Contains(got, `line 5: the text and pictures of "A" take`) {
		t.Errorf("problems:\n%s", got)
	}
	// Spread over two items they fit.
	if _, err := compileWith(t, files, "## A\n### a\n![one](big.svg) {{x}}\n### b\n![two](big.svg) {{y}}\n"); err != nil {
		t.Error(err)
	}
}

func TestCompileWithoutFilesExplainsWhatToDo(t *testing.T) {
	_, err := Compile([]byte(header + "## A\n\n![fig](a.png)\n\n{{x}}\n"))
	if got := problemsOf(t, err); !strings.Contains(got, `line 7: image "a.png": this exam has pictures, so it must be compiled from its file`) {
		t.Errorf("problems:\n%s", got)
	}
	// An exam without pictures does not need any.
	if _, err := CompileFS([]byte(header+"## A\n{{x}}\n"), nil); err != nil {
		t.Error(err)
	}
}

func TestCompileDirReadsNextToTheExamAndStaysInside(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	write := func(path string, data []byte) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(dir, "figures", "a.png"), pngFile(t, 5, 5))
	write(filepath.Join(outside, "secret.png"), pngFile(t, 5, 5))
	exam, err := CompileDir([]byte(header+"## A\n\n![fig](figures/a.png) {{x}}\n"), dir)
	if err != nil || strings.Count(exam.Content.Exercises[0].Body, "<img ") != 1 {
		t.Fatalf("CompileDir = %+v, %v", exam.Content, err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.png"), filepath.Join(dir, "link.png")); err != nil {
		t.Skipf("symbolic links are not available: %v", err)
	}
	if err := os.Symlink("figures/a.png", filepath.Join(dir, "inside.png")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "linked")); err != nil {
		t.Fatal(err)
	}
	if _, err := CompileDir([]byte(header+"## A\n\n![fig](inside.png) {{x}}\n"), dir); err != nil {
		t.Errorf("a link that stays inside the folder must work: %v", err)
	}
	for _, name := range []string{"link.png", "linked/secret.png"} {
		_, err := CompileDir([]byte(header+"## A\n\n![fig]("+name+") {{x}}\n"), dir)
		if got := problemsOf(t, err); !strings.Contains(got, `image "`+name+`": the file cannot be read`) {
			t.Errorf("%s: problems:\n%s", name, got)
		}
	}
	if _, err := CompileDir([]byte(header+"## A\n\n![fig](a.png) {{x}}\n"), filepath.Join(dir, "nowhere")); err == nil {
		t.Error("a missing folder must be reported")
	}
}

func TestPicturesDoNotChangeFill(t *testing.T) {
	files := fstest.MapFS{"a.png": {Data: pngFile(t, 5, 5)}}
	with := header + "## A\n\n![fig](a.png) Name: {{name}}\n\n```answer {#code lang=c}\nint x;\n```\n\n| f | v |\n|---|---|\n| ![cell](a.png) | {{cell}} |\n| ![same](a.png) {{same}} | - |\n"
	without := strings.ReplaceAll(with, "![fig](a.png)", "FIG")
	without = strings.ReplaceAll(without, "![cell](a.png)", "CELL")
	without = strings.ReplaceAll(without, "![same](a.png)", "SAME")
	answers := map[string]string{"1.name": "Ada", "1.code": "int y;", "1.cell": "a|b", "1.same": "v|w"}
	exam, err := CompileFS([]byte(with), files)
	if err != nil {
		t.Fatal(err)
	}
	// No files are available to Fill, as on the proctor machine long after the import.
	filled, err := FillChecked([]byte(with), answers, exam.Content.SlotIDs(), "---\nstudent: x\n---")
	if err != nil {
		t.Fatal(err)
	}
	reference, err := FillChecked([]byte(without), answers, exam.Content.SlotIDs(), "---\nstudent: x\n---")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(string(reference), "FIG", "![fig](a.png)"), "CELL", "![cell](a.png)"), "SAME", "![same](a.png)")
	if string(filled) != want {
		t.Errorf("filled:\n%s\nwant:\n%s", filled, want)
	}
	for _, line := range []string{"![fig](a.png) Name: `Ada`", "| ![cell](a.png) | `a\\|b` |", "| ![same](a.png) `v\\|w` | - |"} {
		if !strings.Contains(string(filled), line) {
			t.Errorf("filled lacks %q:\n%s", line, filled)
		}
	}
	// A picture the stored exam never knew does not change which slots exist.
	if _, err := FillChecked([]byte(with), answers, []string{"1.name", "1.code"}, ""); err == nil {
		t.Error("FillChecked must still compare slot IDs")
	}
	// Fill does not complain about a picture it cannot find, nor about one that would not pass the compiler's checks.
	if _, err := Fill([]byte(header+"## A\n\n![](http://example.com/x.png) ![x](../../etc/passwd) {{x}}\n"), nil, ""); err != nil {
		t.Errorf("Fill must ignore pictures: %v", err)
	}
}
