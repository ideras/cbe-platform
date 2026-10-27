package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"exam-platform/internal/service"
)

const csvHeader = "canvas_id,account_number,name,email,sis_user_id\n"

type publishFixture struct {
	*apiFixture
	token string
}

func newPublishFixture(t *testing.T) *publishFixture {
	t.Helper()
	f := newUnpublishedAPIFixture(t)
	exams := service.NewExams(f.store, f.clock, f.sessions)
	f.sessions = testSessions(f.store, f.clock)
	f.router = New(f.clock, f.sessions, f.logger, Options{AdminToken: testAdminToken, Exams: exams, Publisher: service.NewPublisher(f.store, f.clock),
		Classes: service.NewClasses(f.store, f.clock), MaxRosterBytes: 4 << 10})
	return &publishFixture{f, "Bearer " + testAdminToken}
}

func (f *publishFixture) send(method, path, contentType, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	r.Header.Set("Authorization", f.token)
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, r)
	return w
}

func (f *publishFixture) json(method, path, body string) *httptest.ResponseRecorder {
	return f.send(method, path, "application/json", body)
}

func TestPublishingAndClassRoutesNeedTheAdminCredential(t *testing.T) {
	f := newPublishFixture(t)
	for _, route := range [][2]string{{"GET", "/admin/exams"}, {"POST", "/admin/exam/" + apiExamID + "/publish"}, {"POST", "/admin/unpublish"}, {"GET", "/admin/classes"}, {"POST", "/admin/classes"}, {"GET", "/admin/class/" + apiClassID + "/students"}, {"PATCH", "/admin/class/" + apiClassID}, {"DELETE", "/admin/class/" + apiClassID}, {"PUT", "/admin/class/" + apiClassID + "/roster"}, {"POST", examPath}, {"DELETE", "/admin/exam/" + apiExamID}} {
		for _, authorization := range []string{"", "Bearer wrong", "Bearer " + strings.Repeat("s", 64)} {
			if w := request(f.router, route[0], route[1], `{}`, authorization); w.Code != http.StatusUnauthorized {
				t.Errorf("%s %s with %q = %d", route[0], route[1], authorization, w.Code)
			}
		}
	}
}

