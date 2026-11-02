package dashboard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"exam-platform/shared/examdoc"
)

const (
	secondSession = "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	thirdSession  = "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	answersFolder = "Mathematics_10A-Exam_one-AnswersOnly"
)

// addFinishedWith gives the fake server another finished student with exactly these saved answers.
func (f *downloadFixture) addFinishedWith(id, account, name string, answers map[string]string) {
	f.server.mu.Lock()
	defer f.server.mu.Unlock()
	submitted := at
	f.server.roster.Sessions = append(f.server.roster.Sessions, Session{ID: id, ExamID: examID, Student: Student{account, name}, Status: "submitted", PersistedStatus: "submitted",
		Final: &FinalSummary{VersionID: 2, Sequence: 3, SavedAt: at, SubmittedAt: submitted, Reason: "grace_expired"}})
	f.server.subs[id] = submission{SessionID: id, ExamID: examID, AccountNumber: account, StudentName: name, Status: "submitted", Final: true, Reason: "grace_expired", SubmittedAt: &submitted,
		Version: &submissionVersion{VersionID: 2, Sequence: 3, SavedAt: at, Answers: answers}}
}

// downloadRequests are the requests a download makes: the exam's detail and the students' submissions. The dashboard's own poll
// of the roster and the exam list is not part of them.
func (f *downloadFixture) downloadRequests() []string {
	var found []string
	for _, request := range f.server.requested() {
		if strings.HasPrefix(request, "GET /admin/exam/") || strings.HasPrefix(request, "GET /admin/session/") {
			found = append(found, request)
		}
	}
	return found
}

func (f *downloadFixture) setSource(source *string) {
	f.server.mu.Lock()
	f.server.detail.SourceMarkdown = source
	f.server.mu.Unlock()
}

func (f *downloadFixture) setContent(content examdoc.Content) {
	f.server.mu.Lock()
	f.server.detail.Content = content
	f.server.mu.Unlock()
}

func readText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// frontMatter reads the front matter of a downloaded file into key → value, unquoting double-quoted values the way YAML does.
func frontMatter(t *testing.T, document string) map[string]string {
	t.Helper()
	body, found := strings.CutPrefix(document, "---\n")
	if !found {
		t.Fatalf("no front matter in:\n%s", document)
	}
	block, _, found := strings.Cut(body, "\n---\n")
	if !found {
		t.Fatalf("front matter is not closed in:\n%s", document)
	}
	values := map[string]string{}
	for _, line := range strings.Split(block, "\n") {
		key, value, found := strings.Cut(line, ": ")
		if !found || key == "" || strings.ContainsAny(key, " \"") {
			t.Fatalf("front matter line %q is not key: value", line)
		}
		if strings.HasPrefix(value, `"`) {
			unquoted, err := strconv.Unquote(value)
			if err != nil {
				t.Fatalf("front matter value %s is not a valid quoted string: %v", value, err)
			}
			value = unquoted
		}
		if _, dup := values[key]; dup {
			t.Fatalf("front matter key %q appears twice", key)
		}
		values[key] = value
	}
	return values
}

