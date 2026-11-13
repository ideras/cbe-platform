# Workspace exam format

This document specifies how to write a **workspace exam**: an exam in which each student gets a project, an editor and a terminal inside a container, and works on a TODO list. It is written for **AI agents that generate exams** and for **instructors who write or edit them by hand**; both follow the same rules. A question exam (a single Markdown file with answer fields) is described in [exam-format.md](exam-format.md); the two kinds are never mixed.

- **Agents:** read [Rules at a glance](#rules-at-a-glance), the [authoring workflow](#authoring-workflow-for-agents) and the [delivery checklist](#delivery-checklist). Everything else is reference. Treat every "must" as enforced by the compiler: a package that breaks one cannot be imported.
- **Instructors:** start with the [minimal package](#a-minimal-package), then read [workspace.md](#workspacemd) and [project/ and grading/](#project-and-grading).

The package is only the authoring format. `examc` (`shared/examdoc`) compiles it into one JSON document and two archives; the server never reads the package folder. [`testdata/workspace/lab-listas/`](../testdata/workspace/lab-listas) is a complete example package and [`testdata/workspace/lab-listas.json`](../testdata/workspace/lab-listas.json) is the document it compiles to.

## Rules at a glance

1. A package is a **folder** with `workspace.md` and `project/` in it. `grading/` is optional. Nothing else may be in it, except the pictures the instructions use and names that start with a dot.
2. `workspace.md` **starts with a front matter block** between two `---` lines. `title`, `duration` and `image` are required.
3. The text after the front matter is the **instructions**. **Every `[ ] text` line is a TODO item** and is taken out of the instructions into the TODO list. There must be at least one.
4. A TODO item is **one line**. Its points and ID go at the end, in braces: `[ ] Implement push_front {#t1 points=10}`.
5. **No links, no raw HTML, no `#` heading, no `{{blank}}` fields and no `answer` blocks.** Pictures are allowed as `![alt text](file.svg)` with a PNG, JPEG, GIF or SVG file in the package, never from `grading/`.
6. `project/` holds the **starter files** that are copied into `/work`. It may be empty. At most **20 MiB and 2,000 files**, no device files or pipes, and no symbolic link that points outside the folder.
7. `grading/`, when present, holds the **hidden tests**. It must hold an executable file called `grade`. Students never receive it, and no part of it is in the compiled document.
8. **No secrets in the workspace.md or in `project/`:** no PIN, no solutions, no grading rubric. Everything in `project/` is given to every student.
9. Validate with `build/examc lab/` before delivering. Deliver only a package that passes.

## A minimal package

```text
lab/
  workspace.md
  project/
    main.c
```

```markdown
---
title: Lab 1 – Hello
duration: 1h
image: cbe/c17
run: make run
---

Write a program that prints your name.

[ ] Print your name with `printf`
```

This compiles to a workspace exam with one TODO item (`t1`), the default limits, a terminal and a starter bundle holding `main.c`. Everything below adds to it.

## Package layout

```text
lab-listas/
  workspace.md          settings, instructions, TODO list (required)
  project/              starter files copied to /work (required, may be empty)
  grading/              hidden tests (optional; kept on the server, never sent to students)
  image/Containerfile   per-exam image customization (not supported yet)
```

- `image/` is reserved for a later release. A package that has it is refused, so nobody expects it to take effect.
- A folder or file that is not in the list is refused too, so a mistyped `grader/` cannot silently lose the hidden tests. The exceptions are names that start with a dot (`.git`, `.DS_Store`), `Thumbs.db`, `desktop.ini`, and the pictures the instructions use.
- `workspace.md`, `project/` and `grading/` must be real files and folders, not links.

## workspace.md

```markdown
---
title: Laboratorio 2 – Listas enlazadas
duration: 2h
grace: 2m
image: cbe/cpp20
terminal: on
run: make run
check: make check
submit: [src/**, include/**, tests/**, Makefile]
ignore: [build/**, **/*.o]
points: 40
---

Build with `make`. Run the sample tests with `make check`.

[ ] Implement `push_front` in `src/list.cpp` {#t1 points=10}
[ ] Implement `insert_sorted` {#t2 points=15}
[ ] Add `tests/mine.cpp` with at least three cases of your own {#t3 points=15}
```

### Front matter

One `key: value` per line. A value may be in quotes. A line that starts with `#` is a comment. A key may appear once.

| Key | Required | Meaning |
|-----|----------|---------|
| `title`, `duration` | yes | As in a question exam: `duration` is a length of time in whole seconds, such as `80m` or `1h20m`. |
| `grace`, `opens_at`, `closes_at`, `min_volume`, `calculator`, `points` | no | Exactly as in a question exam (see [exam-format.md](exam-format.md#front-matter)). `points` is the total of the TODO items. |
| `image` | yes | The name of a course image, such as `cbe/cpp20`: lowercase words joined by `/`, `.`, `_` or `-`, **without a tag or digest** (the server pins the version). Import fails if the server does not have the image. |
| `terminal` | no | `on` (default) or `off`. With `off`, students get only the Run and Check buttons. |
| `run`, `check` | no | One line: the command behind the **Run** and **Check** buttons, run in `/work`. A button is hidden when its command is absent. At most 1,000 bytes. |
| `submit` | no | What is submitted: one glob, or a bracketed list. Default: everything in `/work` except what `ignore` names. |
| `ignore` | no | What is left out of a submission. Default: `[build/**, **/*.o, **/*.out, **/__pycache__/**, .cache/**]`. `ignore: []` ignores nothing. |
| `resources` | no | `cpu=1, memory=1g, disk=100m`: the same `setting=value` syntax as `calculator`. Each setting is optional. Defaults: 1 core, 1 GiB, 100 MiB. See [Limits](#limits). |
| `snapshot` | no | How often the server snapshots a student's files while they change, from `10s` to `10m`. Default: the server's setting (60 seconds). Submitting, losing the container's agent and the deadline always take a snapshot. |
| `navigation` | **not allowed** | A workspace exam is one page. |
| `pin`, `focus_threshold` | **not allowed** | As in a question exam: the proctor enters the PIN when importing, and the focus threshold is a setting of the proctor application. |

**Globs.** `submit` and `ignore` patterns match the files under `/work`, written from `/work` down with `/`: `src/*.cpp`, `**/*.o`, `build/**`. In a segment, `*` is any run of characters except `/`, `?` is one character and `[ab]` is a class. A segment that is just `**` stands for any number of folders: `**/*.o` matches `a.o` and `src/a.o`, and `build/**` matches everything below `build` but not a file called `build`. A pattern may not start or end with `/`, hold `\`, an empty, `.` or `..` segment, or `**` inside a segment, and may use `**` at most twice. At most 50 patterns of 200 bytes.

**Sizes.** `memory` and `disk` are written in `m` or `g` (`512m`, `1g`; 1 g is 1,024 m). `cpu` is a number of cores with up to two decimals (`0.5`, `1`, `2`).

### Instructions

Everything after the front matter that is not a TODO item is the **instructions** panel. The question-exam rules for the text apply: Markdown with headings, lists, tables, quotes, code blocks, bold, italics, inline code and strikethrough; **no links** (following one would leave the exam window) and **no raw HTML**. `##` and `###` are ordinary headings here, not exercises or items, and `{#id}` after a heading is just text. `#` is refused: the title belongs in the front matter. `{{blank}}` fields and `answer` blocks belong to question exams and are refused.

Pictures are written `![alt text](diagram.svg)` and are looked up next to `workspace.md` or in a folder under the package. The rules and the size limits are those of [question exams](exam-format.md#images). A picture cannot come from `grading/`.

### TODO items

A TODO item is a task-list line **at the start of a line**:

```markdown
[ ] Implement `push_front` in `src/list.cpp` {#t1 points=10}
- [ ] The same, written as a GitHub task-list item {#t2}
```

- Both forms are accepted: the bare `[ ] text` of the examples and the GitHub form `- [ ] text` (also with `*` or `+`). A line inside a fenced code block is code, not an item.
- An item that is indented, inside a quote or a list, or numbered is **refused**, so it is never silently shown as plain text.
- The text is **one line** of inline Markdown: code, bold and italics are fine. It cannot hold pictures, links, HTML, `{{blank}}` fields, or a block such as a heading, list or quote, and it is at most 2,048 bytes.
- `{#id points=N}` at the end sets the **ID** and the **points**. The ID defaults to `t1`, `t2`, … by position among the items, uses letters, digits, `_` and `-` (at most 64 characters) and must be unique. The grading script reports its results under these IDs. `points` is a positive number; anything else in the braces is refused.
- When `points` is set in the front matter and **every** item has points, they must add up.
- The checkbox state is ignored: every item starts unticked.
- At least one and at most 100 items.

The instructions the student reads do not contain the TODO list; it is shown in its own panel.

## project/ and grading/

`project/` is copied into `/work` when the student's container is created. `grading/` stays on the server.

| | `project/` | `grading/` |
|-|-----------|-----------|
| Required | yes (may be empty) | no |
| Reaches the student | yes, all of it | **never** |
| Size and files | 20 MiB, 2,000 files | 20 MiB, 2,000 files |
| Must hold | – | an executable `grade` |

What a folder may hold: folders, regular files and **symbolic links that stay inside the folder**. Device files, named pipes and sockets are refused. A link may not be absolute and may not climb above the folder; `..` may only come at the start of its target (`../include` is fine, `src/../../x` is not). File names must be valid UTF-8 without control characters, at most 255 bytes a name and 4,096 a path.

### The `grade` program

`grading/grade` is a script or a binary that the grader runs in a fresh container with the student's submission, and that writes this JSON to standard output:

```json
{"t1": {"passed": true,  "points": 10, "log": "4/4 tests passed"},
 "t2": {"passed": false, "points": 6,  "log": "3/5 tests passed"}}
```

The keys are TODO IDs; a key that is not an ID is reported, not dropped. `grade` counts as **executable** when it starts with `#!` or is an ELF program, or when its executable bit is set on the computer that compiles the package. The first rule means a package written on a computer without executable bits still works.

### Bundles

The compiler packs each folder into a **tar archive**, the starter bundle and the grading bundle, and puts only their **hashes** in the document. The archives are deterministic: the same folders always give the same bytes and the same `sha256:` hash, whatever the day, the author's computer or the order the files were made in. For that, every entry is written with owner 0, a fixed modification time, permissions `0755` for folders and executable files and `0644` for everything else (links `0777`), and entries sorted by name. A file is executable when it has an executable bit, starts with `#!` or is an ELF program.

The bytes themselves travel next to the document when an exam is imported. `examc -bundles out/ lab/` writes `starter.tar` and `grading.tar` so they can be inspected with `tar -tvf`.

## Limits

| Limit | Value |
|-------|-------|
| `workspace.md` | 4 MiB |
| Instructions | 256 KiB of HTML (a picture counts a third more than its file); pictures as in question exams |
| TODO items | 100, 2,048 bytes of text each |
| `project/` and `grading/` | 20 MiB and 2,000 files each |
| `submit` and `ignore` | 50 patterns each, 200 bytes a pattern |
| `run`, `check` | 1,000 bytes |
| Problems listed | the first 50 |

`resources` values are checked for sense by the compiler (cpu 0.1 to 64, memory 64m to 64g, disk 1m to 100g, `snapshot` 10s to 10m). The server has its own, lower maximums, and an import that asks for more fails there.

## Authoring workflow for agents

1. Write `project/` first: the starter files, a `Makefile` or script behind `run` and `check`, and sample tests the student can run.
2. Write one TODO item per task a student can finish and a hidden test can check. Give each a short ID that the grading script will use, and points that add up to `points`.
3. Write `grading/grade` and its tests. They never go in `project/`.
4. Write `workspace.md` last, with the front matter, a short instructions text and the TODO list.
5. Run `build/examc lab/`. Fix every problem it lists; each names its line or its path. Run it again until it prints the summary.
6. Check the bundles with `build/examc -bundles /tmp/lab-bundles lab/` and `tar -tvf /tmp/lab-bundles/starter.tar`: nothing a student must not have is in the starter, and `grade` is in the grading bundle.

### Common errors and fixes

| Problem | Fix |
|---------|-----|
| `a workspace package holds only workspace.md, project/, grading/, image/ and the pictures…` | Move the file or folder out of the package, or check the spelling of `grading/`. |
| `a TODO item must start its line` | Write `[ ] text` or `- [ ] text` at the start of the line, outside lists and quotes. |
| `the exam has no TODO items` | List the tasks as `[ ] text` lines. |
| `TODO id "t2" is already used` | An item named `t2` by hand collides with the default ID of the second item; rename it. |
| `image must be the name of a course image` | Write `cbe/cpp20`, with no tag, no digest and no uppercase letters. |
| `project/x: the symbolic link points to … outside the folder` | Replace the link with a copy of the file, or point it inside `project/`. |
| `project/ holds more than 20 MiB of files` | Remove build output and downloads from `project/`. |
| `grading/grade: is not executable` | Begin the script with `#!/bin/sh` (or another interpreter), or set its executable bit. |
| `links are not allowed` | Remove the link; write the address as text if students need it. |
| `answer fields ({{name}}) are not allowed in a workspace exam` | Remove the field; a workspace exam has no answer fields. |

## Delivery checklist

- [ ] `build/examc lab/` prints the summary with no problems.
- [ ] `title`, `duration` and `image` are set, and the image is one the server has.
- [ ] Every TODO item has an ID the grading script uses, and its points add up.
- [ ] `project/` holds what every student should start with and nothing else: no solution, no build output.
- [ ] `grading/grade` runs on the course image and prints the JSON above.
- [ ] No PIN, solution or rubric in `workspace.md`.

## Complete example

[`testdata/workspace/lab-listas/`](../testdata/workspace/lab-listas) is the package of the front-matter example above, with a starter project that builds (its sample test fails until the first TODO item is done) and a grading folder. `build/examc testdata/workspace/lab-listas` prints:

```text
Laboratorio 2 – Listas enlazadas
  workspace exam, 120 min, image cbe/cpp20, terminal on
  run: make run · check: make check
  resources: 1 cpu, 1024 MiB memory, 100 MiB disk
  t1   Implement push_front in src/list.cpp [10 pts]
  t2   Implement insert_sorted [15 pts]
  t3   Add tests/mine.cpp with at least three cases of your own [15 pts]
  total: 40 points
  starter bundle: 5 files, 2110 bytes, sha256:a8f8de68…
  grading bundle: 2 files, 492 bytes, sha256:2d95f25b…
```

and `build/examc -json testdata/workspace/lab-listas` prints [`lab-listas.json`](../testdata/workspace/lab-listas.json).

## How the package is used

1. **Compile.** `examc` (and, in a later release, **Import exam…** in the proctor application) reads the package folder and produces the document and the two bundles. The document has `"kind": "workspace"`; the document of a question exam has no `kind` and is read as `"question"`, so existing exams are unchanged.
2. **Import.** The server stores the document and the bundles. The grading bundle is never sent to a student's application, and the compiled document holds only its hash.
3. **First login.** The student's container is created from the course image, with the limits of `resources`, and the starter bundle is copied into `/work`.
4. **During the exam.** The student edits files and runs commands in the container; the server snapshots `/work` at the interval of `snapshot` and on Submit.
5. **Grading.** `grading/grade` runs in a fresh container, offline, against the submitted snapshot.

Steps 2 to 5 are built in later phases of the workspace plan; this document defines the package they all start from.

## Checking a package outside the dashboard

```bash
build/examc lab/                      # check the package and print a summary
build/examc lab/workspace.md          # the same, naming the file
build/examc -json lab/                # print the compiled document
build/examc -bundles out/ lab/        # also write out/starter.tar and out/grading.tar
```

`examc` exits with 0 when the package compiles, 1 with the list of problems on the standard error when it does not, and 2 for a mistake in the command line.
