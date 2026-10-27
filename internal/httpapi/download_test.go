package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"exam-platform/internal/domain"
	"exam-platform/internal/service"
)

// examBodyWithSource is the test exam document with the Markdown source the proctor now sends with it.
func examBodyWithSource(source string) string {
	encoded, err := json.Marshal(source)
	if err != nil {
		panic(err)
	}
	return strings.Replace(examBody, `"content":`, `"source_markdown":`+string(encoded)+`,"content":`, 1)
}

func TestImportKeepsTheMarkdownSourceAndStudentsNeverReadIt(t *testing.T) {
	source := "## Pregunta\n\nEscriba el resultado: {{q1}}\n"
	f := newAPIFixture(t)
	exams := service.NewExams(f.store, f.clock, f.sessions)
	options := Options{AdminToken: testAdminToken, Exams: exams, Proctor: f.sessions}
	f.router = New(f.clock, f.sessions, f.logger, options)
	created := request(f.router, "POST", examPath, examBodyWithSource(source), "Bearer "+testAdminToken)
	if created.Code != http.StatusCreated {
		t.Fatalf("creation = %d %s", created.Code, created.Body.String())
	}
	id := decoded[struct {
		ID string `json:"exam_id"`
	}](t, created.Body.Bytes()).ID

	detail := request(f.router, "GET", "/admin/exam/"+id, "", "Bearer "+testAdminToken)
	exam := decoded[service.ExamDetail](t, detail.Body.Bytes())
	if detail.Code != http.StatusOK || exam.ExamID != id || exam.SourceMarkdown == nil || *exam.SourceMarkdown != source {
		t.Fatalf("exam detail = %d %s", detail.Code, detail.Body.String())
	}
	if exam.ClassName != "Test class" || len(exam.Content.SlotExercises()) != 2 || exam.Navigation != domain.FreeNavigation {
		t.Fatalf("exam detail is incomplete: %+v", exam)
	}

	f.publish(t, id)
	login := f.login(t, "0012")
	student := request(f.router, "GET", "/exam", "", "Bearer "+login.SessionToken)
	if student.Code != http.StatusOK {
		t.Fatalf("student exam = %d", student.Code)
	}
	if strings.Contains(student.Body.String(), "source_markdown") || strings.Contains(student.Body.String(), "Escriba el resultado") {
		t.Fatal("the student's exam response returned the Markdown source")
	}

	if w := request(f.router, "GET", "/admin/exam/99999999-9999-4999-8999-999999999999", "", "Bearer "+testAdminToken); w.Code != http.StatusNotFound {
		t.Fatalf("unknown exam = %d", w.Code)
	}
	if w := request(f.router, "GET", "/admin/exam/not-a-uuid", "", "Bearer "+testAdminToken); w.Code != http.StatusBadRequest {
		t.Fatalf("invalid exam id = %d", w.Code)
	}
}

func TestExamSourceSizeAndUTF8Checks(t *testing.T) {
	f := newAPIFixture(t)
	exams := service.NewExams(f.store, f.clock, f.sessions)
	input := func(source *string) service.CreateExamInput {
		return service.CreateExamInput{Title: "Source", PIN: "p", DurationSeconds: 60, SourceMarkdown: source, Content: examContentForTest()}
	}
	tooLarge := strings.Repeat("a", service.MaxSourceMarkdownBytes+1)
	invalid := string([]byte{0xff, 0xfe})
	for _, source := range []*string{&tooLarge, &invalid} {
		if _, err := exams.Preview(t.Context(), input(source)); err == nil {
			t.Fatalf("source of %d bytes was accepted", len(*source))
		}
	}
	valid := "## Q\n\n{{q1}}"
	if _, err := exams.Preview(t.Context(), input(&valid)); err != nil {
		t.Fatalf("valid source was refused: %v", err)
	}
	empty := ""
	if created, err := exams.Create(t.Context(), apiClassID, input(&empty)); err != nil || created == "" {
		t.Fatalf("empty source was refused: %v", err)
	}
	stored, err := exams.Detail(t.Context(), mustCreate(t, exams, input(&valid)))
	if err != nil || stored.SourceMarkdown == nil || *stored.SourceMarkdown != valid {
		t.Fatalf("stored source = %+v, %v", stored.SourceMarkdown, err)
	}
}

