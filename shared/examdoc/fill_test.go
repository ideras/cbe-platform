package examdoc

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/yuin/goldmark"
)

const fillHeader = "---\ntitle: Quiz\nduration: 30m\n---"

// fillBody compiles an exam with one exercise (ID 1) and fills it with answers.
func fillExam(t *testing.T, body string, answers map[string]string) string {
	t.Helper()
	filled, err := Fill([]byte(fillHeader+"\n"+body), answers, "---\ntitle: Filled\n---")
	if err != nil {
		t.Fatalf("fill failed: %v", err)
	}
	return string(filled)
}

func TestFillReplacesBlanksEverywhere(t *testing.T) {
	body := strings.Join([]string{
		"## Exercise {points=10}",
		"",
		"Value {{a}} and {{b}}.",
		"",
		"- a list item with {{c}} in it",
		"",
		"| Header |",
		"|--------|",
		"| {{d}}  |",
		"",
		"```answer {#written}",
		"```",
		"",
	}, "\n")
	got := fillExam(t, body, map[string]string{
		"1.a": "16",
		"1.b": "",
		"1.c": "x|y",
		"1.d": "a|b",
	})
	for _, want := range []string{
		"Value `16` and *(no answer)*.",
		"- a list item with `x|y` in it",
		"| `a\\|b`  |",
		"```text title=\"1.written\"\n```\n*(no answer)*",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("filled file does not contain %q:\n%s", want, got)
		}
	}
}

func TestFillEscapesAnswersAsMarkdownAndHTML(t *testing.T) {
	body := "## Exercise {points=10}\n\n{{a}} {{b}} {{c}} {{d}}\n"
	got := fillExam(t, body, map[string]string{
		"1.a": "a*b_c",
		"1.b": "<script>alert(1)</script>",
		"1.c": "line one\nline two\tend",
		"1.d": "`code`",
	})
	for _, want := range []string{
		"`a*b_c`",
		"`<script>alert(1)</script>`",
		"`line one line two end`",
		"`` `code` ``",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("filled file does not contain %q:\n%s", want, got)
		}
	}
}

func TestFillLengthensTheFenceAroundBackticks(t *testing.T) {
	body := "## Exercise {points=10}\n\n{{a}} {{b}}\n"
	got := fillExam(t, body, map[string]string{"1.a": "``", "1.b": "````"})
	if !strings.Contains(got, "``` `` ```") || !strings.Contains(got, "````` ```` `````") {
		t.Fatalf("fences were not longer than the answers:\n%s", got)
	}
}

func TestFillKeepsBlanksWrittenBackToBackAsSeparateCodeSpans(t *testing.T) {
	body := "## Exercise {points=10}\n\n{{a}}{{b}}\n"
	got := fillExam(t, body, map[string]string{"1.a": "A|`x`", "1.b": "B"})
	if !strings.Contains(got, "`` A|`x` `` `B`") {
		t.Fatalf("the two spans were not kept apart:\n%s", got)
	}
	var rendered bytes.Buffer
	if err := goldmark.New().Convert([]byte(got), &rendered); err != nil {
		t.Fatal(err)
	}
	if strings.Count(rendered.String(), "<code>") != 2 {
		t.Fatalf("the answers do not render as two code spans:\n%s", rendered.String())
	}
}

func TestFillAnswerBlocks(t *testing.T) {
	body := strings.Join([]string{
		"## Exercise {points=10}",
		"",
		"```answer {#code lang=mips}",
		"main:",
		"```",
		"",
		"```answer {#fenced}",
		"```",
		"",
		"- an item",
		"",
		"  ```answer {#inside}",
		"  ```",
		"",
	}, "\n")
	got := fillExam(t, body, map[string]string{
		"1.code":   "mult $s0, $s1\nmflo $t0",
		"1.fenced": "a\n```\nb",
		"1.inside": "indented answer",
	})
	for _, want := range []string{
		"```mips title=\"1.code\"\nmult $s0, $s1\nmflo $t0\n```",
		"````text title=\"1.fenced\"\na\n```\nb\n````",
		"  ```text title=\"1.inside\"\n  indented answer\n  ```",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("filled file does not contain %q:\n%s", want, got)
		}
	}
}

func TestFillStarterTextUnchanged(t *testing.T) {
	body := "## Exercise {points=10}\n\n```answer {#code lang=mips}\nmain:\n```\n"
	got := fillExam(t, body, map[string]string{"1.code": "main:\n"})
	if !strings.Contains(got, "*(starter text unchanged)*\n```mips title=\"1.code\"\nmain:\n```") {
		t.Fatalf("starter text was not marked:\n%s", got)
	}
	// The same text without the newline is a different answer and is not marked.
	got = fillExam(t, body, map[string]string{"1.code": "main:"})
	if strings.Contains(got, "starter text unchanged") {
		t.Fatalf("an edited starter was marked unchanged:\n%s", got)
	}
}

func TestFillIgnoresUnknownAnswersAndRejectsMismatchedSlots(t *testing.T) {
	source := []byte(fillHeader + "\n## Exercise {points=10}\n\n{{a}}\n")
	filled, err := Fill(source, map[string]string{"9.nope": "x"}, "---\ntitle: F\n---")
	if err != nil || !strings.Contains(string(filled), "*(no answer)*") {
		t.Fatalf("an answer outside the exam was not ignored: %v\n%s", err, filled)
	}
	if _, err := FillChecked(source, nil, []string{"1.a", "1.b"}, "---\ntitle: F\n---"); err == nil {
		t.Fatal("a stored slot missing from the source was accepted")
	}
	if _, err := FillChecked(source, nil, []string{"1.other"}, "---\ntitle: F\n---"); err == nil {
		t.Fatal("a renamed slot was accepted")
	}
	if _, err := FillChecked(source, nil, []string{"1.a"}, "---\ntitle: F\n---"); err != nil {
		t.Fatalf("matching slots were refused: %v", err)
	}
}

