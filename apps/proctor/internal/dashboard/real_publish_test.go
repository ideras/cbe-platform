package dashboard

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestBuiltServerStartedEmptyIsPreparedAndPublishedFromTheDashboard drives the real server binary the way a proctor does
// on exam day: it starts with no class and no exam, and everything else happens through the dashboard.
func TestBuiltServerStartedEmptyIsPreparedAndPublishedFromTheDashboard(t *testing.T) {
	binary := os.Getenv("EXAM_SERVER_BINARY")
	if binary == "" {
		t.Skip("set EXAM_SERVER_BINARY for built-server integration")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "admin-token"), []byte(secret+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	server := "http://127.0.0.1:" + strconv.Itoa(port)
	start := func() *exec.Cmd {
		cmd := exec.Command(binary, "-bind", "127.0.0.1:"+strconv.Itoa(port), "-database", filepath.Join(dir, "exam.db"), "-screenshots", filepath.Join(dir, "screenshots"), "-admin-token-file", filepath.Join(dir, "admin-token"))
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		client := &http.Client{Timeout: time.Second}
		wait(t, func() bool {
			resp, err := client.Get(server + "/healthz")
			if err != nil {
				return false
			}
			resp.Body.Close()
			return resp.StatusCode == 200
		})
		return cmd
	}
	stop := func(cmd *exec.Cmd) {
		cmd.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() { cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			cmd.Process.Kill()
			<-done
		}
	}
	running := start()
	t.Cleanup(func() { stop(running) })

	login := func(account, pin string) (int, string) {
		body, _ := json.Marshal(map[string]string{"account_number": account, "exam_pin": pin})
		resp, err := http.Post(server+"/login", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out struct {
			Token string `json:"session_token"`
		}
		json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out.Token
	}

	var chosen string
	opts := DefaultOptions()
	opts.Server, opts.Token, opts.Poll = server, secret, 20*time.Millisecond
	opts.Choose = func(FileDialog) (string, error) { return chosen, nil }
	s, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.ServiceShutdown)
	if _, err := s.Connect(server); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool { v := s.Snapshot(); return v.CatalogLoaded && v.ClassesLoaded && !v.Stale })
	if v := s.Snapshot(); len(v.Classes) != 0 || len(v.Catalog.Exams) != 0 || v.ExamID != "" {
		t.Fatalf("an empty server looks configured: %+v", v)
	}
	if status, _ := login("0001", "midterm-pin"); status != http.StatusServiceUnavailable {
		t.Fatalf("login on an empty server = %d", status)
	}

	// Classes: each roster is checked first, then stored as a class that can be read back.
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	const header = "canvas_id,account_number,name,email,sis_user_id\n"
	chosen = write("Class-A.csv", "\xef\xbb\xbf"+header+"1,0001,Ada,ada@x.invalid,S1\n2,0002,Bruno,bruno@x.invalid,S2\n3,,Nobody,n@x.invalid,S3\n")
	preview, err := s.ChooseClassFile()
	if err != nil || !preview.Valid || preview.Students != 2 || preview.WarningCount != 1 || preview.SuggestedName != "Class-A" {
		t.Fatalf("class preview = %+v, %v", preview, err)
	}
	if v := s.Snapshot(); len(v.Classes) != 0 {
		t.Fatalf("previewing stored a class: %+v", v.Classes)
	}
	classA, err := s.CreateClass(preview.SuggestedName)
	if err != nil || !validID(classA.ID) || classA.StudentCount != 2 {
		t.Fatalf("create class A = %+v, %v", classA, err)
	}
	chosen = write("Class-B.csv", header+"1,0003,Alan,alan@x.invalid,S3\n")
	if _, err := s.ChooseClassFile(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateClass("class-a"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("a duplicate class name = %v", err)
	}
	classB, err := s.CreateClass("Class B")
	if err != nil || classB.StudentCount != 1 {
		t.Fatalf("create class B = %+v, %v", classB, err)
	}
	wait(t, func() bool { v := s.Snapshot(); return len(v.Classes) == 2 && !v.Busy })
	roster, err := s.ClassRoster(classA.ID)
	if err != nil || roster.Total != 2 || roster.Students[0] != (Student{Account: "0001", Name: "Ada"}) || roster.Students[1] != (Student{Account: "0002", Name: "Bruno"}) {
		t.Fatalf("class A roster = %+v, %v", roster, err)
	}
	if data, _ := json.Marshal(roster); bytes.Contains(data, []byte("x.invalid")) {
		t.Fatal("an e-mail address reached the dashboard window")
	}
	chosen = write("bad.csv", "account_number\n1\n")
	if preview, err := s.ChooseClassFile(); err != nil || preview.Valid || len(preview.Problems) == 0 || !strings.Contains(preview.Problems[0], "missing required column") {
		t.Fatalf("bad roster = %+v, %v", preview, err)
	}

	// Managing a class: rename it, and replace a roster after seeing what the replacement would do.
	renamed, err := s.RenameClass(classB.ID, "Class Bee")
	if err != nil || renamed.Name != "Class Bee" || renamed.StudentCount != 1 {
		t.Fatalf("rename = %+v, %v", renamed, err)
	}
	if _, err := s.RenameClass(classB.ID, "CLASS-A"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("a name taken by another class = %v", err)
	}
	wait(t, func() bool {
		v := s.Snapshot()
		return len(v.Classes) == 2 && v.Classes[0].Name == "Class Bee" && !v.Busy
	})
	chosen = write("Class-A-new.csv", header+"1,0001,Ada Lovelace,ada@x.invalid,S1\n2,0004,Diego,diego@x.invalid,S4\n")
	change, err := s.ChooseRosterFile(classA.ID)
	if err != nil || !change.Valid || change.Students != 2 || change.Added != 1 || change.Removed != 1 || change.Admitting || change.Applied {
		t.Fatalf("roster preview = %+v, %v", change, err)
	}
	if roster, _ := s.ClassRoster(classA.ID); roster.Total != 2 || roster.Students[0].Name != "Ada" || roster.Students[1].Name != "Bruno" {
		t.Fatalf("previewing changed the roster: %+v", roster)
	}
	applied, err := s.ReplaceClassRoster(classA.ID, true)
	if err != nil || !applied.Applied || applied.Added != 1 || applied.Removed != 1 || applied.Pruned != 1 {
		t.Fatalf("replacement = %+v, %v (Bruno never logged in and is in no other class)", applied, err)
	}
	roster, err = s.ClassRoster(classA.ID)
	if err != nil || roster.Total != 2 || roster.Students[0] != (Student{Account: "0001", Name: "Ada Lovelace"}) || roster.Students[1] != (Student{Account: "0004", Name: "Diego"}) {
		t.Fatalf("class A after the replacement = %+v, %v", roster, err)
	}
	if data, _ := json.Marshal(roster); bytes.Contains(data, []byte("x.invalid")) {
		t.Fatal("an e-mail address reached the dashboard window")
	}

	// Exams are imported into a class: a broken file is explained, valid ones are stored without admitting anyone.
	chosen = write("broken.md", "---\n---\nNo title, no duration, no exercises.\n")
	if preview, err := s.ChooseExam(classA.ID); err != nil || preview.Valid || len(preview.Problems) < 3 {
		t.Fatalf("broken exam = %+v, %v", preview, err)
	}
	if _, err := s.ImportExam(classA.ID, "p", false); err == nil {
		t.Fatal("a broken exam was imported")
	}
	exam := func(class, title, pin string) string {
		chosen = write(strings.ReplaceAll(title, " ", "-")+".md", "---\ntitle: "+title+"\nduration: 10m\ngrace: 30s\nmin_volume: 60\n---\n## One\n{{q1}}\n\n## Two\n```answer {#q2}\n```\n")
		preview, err := s.ChooseExam(class)
		if err != nil || !preview.Valid || preview.Title != title || preview.ExerciseCount != 2 || preview.SlotCount != 2 || preview.MinVolumePercent != 60 {
			t.Fatalf("exam preview = %+v, %v", preview, err)
		}
		result, err := s.ImportExam(class, pin, false)
		if err != nil || !validID(result.ExamID) {
			t.Fatalf("import = %+v, %v", result, err)
		}
		return result.ExamID
	}
	first := exam(classA.ID, "Midterm", "midterm-pin")
	second := exam(classB.ID, "Retake", "retake-pin")
	draft := exam(classA.ID, "Draft", "draft-pin")
	wait(t, func() bool {
		v := s.Snapshot()
		return len(v.Catalog.Exams) == 3 && v.Catalog.CurrentExamID == "" && !v.Busy
	})
	v := s.Snapshot()
	byID := map[string]ExamEntry{}
	for _, e := range v.Catalog.Exams {
		byID[e.ID] = e
	}
	if byID[first].ClassID != classA.ID || byID[first].ClassName != "Class-A" || byID[second].ClassID != classB.ID || byID[second].ClassName != "Class Bee" || byID[draft].ClassID != classA.ID {
		t.Fatalf("exams are not in the classes they were imported into: %+v", byID)
	}
	for _, c := range v.Classes {
		if want := map[string]int{classA.ID: 2, classB.ID: 1}[c.ID]; c.ExamCount != want || c.PublishedExam != nil {
			t.Fatalf("class %s = %+v, want %d exams and none published", c.Name, c, want)
		}
	}
	if status, _ := login("0001", "midterm-pin"); status != http.StatusServiceUnavailable {
		t.Fatalf("an imported exam admitted students before it was published: %d", status)
	}

	// A class keeps its exams: it cannot be deleted while it has any, and the dashboard says what to do.
	if _, err := s.DeleteClass(classA.ID, true); err == nil || !strings.Contains(err.Error(), "still has exams") {
		t.Fatalf("deleting a class with exams = %v", err)
	}

	// Publishing lets the students of the exam's class in with that exam's PIN, and the dashboard monitors it.
	if _, err := s.Publish(first, "", false, false); err == nil {
		t.Fatal("unconfirmed publish accepted")
	}
	if outcome, err := s.Publish(first, "", false, true); err != nil || outcome.ExamID != first || outcome.ClassID != classA.ID {
		t.Fatalf("publish = %+v, %v", outcome, err)
	}
	wait(t, func() bool {
		v := s.Snapshot()
		return v.ExamID == first && v.Loaded && v.Catalog.CurrentExamID == first && v.Catalog.CurrentClassID == classA.ID && v.Catalog.CurrentClassName == "Class-A" && !v.Stale
	})
	if status, _ := login("0003", "midterm-pin"); status != http.StatusUnauthorized {
		t.Fatalf("a student of another class logged in: %d", status)
	}
	status, token := login("0001", "midterm-pin")
	if status != http.StatusOK || token == "" {
		t.Fatalf("published exam login = %d", status)
	}
	if status, _ := login("0001", "retake-pin"); status != http.StatusUnauthorized {
		t.Fatalf("another exam's PIN worked: %d", status)
	}
	s.Refresh()
	wait(t, func() bool {
		v := s.Snapshot()
		working := 0
		for _, exam := range v.Catalog.Exams {
			working += exam.ActiveSessions
		}
		return len(v.Roster.Sessions) == 1 && len(v.Roster.Missing) == 1 && v.Roster.Missing[0].Account == "0004" && working == 1
	})
	if _, err := s.DeleteExam(first, true); err == nil || !strings.Contains(err.Error(), "published or students have taken it") {
		t.Fatalf("deleting the published exam = %v", err)
	}

	// With a student working, replacing the exam needs the confirmation the server enforces.
	_, err = s.Publish(second, first, false, true)
	var refused *rejection
	if err == nil || !strings.Contains(err.Error(), "still working") || !errors.As(err, &refused) || refused.Active != 1 || refused.Status != http.StatusConflict {
		t.Fatalf("publish over a working student = %v", err)
	}
	if _, err := s.Publish(second, "", true, true); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("publish based on a stale view = %v", err)
	}
	if outcome, err := s.Publish(second, first, true, true); err != nil || outcome.ExamID != second || outcome.ClassID != classB.ID || outcome.ReplacedExamID != first || outcome.ReplacedClassID != classA.ID || outcome.ActiveSessions != 1 {
		t.Fatalf("confirmed publish = %+v, %v", outcome, err)
	}
	wait(t, func() bool {
		v := s.Snapshot()
		return v.ExamID == second && v.Loaded && v.Catalog.CurrentExamID == second && v.Catalog.CurrentClassID == classB.ID && !v.Stale
	})
	if status, _ := login("0003", "retake-pin"); status != http.StatusOK {
		t.Fatalf("login to the newly published exam = %d", status)
	}
	if status, _ := login("0004", "retake-pin"); status != http.StatusUnauthorized {
		t.Fatalf("a student of the previous class logged in to the new exam: %d", status)
	}
	if status, _ := login("0001", "midterm-pin"); status != http.StatusUnauthorized {
		t.Fatalf("the replaced exam still admits students: %d", status)
	}
	wait(t, func() bool {
		for _, c := range s.Snapshot().Classes {
			if c.ID == classB.ID {
				return c.PublishedExam != nil && c.PublishedExam.ExamID == second
			}
		}
		return false
	})

	// Closing admission survives a server restart, because it is stored.
	if _, err := s.Unpublish(second, true, true); err != nil {
		t.Fatal(err)
	}
	if status, _ := login("0001", "retake-pin"); status != http.StatusServiceUnavailable {
		t.Fatalf("login after closing admission = %d", status)
	}

	// Only an exam nobody has taken can be deleted, and once a class has none left it can be deleted too.
	if _, err := s.DeleteExam(first, true); err == nil || !strings.Contains(err.Error(), "published or students have taken it") {
		t.Fatalf("deleting an exam a student took = %v", err)
	}
	if _, err := s.DeleteExam(second, true); err == nil {
		t.Fatal("deleting an exam a student took was accepted")
	}
	if deleted, err := s.DeleteExam(draft, true); err != nil || deleted.ID != draft || deleted.Title != "Draft" {
		t.Fatalf("deleting an exam nobody took = %+v, %v", deleted, err)
	}
	wait(t, func() bool { v := s.Snapshot(); return len(v.Catalog.Exams) == 2 && !v.Busy })
	chosen = write("Class-C.csv", header+"1,0009,Eve,eve@x.invalid,S9\n")
	if _, err := s.ChooseClassFile(); err != nil {
		t.Fatal(err)
	}
	classC, err := s.CreateClass("Class C")
	if err != nil {
		t.Fatal(err)
	}
	spare := exam(classC.ID, "Spare", "spare-pin")
	wait(t, func() bool { v := s.Snapshot(); return catalogHas(v.Catalog, spare) && !v.Busy }) // the proctor deletes what the list shows
	if _, err := s.DeleteClass(classC.ID, true); err == nil || !strings.Contains(err.Error(), "Delete its exams first") {
		t.Fatalf("deleting a class with an exam nobody took = %v", err)
	}
	if _, err := s.DeleteExam(spare, true); err != nil {
		t.Fatal(err)
	}
	if deleted, err := s.DeleteClass(classC.ID, true); err != nil || deleted.Name != "Class C" || deleted.Students != 1 || deleted.Pruned != 1 {
		t.Fatalf("deleting a class whose exams are gone = %+v, %v", deleted, err)
	}

	if _, err := s.Publish(first, "", false, true); err != nil {
		t.Fatal(err)
	}
	stop(running)
	running = start()
	if status, _ := login("0001", "midterm-pin"); status != http.StatusOK {
		t.Fatalf("the published exam was forgotten by a restart: %d", status)
	}
	wait(t, func() bool { v := s.Snapshot(); return v.Catalog.CurrentExamID == first && !v.Stale })
	if v := s.Snapshot(); len(v.Classes) != 2 || len(v.Catalog.Exams) != 2 {
		t.Fatalf("the classes or exams were forgotten by a restart: %+v", v)
	}

	data, _ := json.Marshal(s.Snapshot())
	if bytes.Contains(data, []byte(secret)) || bytes.Contains(data, []byte("midterm-pin")) || bytes.Contains(data, []byte("retake-pin")) || bytes.Contains(data, []byte("draft-pin")) {
		t.Fatal("a credential or PIN is in the dashboard's public view")
	}
}