func TestPublishFlowOverHTTP(t *testing.T) {
	f := newPublishFixture(t)
	if w := request(f.router, "POST", "/login", `{"account_number":"0012","exam_pin":"exam-secret"}`, ""); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("login before publishing = %d", w.Code)
	}
	w := f.json("GET", "/admin/exams", "")
	catalog := decoded[struct {
		CurrentExamID    string `json:"current_exam_id"`
		CurrentClassID   string `json:"current_class_id"`
		CurrentClassName string `json:"current_class_name"`
		Exams            []struct {
			ID        string `json:"exam_id"`
			ClassID   string `json:"class_id"`
			ClassName string `json:"class_name"`
			Title     string `json:"title"`
			Published bool   `json:"published"`
		} `json:"exams"`
	}](t, w.Body.Bytes())
	if w.Code != http.StatusOK || len(catalog.Exams) != 1 || catalog.CurrentExamID != "" || catalog.CurrentClassID != "" || catalog.CurrentClassName != "" || catalog.Exams[0].Published ||
		catalog.Exams[0].ClassID != apiClassID || catalog.Exams[0].ClassName != "Test class" {
		t.Fatalf("catalog = %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "exam-secret") || strings.Contains(w.Body.String(), `"pin"`) || strings.Contains(w.Body.String(), `"content"`) {
		t.Fatalf("catalog exposes exam content: %s", w.Body.String())
	}

	const missing = "00000000-0000-4000-8000-0000000000ff"
	publish := "/admin/exam/" + apiExamID + "/publish"
	// The class belongs to the exam now, so a request that still names one is a request from an old client.
	for _, body := range []string{`{"unknown":1}`, `{"class_id":"` + apiClassID + `"}`, `{"expected_class":"` + apiClassID + `"}`} {
		if w := f.json("POST", publish, body); w.Code != http.StatusBadRequest {
			t.Fatalf("%s = %d", body, w.Code)
		}
	}
	if w := f.json("POST", "/admin/exam/not-an-id/publish", `{}`); w.Code != http.StatusBadRequest {
		t.Fatalf("bad id = %d", w.Code)
	}
	if w := f.json("POST", publish, `{"expected_current":"bad"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("bad expected exam = %d", w.Code)
	}
	if w := f.json("POST", "/admin/exam/"+missing+"/publish", `{}`); w.Code != http.StatusNotFound {
		t.Fatalf("unknown exam = %d", w.Code)
	}
	if w := f.json("POST", publish, `{}`); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"current_class_id":"`+apiClassID+`"`) {
		t.Fatalf("publish = %d %s", w.Code, w.Body.String())
	}
	if w := f.json("GET", "/admin/exams", ""); !strings.Contains(w.Body.String(), `"current_class_name":"Test class"`) {
		t.Fatalf("catalog does not name the class: %s", w.Body.String())
	}
	login := f.login(t, "0012")
	if login.SessionToken == "" {
		t.Fatal("published exam does not admit students")
	}

	second := createExam(t, f, `{"title":"Second","pin":"second-pin","duration_seconds":60,`+oneSlotContent+`}`)
	seen := `"expected_current":"` + apiExamID + `"`
	if w := f.json("POST", "/admin/exam/"+second+"/publish", `{}`); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "changed") {
		t.Fatalf("stale publish = %d %s", w.Code, w.Body.String())
	}
	w = f.json("POST", "/admin/exam/"+second+"/publish", `{`+seen+`}`)
	body := decoded[struct {
		Error          string `json:"error"`
		ActiveSessions int    `json:"active_sessions"`
	}](t, w.Body.Bytes())
	if w.Code != http.StatusConflict || body.ActiveSessions != 1 {
		t.Fatalf("publish over a working student = %d %s", w.Code, w.Body.String())
	}
	if w := f.json("POST", "/admin/exam/"+second+"/publish", `{`+seen+`,"allow_active":true}`); w.Code != http.StatusOK ||
		!strings.Contains(w.Body.String(), `"replaced_exam_id":"`+apiExamID+`"`) {
		t.Fatalf("confirmed publish = %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(f.logs.String(), `"msg":"exam published"`) {
		t.Fatal("publishing was not logged")
	}
	if w := f.json("POST", "/admin/unpublish", `{"expected_current":"`+second+`","expected_class":"`+apiClassID+`"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("unpublish naming a class = %d", w.Code)
	}
	if w := f.json("POST", "/admin/unpublish", `{"expected_current":"`+second+`"}`); w.Code != http.StatusOK {
		t.Fatalf("unpublish = %d %s", w.Code, w.Body.String())
	}
	if w := request(f.router, "POST", "/login", `{"account_number":"0034","exam_pin":"second-pin"}`, ""); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("login after unpublishing = %d", w.Code)
	}
}

func createExam(t *testing.T, f *publishFixture, body string) string {
	t.Helper()
	w := f.json("POST", examPath, body)
	if w.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", w.Code, w.Body.String())
	}
	return decoded[struct {
		ID string `json:"exam_id"`
	}](t, w.Body.Bytes()).ID
}

func TestExamDryRunAndProblemReports(t *testing.T) {
	f := newPublishFixture(t)
	before := len(decoded[struct {
		Exams []struct{} `json:"exams"`
	}](t, f.json("GET", "/admin/exams", "").Body.Bytes()).Exams)

	w := f.json("POST", examPath+"?dry_run=true", examBody)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"exercise_count":1,"slot_count":2,"navigation":"free"`) || strings.Contains(w.Body.String(), "exam-secret") {
		t.Fatalf("dry run = %d %s", w.Code, w.Body.String())
	}
	after := len(decoded[struct {
		Exams []struct{} `json:"exams"`
	}](t, f.json("GET", "/admin/exams", "").Body.Bytes()).Exams)
	if before != after {
		t.Fatal("a dry run stored an exam")
	}

	w = f.json("POST", examPath+"?dry_run=true", `{"title":"","pin":"hunter2-pin","duration_seconds":0,"content":{"exercises":[{"id":"a","title":"x"},{"id":"a","title":"y"}]}}`)
	problems := decoded[struct {
		Error    string   `json:"error"`
		Problems []string `json:"problems"`
	}](t, w.Body.Bytes())
	if w.Code != http.StatusBadRequest || len(problems.Problems) != 5 || !strings.Contains(w.Body.String(), "exercise 2 (a): id is already used") || strings.Contains(w.Body.String(), "hunter2-pin") {
		t.Fatalf("problems = %d %s", w.Code, w.Body.String())
	}
	if w := f.json("POST", examPath, `{"title":"","pin":"p","duration_seconds":1,"content":{"exercises":[]}}`); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "title is required") {
		t.Fatalf("create problems = %d %s", w.Code, w.Body.String())
	}

	for body, want := range map[string]string{
		`{"title":"T","pin":"p","durations":5,"content":{"exercises":[]}}`:           `unknown field \"durations\"`,
		`{"title":"T","pin":"p","duration_seconds":"60","content":{"exercises":[]}}`: "field duration_seconds",
		`{"title":"T",`: "unexpected EOF",
		`{"title":"T","pin":"p","duration_seconds":60,"content":{"exercises":[]}} {}`:                   "unexpected data",
		`{"title":"T","pin":"p","duration_seconds":60,"min_volume_percent":1.5,` + oneSlotContent + `}`: "min_volume_percent",
	} {
		w := f.json("POST", examPath+"?dry_run=true", body)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), want) || strings.Contains(w.Body.String(), "CreateExamInput") {
			t.Errorf("%s -> %d %s (want %q)", body, w.Code, w.Body.String(), want)
		}
	}
	if w := f.json("POST", examPath+"?dry_run=maybe", examBody); w.Code != http.StatusBadRequest {
		t.Fatalf("bad dry_run value = %d", w.Code)
	}
	if w := f.json("POST", examPath+"?other=1", examBody); w.Code != http.StatusBadRequest {
		t.Fatalf("unknown query = %d", w.Code)
	}
	if w := f.send("POST", examPath, "text/plain", examBody); w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("content type = %d", w.Code)
	}
}

