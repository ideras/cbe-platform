// Command qexam is the Phase 8 regression driver (standing rule 7: question exams keep working exactly as they do). It starts a server binary
// with an empty database, makes a class, imports and publishes a question exam, has four students take it through the HTTP routes the student's
// application uses (heartbeats, focus events, autosaves, a lock and the proctor's unlock, a submission, a forced submission), reads what the
// proctor's views say, and prints all of it as JSON with what changes from one run to the next (times, IDs, tokens) replaced by placeholders.
// Run it against two builds and compare the two outputs: scripts/regression/qexam-diff.sh.
//
//	go run ./scripts/regression -server-bin build/cbe-portal-server > new.json
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

const adminToken = "regression-admin-token-0123456789abcdef0123456789abcdef"

type driver struct {
	base string
	http *http.Client
}

func (d *driver) call(method, path, token string, body any) (int, json.RawMessage) {
	var reader io.Reader
	contentType := ""
	switch v := body.(type) {
	case nil:
	case string:
		reader, contentType = strings.NewReader(v), "text/csv"
	default:
		data, _ := json.Marshal(v)
		reader, contentType = bytes.NewReader(data), "application/json"
	}
	req, _ := http.NewRequest(method, d.base+path, reader)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := d.http.Do(req)
	if err != nil {
		fatal("%s %s: %v", method, path, err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	return response.StatusCode, data
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "qexam: "+format+"\n", args...)
	os.Exit(1)
}

func (d *driver) must(want int, method, path, token string, body any) json.RawMessage {
	status, data := d.call(method, path, token, body)
	if status != want {
		fatal("%s %s = %d, want %d: %.300s", method, path, status, want, data)
	}
	return data
}

type student struct {
	account, session, token string
}

var (
	uuid    = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	instant = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})`)
	hexID   = regexp.MustCompile(`"[0-9a-f]{32,}"`)
)

// normalize replaces what differs between two runs of the same session: UUIDs, instants and long hexadecimal tokens, in every string, and
// the keys that hold a counter of time (durations measured by the server), which it only checks for presence.
func normalize(value any, ids map[string]string) any {
	switch v := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, item := range v {
			switch key {
			case "event_log_id", "id", "log_id", "version_id", "final_version_id", "sequence", "elapsed_seconds", "seconds_remaining", "server_time_ms", "latency_ms", "unfocused_duration_ms", "heartbeat_age_ms":
				out[key] = "<n>"
			default:
				out[key] = normalize(item, ids)
			}
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = normalize(item, ids)
		}
		return out
	case string:
		v = instant.ReplaceAllString(v, "<time>")
		v = uuid.ReplaceAllStringFunc(v, func(found string) string {
			if name, seen := ids[found]; seen {
				return name
			}
			name := fmt.Sprintf("<id%d>", len(ids)+1)
			ids[found] = name
			return name
		})
		return hexID.ReplaceAllString(v, `"<hex>"`)
	}
	return value
}

func main() {
	serverBin := flag.String("server-bin", "", "the server binary to drive")
	flag.Parse()
	if *serverBin == "" {
		fatal("-server-bin is required")
	}
	dir, err := os.MkdirTemp("", "qexam")
	if err != nil {
		fatal("%v", err)
	}
	defer os.RemoveAll(dir)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fatal("%v", err)
	}
	addr := listener.Addr().String()
	listener.Close()
	if err := os.WriteFile(filepath.Join(dir, "admin-token"), []byte(adminToken+"\n"), 0o600); err != nil {
		fatal("%v", err)
	}
	logFile, _ := os.Create(filepath.Join(dir, "server.log"))
	cmd := exec.Command(*serverBin, "-bind", addr, "-database", filepath.Join(dir, "exams.db"), "-screenshots", filepath.Join(dir, "shots"), "-admin-token-file", filepath.Join(dir, "admin-token"), "-worker-interval", "100ms")
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		fatal("%v", err)
	}
	defer func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		_, _ = cmd.Process.Wait()
	}()
	d := &driver{base: "http://" + addr, http: &http.Client{Timeout: 30 * time.Second}}
	for i := 0; ; i++ {
		if response, err := http.Get(d.base + "/healthz"); err == nil {
			response.Body.Close()
			break
		}
		if i > 100 {
			fatal("the server did not start; log in %s", dir)
		}
		time.Sleep(100 * time.Millisecond)
	}

	// The class and the exam: the repository's own question exam, compiled.
	roster, err := os.ReadFile("testdata/roster.csv")
	if err != nil {
		fatal("run it from the repository's root: %v", err)
	}
	var class struct {
		Class struct {
			ID string `json:"class_id"`
		} `json:"class"`
	}
	_ = json.Unmarshal(d.must(201, "POST", "/admin/classes?name=Regression", adminToken, string(roster)), &class)
	examJSON, err := os.ReadFile("testdata/exam.json")
	if err != nil {
		fatal("%v", err)
	}
	var exam map[string]any
	_ = json.Unmarshal(examJSON, &exam)
	exam["pin"] = "regression-pin"
	var created struct {
		ID string `json:"exam_id"`
	}
	_ = json.Unmarshal(d.must(201, "POST", "/admin/class/"+class.Class.ID+"/exam", adminToken, exam), &created)
	d.must(200, "POST", "/admin/exam/"+created.ID+"/publish", adminToken, map[string]any{"expected_current": ""})

	login := func(account string) student {
		var out struct {
			Token   string `json:"session_token"`
			Session string `json:"session_id"`
		}
		_ = json.Unmarshal(d.must(200, "POST", "/login", "", map[string]string{"account_number": account, "exam_pin": "regression-pin"}), &out)
		return student{account: account, session: out.Session, token: out.Token}
	}
	record := map[string]any{}
	steps := []map[string]any{}
	step := func(name string, status int, data json.RawMessage) {
		var value any
		if len(data) > 0 && json.Unmarshal(data, &value) != nil {
			value = string(data)
		}
		steps = append(steps, map[string]any{"step": name, "status": status, "body": value})
	}
	do := func(name, method, path, token string, body any) {
		status, data := d.call(method, path, token, body)
		step(name, status, data)
	}

	ada, grace := login("001234"), login("005678")
	do("ada: exam", "GET", "/exam", ada.token, nil)
	do("ada: status", "GET", "/session-status", ada.token, nil)
	do("ada: heartbeat", "POST", "/heartbeat", ada.token, map[string]string{"hostname": "regression-ada"})
	do("ada: app_started", "POST", "/event", ada.token, map[string]any{"event_type": "app_started"})
	do("ada: autosave 1", "POST", "/autosave", ada.token, map[string]any{"answers": map[string]string{"1a.alu": "12", "1a.mult": "1"}})
	do("ada: focus_lost", "POST", "/event", ada.token, map[string]any{"event_type": "focus_lost", "detail": map[string]any{"duration_ms": 1800}})
	do("ada: focus_regained", "POST", "/event", ada.token, map[string]any{"event_type": "focus_regained"})
	do("ada: autosave 2", "POST", "/autosave", ada.token, map[string]any{"answers": map[string]string{"1a.alu": "12", "1a.mult": "1", "1a.total": "20"}})
	do("ada: capture_ready", "POST", "/event", ada.token, map[string]any{"event_type": "capture_ready"})
	do("ada: calc_opened (the exam has no calculator)", "POST", "/event", ada.token, map[string]any{"event_type": "calc_opened"})
	do("ada: bad event", "POST", "/event", ada.token, map[string]any{"event_type": "no_such_event"})
	do("ada: submit", "POST", "/submit", ada.token, map[string]any{"answers": map[string]string{"1a.alu": "12", "1a.mult": "1", "1a.total": "20"}})
	do("ada: autosave after submit", "POST", "/autosave", ada.token, map[string]any{"answers": map[string]string{"1a.alu": "99"}})
	do("grace: exam", "GET", "/exam", grace.token, nil)
	do("grace: focus_lost", "POST", "/event", grace.token, map[string]any{"event_type": "focus_lost"})
	do("grace: autosave", "POST", "/autosave", grace.token, map[string]any{"answers": map[string]string{"1a.alu": "7"}})
	do("grace: bad token", "GET", "/exam", "not-a-token", nil)
	do("grace: wrong pin login", "POST", "/login", "", map[string]string{"account_number": "005678", "exam_pin": "wrong"})
	do("grace: second login", "POST", "/login", "", map[string]string{"account_number": "005678", "exam_pin": "regression-pin"})
	do("proctor: force submit grace", "POST", "/admin/force-submit/"+grace.session, adminToken, map[string]any{"reason": "regression: the proctor ends this exam"})
	do("grace: autosave after force", "POST", "/autosave", grace.token, map[string]any{"answers": map[string]string{"1a.alu": "8"}})

	// What the proctor's views say.
	do("proctor: sessions", "GET", "/admin/sessions?exam_id="+created.ID, adminToken, nil)
	for _, who := range []struct {
		name string
		s    student
	}{{"ada", ada}, {"grace", grace}} {
		do("proctor: timeline "+who.name, "GET", "/admin/session/"+who.s.session+"/timeline", adminToken, nil)
		do("proctor: submission "+who.name, "GET", "/admin/session/"+who.s.session+"/submission", adminToken, nil)
	}
	do("proctor: exams", "GET", "/admin/exams", adminToken, nil)
	do("proctor: exam", "GET", "/admin/exam/"+created.ID, adminToken, nil)
	do("proctor: classes", "GET", "/admin/classes", adminToken, nil)
	do("proctor: settings", "GET", "/admin/settings", adminToken, nil)

	record["steps"] = steps
	ids := map[string]string{ada.session: "<ada>", grace.session: "<grace>", created.ID: "<exam>", class.Class.ID: "<class>", ada.token: "<ada-token>", grace.token: "<grace-token>"}
	data, _ := json.Marshal(record)
	var generic any
	_ = json.Unmarshal(data, &generic)
	out, _ := json.MarshalIndent(normalize(generic, ids), "", "  ")
	fmt.Println(string(out))
}
