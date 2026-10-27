package dashboard

import (
	"bytes"
	"encoding/json"
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

// TestRealServerDownloadedSubmissionsAreTheExamWithAnswers drives the built server the way the proctor does after an exam:
// two students take an imported exam, one submits with a name full of accents, and the download writes the exam with their
// answers in it, plus the _index.md checklist. Downloading again asks before replacing the files.
func TestRealServerDownloadedSubmissionsAreTheExamWithAnswers(t *testing.T) {
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
	cmd := exec.Command(binary, "-bind", "127.0.0.1:"+strconv.Itoa(port), "-database", filepath.Join(dir, "exam.db"), "-screenshots", filepath.Join(dir, "screenshots"), "-admin-token-file", filepath.Join(dir, "admin-token"))
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cmd.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() { cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			cmd.Process.Kill()
			<-done
		}
	})
	client := &http.Client{Timeout: time.Second}
	wait(t, func() bool {
		resp, err := client.Get(server + "/healthz")
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == 200
	})

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
	save := func(path, token, body string) int {
		req, _ := http.NewRequest("POST", server+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	var chosen, chosenFolder string
	opened := ""
	opts := DefaultOptions()
	opts.Server, opts.Token, opts.Poll = server, secret, 20*time.Millisecond
	opts.Choose = func(FileDialog) (string, error) { return chosen, nil }
	opts.ChooseFolder = func(FolderDialog) (string, error) { return chosenFolder, nil }
	opts.OpenFolder = func(path string) error { opened = path; return nil }
	s, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.ServiceShutdown)
	if _, err := s.Connect(server); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool { v := s.Snapshot(); return v.CatalogLoaded && v.ClassesLoaded && !v.Stale })

	const header = "canvas_id,account_number,name,email,sis_user_id\n"
	roster := header + "1,0001,José Ñúñez Martínez,jose@x.invalid,S1\n2,0002,Bruno,bruno@x.invalid,S2\n3,0003,Carla,carla@x.invalid,S3\n"
	chosen = filepath.Join(dir, "Class-D.csv")
	if err := os.WriteFile(chosen, []byte(roster), 0600); err != nil {
		t.Fatal(err)
	}
	if preview, err := s.ChooseClassFile(); err != nil || !preview.Valid || preview.Students != 3 {
		t.Fatalf("class preview = %+v, %v", preview, err)
	}
	klass, err := s.CreateClass("CCC403 Sección 1")
	if err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool { return len(s.Snapshot().Classes) == 1 && !s.Snapshot().Busy })

	source := "---\ntitle: Examen I Parcial – Organización de Computadoras\nduration: 10m\ngrace: 30s\nmin_volume: 0\n---\n\n## Ejercicio 1 — Tabla {points=10}\n\n| Cantidad |\n|----------|\n| {{q1}}   |\n\n```answer {#code lang=mips}\n```\n"
	chosen = filepath.Join(dir, "exam.md")
	if err := os.WriteFile(chosen, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	if preview, err := s.ChooseExam(klass.ID); err != nil || !preview.Valid {
		t.Fatalf("exam preview = %+v, %v", preview, err)
	}
	imported, err := s.ImportExam(klass.ID, "download-pin", false)
	if err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool { return catalogHas(s.Snapshot().Catalog, imported.ExamID) && !s.Snapshot().Busy })
	if _, err := s.Publish(imported.ExamID, "", false, true); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool { v := s.Snapshot(); return v.Catalog.CurrentExamID == imported.ExamID && !v.Stale })

	status, jose := login("0001", "download-pin")
	if status != http.StatusOK {
		t.Fatalf("login = %d", status)
	}
	const answers = `{"answers":{"1.q1":"16","1.code":"mult $s0, $s1"}}`
	if status := save("/autosave", jose, answers); status != http.StatusOK {
		t.Fatalf("autosave = %d", status)
	}
	if status := save("/submit", jose, answers); status != http.StatusOK {
		t.Fatalf("submit = %d", status)
	}
	status, bruno := login("0002", "download-pin")
	if status != http.StatusOK {
		t.Fatalf("second login = %d", status)
	}
	if status := save("/autosave", bruno, `{"answers":{"1.q1":"5"}}`); status != http.StatusOK {
		t.Fatalf("second autosave = %d", status)
	}

	preview, err := s.PrepareDownload(imported.ExamID)
	if err != nil || preview.Finished != 1 || preview.Working != 1 || preview.NeverLoggedIn != 1 || preview.Finishing != 0 || !preview.SourceAvailable {
		t.Fatalf("download preview = %+v, %v", preview, err)
	}
	chosenFolder = t.TempDir()
	if parent, err := s.ChooseDownloadFolder(); err != nil || parent != chosenFolder {
		t.Fatalf("choose folder = %q, %v", parent, err)
	}
	result, err := s.DownloadSubmissions(imported.ExamID, preview.FolderName, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Files) != 1 || result.Files[0] != "Jose_Nunez_Martinez.md" || len(result.Skipped) != 1 || len(result.Failed) != 0 || result.Warnings == nil {
		t.Fatalf("download = %+v", result)
	}
	content, err := os.ReadFile(filepath.Join(result.Folder, "Jose_Nunez_Martinez.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`student: "José Ñúñez Martínez"`, "| `16`   |", "```mips title=\"1.code\"\nmult $s0, $s1\n```", "## Ejercicio 1 — Tabla {points=10}"} {
		if !strings.Contains(string(content), want) {
			t.Errorf("downloaded file does not contain %q:\n%s", want, content)
		}
	}
	index, err := os.ReadFile(filepath.Join(result.Folder, indexFileName))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"| Bruno | 0002 | still working |", "| Carla | 0003 | never logged in |", "[Jose_Nunez_Martinez.md](Jose_Nunez_Martinez.md)"} {
		if !strings.Contains(string(index), want) {
			t.Errorf("_index.md does not contain %q:\n%s", want, index)
		}
	}
	if err := s.OpenDownloadFolder(); err != nil || opened != result.Folder {
		t.Fatalf("open folder = %q, %v", opened, err)
	}
	// A second download into the same place asks before replacing the files; the chosen folder is remembered.
	again, err := s.DownloadSubmissions(imported.ExamID, preview.FolderName, false, true)
	if err != nil || !again.NeedsConfirmation || again.ReplaceCount != 2 {
		t.Fatalf("second download = %+v, %v", again, err)
	}
	replaced, err := s.DownloadSubmissions(imported.ExamID, preview.FolderName, true, true)
	if err != nil || replaced.NeedsConfirmation || len(replaced.Files) != 1 {
		t.Fatalf("replace = %+v, %v", replaced, err)
	}
	// The user may give the output folder another name before starting.
	renamed, err := s.DownloadSubmissions(imported.ExamID, "Notas del Parcial", false, true)
	if err != nil || renamed.NeedsConfirmation || len(renamed.Files) != 1 || renamed.Folder != filepath.Join(chosenFolder, "Notas del Parcial") {
		t.Fatalf("renamed download = %+v, %v", renamed, err)
	}
}
