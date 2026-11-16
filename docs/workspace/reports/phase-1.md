# Phase 1 report: package format and compiler

**Date:** 2026-11-13. **Branch:** `feat/workspace-phase-1-package-format` (from `feat/workspace-phase-0-discovery`, which is pushed; this branch is pushed too). **Plan:** `.agents/WORKSPACE-PLAN.md` v1.1.
**Status:** done; all five acceptance items are met. The owner confirmed decisions P1-1 to P1-17 as proposed (P1-18).

## What changed

Only the compiler, its command and the documents. **No server, student or proctor code changed.**

| Path | What |
|------|------|
| `shared/examdoc/workspace.go` | The types of the document (section 5.4), `Bundle`, the limits, the defaults, `DocumentKind` |
| `shared/examdoc/workspace_compile.go` | `CompileWorkspaceDir` and `CompileWorkspaceFS`: the package layout, the front matter, the instructions, the TODO items |
| `shared/examdoc/workspace_bundle.go` | The deterministic tar bundles, the limits, links and special files |
| `shared/examdoc/workspace_glob.go` | The glob dialect of `submit` and `ignore`: `validGlob` and `GlobMatch` |
| `shared/examdoc/frontmatter.go`, `compile.go` | **Refactored, behaviour unchanged:** the front matter reader and the Markdown setup are shared with the workspace compiler (task 3) |
| `shared/examdoc/cmd/examc/main.go` | A folder (or the path of `workspace.md`) is a package; `-json`, and a new `-bundles dir` |
| `shared/examdoc/workspace*_test.go`, `cmd/examc/workspace_test.go` | The tests |
| `testdata/workspace/lab-listas/`, `testdata/workspace/lab-listas.json` | The plan's example as a package, and what it compiles to |
| `docs/workspace-format.md` | The format, in the style of `docs/exam-format.md` (task 4); `exam-format.md` and the `Makefile` comment point to it |
| `docs/workspace/DECISIONS.md`, `reports/phase-1*.{md,log}` | P1-1 to P1-17, this report, the test log |

## Tests run and results

- **The new tests** (`shared/examdoc`, 286 test results with their subtests on this Linux machine, none failed or skipped): the example package against its golden document and
  against each key of section 5.4; **115 failing-input cases**, one per rule of section 5 and each with its message and line (or path); the bounded list of
  problems; determinism (other times, other permission bits, a copy in another folder: the same bundle bytes and hash); the sorted tar and its modes; the glob rules and
  `GlobMatch`; real symbolic links, a symbolic-link loop, a named pipe (never opened), an executable bit; `examc` summary, `-json`, `-bundles`, problems and options.
- **Every existing test passes unchanged.** The baseline list of Phase 0 was re-run on this branch (`reports/phase-1-tests.log`, at commit `de574e6`, before a last small fix):

  | | Phase 0 | Phase 1 |
  |-|---------|---------|
  | `scripts/ci/test.py --race`, 7 modules, vet and tests | pass | **pass** |
  | student `npm test` | 45/45 | **45/45** |
  | proctor `npm test` | 74/74 | **74/74** |
  | student Playwright | 131 passed | **131 passed** |
  | proctor Playwright | 294 passed | **294 passed** |

  Go's test cache served the packages whose code and inputs did not change (the whole server, the student application); `shared/examdoc` ran fresh with `-race`.
  After the baseline I made one more change (the Windows link fix and two more tests) and re-ran `go test -race -count=1` for `shared/examdoc` and `apps/proctor`
  (which compiles questions with `examdoc`): both pass.
- **Checks that the tests catch what they claim.** I broke three rules on purpose (links never refused; the grading folder packed as the starter; pictures allowed from `grading/`);
  each made tests fail, and I reverted each.
- **Cross-compiled:** `go vet` for Windows and macOS is clean.
- **Not run:** the Windows and macOS CI jobs, `desktop-test`, `student-client-test`, `proctor-client-test` (as in Phase 0). The real-symlink tests are skipped on Windows; the same
  rules run there with links in memory.

## Acceptance (plan, Phase 1)

- [x] **The example package compiles. Its JSON matches section 5.4.** `TestExampleWorkspaceMatchesItsCompiledDocument` compares it with `lab-listas.json` and checks each key and value of 5.4.
- [x] **Each rule in section 5 has a failing-input test with the expected message and line.** `TestWorkspaceProblemsNameTheirLines`, 115 cases.
- [x] **A symlink escaping `project/`, a device file, or a bundle over the limits fails with a clear message.** Absolute and `..` links, a link through a name, a device, a pipe, a socket, too many files, too many bytes: the same cases with real files in `workspace_dir_test.go`.
- [x] **`grading/` content never appears in the compiled student document.** `TestGradingNeverAppearsInTheStudentDocument`: its text, its file names and its output are not in the document or the starter bundle, and a picture cannot come from it.
- [x] **Every existing question-exam test passes unchanged.** See above; `testdata/exam.json` did not change and a question document carries no `kind`.

## Deviations from the plan, and conflicts found (standing rule 1)

None was settled silently; each is a numbered decision in `DECISIONS.md`.

1. **Task 5, `kind` "in the stored exam document" (P1-3). The one the owner should look at.** The compiled workspace document has `"kind": "workspace"`, and a document without a `kind` reads as a question exam (`DocumentKind`). The question document cannot gain a field without changing `testdata/exam.json`, and the server stores exams as database rows with no use for a kind until Phase 3 adds the workspace tables, so **I did not change the server**: it refuses a workspace document as an unknown field. If you want the server to accept `kind` now, it is a small change.
2. **The plan disagrees with itself on TODO items (P1-1).** Its example writes `[ ] text`, its text says "GFM task-list items" (`- [ ] text`). Both are accepted.
3. **5.1 and 5.4 differ (P1-2).** The 5.4 document has an `ignore` list and a full `resources` object that the 5.1 example does not set. The example package adds `ignore: [build/**, **/*.o]`, and the compiler fills the resource defaults.
4. **Things the plan is silent on, decided strictly (P1-5, P1-6):** at least one TODO item; anything in the package folder beyond the known entries and the pictures is refused. Say if you want either relaxed.
5. **Added beyond the plan:** `examc -bundles dir` (P1-16), `GlobMatch` (P1-12, for Phase 5), the exact glob dialect.
6. **How the bundle bytes reach the server is not in this phase.** The compiler returns the document with the two hashes and the two archives; the import route that carries them is a later phase.

## Left behind

Nothing outside the repository changed in this phase. The host setup of Phase 0 is as it was. The example package was compiled into the scratchpad, never into `build/`.