func TestClassesOverHTTP(t *testing.T) {
	f := newPublishFixture(t)
	w := f.json("GET", "/admin/classes", "")
	list := decoded[struct {
		Classes []struct {
			ID            string `json:"class_id"`
			Name          string `json:"name"`
			StudentCount  int    `json:"student_count"`
			ExamCount     int    `json:"exam_count"`
			PublishedExam any    `json:"published_exam"`
		} `json:"classes"`
	}](t, w.Body.Bytes())
	if w.Code != http.StatusOK || len(list.Classes) != 1 || list.Classes[0].ID != apiClassID || list.Classes[0].Name != "Test class" || list.Classes[0].StudentCount != 2 || list.Classes[0].ExamCount != 1 || list.Classes[0].PublishedExam != nil {
		t.Fatalf("list = %d %s", w.Code, w.Body.String())
	}

	document := csvHeader + "9,0099,Linus,linus@example.invalid,SIS-9\n1,0034,Grace B,g@example.invalid,S2\n,,,,\n"
	w = f.send("POST", "/admin/classes?dry_run=true", "text/csv; charset=utf-8", document)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"students":2`) || !strings.Contains(w.Body.String(), `"applied":false`) || !strings.Contains(w.Body.String(), `"class":null`) {
		t.Fatalf("dry run = %d %s", w.Code, w.Body.String())
	}
	if w := f.json("GET", "/admin/classes", ""); strings.Count(w.Body.String(), `"class_id"`) != 1 {
		t.Fatalf("a dry run stored a class: %s", w.Body.String())
	}
	w = f.send("POST", "/admin/classes?name=History%2011B", "text/csv", document)
	created := decoded[struct {
		Applied bool `json:"applied"`
		Class   struct {
			ID           string `json:"class_id"`
			Name         string `json:"name"`
			StudentCount int    `json:"student_count"`
		} `json:"class"`
	}](t, w.Body.Bytes())
	if w.Code != http.StatusCreated || !created.Applied || created.Class.Name != "History 11B" || created.Class.StudentCount != 2 || created.Class.ID == "" {
		t.Fatalf("create = %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(f.logs.String(), `"msg":"class created"`) || strings.Contains(f.logs.String(), "Linus") || strings.Contains(f.logs.String(), "example.invalid") {
		t.Fatalf("log = %s", f.logs.String())
	}

	w = f.json("GET", "/admin/class/"+created.Class.ID+"/students", "")
	want := `{"class_id":"` + created.Class.ID + `","name":"History 11B","total":2,"students":[{"account_number":"0034","student_name":"Grace B"},{"account_number":"0099","student_name":"Linus"}]}` + "\n"
	if w.Code != http.StatusOK || w.Body.String() != want {
		t.Fatalf("students = %d %q", w.Code, w.Body.String())
	}
	for _, secret := range []string{"example.invalid", "SIS-9", "canvas"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatalf("the roster leaked %q", secret)
		}
	}
	if w := f.json("GET", "/admin/class/"+apiClassID+"/students", ""); !strings.Contains(w.Body.String(), `"student_name":"Ada"`) || strings.Contains(w.Body.String(), "ada@example.invalid") {
		t.Fatalf("seeded roster = %s", w.Body.String())
	}
	if w := f.json("GET", "/admin/class/"+apiClassID+"/students?extra=1", ""); w.Code != http.StatusBadRequest {
		t.Fatalf("unknown query = %d", w.Code)
	}
	if w := f.json("GET", "/admin/class/not-an-id/students", ""); w.Code != http.StatusBadRequest {
		t.Fatalf("bad id = %d", w.Code)
	}
	if w := f.json("GET", "/admin/class/00000000-0000-4000-8000-0000000000ff/students", ""); w.Code != http.StatusNotFound {
		t.Fatalf("unknown class = %d", w.Code)
	}
}

func TestClassUploadRejectionsAndLimits(t *testing.T) {
	f := newPublishFixture(t)
	document := csvHeader + "1,0099,Linus,l@x.invalid,S3\n"
	if w := f.send("POST", "/admin/classes?name=test%20CLASS", "text/csv", document); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "already exists") {
		t.Fatalf("duplicate name = %d %s", w.Code, w.Body.String())
	}
	if w := f.send("POST", "/admin/classes?dry_run=true&name=Test%20Class", "text/csv", document); w.Code != http.StatusConflict {
		t.Fatalf("duplicate name on a dry run = %d", w.Code)
	}
	if w := f.send("POST", "/admin/classes", "text/csv", document); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "class name is required") {
		t.Fatalf("missing name = %d %s", w.Code, w.Body.String())
	}
	if w := f.send("POST", "/admin/classes?name=A", "application/json", document); w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("json body = %d", w.Code)
	}
	if w := f.send("POST", "/admin/classes?name=A", "", document); w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("no content type = %d", w.Code)
	}
	if w := f.send("POST", "/admin/classes?name=A", "text/csv", csvHeader); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "no usable") {
		t.Fatalf("empty roster = %d %s", w.Code, w.Body.String())
	}
	if w := f.send("POST", "/admin/classes?name=A", "text/csv", "account_number\n1\n"); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "missing required column") {
		t.Fatalf("missing column = %d %s", w.Code, w.Body.String())
	}
	if w := f.send("POST", "/admin/classes?name=A", "text/csv", csvHeader+strings.Repeat("1,0001,Name,e@x.invalid,S\n", 400)); w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized roster = %d", w.Code)
	}
	if w := f.send("POST", "/admin/classes?name=A&extra=1", "text/csv", document); w.Code != http.StatusBadRequest {
		t.Fatalf("unknown query = %d", w.Code)
	}
	if w := f.send("POST", "/admin/classes?name=A&dry_run=maybe", "text/csv", document); w.Code != http.StatusBadRequest {
		t.Fatalf("bad dry_run = %d", w.Code)
	}
	if w := f.json("GET", "/admin/classes", ""); strings.Count(w.Body.String(), `"class_id"`) != 1 {
		t.Fatalf("rejected uploads created a class: %s", w.Body.String())
	}
}

func TestOnlyTheStudentsOfTheExamsClassCanLogInOverHTTP(t *testing.T) {
	f := newPublishFixture(t)
	w := f.send("POST", "/admin/classes?name=History", "text/csv", csvHeader+"1,0077,Linus,l@x.invalid,S3\n")
	other := decoded[struct {
		Class struct {
			ID string `json:"class_id"`
		} `json:"class"`
	}](t, w.Body.Bytes()).Class.ID
	historyExam := createExamIn(t, f, other, examBody)
	if w := f.json("POST", "/admin/exam/"+historyExam+"/publish", `{}`); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"current_class_id":"`+other+`"`) {
		t.Fatalf("publish = %d %s", w.Code, w.Body.String())
	}
	if w := request(f.router, "POST", "/login", `{"account_number":"0012","exam_pin":"exam-secret"}`, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("a student of another class logged in: %d", w.Code)
	}
	if w := request(f.router, "POST", "/login", `{"account_number":"0077","exam_pin":"exam-secret"}`, ""); w.Code != http.StatusOK {
		t.Fatalf("a student of the exam's class was refused: %d %s", w.Code, w.Body.String())
	}
	if w := f.json("GET", "/admin/classes", ""); !strings.Contains(w.Body.String(), `"published_exam":{"exam_id":"`+historyExam+`","title":"Unicode exam"}`) || strings.Count(w.Body.String(), `"published_exam":null`) != 1 {
		t.Fatalf("the class list does not show the publication: %s", w.Body.String())
	}
}