func (f *downloadFixture) answersOnly(t *testing.T, folderName string, replace bool) DownloadResult {
	t.Helper()
	result, err := f.DownloadSubmissionsWithMode(examID, folderName, FormatAnswersOnly, replace, true)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestThePreviewOffersADifferentFolderForEachFormat(t *testing.T) {
	f := newDownloadFixture(t)
	preview, err := f.PrepareDownload(examID)
	if err != nil {
		t.Fatal(err)
	}
	if preview.FolderName != "Mathematics_10A-Exam_one" || preview.AnswersOnlyFolderName != "Mathematics_10A-Exam_one-AnswersOnly" {
		t.Fatalf("folder names = %q, %q", preview.FolderName, preview.AnswersOnlyFolderName)
	}
	if problem := downloadFolderProblem(preview.AnswersOnlyFolderName); problem != "" {
		t.Fatalf("the suggested folder is refused: %s", problem)
	}
	if got := answersOnlyFolderName("", ""); got != "class-exam-AnswersOnly" {
		t.Fatalf("empty names give %q", got)
	}
}

func TestTheOldDownloadMethodStillWritesTheCompleteFormat(t *testing.T) {
	f := newDownloadFixture(t)
	f.chosenFolder = t.TempDir()
	f.choose(t)
	result, err := f.DownloadSubmissions(examID, testFolderName, false, true)
	if err != nil || result.Format != FormatComplete || len(result.Files) != 1 {
		t.Fatalf("result = %+v, %v", result, err)
	}
	file := readText(t, filepath.Join(result.Folder, "Jose_Nunez_Martinez.md"))
	for _, want := range []string{"times_locked: 0", "Value: `16`", "## Exercise {points=10}"} {
		if !strings.Contains(file, want) {
			t.Errorf("the complete file does not contain %q:\n%s", want, file)
		}
	}
	for _, unwanted := range []string{"export_format", "export_schema"} {
		if strings.Contains(file, unwanted) || strings.Contains(readText(t, filepath.Join(result.Folder, indexFileName)), unwanted) {
			t.Errorf("a complete file or its index carries %q", unwanted)
		}
	}
	explicit, err := f.DownloadSubmissionsWithMode(examID, "Explicit", FormatComplete, false, true)
	if err != nil || explicit.Format != FormatComplete || !strings.Contains(readText(t, filepath.Join(explicit.Folder, "Jose_Nunez_Martinez.md")), "Value: `16`") {
		t.Fatalf("the explicit complete format differs from the old method: %+v, %v", explicit, err)
	}
}

func TestAnswersOnlyDoesNotNeedTheExamSource(t *testing.T) {
	malformed := "---\ntitle: [unclosed\n---\n\n## "
	mismatched := "---\ntitle: Exam one\nduration: 10m\npoints: 10\n---\n\n## Exercise {points=10}\n\nValue: {{renamed}}\n"
	present := "---\ntitle: Exam one\nduration: 10m\npoints: 10\n---\n\n## Exercise {points=10}\n\nValue: {{a}}\n\n```answer {#code lang=mips}\n```\n"
	for name, source := range map[string]*string{"present": &present, "absent": nil, "malformed": &malformed, "mismatched": &mismatched} {
		t.Run(name, func(t *testing.T) {
			f := newDownloadFixture(t)
			f.setSource(source)
			f.chosenFolder = t.TempDir()
			f.choose(t)
			result := f.answersOnly(t, answersFolder, false)
			if result.Format != FormatAnswersOnly || len(result.Files) != 1 || len(result.Failed) != 0 || len(result.Warnings) != 0 {
				t.Fatalf("result = %+v", result)
			}
			file := readText(t, filepath.Join(result.Folder, "Jose_Nunez_Martinez.md"))
			for _, want := range []string{
				"export_format: \"answers-only\"\nexport_schema: 1\n",
				"# Exam one — José Ñúñez Martínez\n\nSaved responses only. Consult the original exam for the questions.\n",
				"## 1 — Exercise\n",
				"**Field:** `1.a`  \n**Kind:** `blank`  \n**Answer state:** `answered`\n\n```text title=\"1.a\"\n16\n```\n",
				"**Field:** `1.code`  \n**Kind:** `code`  \n**Answer state:** `answered`\n\n```mips title=\"1.code\"\nmult $s0, $s1\n```\n",
			} {
				if !strings.Contains(file, want) {
					t.Errorf("the answers-only file does not contain %q:\n%s", want, file)
				}
			}
			for _, unwanted := range []string{"Value:", "{points", "renamed", "times_locked", "(no answer)"} {
				if strings.Contains(file, unwanted) {
					t.Errorf("the answers-only file contains %q:\n%s", unwanted, file)
				}
			}
		})
	}
}

func TestAnswersOnlyFrontMatterIsProvenance(t *testing.T) {
	f := newDownloadFixture(t)
	f.chosenFolder = t.TempDir()
	f.choose(t)
	result := f.answersOnly(t, answersFolder, false)
	file := readText(t, filepath.Join(result.Folder, "Jose_Nunez_Martinez.md"))
	values := frontMatter(t, file)
	want := map[string]string{
		"export_format": "answers-only", "export_schema": "1", "exam": "Exam one", "exam_id": examID, "class": "Mathematics 10A",
		"student": "José Ñúñez Martínez", "account_number": "0012", "session_id": finishedSession, "status": "submitted by the student",
		"submitted_at": localStamp(at), "answer_version": "14",
	}
	for key, value := range want {
		if values[key] != value {
			t.Errorf("%s = %q, want %q", key, values[key], value)
		}
	}
	if stamp, err := time.Parse("2006-01-02 15:04:05 -07:00", values["downloaded_at"]); err != nil || time.Since(stamp) > time.Minute {
		t.Errorf("downloaded_at = %q, %v", values["downloaded_at"], err)
	}
	if len(values) != len(want)+1 {
		t.Errorf("front matter keys = %v", values)
	}
	// The account number stays a string, so leading zeros survive; the answer version is the sequence, not an exam revision.
	if !strings.Contains(file, "account_number: \"0012\"\n") || !strings.Contains(file, "answer_version: 14\n") || strings.Contains(file, "exam_version") {
		t.Errorf("front matter:\n%s", file)
	}
}

func TestAnswersOnlyFrontMatterCannotBeBroken(t *testing.T) {
	hostile := "Ana \"the\" \\ Rey\nNext: line\n---\ninjected: yes\x00\u2028é\xff"
	detail := examDetail{ExamID: examID, Title: "Exam \"one\"\nclass: forged", ClassName: "Class\\", Content: examdoc.Content{Exercises: []examdoc.Exercise{{ID: "1", Title: "One", Slots: []examdoc.Slot{{ID: "1.a", Kind: examdoc.Text}}}}}}
	submitted := at
	sub := submission{SessionID: finishedSession, ExamID: examID, AccountNumber: "00\"12", StudentName: hostile, Reason: "force_submit", OperatorReason: "NOTE", SubmittedAt: &submitted,
		Version: &submissionVersion{Sequence: 5, Answers: map[string]string{"1.a": "x"}}}
	data, _, err := renderAnswersOnly(detail, sub, at)
	if err != nil {
		t.Fatal(err)
	}
	values := frontMatter(t, string(data))
	if values["student"] != strings.ReplaceAll(hostile, "\xff", "\ufffd") || values["account_number"] != "00\"12" || values["exam"] != detail.Title || values["class"] != "Class\\" {
		t.Errorf("front matter values were changed: %q", values)
	}
	if len(values) != 12 || values["injected"] != "" || values["status"] != "submitted by the proctor" {
		t.Errorf("front matter = %q", values)
	}
}

func TestOnlyFinishedSessionsAreExportedFromTheirFinalVersion(t *testing.T) {
	f := newDownloadFixture(t)
	f.chosenFolder = t.TempDir()
	f.choose(t)
	f.server.resetRequests()
	result := f.answersOnly(t, answersFolder, false)
	if len(result.Files) != 1 || len(result.Skipped) != 2 || result.Skipped[0].Status != "working" || result.Skipped[1].Status != "finishing" {
		t.Fatalf("result = %+v", result)
	}
	want := []string{"GET /admin/exam/" + examID, "GET /admin/session/" + finishedSession + "/submission"}
	if got := f.downloadRequests(); !reflect.DeepEqual(got, want) {
		t.Fatalf("requests = %v, want %v", got, want)
	}
	files, _ := os.ReadDir(result.Folder)
	var names []string
	for _, entry := range files {
		names = append(names, entry.Name())
	}
	if !reflect.DeepEqual(names, []string{"Jose_Nunez_Martinez.md", indexFileName}) {
		t.Fatalf("files = %v", names)
	}
	if values := frontMatter(t, readText(t, filepath.Join(result.Folder, "Jose_Nunez_Martinez.md"))); values["answer_version"] != "14" {
		t.Errorf("answer_version = %q", values["answer_version"])
	}
}

func TestAnInvalidFormatOrNoConfirmationFailsBeforeAnyRequestOrWrite(t *testing.T) {
	f := newDownloadFixture(t)
	parent := t.TempDir()
	f.chosenFolder = parent
	f.choose(t)
	f.server.resetRequests()
	for _, mode := range []string{"", "Answers-Only", "answers_only", "answers-only ", "complete\n", "ANSWERS", "../x", "full"} {
		if _, err := f.DownloadSubmissionsWithMode(examID, testFolderName, mode, false, true); err == nil {
			t.Errorf("the format %q was accepted", mode)
		}
	}
	for _, mode := range []string{FormatComplete, FormatAnswersOnly} {
		if _, err := f.DownloadSubmissionsWithMode(examID, testFolderName, mode, false, false); err == nil {
			t.Errorf("a %s download without confirmation was accepted", mode)
		}
		if _, err := f.DownloadSubmissionsWithMode(examID, testFolderName, mode, true, false); err == nil {
			t.Errorf("a %s replacement without confirmation was accepted", mode)
		}
	}
	if got := f.downloadRequests(); len(got) != 0 {
		t.Errorf("rejected downloads asked the server: %v", got)
	}
	if entries, _ := os.ReadDir(parent); len(entries) != 0 {
		t.Errorf("rejected downloads wrote %d entries", len(entries))
	}
	if progress := f.DownloadProgress(); progress.Running || f.Snapshot().Busy {
		t.Errorf("a rejected download left work running: %+v", progress)
	}
}

func TestADownloadOnlyReadsFromTheServer(t *testing.T) {
	snapshot := func(f *downloadFixture) string {
		f.server.mu.Lock()
		defer f.server.mu.Unlock()
		data, err := json.Marshal(struct {
			Detail examDetail
			Subs   map[string]submission
			Roster Roster
		}{f.server.detail, f.server.subs, f.server.roster})
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	for _, mode := range []string{FormatComplete, FormatAnswersOnly} {
		f := newDownloadFixture(t)
		f.chosenFolder = t.TempDir()
		f.choose(t)
		before := snapshot(f)
		f.server.resetRequests()
		if _, err := f.DownloadSubmissionsWithMode(examID, "Out", mode, false, true); err != nil {
			t.Fatal(err)
		}
		for _, request := range f.server.requested() {
			if !strings.HasPrefix(request, "GET ") {
				t.Errorf("%s download made the request %q", mode, request)
			}
		}
		if after := snapshot(f); after != before {
			t.Errorf("the %s download changed what the server holds", mode)
		}
	}
}

func TestAnswersOnlyReportsFailuresMissingAndUnmappedFieldsWithoutLosingTheRest(t *testing.T) {
	f := newDownloadFixture(t)
	f.addFinishedWith(secondSession, "0015", "Second Student", map[string]string{"1.a": "7", "9.old": "LEGACY-ANSWER-BODY"})
	f.addFinishedWith(thirdSession, "0016", "Third Student", map[string]string{"1.a": "1"})
	f.failSubmission(thirdSession)
	parent := t.TempDir()
	f.chosenFolder = parent
	f.choose(t)
	// An earlier download left a file for the student whose fetch now fails.
	folder := filepath.Join(parent, answersFolder)
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(folder, "Third_Student.md")
	if err := os.WriteFile(stale, []byte("STALE EARLIER FILE"), 0o600); err != nil {
		t.Fatal(err)
	}
	result := f.answersOnly(t, answersFolder, true)
	if !reflect.DeepEqual(result.Files, []string{"Jose_Nunez_Martinez.md", "Second_Student.md"}) || len(result.Failed) != 1 || result.Failed[0].AccountNumber != "0016" {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Warnings) != 2 || !strings.Contains(result.Warnings[0], "1 student's file has answer fields with no saved value (Second Student)") ||
		!strings.Contains(result.Warnings[1], "1 student's file has saved answers that match no field of the exam (Second Student)") {
		t.Fatalf("warnings = %q", result.Warnings)
	}
	for _, line := range result.Warnings {
		if strings.Contains(line, "LEGACY-ANSWER-BODY") {
			t.Errorf("a warning shows an answer: %q", line)
		}
	}
	second := readText(t, filepath.Join(folder, "Second_Student.md"))
	for _, want := range []string{
		"**Field:** `1.code`  \n**Kind:** `code`  \n**Answer state:** `missing`\n\nNo saved value for this field.\n\n```mips title=\"1.code\"\n\n```\n",
		"## Unmapped saved answers\n",
		"**Saved key:** `\"9.old\"`  \n**Answer state:** `unmapped`\n\n```text title=\"unmapped:1\"\nLEGACY-ANSWER-BODY\n```\n",
	} {
		if !strings.Contains(second, want) {
			t.Errorf("the second student's file does not contain %q:\n%s", want, second)
		}
	}
	index := readText(t, filepath.Join(folder, indexFileName))
	for _, want := range []string{
		"export_format: \"answers-only\"\nexport_schema: 1\nexam: \"Exam one\"\nexam_id: \"" + examID + "\"\n",
		"This is an answers-only export",
		"do not hand it to students",
		"| Student | Account | Status | Submitted | Locks | Export | File |",
		"| José Ñúñez Martínez | 0012 | submitted | " + localStamp(at) + " | — | written | [Jose_Nunez_Martinez.md](Jose_Nunez_Martinez.md) |",
		"| Second Student | 0015 | time ran out | " + localStamp(at) + " | — | written | [Second_Student.md](Second_Student.md) |",
		"| Student Working | 0013 | still working | — | — | skipped | — |",
		"| Student Finishing | 0014 | still finishing | — | — | skipped | — |",
		"| Never There | 0003 | never logged in | — | — | — | — |",
		"## Fields to check",
		"| Second Student | 0015 | 1.code | \"9.old\" |",
	} {
		if !strings.Contains(index, want) {
			t.Errorf("_index.md does not contain %q:\n%s", want, index)
		}
	}
	// The failed student is not linked, and neither is the stale file; the stale file is left where it was, unlinked.
	failedRow := ""
	for _, line := range strings.Split(index, "\n") {
		if strings.HasPrefix(line, "| Third Student |") {
			failedRow = line
		}
	}
	if !strings.HasPrefix(failedRow, "| Third Student | 0016 | time ran out | "+localStamp(at)+" | — | failed: ") || !strings.HasSuffix(failedRow, " | — |") || strings.Contains(index, "Third_Student.md") {
		t.Errorf("the failed student's row is wrong: %q", failedRow)
	}
	if strings.Contains(index, "LEGACY-ANSWER-BODY") || strings.Contains(index, secret) {
		t.Errorf("_index.md shows an answer or a credential:\n%s", index)
	}
	if readText(t, stale) != "STALE EARLIER FILE" {
		t.Error("the stale file was touched")
	}
	// Progress is complete after a success, a skip and a failure, and Open folder is for the folder with the files.
	if progress := f.DownloadProgress(); progress.Running || progress.Total != 3 || progress.Done != 3 {
		t.Errorf("progress = %+v", progress)
	}
	if err := f.OpenDownloadFolder(); err != nil || len(f.opened) != 1 || f.opened[0] != folder {
		t.Errorf("open folder = %v, %v", f.opened, err)
	}
}

func TestAnAnswersOnlyDownloadThatWritesNoStudentFileOffersNoFolder(t *testing.T) {
	f := newDownloadFixture(t)
	f.failSubmission(finishedSession)
	f.chosenFolder = t.TempDir()
	f.choose(t)
	result := f.answersOnly(t, answersFolder, false)
	if len(result.Files) != 0 || len(result.Failed) != 1 {
		t.Fatalf("result = %+v", result)
	}
	if err := f.OpenDownloadFolder(); err == nil || len(f.opened) != 0 {
		t.Fatalf("a folder without a student file was opened: %v, %v", f.opened, err)
	}
	if index := readText(t, filepath.Join(result.Folder, indexFileName)); !strings.Contains(index, "| failed: ") || strings.Contains(index, "Jose_Nunez_Martinez.md") {
		t.Errorf("_index.md:\n%s", index)
	}
}

func TestAnswersOnlyFilesLeaveOutOperatorNotesLocksAndOtherStudents(t *testing.T) {
	f := newDownloadFixture(t)
	f.addFinishedWith(secondSession, "0777", "Other Student", map[string]string{"1.a": "OTHER-STUDENT-ANSWER", "1.code": "x"})
	f.server.mu.Lock()
	forced := f.server.subs[finishedSession]
	forced.Reason, forced.OperatorReason = "force_submit", "SECRET-PROCTOR-NOTE"
	f.server.subs[finishedSession] = forced
	f.server.roster.Sessions[0].Final.Reason, f.server.roster.Sessions[0].Final.OperatorReason = "force_submit", "SECRET-PROCTOR-NOTE"
	f.server.roster.Sessions[0].Locks = []LockRecord{
		{Type: "locked", At: at, Reason: "away"},
		{Type: "unlocked", At: at, OperatorReason: "UNLOCK-NOTE the student came back", LockedMillis: 420000, ExtendedMillis: 420000},
	}
	f.server.mu.Unlock()
	f.chosenFolder = t.TempDir()
	f.choose(t)
	complete, err := f.DownloadSubmissionsWithMode(examID, "Complete", FormatComplete, false, true)
	if err != nil {
		t.Fatal(err)
	}
	reference := readText(t, filepath.Join(complete.Folder, "Jose_Nunez_Martinez.md"))
	for _, want := range []string{"SECRET-PROCTOR-NOTE", "UNLOCK-NOTE", "times_locked: 1"} {
		if !strings.Contains(reference, want) {
			t.Fatalf("the complete file does not show %q, so this test would prove nothing:\n%s", want, reference)
		}
	}
	result := f.answersOnly(t, answersFolder, false)
	file := readText(t, filepath.Join(result.Folder, "Jose_Nunez_Martinez.md"))
	for _, unwanted := range []string{"SECRET-PROCTOR-NOTE", "UNLOCK-NOTE", "times_locked", "unlocks", "minutes_given_back", "locked", "Other Student", "0777", "OTHER-STUDENT-ANSWER", secret, "screenshot", "@"} {
		if strings.Contains(file, unwanted) {
			t.Errorf("the student's answers-only file contains %q:\n%s", unwanted, file)
		}
	}
	if values := frontMatter(t, file); values["status"] != "submitted by the proctor" {
		t.Errorf("status = %q", values["status"])
	}
	if index := readText(t, filepath.Join(result.Folder, indexFileName)); strings.Contains(index, "SECRET-PROCTOR-NOTE") || strings.Contains(index, "UNLOCK-NOTE") || strings.Contains(index, "OTHER-STUDENT-ANSWER") {
		t.Errorf("_index.md shows a note or an answer:\n%s", index)
	}
}

func TestBothFormatsGiveAStudentTheSameFileName(t *testing.T) {
	f := newDownloadFixture(t)
	// A second student whose name normalizes to the first's: both get their account number, in either format.
	f.addFinishedWith(secondSession, "0099", "Jose Nunez Martinez", map[string]string{"1.a": "2", "1.code": "y"})
	f.chosenFolder = t.TempDir()
	f.choose(t)
	complete, err := f.DownloadSubmissionsWithMode(examID, "Complete", FormatComplete, false, true)
	if err != nil {
		t.Fatal(err)
	}
	answers := f.answersOnly(t, "Answers", false)
	sort.Strings(complete.Files)
	sort.Strings(answers.Files)
	if !reflect.DeepEqual(complete.Files, []string{"Jose_Nunez_Martinez_0012.md", "Jose_Nunez_Martinez_0099.md"}) || !reflect.DeepEqual(answers.Files, complete.Files) {
		t.Fatalf("complete files %v, answers-only files %v", complete.Files, answers.Files)
	}
}

func TestReplacingAFolderStillNeedsASecondConfirmationAndNamesTheOtherFormat(t *testing.T) {
	f := newDownloadFixture(t)
	parent := t.TempDir()
	f.chosenFolder = parent
	f.choose(t)
	folder := filepath.Join(parent, answersFolder)
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	notes := filepath.Join(folder, "Jose_Nunez_Martinez.md")
	own := filepath.Join(folder, "my-notes.txt")
	// An index from before the formats existed has no format: it is a complete download.
	legacyIndex := "---\nexam: \"Exam one\"\nclass: \"Mathematics 10A\"\n---\n\n| Student |\n"
	for path, content := range map[string]string{notes: "my grading notes", own: "mine", filepath.Join(folder, indexFileName): legacyIndex} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	refused := f.answersOnly(t, answersFolder, false)
	if !refused.NeedsConfirmation || refused.Format != FormatAnswersOnly || refused.ExistingFormat != FormatComplete || refused.ReplaceCount != 2 || len(refused.Files) != 0 {
		t.Fatalf("refusal = %+v", refused)
	}
	if readText(t, notes) != "my grading notes" || readText(t, filepath.Join(folder, indexFileName)) != legacyIndex {
		t.Fatal("the refusal changed the earlier download")
	}
	replaced := f.answersOnly(t, answersFolder, true)
	if replaced.NeedsConfirmation || len(replaced.Files) != 1 || replaced.Format != FormatAnswersOnly {
		t.Fatalf("replacement = %+v", replaced)
	}
	if !strings.Contains(readText(t, notes), `export_format: "answers-only"`) || readText(t, own) != "mine" {
		t.Fatal("the replacement did not write its file, or touched one it did not write")
	}
	// The other way round: a complete download into an answers-only folder names that format too, and replaces nothing yet.
	back, err := f.DownloadSubmissionsWithMode(examID, answersFolder, FormatComplete, false, true)
	if err != nil || !back.NeedsConfirmation || back.Format != FormatComplete || back.ExistingFormat != FormatAnswersOnly || back.ReplaceCount != 2 {
		t.Fatalf("complete into answers-only = %+v, %v", back, err)
	}
	if !strings.Contains(readText(t, notes), `export_format: "answers-only"`) {
		t.Fatal("the refusal replaced an answers-only file")
	}
	// A folder with no index says nothing about a format.
	empty := filepath.Join(parent, "Empty")
	if err := os.MkdirAll(empty, 0o700); err != nil {
		t.Fatal(err)
	}
	if unknown := f.answersOnly(t, "Empty", false); !unknown.NeedsConfirmation || unknown.ExistingFormat != "" {
		t.Fatalf("an empty folder = %+v", unknown)
	}
}

func TestExistingExportFormatReadsOnlyTheFrontMatter(t *testing.T) {
	folder := t.TempDir()
	write := func(content string) {
		if err := os.WriteFile(filepath.Join(folder, indexFileName), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if got := existingExportFormat(folder); got != "" {
		t.Errorf("no index: %q", got)
	}
	for content, want := range map[string]string{
		"": FormatComplete,
		"just my notes\nexport_format: \"answers-only\"\n":         FormatComplete,
		"---\nexam: \"x\"\n---\n":                                  FormatComplete,
		"---\nexport_format: \"complete\"\n---\n":                  FormatComplete,
		"---\nexport_format: \"answers-only\"\n---\n":              FormatAnswersOnly,
		"---\r\nexport_format: \"answers-only\"\r\n---\r\n":        FormatAnswersOnly,
		"---\nexam: \"x\"\n---\nexport_format: \"answers-only\"\n": FormatComplete,
		"---\nexport_format: \"surprise\"\n---\n":                  FormatComplete,
	} {
		write(content)
		if got := existingExportFormat(folder); got != want {
			t.Errorf("index %q reads as %q, want %q", content, got, want)
		}
	}
	// An _index.md that is a folder is not read.
	other := t.TempDir()
	if err := os.Mkdir(filepath.Join(other, indexFileName), 0o700); err != nil {
		t.Fatal(err)
	}
	if got := existingExportFormat(other); got != "" {
		t.Errorf("an index folder reads as %q", got)
	}
}

func TestAnswersOnlyIsExportedForTheCourseShapedExam(t *testing.T) {
	var exercises []examdoc.Exercise
	for _, part := range []struct{ id, title, kind, language string }{{"1", "Regular expressions", examdoc.Text, ""}, {"2", "Code", examdoc.Code, "cpp"}} {
		exercise := examdoc.Exercise{ID: part.id, Title: part.title, Body: "<p>QUESTION-TEXT-" + part.id + "</p>"}
		for _, letter := range []string{"a", "b", "c"} {
			name := "regex"
			if part.kind == examdoc.Code {
				name = "code"
			}
			slot := examdoc.Slot{ID: part.id + letter + "." + name, Kind: part.kind, Language: part.language}
			if part.kind == examdoc.Code && letter != "c" {
				slot.Starter = "int f() {\n}\n"
			}
			exercise.Items = append(exercise.Items, examdoc.Item{ID: part.id + letter, Title: "Part " + letter, Body: "<p>QUESTION-TEXT-" + part.id + letter + "</p>", Slots: []examdoc.Slot{slot}})
		}
		exercises = append(exercises, exercise)
	}
	f := newDownloadFixture(t)
	f.setContent(examdoc.Content{Instructions: "QUESTION-TEXT-INSTRUCTIONS", Exercises: exercises})
	f.server.mu.Lock()
	sub := f.server.subs[finishedSession]
	sub.Version.Answers = map[string]string{"1a.regex": "0[bB]([01]+_?[01]+)", "1b.regex": "", "1c.regex": "[a-z]+", "2a.code": "int f() {\n}\n", "2b.code": "int f() {\n  return 1;\n}\r\n", "2c.code": ""}
	f.server.subs[finishedSession] = sub
	f.server.mu.Unlock()
	f.chosenFolder = t.TempDir()
	f.choose(t)
	result := f.answersOnly(t, answersFolder, false)
	if len(result.Files) != 1 || len(result.Warnings) != 0 {
		t.Fatalf("result = %+v", result)
	}
	file := readText(t, filepath.Join(result.Folder, "Jose_Nunez_Martinez.md"))
	for _, want := range []string{
		"```text title=\"1a.regex\"\n0[bB]([01]+_?[01]+)\n```\n",
		"**Answer state:** `empty`\n\nNo answer.\n\n```text title=\"1b.regex\"\n\n```\n",
		"**Answer state:** `starter-unchanged`\n\nThe saved value is the unchanged starter text.\n\n```cpp title=\"2a.code\"\nint f() {\n}\n\n```\n",
		"```cpp title=\"2b.code\"\nint f() {\n  return 1;\n}\r\n\n```\n",
		"**Answer state:** `empty`\n\nNo answer.\n\n```cpp title=\"2c.code\"\n\n```\n",
	} {
		if !strings.Contains(file, want) {
			t.Errorf("the file does not contain %q:\n%s", want, file)
		}
	}
	if strings.Contains(file, "QUESTION-TEXT") {
		t.Errorf("the file contains the exam's text:\n%s", file)
	}
	ids := regexpAll(`\*\*Field:\*\* `+"`"+`([^`+"`"+`]+)`+"`", file)
	if !reflect.DeepEqual(ids, []string{"1a.regex", "1b.regex", "1c.regex", "2a.code", "2b.code", "2c.code"}) {
		t.Errorf("fields = %v", ids)
	}
}

func TestAnswersOnlyReportsAnExamItCannotExportForEveryStudent(t *testing.T) {
	f := newDownloadFixture(t)
	f.addFinishedWith(secondSession, "0015", "Second Student", map[string]string{"1.a": "7", "1.code": "x"})
	f.setContent(examdoc.Content{Exercises: []examdoc.Exercise{{ID: "1", Title: "One", Slots: []examdoc.Slot{{ID: "1.a", Kind: examdoc.Text}, {ID: "1.a", Kind: examdoc.Text}}}}})
	f.chosenFolder = t.TempDir()
	f.choose(t)
	result := f.answersOnly(t, answersFolder, false)
	if len(result.Files) != 0 || len(result.Failed) != 2 || !strings.Contains(result.Failed[0].Error, `two answer fields with the ID "1.a"`) {
		t.Fatalf("result = %+v", result)
	}
	entries, _ := os.ReadDir(result.Folder)
	if len(entries) != 1 || entries[0].Name() != indexFileName {
		t.Errorf("a file was written for an exam that cannot be exported: %v", entries)
	}
}

func TestAnswersOnlyDoesNotTurnAMissingVersionIntoAnEmptySubmission(t *testing.T) {
	f := newDownloadFixture(t)
	f.server.mu.Lock()
	sub := f.server.subs[finishedSession]
	sub.Version = nil
	f.server.subs[finishedSession] = sub
	f.server.mu.Unlock()
	f.chosenFolder = t.TempDir()
	f.choose(t)
	result := f.answersOnly(t, answersFolder, false)
	if len(result.Files) != 0 || len(result.Failed) != 1 || result.Failed[0].Error != "The server's submission is incomplete." {
		t.Fatalf("result = %+v", result)
	}
}

func TestAnswersOnlyIndexIsDeterministic(t *testing.T) {
	roster := Roster{ExamID: examID, Sessions: []Session{
		{ID: workingSession, Student: Student{"2", "Zamora"}, Status: "active"},
		{ID: finishedSession, Student: Student{"4", "álvaro"}, Status: "submitted", Final: &FinalSummary{SubmittedAt: at, Reason: "explicit_submit"}},
	}, Missing: []Student{{"5", "Alonso"}, {"3", "beltrán"}}}
	names := map[string]string{"4": "alvaro.md"}
	outcomes := map[string]exportOutcome{"4": {written: true, warnings: []examdoc.AnswersOnlyWarning{{Kind: examdoc.WarnMissing, Field: "1.b"}, {Kind: examdoc.WarnMissing, Field: "1.a"}, {Kind: examdoc.WarnUnmapped, Field: "k\"ey|x"}}}}
	first, err := renderAnswersOnlyIndex(examDetail{ExamID: examID, Title: "Exam", ClassName: "Class"}, roster, names, outcomes, at)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		again, _ := renderAnswersOnlyIndex(examDetail{ExamID: examID, Title: "Exam", ClassName: "Class"}, roster, names, outcomes, at)
		if string(again) != string(first) {
			t.Fatal("the index differs between renderings of the same input")
		}
	}
	last := -1
	for _, name := range []string{"| Alonso |", "| álvaro |", "| beltrán |", "| Zamora |"} {
		position := strings.Index(string(first), name)
		if position <= last {
			t.Fatalf("%q is out of order in:\n%s", name, first)
		}
		last = position
	}
	// The field list keeps the order of the exam, and a quoted key cannot split the table row.
	if !strings.Contains(string(first), `| álvaro | 4 | 1.b, 1.a | "k\\"ey\|x" |`) {
		t.Errorf("fields to check:\n%s", first)
	}
}

func TestYAMLTextEscapesWhatYAMLCannotHoldRaw(t *testing.T) {
	cases := map[string]string{
		"plain":                `"plain"`,
		`quote " and \ slash`:  `"quote \" and \\ slash"`,
		"line\nbreak\r\ttab":   `"line\nbreak\r\ttab"`,
		"nul\x00bell\a":        `"nul\u0000bell\u0007"`,
		"next\u0085line\u2028": `"next\u0085line\u2028"`,
		"é ñ 日本 😀":             `"é ñ 日本 😀"`,
		"\xff":                 `"\uFFFD"`,
		"\u00a0":               `"\u00A0"`,
		"":                     `""`,
	}
	for in, want := range cases {
		if got := yamlText(in); got != want {
			t.Errorf("yamlText(%q) = %s, want %s", in, got, want)
		}
	}
}

// regexpAll is the first group of every match of pattern in text.
func regexpAll(pattern, text string) []string {
	var found []string
	for _, match := range regexp.MustCompile(pattern).FindAllStringSubmatch(text, -1) {
		found = append(found, match[1])
	}
	return found
}