func TestOversizeBodiesNameTheirCause(t *testing.T) {
	f := newPublishFixture(t)
	// An imported exam names its source as part of what must fit, and the limit it was held to.
	w := f.json("POST", examPath, examBodyWithSource(strings.Repeat("a", 1<<20)))
	problem := decoded[struct {
		Error    string   `json:"error"`
		Problems []string `json:"problems"`
	}](t, w.Body.Bytes())
	if w.Code != http.StatusRequestEntityTooLarge || problem.Error != "request body too large" || len(problem.Problems) != 1 || !strings.Contains(problem.Problems[0], "source_markdown") || !strings.Contains(problem.Problems[0], "1048576") {
		t.Fatalf("oversize import = %d %s", w.Code, w.Body.String())
	}
	// Other administrator documents keep the plain message: the source has nothing to do with them.
	w = f.json("PATCH", "/admin/class/"+apiClassID, `{"name":"`+strings.Repeat("n", 3000)+`"}`)
	if w.Code != http.StatusRequestEntityTooLarge || strings.Contains(w.Body.String(), "source_markdown") || strings.Contains(w.Body.String(), "max-json-bytes") || !strings.Contains(w.Body.String(), "request body too large") {
		t.Fatalf("oversize class rename = %d %s", w.Code, w.Body.String())
	}
}

