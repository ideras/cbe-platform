package service_test

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"reflect"
	"strings"
	"testing"

	"exam-platform/internal/domain"
	"exam-platform/internal/service"
	"exam-platform/shared/examimage"
)

func squarePNG(t *testing.T) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := png.Encode(&out, image.NewGray(image.Rect(0, 0, 6, 6))); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// paddedSVG is a valid SVG of about size bytes.
func paddedSVG(size int) []byte {
	padding := strings.Repeat("<!-- padding -->\n", size/17)
	return []byte(`<svg xmlns="http://www.w3.org/2000/svg">` + padding + `</svg>`)
}

// withPicture puts an <img> in the exercise body, next to its answer slot.
func withPicture(input *service.CreateExamInput, img string) {
	input.Content.Exercises[0].Body = "<p>" + img + "</p>" + input.Content.Exercises[0].Body
}

// The server does not trust the compiler: every <img> must be exactly what the compiler writes, and the picture must pass the
// checks the compiler applies.
func TestExamPicturesAreCheckedByTheServer(t *testing.T) {
	f := newFixture(t, 0)
	exams := service.NewExams(f.store, f.clock, f.sessions)
	valid := func() service.CreateExamInput {
		return service.CreateExamInput{Title: "Pictures", PIN: "pin", DurationSeconds: 60, Content: examContent([]string{"q1"})}
	}
	good := squarePNG(t)
	badSignature := append([]byte{0}, good[1:]...)
	good2 := examimage.HTML(examimage.PNG, good, "A grey square")
	for name, test := range map[string]struct {
		mutate func(*service.CreateExamInput)
		want   string // empty when the exam is accepted
	}{
		"a picture in a body":           {func(i *service.CreateExamInput) { withPicture(i, good2) }, ""},
		"a picture in the instructions": {func(i *service.CreateExamInput) { i.Content.Instructions = good2 }, ""},
		"a picture in an item": {func(i *service.CreateExamInput) {
			i.Content.Exercises[0].Items = []domain.Item{{ID: "1a", Title: "a", Body: good2 + `<div data-slot="a"></div>`, Slots: []domain.Slot{{ID: "a", Kind: domain.TextSlot}}}}
		}, ""},
		"an svg": {func(i *service.CreateExamInput) {
			withPicture(i, examimage.HTML(examimage.SVG, []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), "Empty drawing"))
		}, ""},
		"remote src":     {func(i *service.CreateExamInput) { withPicture(i, `<img src="https://example.com/a.png" alt="x">`) }, "exercise 1 (1): it has an <img> that is not an embedded"},
		"http src":       {func(i *service.CreateExamInput) { withPicture(i, `<img src="http://example.com/a.png" alt="x">`) }, "not an embedded"},
		"javascript src": {func(i *service.CreateExamInput) { withPicture(i, `<img src="javascript:alert(1)" alt="x">`) }, "not an embedded"},
		"image in the instructions with a remote src": {func(i *service.CreateExamInput) {
			i.Content.Instructions = `<img src="//example.com/a.png" alt="x">`
		}, "instructions: it has an <img> that is not an embedded"},
		"image in an item with a remote src": {func(i *service.CreateExamInput) {
			i.Content.Exercises[0].Items = []domain.Item{{ID: "1a", Title: "a", Body: `<img src="x.png" alt="x"><div data-slot="a"></div>`, Slots: []domain.Slot{{ID: "a", Kind: domain.TextSlot}}}}
		}, "exercise 1 (1) item 1 (1a): it has an <img> that is not an embedded"},
		"extra attribute": {func(i *service.CreateExamInput) {
			withPicture(i, strings.Replace(good2, ` alt=`, ` onerror="alert(1)" alt=`, 1))
		}, "not an embedded"},
		"an attribute after alt": {func(i *service.CreateExamInput) { withPicture(i, strings.Replace(good2, `">`, `" style="x">`, 1)) }, "not an embedded"},
		"upper case tag":         {func(i *service.CreateExamInput) { withPicture(i, strings.Replace(good2, "<img", "<IMG", 1)) }, "not an embedded"},
		"one good and one bad": {func(i *service.CreateExamInput) {
			withPicture(i, good2+`<img src="data:image/png;base64,AAAA" alt="x" width="9999999">`)
		}, "not an embedded"},
		"oversize picture": {func(i *service.CreateExamInput) {
			withPicture(i, examimage.HTML(examimage.PNG, append(append([]byte(nil), good...), make([]byte, examimage.MaxImageBytes)...), "big"))
		}, "larger than 128 KiB"},
		"png with a bad signature":   {func(i *service.CreateExamInput) { withPicture(i, examimage.HTML(examimage.PNG, badSignature, "x")) }, "not a PNG, JPEG, GIF or SVG image"},
		"declared as the wrong type": {func(i *service.CreateExamInput) { withPicture(i, examimage.HTML(examimage.JPEG, good, "x")) }, "declared as image/jpeg but its content is image/png"},
		"svg with a script": {func(i *service.CreateExamInput) {
			withPicture(i, examimage.HTML(examimage.SVG, []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`), "x"))
		}, "<script> element"},
		"svg with onload": {func(i *service.CreateExamInput) {
			withPicture(i, examimage.HTML(examimage.SVG, []byte(`<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"/>`), "x"))
		}, "event handler (onload)"},
		"svg with an external reference": {func(i *service.CreateExamInput) {
			withPicture(i, examimage.HTML(examimage.SVG, []byte(`<svg xmlns="http://www.w3.org/2000/svg"><use href="https://example.com/a.svg#b"/></svg>`), "x"))
		}, "outside itself"},
		"missing alternative text": {func(i *service.CreateExamInput) { withPicture(i, examimage.HTML(examimage.PNG, good, "")) }, "needs alternative text"},
		"too many pictures in total": {func(i *service.CreateExamInput) {
			svg := examimage.HTML(examimage.SVG, paddedSVG(110<<10), "padding")
			i.Content = examContent([]string{"a"}, []string{"b"}, []string{"c"}, []string{"d"}, []string{"e"})
			for k := range i.Content.Exercises {
				i.Content.Exercises[k].Body = svg + i.Content.Exercises[k].Body
			}
		}, "pictures of the exam add up to more than 512 KiB"},
	} {
		t.Run(name, func(t *testing.T) {
			input := valid()
			test.mutate(&input)
			id, err := exams.Create(t.Context(), testClassID, input)
			if test.want == "" {
				if err != nil {
					t.Fatalf("a valid exam was refused: %v", err)
				}
				stored, err := f.store.Exam(t.Context(), id)
				if err != nil || !reflect.DeepEqual(stored.Content, input.Content) {
					t.Fatalf("the stored content changed: %v", err)
				}
				return
			}
			var invalid *service.ValidationError
			if !errors.Is(err, service.ErrInvalidInput) || !errors.As(err, &invalid) || !strings.Contains(strings.Join(invalid.Problems, "\n"), test.want) {
				t.Fatalf("err = %v; want a problem with %q", err, test.want)
			}
		})
	}
	if got := f.count(t, `SELECT count(*) FROM exams WHERE title = 'Pictures'`); got != 4 {
		t.Fatalf("%d exams with pictures were stored, want the 4 valid ones", got)
	}
}

// The server's fragment cap is the one the compiler checks, so the author hears about an oversized part from the compiler.
func TestTheCompilersFragmentLimitIsTheServers(t *testing.T) {
	f := newFixture(t, 0)
	exams := service.NewExams(f.store, f.clock, f.sessions)
	input := service.CreateExamInput{Title: "Big", PIN: "pin", DurationSeconds: 60, Content: examContent([]string{"q1"})}
	const slot = `<div class="slot" data-slot="q1"></div>`
	input.Content.Exercises[0].Body = strings.Repeat("x", examimage.MaxFragmentBytes-len(slot)+1) + slot
	if _, err := exams.Create(t.Context(), testClassID, input); err == nil {
		t.Fatal("a body over the limit was accepted")
	}
	input.Content.Exercises[0].Body = strings.Repeat("x", examimage.MaxFragmentBytes-len(slot)) + slot
	if _, err := exams.Create(t.Context(), testClassID, input); err != nil {
		t.Fatalf("a body of exactly %d bytes was refused: %v", examimage.MaxFragmentBytes, err)
	}
}