// createExamIn imports an exam document into a class.
func createExamIn(t *testing.T, f *publishFixture, classID, body string) string {
	t.Helper()
	w := f.json("POST", "/admin/class/"+classID+"/exam", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", w.Code, w.Body.String())
	}
	return decoded[struct {
		ID string `json:"exam_id"`
	}](t, w.Body.Bytes()).ID
}

func TestAnExamIsImportedIntoAClassAndDeletedOverHTTP(t *testing.T) {
	f := newPublishFixture(t)
	const missing = "00000000-0000-4000-8000-0000000000ff"
	// The route names the class; the document does not, and the old route is gone.
	if w := f.json("POST", "/admin/class/"+missing+"/exam", examBody); w.Code != http.StatusNotFound {
		t.Fatalf("import into an unknown class = %d %s", w.Code, w.Body.String())
	}
	if w := f.json("POST", "/admin/class/not-an-id/exam", examBody); w.Code != http.StatusBadRequest {
		t.Fatalf("import into a bad class id = %d", w.Code)
	}
	if w := f.json("POST", "/admin/exam", examBody); w.Code != http.StatusNotFound {
		t.Fatalf("the old import route answered %d", w.Code)
	}
	if w := f.json("POST", examPath, strings.Replace(examBody, `"content":`, `"class_id":"`+apiClassID+`","content":`, 1)); w.Code != http.StatusBadRequest {
		t.Fatalf("a document naming its class = %d", w.Code)
	}
	spare := createExamIn(t, f, apiClassID, examBody)
	w := f.json("GET", "/admin/exams", "")
	if !strings.Contains(w.Body.String(), `"exam_id":"`+spare+`","class_id":"`+apiClassID+`","class_name":"Test class"`) {
		t.Fatalf("the catalog does not place the exam in its class: %s", w.Body.String())
	}

	// A class with exams cannot be deleted, and the message says what to do about it.
	if w := f.send("DELETE", "/admin/class/"+apiClassID, "", ""); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "delete its exams first") {
		t.Fatalf("delete a class with exams = %d %s", w.Code, w.Body.String())
	}

	// An exam that is published or that students have taken cannot be deleted; any other can.
	if w := f.json("POST", "/admin/exam/"+spare+"/publish", `{}`); w.Code != http.StatusOK {
		t.Fatalf("publish = %d %s", w.Code, w.Body.String())
	}
	if w := f.send("DELETE", "/admin/exam/"+spare, "", ""); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "published or students have taken it") {
		t.Fatalf("delete the published exam = %d %s", w.Code, w.Body.String())
	}
	f.login(t, "0012") // the published exam is the spare one, with the shared PIN
	if w := f.json("POST", "/admin/exam/"+apiExamID+"/publish", `{"expected_current":"`+spare+`","allow_active":true}`); w.Code != http.StatusOK {
		t.Fatalf("publish = %d %s", w.Code, w.Body.String())
	}
	if w := f.send("DELETE", "/admin/exam/"+spare, "", ""); w.Code != http.StatusConflict {
		t.Fatalf("delete an exam a student took = %d %s", w.Code, w.Body.String())
	}
	untouched := createExamIn(t, f, apiClassID, examBody)
	if w := f.send("DELETE", "/admin/exam/"+untouched+"?x=1", "", ""); w.Code != http.StatusBadRequest {
		t.Fatalf("unknown query = %d", w.Code)
	}
	if w := f.send("DELETE", "/admin/exam/not-an-id", "", ""); w.Code != http.StatusBadRequest {
		t.Fatalf("bad id = %d", w.Code)
	}
	w = f.send("DELETE", "/admin/exam/"+untouched, "", "")
	if w.Code != http.StatusOK || w.Body.String() != `{"exam_id":"`+untouched+`","title":"Unicode exam"}`+"\n" {
		t.Fatalf("delete = %d %q", w.Code, w.Body.String())
	}
	if !strings.Contains(f.logs.String(), `"msg":"exam deleted"`) {
		t.Fatal("deleting an exam was not logged")
	}
	if w := f.send("DELETE", "/admin/exam/"+untouched, "", ""); w.Code != http.StatusNotFound {
		t.Fatalf("deleting twice = %d", w.Code)
	}
	if w := f.json("GET", "/admin/exams", ""); strings.Contains(w.Body.String(), untouched) {
		t.Fatalf("a deleted exam is still listed: %s", w.Body.String())
	}
}

