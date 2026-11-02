# Downloading submissions

After an exam, the proctor dashboard writes one Markdown file per finished student into a folder you choose. Open an exam's card in its class and use **Download submissions…**. Only finished sessions are downloaded, using each student's final saved answers; students still working or finishing are skipped (download again when they finish), and students who never logged in appear only in `_index.md`. Downloading reads from the server and changes nothing on it.

## Choose a format

The dialog reads top to bottom in three steps: **Format**, **Who is included** and **Destination**. The **Format** step has two choices, shown as cards. **Complete exam** is selected first.

| Format | What each student's file holds | Use it for |
|---|---|---|
| **Complete exam** | The exam's text with each student's answers inserted. | The archival record, and grading with the questions in front of you. |
| **Answers only** | Each answer field's ID, kind and saved answer, under the exam's exercise and item labels. No questions, instructions, examples, tables, pictures or supplied code. | Giving students their own responses next to a separate evaluation report, and giving an AI grader compact per-student files together with one shared exam, rubric and reference solution. |

Choosing a format changes only the folder name the dialog suggests. Nothing is downloaded until you press the download button at the bottom, which says how many student files it will write (for example **Download 28 files**). **Who is included** counts the finished students (one file each), those still working or finishing (skipped; download again when they finish) and those who never logged in (listed in `_index.md` only). The format cannot be changed while the download runs or while the replace question is open, and the result replaces the options once the download is done.

Keep the complete downloads as the archival evidence. An answers-only file is a faithful record of what was saved. It is not a corrected answer, a model solution or an evaluation, and it cannot be graded without the original exam, which you supply yourself: the rubric and the reference solution are never part of a student's file.

## Folder and file names

Each student has the same file name in both formats (for example `Jose_Nunez_Martinez.md`; two students whose names normalize to the same text both get `_<account number>`). The suggested **folder** differs, so the two formats do not land in the same place by default:

- Complete exam: `<class>-<exam>`, for example `CCC403-Examen_I_Parcial`.
- Answers only: the same name followed by `-AnswersOnly`.

Once you type a folder name yourself, changing the format leaves it alone, and **Use default name** brings the suggestion back. A name with a character the dashboard refuses (`/ \ : * ? " < > |`) is flagged while you type, and the download button stays disabled until it is fixed. Choose the parent folder with **Choose folder…**; the dashboard creates the output folder inside it, and the dialog shows the full path the files will go into.

When the download is done the dialog shows the folder (with **Copy path**), how many student files were written, who was not included, and any warnings. **Open folder** opens the folder in the file manager and **Done** closes the dialog.

If the output folder already exists, the dialog asks before replacing its files, because you may have written grading notes in them. When the folder's `_index.md` says it holds the other format, the question says so (an `_index.md` without a format is a complete download from an earlier version). Replacing writes only the files of this download; files you added yourself are left alone. The replace question repeats exactly the format and folder name of the first attempt.

## Contents of an answers-only file

````markdown
---
export_format: "answers-only"
export_schema: 1
exam: "Example examination"
exam_id: "example-exam-id"
class: "Example class"
student: "Example Student"
account_number: "0123"
session_id: "example-session-id"
status: "submitted by the student"
submitted_at: "2026-10-29 21:00:00 -06:00"
answer_version: 12
downloaded_at: "2026-10-30 08:00:00 -06:00"
---

# Example examination — Example Student

Saved responses only. Consult the original exam for the questions.

## 1 — Regular expressions

### 1a — Binary literals

**Field:** `1a.regex`  
**Kind:** `text`  
**Answer state:** `answered`

```text title="1a.regex"
0[bB]([01]+_?[01]+)
```
````

- **Provenance.** `exam_id` identifies the exam; `answer_version` is the number of the student's final saved answers, not a revision of the exam. `account_number` is quoted, so leading zeros stay. The file has no emails, PINs, screenshots, telemetry, lock or unlock history, or notes a proctor typed when unlocking or force-submitting a session.
- **Order and IDs.** Fields appear in the exam's reading order: an exercise's own fields, then each item's. Field IDs are the exam's own (`1a.regex`, `2c.code`). Exercise and item titles are written as plain text, so a title cannot add a link, a picture or a heading.
- **One block per field.** Every field has exactly one fenced block, including blank fields and fields that were never saved. A code field carries its language on the fence; everything else is `text`.

### Answer states

The state is written outside the block and describes what was saved, not whether it is right.

| State | Meaning |
|---|---|
| `answered` | Any other saved value. |
| `empty` | The field was saved as an empty string; the file adds "No answer." |
| `missing` | The field has no saved value at all; the file adds "No saved value for this field." It does not claim the student left it blank. |
| `whitespace-only` | The saved value is only spaces, tabs or line breaks, kept exactly as saved. |
| `starter-unchanged` | The saved value is exactly the field's starter text. Nothing is erased or graded. |

A student who typed `*(no answer)*` submitted that text: it stays inside the block as an answer, and no marker is ever written inside a block.

### The saved answer is exact

The answer is not trimmed, re-indented, reflowed, escaped, decoded or corrected: spaces, tabs, blank lines, line endings, backslashes, quotes and Unicode are written as saved, even a wrong regular expression. The fence is longer than any run of backticks in the answer, so an answer containing ` ``` ` cannot end its own block.

To recover an answer from the file, take the text between the opening fence line and the closing fence line and remove exactly one trailing line feed. That line feed is only framing and is always written, even when the answer already ends in a newline. Do not trim anything else. A Markdown viewer or a PDF converter may show line endings or a final blank line differently; the downloaded `.md` file is the record.

### Saved answers that match no field

If a student's final answers hold a key that matches no field of the exam, the file keeps it after the last field under **Unmapped saved answers**, in key order, with the key shown quoted and the value in its own block. Nothing is guessed about its question or language.

## `_index.md`

`_index.md` is the instructor's checklist. Do not give it to students. For an answers-only download it begins with the same `export_format` and `export_schema` lines and lists every student of the class in name order: finished, still working, still finishing and never logged in. An **Export** column says whether each finished student's file was `written`, `failed: <reason>` or `skipped`, and only files written in this download are linked. If a student's file failed, an older file with the same name in the folder is left as it was and is not linked.

A **Fields to check** table names, per student, the fields with no saved value and any saved keys that match no field. The dialog summarizes the same warnings after the download. Neither shows an answer.

## Limits

- The dashboard does not make PDFs, grading reports, Canvas files or archives, and does not post to Canvas or email students.
- Active or grace-period sessions are not exported as if they were final submissions.
- An answers-only download needs the exam's stored structure, not its Markdown, so it works when the Markdown is missing or no longer matches. If the stored structure itself is unusable (two fields with the same ID, for example), each student's file fails with that reason and nothing is written for them.
- `answer_version` is the only version recorded. Do not rename or remove answer fields after students have started: the field ID is the key of every saved answer.