func TestFillGoldenSample(t *testing.T) {
	source, err := os.ReadFile("../../testdata/exam.md")
	if err != nil {
		t.Fatal(err)
	}
	header := strings.TrimRight(`---
exam: "Examen I Parcial – Organización de Computadoras"
class: "CCC403 Sección 1"
student: "José Ñúñez Martínez"
account_number: "20231234"
status: "submitted by the student"
submitted_at: "2026-10-20 10:12:31 -06:00"
answer_version: 14
downloaded_at: "2026-10-21 08:00:05 -06:00"
---`, "\n")
	answers := map[string]string{
		"1a.alu": "16", "1a.mult": "5", "1a.branch": "5", "1a.jump": "1", "1a.total": "27",
		"1a.trace": strings.Join([]string{
			"i=0: $a1=0  $v0=0   $a0=1  $t0=1",
			"i=1: $a1=1  $v0=1   $a0=2  $t0=1",
			"i=2: $a1=4  $v0=5   $a0=3  $t0=1",
			"i=3: $a1=9  $v0=14  $a0=4  $t0=1",
			"i=4: $a1=16 $v0=30  $a0=5  $t0=0",
		}, "\n"),
		"1b.cycles": "44", "1b.cpi": "1.63", "1b.new_cpi": "1.44", "1b.speedup": "1.13",
		"1b.work": strings.Join([]string{
			"16 ALU + 5 mult + 5 branches + 1 jump = 27 instrucciones",
			"Ciclos: 16·1 + 5·2 + 5·2 + 1·2 = 44",
			"CPI = 44 / 27 ≈ 1.63",
		}, "\n"),
		"2a.code": "mult $s0, $s1\nmflo $t0\nadd $t0, $t0, $s2",
		"2b.code": "lw   $t0, 16($s0)\naddi $t0, $t0, 1\nsw   $t0, 8($s0)",
		"2c.code": "lbu  $t0, 0($s0)\nlbu  $t1, 1($s0)\nadd  $t2, $t0, $t1\nsb   $t2, 2($s0)",
		"3.code": strings.Join([]string{
			"sum_array:",
			"        add  $v0, $zero, $zero",
			"        add  $t0, $zero, $zero",
			"loop:   slt  $t1, $t0, $a1",
			"        beq  $t1, $zero, done",
			"        sll  $t2, $t0, 2",
			"        add  $t2, $a0, $t2",
			"        lw   $t3, 0($t2)",
			"        add  $v0, $v0, $t3",
			"        addi $t0, $t0, 1",
			"        j    loop",
			"done:   jr   $ra",
		}, "\n"),
		"4.code": strings.Join([]string{
			"min_max:",
			"        lw   $t0, 0($a0)",
			"        sw   $t0, 0($a2)",
			"        sw   $t0, 0($a3)",
			"        addi $t1, $zero, 1",
			"loop:   slt  $t2, $t1, $a1",
			"        beq  $t2, $zero, done",
			"        sll  $t2, $t1, 2",
			"        add  $t2, $a0, $t2",
			"        lw   $t3, 0($t2)",
			"        lw   $t4, 0($a2)",
			"        slt  $t5, $t3, $t4",
			"        beq  $t5, $zero, checkmax",
			"        sw   $t3, 0($a2)",
			"checkmax: lw $t4, 0($a3)",
			"        slt  $t5, $t4, $t3",
			"        beq  $t5, $zero, next",
			"        sw   $t3, 0($a3)",
			"next:   addi $t1, $t1, 1",
			"        j    loop",
			"done:   jr   $ra",
		}, "\n"),
		"5.code": strings.Join([]string{
			"multiply:",
			"        mult $a0, $a1",
			"        mflo $v0",
			"        jr   $ra",
			"",
			"scale_array:",
			"        add  $v0, $zero, $zero",
			"        add  $t0, $zero, $zero",
			"loop:   slt  $t1, $t0, $a1",
			"        beq  $t1, $zero, done",
			"        sll  $t2, $t0, 2",
			"        add  $t2, $a0, $t2",
			"        lw   $t3, 0($t2)",
			"        add  $a0, $t3, $zero",
			"        add  $a1, $a3, $zero",
			"        addi $sp, $sp, -4",
			"        sw   $ra, 0($sp)",
			"        jal  multiply",
			"        lw   $ra, 0($sp)",
			"        addi $sp, $sp, 4",
			"        sll  $t2, $t0, 2",
			"        add  $t2, $a0, $t2",
			"        sw   $v0, 0($t2)",
			"        add  $v0, $v0, $v0",
			"        addi $t0, $t0, 1",
			"        j    loop",
			"done:   sw   $v0, 0($a3)",
			"        jr   $ra",
		}, "\n"),
	}
	filled, err := Fill(source, answers, header)
	if err != nil {
		t.Fatal(err)
	}
	if *update {
		if err := os.WriteFile("../../testdata/exam.filled.md", filled, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	golden, err := os.ReadFile("../../testdata/exam.filled.md")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(golden, filled) {
		t.Fatal("testdata/exam.filled.md is not the filled testdata/exam.md; run go test ./... -update in shared/examdoc")
	}
	if !bytes.Contains(filled, []byte("| ALU / Escritura de registros    | `16`    |")) {
		t.Fatal("the golden file does not hold the answers in the table")
	}
}