func TestClassRenameDeleteAndRosterReplacementOverHTTP(t *testing.T) {
	f := newPublishFixture(t)
	other := decoded[struct {
		Class struct {
			ID string `json:"class_id"`
		} `json:"class"`
	}](t, f.send("POST", "/admin/classes?name=History", "text/csv", csvHeader+"1,0077,Linus,linus@example.invalid,S3\n").Body.Bytes()).Class.ID

	// Rename.
	w := f.send("PATCH", "/admin/class/"+other, "application/json", `{"name":" History 11B "}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"name":"History 11B"`) || !strings.Contains(w.Body.String(), `"student_count":1`) {
		t.Fatalf("rename = %d %s", w.Code, w.Body.String())
	}
	for body, want := range map[string]int{`{"name":"test CLASS"}`: http.StatusConflict, `{"name":""}`: http.StatusBadRequest, `{"name":"x","extra":1}`: http.StatusBadRequest, `not json`: http.StatusBadRequest} {
		if w := f.send("PATCH", "/admin/class/"+other, "application/json", body); w.Code != want {
			t.Errorf("rename %s = %d %s, want %d", body, w.Code, w.Body.String(), want)
		}
	}
	if w := f.send("PATCH", "/admin/class/"+other, "text/plain", `{"name":"x"}`); w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("content type = %d", w.Code)
	}
	if w := f.json("PATCH", "/admin/class/00000000-0000-4000-8000-0000000000ff", `{"name":"x"}`); w.Code != http.StatusNotFound {
		t.Fatalf("unknown class = %d", w.Code)
	}
	if w := f.json("PATCH", "/admin/class/not-an-id", `{"name":"x"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("bad id = %d", w.Code)
	}

	// Replace the roster of the class students are admitted from: a dry run first.
	if w := f.json("POST", "/admin/exam/"+apiExamID+"/publish", `{}`); w.Code != http.StatusOK {
		t.Fatalf("publish = %d %s", w.Code, w.Body.String())
	}
	document := csvHeader + "1,0034,Grace B,g@example.invalid,S2\n2,0099,Linus,l@example.invalid,S3\n"
	w = f.send("PUT", "/admin/class/"+apiClassID+"/roster?dry_run=true", "text/csv; charset=utf-8", document)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"added":1`) || !strings.Contains(w.Body.String(), `"removed":1`) || !strings.Contains(w.Body.String(), `"applied":false`) || !strings.Contains(w.Body.String(), `"admitting":true`) {
		t.Fatalf("dry run = %d %s", w.Code, w.Body.String())
	}
	if w := request(f.router, "POST", "/login", `{"account_number":"0012","exam_pin":"exam-secret"}`, ""); w.Code != http.StatusOK {
		t.Fatalf("a dry run removed a student: %d", w.Code)
	}
	w = f.send("PUT", "/admin/class/"+apiClassID+"/roster", "text/csv", document)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"applied":true`) {
		t.Fatalf("apply = %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(f.logs.String(), `"msg":"class roster replaced"`) || strings.Contains(f.logs.String(), "Linus") || strings.Contains(f.logs.String(), "example.invalid") {
		t.Fatalf("log = %s", f.logs.String())
	}
	if w := request(f.router, "POST", "/login", `{"account_number":"0034","exam_pin":"exam-secret"}`, ""); w.Code != http.StatusOK {
		t.Fatalf("a student who stayed cannot log in: %d", w.Code)
	}
	if w := request(f.router, "POST", "/login", `{"account_number":"0099","exam_pin":"exam-secret"}`, ""); w.Code != http.StatusOK {
		t.Fatalf("an added student cannot log in: %d", w.Code)
	}
	for name, c := range map[string]struct {
		method, path, contentType, body string
		want                            int
	}{
		"json body":         {"PUT", "/admin/class/" + apiClassID + "/roster", "application/json", document, http.StatusUnsupportedMediaType},
		"empty roster":      {"PUT", "/admin/class/" + apiClassID + "/roster", "text/csv", csvHeader, http.StatusBadRequest},
		"missing column":    {"PUT", "/admin/class/" + apiClassID + "/roster", "text/csv", "account_number\n1\n", http.StatusBadRequest},
		"oversized":         {"PUT", "/admin/class/" + apiClassID + "/roster", "text/csv", csvHeader + strings.Repeat("1,0001,Name,e@x.invalid,S\n", 400), http.StatusRequestEntityTooLarge},
		"unknown class":     {"PUT", "/admin/class/00000000-0000-4000-8000-0000000000ff/roster", "text/csv", document, http.StatusNotFound},
		"bad class id":      {"PUT", "/admin/class/nope/roster", "text/csv", document, http.StatusBadRequest},
		"unknown query":     {"PUT", "/admin/class/" + apiClassID + "/roster?x=1", "text/csv", document, http.StatusBadRequest},
		"bad dry_run":       {"PUT", "/admin/class/" + apiClassID + "/roster?dry_run=maybe", "text/csv", document, http.StatusBadRequest},
		"delete bad id":     {"DELETE", "/admin/class/nope", "", "", http.StatusBadRequest},
		"delete unknown":    {"DELETE", "/admin/class/00000000-0000-4000-8000-0000000000ff", "", "", http.StatusNotFound},
		"delete query":      {"DELETE", "/admin/class/" + other + "?x=1", "", "", http.StatusBadRequest},
		"delete with exams": {"DELETE", "/admin/class/" + apiClassID, "", "", http.StatusConflict},
	} {
		if w := f.send(c.method, c.path, c.contentType, c.body); w.Code != c.want {
			t.Errorf("%s = %d %s, want %d", name, w.Code, w.Body.String(), c.want)
		}
	}

	// Delete the class that has no exams.
	w = f.send("DELETE", "/admin/class/"+other, "", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"name":"History 11B"`) || !strings.Contains(w.Body.String(), `"students":1`) || !strings.Contains(w.Body.String(), `"pruned":1`) {
		t.Fatalf("delete = %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(f.logs.String(), `"msg":"class deleted"`) || strings.Contains(f.logs.String(), "Linus") {
		t.Fatalf("log = %s", f.logs.String())
	}
	if w := f.json("GET", "/admin/classes", ""); strings.Count(w.Body.String(), `"class_id"`) != 1 {
		t.Fatalf("classes after deleting = %s", w.Body.String())
	}
	if w := f.send("DELETE", "/admin/class/"+other, "", ""); w.Code != http.StatusNotFound {
		t.Fatalf("deleting twice = %d", w.Code)
	}
}
