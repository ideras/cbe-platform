package workspace

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"
)

// maxReportBytes bounds what a report holds of the student's files: past it the rest are listed by name only.
const maxReportBytes = 8 << 20

var fenceLanguages = map[string]string{
	"c": "c", "h": "c", "cc": "cpp", "cpp": "cpp", "cxx": "cpp", "hpp": "cpp", "hh": "cpp", "hxx": "cpp", "py": "python", "s": "asm", "asm": "asm",
	"sh": "sh", "md": "markdown", "json": "json", "txt": "", "java": "java", "js": "javascript", "rs": "rust", "go": "go", "html": "html", "css": "css", "xml": "xml", "yml": "yaml", "yaml": "yaml",
}

// fenceFor is a code fence that the file's own text cannot close: longer than any run of backticks in it.
func fenceFor(content string) string {
	longest, run := 0, 0
	for _, r := range content {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	return strings.Repeat("`", max(3, longest+1))
}

// Report writes the Markdown report of a student's latest submission: the exam's instructions, the TODO list as the student ticked it
// (and it says that is theirs, not checked), what the session did, and each submitted text file as a fenced block. It is made from the
// submission as it was recorded, so it matches the zip of the same submission.
func (s *Service) Report(ctx context.Context, session string) (string, error) {
	info, err := s.Repo.Session(ctx, session)
	if err != nil {
		return "", err
	}
	exam, err := s.Repo.Exam(ctx, info.ExamID)
	if err != nil {
		return "", err
	}
	submissions, err := s.Repo.Submissions(ctx, session)
	if err != nil {
		return "", err
	}
	if len(submissions) == 0 {
		return "", ErrNotFound
	}
	latest := submissions[len(submissions)-1]
	_, manifest, err := s.Manager.SubmissionManifest(ctx, latest.ID)
	if err != nil {
		return "", err
	}
	done, err := s.Repo.Todo(ctx, session)
	if err != nil {
		return "", err
	}
	terminals, err := s.Repo.Terminals(ctx, session)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	at := func(t time.Time) string { return t.UTC().Format("2006-01-02 15:04:05 UTC") }
	fmt.Fprintf(&out, "# %s — student %s\n\n", exam.Title, info.Account)
	fmt.Fprintf(&out, "- Session: `%s`\n- Session status: %s\n", info.SessionID, info.Status)
	kind := "pressed Save"
	if latest.Kind == SubmissionFinal {
		kind = "made when the exam ended"
	}
	fmt.Fprintf(&out, "- Submission %d (%s): %s, snapshot %d, %d files, %d bytes\n", latest.ID, kind, at(latest.SubmittedAt), latest.SnapshotID, latest.Files, latest.Bytes)
	fmt.Fprintf(&out, "- Submissions made: %d\n- Workspace restarts: %d\n\n", len(submissions), info.AgentRestarts)
	out.WriteString("## Instructions\n\n" + markdownOf(exam.Document.Content.Instructions) + "\n")
	out.WriteString("## TODO list\n\nThe student's own ticks. They are self-reported and nothing checked them.\n\n")
	for _, item := range exam.Document.Content.Todos {
		box := "[ ]"
		if done[item.ID] {
			box = "[x]"
		}
		points := ""
		if item.Points != nil {
			points = fmt.Sprintf(" (%g points)", *item.Points)
		}
		fmt.Fprintf(&out, "- %s %s%s\n", box, strings.TrimSpace(strings.ReplaceAll(markdownOf(item.TextHTML), "\n", " ")), points)
	}
	out.WriteString("\n")
	if len(terminals) > 0 {
		out.WriteString("## Terminals\n\n| Kind | Started | Ended | Exit | Output |\n| --- | --- | --- | --- | --- |\n")
		for _, t := range terminals {
			ended, exit := "-", "-"
			if t.EndedAt != nil {
				ended = at(*t.EndedAt)
			}
			if t.ExitCode != nil {
				exit = fmt.Sprint(*t.ExitCode)
			}
			note := fmt.Sprintf("%d bytes", t.Bytes)
			if t.Truncated {
				note += " (cut)"
			}
			fmt.Fprintf(&out, "| %s | %s | %s | %s | %s |\n", t.Kind, at(t.StartedAt), ended, exit, note)
		}
		out.WriteString("\n")
	}
	out.WriteString("## Files\n\n")
	size := 0
	var omitted []string
	for _, entry := range manifest.Entries {
		switch entry.Type {
		case "dir":
			continue
		case "link":
			omitted = append(omitted, fmt.Sprintf("`%s` is a symbolic link to `%s`", entry.Path, entry.Link))
			continue
		}
		file, err := s.Manager.readText(entry)
		switch {
		case err != nil:
			omitted = append(omitted, fmt.Sprintf("`%s` could not be read", entry.Path))
		case file.TooLarge:
			omitted = append(omitted, fmt.Sprintf("`%s` is larger than 1 MiB (%d bytes)", entry.Path, entry.Size))
		case file.Binary:
			omitted = append(omitted, fmt.Sprintf("`%s` is not text (%d bytes)", entry.Path, entry.Size))
		case size+len(file.Content) > maxReportBytes:
			omitted = append(omitted, fmt.Sprintf("`%s` (%d bytes) is left out: the report holds at most %d MiB", entry.Path, entry.Size, maxReportBytes>>20))
		default:
			size += len(file.Content)
			extension := strings.ToLower(strings.TrimPrefix(path.Ext(entry.Path), "."))
			fence := fenceFor(file.Content)
			body := file.Content
			if body != "" && !strings.HasSuffix(body, "\n") {
				body += "\n"
			}
			fmt.Fprintf(&out, "### `%s`\n\n%s%s\n%s%s\n\n", entry.Path, fence, fenceLanguages[extension], body, fence)
		}
	}
	if len(omitted) > 0 {
		out.WriteString("### Not shown\n\n")
		for _, line := range omitted {
			out.WriteString("- " + line + "\n")
		}
	}
	return out.String(), nil
}
