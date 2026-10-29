# Exam format

This document specifies how to write an exam for the platform as a single Markdown file. It is written for **AI agents that generate exams** and for **instructors who write or edit them by hand**; both follow the same rules.

- **Agents:** read [Rules at a glance](#rules-at-a-glance), the [authoring workflow](#authoring-workflow-for-agents) and the [delivery checklist](#delivery-checklist). Everything else is reference. Treat every "must" as enforced by the compiler: a file that breaks one cannot be imported.
- **Instructors:** start with the [minimal exam](#a-minimal-exam), then read [Answer fields](#answer-fields), the part that is specific to this platform.

The Markdown is only the authoring format. The proctor dashboard compiles the file when you choose it in **Import exam…** (`shared/examdoc`), turning it into the JSON document the server stores; the server never parses Markdown. [`testdata/exam.md`](../testdata/exam.md) is a complete real exam and [`testdata/exam.json`](../testdata/exam.json) is what it compiles to.

## Rules at a glance

1. The file is UTF-8 and **starts with a front matter block** between two `---` lines. `title` and `duration` are required.
2. **`##` starts an exercise** (one page for the student), **`###` starts an item** of that exercise (1.a, 1.b…). `#` is forbidden. Headings must begin at the start of a line.
3. Text before the first `##` is the **general instructions**. It cannot contain answer fields.
4. **Every exercise or item must contain at least one answer field**, otherwise the file is rejected. Answer fields are `{{name}}` (one-line blank) or a fenced block with the language `answer` (text or code box).
5. Points go in **heading attributes** (`## Exercise 1 {points=20}`), never in the title text. Sums must match at every level.
6. **No links, no images, no raw HTML.** Tables, lists, quotes, code blocks, bold, italics, inline code and strikethrough are fine.
7. **No secrets in the file:** no PIN, no solutions, no grading rubric. The PIN is typed by the proctor on import; solutions live in separate files.
8. Names and IDs use only letters, digits, `_` and `-` (at most 64 characters) and must be unique where stated below.
9. Validate with `build/examc exam.md` before delivering. Deliver only a file that passes.

## A minimal exam

```markdown
---
title: Quiz 1 – Number Systems
duration: 20m
---

## Conversions

Convert 255 to hexadecimal: {{hex}}

Convert 1011₂ to decimal: {{dec}}
```

This produces one exercise with two one-line answer fields (`1.hex` and `1.dec`). Everything below adds structure to this.

## Shape of a file

````markdown
---
title: Examen I Parcial – Organización de Computadoras
duration: 80m
grace: 2m
navigation: forward_only
---

### Instrucciones Generales

Everything before the first `##` heading is the general instructions.

## Ejercicio 1 — Análisis de Rendimiento {points=10}

Context shared by every item of the exercise: shown once, above them.

### a. Conteo de Instrucciones {points=10}

| Categoría | Cantidad |
|-----------|:--------:|
| ALU       | {{alu}}  |

```answer {#trace lines=10}
```

## Ejercicio 2 — Traducción {points=15}

```answer {#code lang=mips}
main:
```
````

- **Front matter** (required) holds the exam's settings, one `key: value` per line. Values may be quoted with `"` or `'` (optional: `title: Final: part 2` also works, only the first `:` separates key from value), and a line starting with `#` is a comment.
- **`##` starts an exercise.** Students see one exercise per page.
- **`###` starts an item** of the current exercise, such as 1.a. Items are optional: an exercise may hold its answer fields directly.
- Text between an exercise heading and its first item is the exercise's **shared context**, shown once above all items.
- A `###` heading *before* the first `##` is part of the general instructions (the sample exam uses this for "Instrucciones Generales").
- `#` headings are not allowed (the title is in the front matter). `####` and smaller are ordinary text headings with no structural meaning.
- The language of the exam is free (the sample is in Spanish). Everything the student reads is whatever you write; the platform adds no text of its own to the exam body.

## Front matter

| Key | Required | Meaning |
|---|---|---|
| `title` | yes | The exam title, at most 512 bytes. |
| `duration` | yes | Time per student, counted from their first login, as a Go duration in whole seconds: `80m`, `1h20m`, `90s`. A bare number like `90` is rejected. |
| `grace` | no | Extra time after the deadline in which students can still save and submit, such as `2m` or `45s`. When omitted, the proctor's Settings (*Default grace period*, 30 s unless changed) apply. |
| `min_volume` | no | The lowest system volume (0–100, a trailing `%` is accepted) the student application keeps during the exam; `0` leaves the student's volume alone. Default 80. |
| `opens_at`, `closes_at` | no | The window in which students may **start** the exam, as RFC 3339 with an offset: `2026-10-20T08:00:00-06:00`. `opens_at` must be earlier than `closes_at`. A student who has already started can resume. |
| `navigation` | no | `free` (default) or `forward_only`. See [Navigation](#navigation). |
| `calculator` | no | The calculator students may open. See [The calculator](#the-calculator). |
| `points` | no | The exam's total. When set and every exercise has points, the exercises must add up to it. |

Each key may appear once. An unknown key is an error, and so is `pin:` (see rule 7) and `focus_threshold:` (that is a proctor Setting now).

## Headings, IDs and points

The heading text is the title students see, exactly as written: write `## Ejercicio 1 — …` or `### a. …` yourself, the platform does not number anything on screen. Optional attributes go at the **end** of the heading, in braces, separated by spaces:

```markdown
## Ejercicio 3 — Funciones {#funciones points=25}
### a. Suma de arreglo {points=10}
```

| Attribute | Meaning |
|---|---|
| `points=N` | Positive number up to 10000 (decimals such as `2.5` are fine). Shown next to the title. |
| `#id` | Sets the ID. Letters, digits, `_`, `-`; unique across all exercises and items. |

**IDs.** By default exercises are numbered `1`, `2`, … and items take the exercise ID plus a letter: `1a`, `1b`, … (after 26 items, numbers). An exercise with `{#funciones}` gives its items `funcionesa`, `funcionesb`, …, unless they set their own `#id`. Set `#id` explicitly when the exercise order may change, because answers are stored and graded under IDs.

**Points.** Write points only as attributes, not as `(20 pts)` in the title, or they show twice. Consistency is checked:

- If an exercise has points and **every** item has points, the items must add up to the exercise.
- If the front matter has `points` and **every** exercise has points, the exercises must add up to it.
- If any exercise (or item) lacks points, that level is not checked.

Points inside the text (such as `**(3 pts)**` in a list item of a multi-part question) are just text and are not checked, so keep them consistent by hand.

## Answer fields

Each answer field gets an ID made of its exercise or item ID and its name, such as `1a.alu` or `3.code`. Answers are stored, exported and graded under that ID. `build/examc exam.md` lists them all, which helps when writing grading files (keep solutions in **separate files keyed by these IDs**, never in the exam file).

### Which kind to use

| The student must give… | Use | Example |
|---|---|---|
| A short value: a number, a word, a table cell | Blank `{{name}}` | `Result: {{r}}` |
| A procedure, explanation or free text | Text answer ```` ```answer ```` | `{#trace lines=10}` |
| Source code or assembly | Code answer ```` ```answer {lang=…} ```` | `{#code lang=mips lines=20}` |

### Blanks

`{{name}}` anywhere in running text (a table cell, a list item, a sentence) becomes a one-line input there. Put several in one sentence when a question has several answers: `Nuevo CPI: {{new_cpi}} — Speedup: {{speedup}}`.

- The name uses letters, digits, `_`, `-`.
- It must close with `}}` **on the same line**.
- The same name may not be used twice in the same exercise or item. The same name in different items is fine (`1a.total` and `1b.total`).
- Inside inline code (`` `{{x}}` ``) and ordinary code blocks, `{{x}}` is literal text, not a field.
- Not allowed in headings or in the general instructions.

### Text and code answers

Text and code answers are fenced blocks whose language is `answer`:

````markdown
```answer {#trace lines=10}
```

```answer {#code lang=mips lines=30}
multiply:

scale_array:
```
````

| Option | Meaning |
|---|---|
| `#name` | The field's name. Default `answer`; give each field in the same exercise or item its own name. |
| `lang=mips` | Makes it a code answer edited in the code editor; the language is shown as a label. Short lowercase name: `c`, `mips`, `python`, `"c++"`, `"c#"` (the last two quoted). |
| `kind=text` / `kind=code` | Sets the kind explicitly. Without `lang` or `kind` the field is a free-text box. `lang` with `kind=text` is an error. |
| `lines=N` | Visible height, a whole number 1–100. Text boxes default to 6 lines. |

The options go in braces after `answer`, separated by spaces: `{#code lang=c lines=20}` (`{id=code}` is an equivalent spelling of `#code`). Any other option is an error. Quote values that contain symbols: `lang="c++"`, `lang="c#"`.

What the block holds is the **starter text** the student begins with (for example a function label or a skeleton). An empty block starts empty. Answer blocks may also sit inside a list item, indented like any other block in it.

**Leave room for work that earns points.** If the grading rubric gives points for a trace or a procedure, add a text answer for it; on screen there is no margin to write in.

### Presenting code the student must read

A normal fenced block (```` ```c ````, ```` ```mips ````, or no language) shows read-only code. Never use the language `answer` for it.

## Writing the content

Supported Markdown (CommonMark plus GFM tables and strikethrough): paragraphs, `**bold**`, `*italics*`, `~~strikethrough~~`, `` `inline code` ``, lists (bulleted and numbered), block quotes, tables, horizontal rules and fenced code blocks.

**Not allowed** (each is reported with its line):

- **Links** (`[text](url)`, `<http://…>`): following one would leave the exam window. Plain URLs stay plain text.
- **Images**: not supported yet. Describe a figure in text, a table or a code block (ASCII diagrams in a code block work well).
- **Raw HTML**: use Markdown.
- **`#` headings**, and `##`/`###` headings inside lists or quotes.

Practical advice:

- **No math typesetting.** LaTeX is not rendered. Write formulas with Unicode and inline code: `` `CPI = ciclos / instrucciones` ``, `x²`, `√2`, `π`.
- **Use a code block for anything the student should read as code, a trace or a layout** (alignment is preserved).
- **Tables** are good for "fill the cells" questions: put a `{{name}}` in each cell to fill.
- **State what the student should enter in the field**, and in what format (units, decimals, base), because blanks are compared by humans or scripts against a solution file.
- **Keep one idea per item** so each answer field maps to a clear grading criterion.
- **Dollar signs and braces** are ordinary characters (`$s0`, `{ ... }`), except `{{` which opens a blank. To show `{{` literally, put it in inline code.

## The calculator

The `calculator` key turns the calculator on for that exam and carries its settings as `setting=value` pairs, where collections sit in brackets:

```markdown
calculator: mode=[scientific, neural], angle=rad, disabled=[softmax]
```

The key's presence turns the calculator on; without it, students have no calculator. The bare key, `calculator:` alone, enables it with the defaults.

| Setting | Meaning |
|---|---|
| `mode` | The enabled modes, as a bracketed list or a single value. The modes are `scientific`, `programmer` and `neural`. `neural` adds vectors, matrices and the neural network functions to the scientific panel rather than a panel of its own. Default: `scientific` alone. |
| `angle` | How the scientific calculator reads trigonometric input: `deg`, `rad`, or `user` (the student toggles; the default). |
| `disabled` | Names to switch off even in an enabled mode, as a bracketed list or a single value (see below). |

Names accepted by `disabled`:

- **scientific:** `pi`, `e`, `log10`, `ln`, `log`, `sqrt`, `cbrt`, `exp`, `abs`, `floor`, `ceil`, `round`, `mod`, `sin`, `cos`, `tan`, `asin`, `acos`, `atan`, `atan2`, `sinh`, `cosh`, `tanh`, `factorial` (the postfix `!`)
- **programmer:** `nand`, `nor`, `rol`, `ror`, `ieee754` (the IEEE 754 inspector panel)
- **neural:** `tanh`, `sigmoid`, `relu`, `leaky_relu`, `dsigmoid`, `dtanh`, `drelu`, `dleaky_relu`, `softmax`, `sum`, `mean`, `max`, `min`, `argmax`, `len`, `norm`, `transpose`, `dot`, `matmul`, `mse`, `bce`

Operators cannot be disabled. Naming a function of a mode that is not enabled is accepted and has no effect. Every problem is reported with the key's line: an unknown mode, an explicitly empty list (`mode=[]`), a name that is not in the registry, an unknown setting, an unclosed bracket, or a setting given twice. There is no way to turn calculator logging off; every expression a student evaluates is recorded for the proctor's review.

Disable a function when the exam asks the student to compute it by hand (for example `disabled=[sqrt]` for an exam on Newton's method).

## Navigation

Under **free** navigation students move between exercises at will. Under **forward only**, a student who continues to the next exercise cannot return to the previous one; the student application asks for confirmation first, and the **server** enforces it. The front matter's `navigation:` is the author's intent, but the proctor makes the final choice in the import dialog, where it cannot be changed after the exam is published.

Under forward-only navigation, put everything an exercise needs inside that exercise, because students cannot look back at earlier text. The general instructions stay available throughout.

## Authoring workflow for agents

1. **Collect the inputs** you cannot invent: subject, language, duration, number of exercises and their topics, point distribution (and total), whether a calculator is allowed and which modes, and whether navigation should be forward-only. Ask when any is missing; use the defaults in this document only for what the instructor leaves open.
2. **Plan the answer fields first.** For each question decide what exactly the student types, and so which kind of field it is. A question without a field cannot exist: the compiler rejects it.
3. **Write the file** following this specification. Number the points so every level adds up. Prefer explicit `#id`s on exercises when the instructor may reorder them.
4. **Validate**: run `build/examc exam.md` (build it with `make examc`). Fix every reported problem; errors are listed with their line numbers, up to 50 at a time, so re-run until it is clean. Use `build/examc -json exam.md` to check that points and slots are what you intended.
5. **Deliver** the exam file, and separately (if the instructor wants one) a solution/rubric file keyed by the answer field IDs that `examc` prints. Never put solutions, a PIN or grading notes in the exam file: the exam is stored with its Markdown, and filled copies of it are downloaded and shared.
6. **Report** what you did not decide for the instructor: the PIN to be entered at import, the navigation mode if the proctor must choose, and anything you assumed.

### Common errors and fixes

| Message contains | Fix |
|---|---|
| `must start with a front matter block` | Put `---` on line 1, the settings, then a closing `---`. |
| `must set a title` / `must set a duration` | Add `title:` / `duration: 80m`. |
| `duration must be a positive length of time` | Use units: `80m`, `1h20m`. |
| `the PIN does not belong in the exam file` | Delete the `pin:` line. |
| `unknown front matter key` | Only the keys in the [Front matter](#front-matter) table exist. |
| `use ## for an exercise and ### for an item` | Remove the `#` heading; the title is in the front matter. |
| `the exam has no exercises` | Add at least one `## ` heading. |
| `exercise … has no answer field` / `item … has no answer field` | Add a `{{blank}}` or an `answer` block under it, or merge the item into a neighbour. |
| `answer fields cannot be in the general instructions` | Move the field under a `##` or `###`. |
| `answer fields cannot be in headings` | Put the `{{name}}` in the text below the heading. |
| `answer field "…" is already used` | Rename one of the two fields. |
| `id "…" is already used` | Make exercise and item `#id`s unique. |
| `the items of exercise … add up to X points` | Fix the item points or the exercise points. |
| `the exercises add up to X points but the front matter says Y` | Fix the exercise points or the front matter `points`. |
| `links are not allowed` / `images are not supported` / `HTML is not allowed` | Rewrite as plain text or Markdown. |
| `the attributes at the end of the heading could not be read` | Write `{#id points=10}`: braces, no commas, nothing after them. |
| `unknown answer option` | Only `#name`, `kind`, `lang` and `lines` exist. |
| `{{ opens an answer field that is not closed` | Close with `}}` on the same line, or put a literal `{{` in inline code. |
| `lang only applies to code answers` | Drop `lang`, or use `kind=code`. |

## Limits

| What | Limit |
|---|---|
| Exercises per exam | 100 |
| Items per exercise | 52 |
| Answer fields per exam | 1000 |
| Points per exercise, item or exam | 10000 |
| Text of one part (instructions, an exercise body, an item body) | 256 KiB |
| Compiled exam content | 1 MiB |
| Starter text of one answer field | 64 KiB |
| `lines` of an answer field | 1–100 |
| Exam file | 4 MiB |
| One student answer | 64 KiB (512 KiB in total per student) |

## Delivery checklist

- [ ] The file starts with `---`, has `title` and `duration`, and no `pin`.
- [ ] No `#` headings, no links, no images, no HTML.
- [ ] Every `##` and `###` has at least one answer field below it.
- [ ] Field names are unique within their exercise or item; IDs are unique across the exam.
- [ ] Points are attributes, and sums match at every level (and the front matter `points`, when set).
- [ ] Each question says what format of answer is expected.
- [ ] Anything the student needs is inside the exercise when navigation is `forward_only`.
- [ ] The calculator is configured as intended (or absent).
- [ ] No solutions or grading notes in the file.
- [ ] `build/examc exam.md` reports no problems and lists the answer fields you expect.

## Complete example

This file passes `build/examc`. It uses every feature once.

````markdown
---
title: Midterm – Computer Organization
duration: 90m
grace: 2m
min_volume: 80
opens_at: 2026-10-20T08:00:00-06:00
closes_at: 2026-10-20T08:30:00-06:00
navigation: forward_only
calculator: mode=[scientific, programmer], angle=user, disabled=[ieee754]
points: 100
---

### General instructions

- Duration: **1 hour 30 minutes**.
- Use only the instructions seen in class: `add`, `sub`, `addi`, `lw`, `sw`, `beq`, `bne`, `slt`, `j`, `jr`, `jal`.
- Pseudo-instructions such as `li` or `move` are **not** allowed.

## Exercise 1 — Number systems {points=30}

Answer with the value only, without prefixes or units.

### a. Conversions {#conv points=10}

| Decimal | Binary (8 bits) | Hexadecimal |
|:-------:|:---------------:|:-----------:|
| 77      | {{bin}}         | {{hex}}     |
| 200     | 11001000        | {{hex2}}    |

### b. Performance {points=20}

A program runs 27 instructions with a total of 44 cycles.

1. **(10 pts)** Compute the CPI: {{cpi}}
2. **(10 pts)** Show your calculation:

```answer {#work lines=6}
```

## Exercise 2 — Assembly {points=40}

Translate the function to MIPS32 using the standard calling convention.

```c
int twice(int x) {
    return x + x;
}
```

```answer {#code lang=mips lines=12}
twice:
    # your code here
```

Explain in one sentence why `twice` does not need the stack:

```answer {#why lines=3}
```

## Exercise 3 — Short questions {points=30}

- Which register holds the return address? {{ra}}
- Which instruction is used to call a function? {{call}}
- Is `$zero` writable? {{zero}}
````

Its answer fields are `conv.bin`, `conv.hex`, `conv.hex2`, `1b.cpi`, `1b.work`, `2.code`, `2.why`, `3.ra`, `3.call` and `3.zero`: item `a` set its own ID (`#conv`), item `b` took the default `1b`. `build/examc` prints the list.

## How the file is used

Understanding this helps you avoid changes that break grading.

- **Compile:** the proctor dashboard compiles the file locally and sends the result to `POST /admin/class/{id}/exam`. Bodies become HTML fragments; each answer field becomes an element with a `data-slot` attribute that the student application replaces with an input. Before showing any HTML, the student and proctor applications reduce it to the elements the compiler writes (`shared/exam/sanitize.js`).
- **The source is kept.** The proctor also sends the file's Markdown as `source_markdown` (stored as opaque text, at most 4 MiB, never returned to students). **Download submissions…** uses it to give the instructor the exam itself with each student's answers filled in: blanks become inline code, text and code answers become fenced blocks titled with the field ID, and empty answers read `*(no answer)*`. An exam imported without a source still works: downloads then list the answers under their field IDs without the questions.
- **Do not rename or remove answer fields after students have started.** The field ID is the key of every saved answer, and a downloaded file cannot be rebuilt from a source whose fields no longer match the stored exam. To change an exam in use, import a new one.
- **Compiled shape** (for reference):

```json
{
  "title": "…", "pin": "typed by the proctor", "duration_seconds": 4800, "grace_seconds": 120, "navigation": "forward_only",
  "content": {
    "instructions": "<h3>Instrucciones Generales</h3>…",
    "exercises": [{
      "id": "1", "title": "Ejercicio 1 — Análisis de Rendimiento", "points": 20, "body": "<p>Considere…</p>",
      "items": [{
        "id": "1a", "title": "a. Conteo de Instrucciones", "points": 10,
        "body": "<table>…<span class=\"slot\" data-slot=\"1a.alu\"></span>…</table><div class=\"slot\" data-slot=\"1a.trace\"></div>",
        "slots": [{"id": "1a.alu", "kind": "blank"}, {"id": "1a.trace", "kind": "text", "lines": 10}]
      }]
    }]
  }
}
```

## Checking a file outside the dashboard

```bash
make examc
build/examc exam.md          # summary: exercises, items, answer fields and points, or every problem with its line
build/examc -json exam.md    # the compiled document (without a PIN)
build/examc -fill answers.json exam.md   # the exam with those answers filled in, as a downloaded student file looks
```

`answers.json` is one JSON object of answer field ID to the answer text, such as `{"1a.alu": "16", "2a.code": "mult $s0, $s1"}`. The original front matter is kept in the output; the dashboard replaces it with the submission's.