func mustCreate(t *testing.T, exams *service.Exams, input service.CreateExamInput) string {
	t.Helper()
	id, err := exams.Create(t.Context(), apiClassID, input)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// examContentForTest is one exercise with one blank, q1.
func examContentForTest() domain.ExamContent {
	return domain.ExamContent{Exercises: []domain.Exercise{{ID: "1", Title: "T", Body: `<span data-slot="q1"></span>`, Slots: []domain.Slot{{ID: "q1", Kind: domain.BlankSlot}}}}}
}

func TestSubmissionForActiveSavedFinalAndPendingSessions(t *testing.T) {
	t.Run("active and never saved", func(t *testing.T) {
		f, id := proctorFixture(t)
		login := f.login(t, "0012")
		reply := request(f.router, "GET", "/admin/session/"+login.SessionID+"/submission", "", "Bearer "+testAdminToken)
		sub := decoded[service.Submission](t, reply.Body.Bytes())
		if reply.Code != http.StatusOK || sub.SessionID != login.SessionID || sub.ExamID != id || sub.AccountNumber != "0012" || sub.StudentName != "Ada" || sub.Final || sub.Version != nil || sub.Status != domain.Active || sub.SubmittedAt != nil {
			t.Fatalf("never-saved submission = %d %s", reply.Code, reply.Body.String())
		}
	})
	t.Run("saved", func(t *testing.T) {
		f, _ := proctorFixture(t)
		login := f.login(t, "0012")
		if w := request(f.router, "POST", "/autosave", `{"answers":{"q1":"saved 世界"}}`, "Bearer "+login.SessionToken); w.Code != http.StatusOK {
			t.Fatalf("autosave = %d", w.Code)
		}
		reply := request(f.router, "GET", "/admin/session/"+login.SessionID+"/submission", "", "Bearer "+testAdminToken)
		sub := decoded[service.Submission](t, reply.Body.Bytes())
		if reply.Code != http.StatusOK || sub.Final || sub.Version == nil || sub.Version.Answers["q1"] != "saved 世界" || sub.Version.Sequence != 1 || sub.Status != domain.Active {
			t.Fatalf("saved submission = %s", reply.Body.String())
		}
	})
	t.Run("submitted by the student", func(t *testing.T) {
		f, _ := proctorFixture(t)
		login := f.login(t, "0012")
		if w := request(f.router, "POST", "/submit", `{"answers":{"q1":"final"}}`, "Bearer "+login.SessionToken); w.Code != http.StatusOK {
			t.Fatalf("submit = %d %s", w.Code, w.Body.String())
		}
		reply := request(f.router, "GET", "/admin/session/"+login.SessionID+"/submission", "", "Bearer "+testAdminToken)
		sub := decoded[service.Submission](t, reply.Body.Bytes())
		if reply.Code != http.StatusOK || !sub.Final || sub.Status != domain.Submitted || sub.Reason != "explicit_submit" || sub.SubmittedAt == nil || sub.Version == nil || sub.Version.Answers["q1"] != "final" {
			t.Fatalf("submitted submission = %s", reply.Body.String())
		}
	})
	t.Run("submitted by the proctor", func(t *testing.T) {
		f, _ := proctorFixture(t)
		login := f.login(t, "0012")
		if w := request(f.router, "POST", "/autosave", `{"answers":{"q1":"before the force"}}`, "Bearer "+login.SessionToken); w.Code != http.StatusOK {
			t.Fatalf("autosave = %d", w.Code)
		}
		if w := request(f.router, "POST", "/admin/force-submit/"+login.SessionID, `{"reason":"left the room"}`, "Bearer "+testAdminToken); w.Code != http.StatusOK {
			t.Fatalf("force submit = %d %s", w.Code, w.Body.String())
		}
		reply := request(f.router, "GET", "/admin/session/"+login.SessionID+"/submission", "", "Bearer "+testAdminToken)
		sub := decoded[service.Submission](t, reply.Body.Bytes())
		if reply.Code != http.StatusOK || !sub.Final || sub.Status != domain.ForceSubmitted || sub.Reason != "force_submit" || sub.OperatorReason != "left the room" || sub.Version == nil || sub.Version.Answers["q1"] != "before the force" {
			t.Fatalf("force-submitted submission = %s", reply.Body.String())
		}
	})
	t.Run("submitted when time ran out", func(t *testing.T) {
		f, _ := proctorFixture(t)
		login := f.login(t, "0012")
		if w := request(f.router, "POST", "/autosave", `{"answers":{"q1":"at the deadline"}}`, "Bearer "+login.SessionToken); w.Code != http.StatusOK {
			t.Fatalf("autosave = %d", w.Code)
		}
		f.clock.Set(login.DeadlineAt.Add(30 * time.Second))
		// Any proctor action on the session finalizes an expired one; the read itself never does.
		request(f.router, "POST", "/admin/force-submit/"+login.SessionID, `{"reason":"too late"}`, "Bearer "+testAdminToken)
		reply := request(f.router, "GET", "/admin/session/"+login.SessionID+"/submission", "", "Bearer "+testAdminToken)
		sub := decoded[service.Submission](t, reply.Body.Bytes())
		if reply.Code != http.StatusOK || !sub.Final || sub.Status != domain.Submitted || sub.Reason != "grace_expired" || sub.OperatorReason != "" || sub.Version == nil || sub.Version.Answers["q1"] != "at the deadline" {
			t.Fatalf("expired submission = %s", reply.Body.String())
		}
	})
	t.Run("past grace, not finalized yet", func(t *testing.T) {
		f, _ := proctorFixture(t)
		start := f.clock.Now()
		login := f.login(t, "0012")
		if w := request(f.router, "POST", "/autosave", `{"answers":{"q1":"late"}}`, "Bearer "+login.SessionToken); w.Code != http.StatusOK {
			t.Fatalf("autosave = %d", w.Code)
		}
		f.clock.Set(start.Add(95 * time.Second)) // the 60 s exam plus 30 s grace are over; the worker has not run
		reply := request(f.router, "GET", "/admin/session/"+login.SessionID+"/submission", "", "Bearer "+testAdminToken)
		sub := decoded[service.Submission](t, reply.Body.Bytes())
		if reply.Code != http.StatusOK || sub.Final || sub.Status != domain.Grace || sub.Version == nil || sub.Version.Answers["q1"] != "late" {
			t.Fatalf("pending submission = %s", reply.Body.String())
		}
	})
	t.Run("unknown and invalid sessions", func(t *testing.T) {
		f, _ := proctorFixture(t)
		if w := request(f.router, "GET", "/admin/session/99999999-9999-4999-8999-999999999999/submission", "", "Bearer "+testAdminToken); w.Code != http.StatusNotFound {
			t.Fatalf("unknown session = %d", w.Code)
		}
		if w := request(f.router, "GET", "/admin/session/not-a-uuid/submission", "", "Bearer "+testAdminToken); w.Code != http.StatusBadRequest {
			t.Fatalf("invalid session = %d", w.Code)
		}
	})
}
