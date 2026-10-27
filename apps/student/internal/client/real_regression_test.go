package client

import (
	"encoding/json"
	"os"
	"regexp"
	"testing"
	"time"
)

// TestRealServerScriptedExamRecord is the regression script of the tools plan's Phase 8: one scripted exam with
// the calculator off, against the built server, written out as the session's timeline with its times, ids and
// durations replaced by placeholders. Run on two builds, the files must be identical:
//
//	REGRESSION_OUT=/tmp/new.json make student-client-test   (or go test -run ScriptedExamRecord with EXAM_SERVER_BINARY)
//
// It runs only when REGRESSION_OUT names the output file. The file uses only what the client package and its
// real-server harness offered before the tools feature, so the same file runs on the pre-feature build.
func TestRealServerScriptedExamRecord(t *testing.T) {
	out := os.Getenv("REGRESSION_OUT")
	if out == "" {
		t.Skip("REGRESSION_OUT names the file the scripted exam's record is written to")
	}
	server := setupRealServer(t)
	exam := server.create(t, 60, 10)
	stop := server.start(t, exam, fastPolicy())
	defer stop()
	s := realClient(t, server.base)
	ReportNativeFocus(s, true)
	if _, err := s.Login(t.Context(), server.base, "00123", "synthetic PIN"); err != nil {
		t.Fatal(err)
	}
	waitMonitor(t, s, func(v MonitoringView) bool { return v.HeartbeatAt != nil && v.Queued == 0 })
	// One focus loss, back before the screenshot interval, then answers, then the submission.
	ReportNativeFocus(s, false)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.Incident != nil && v.Incident.LossEventID > 0 })
	ReportNativeFocus(s, true)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.Focus == "focused" && v.Queued == 0 })
	update(t, s, "scripted answer", 1)
	waitView(t, s, func(v View) bool { return v.AcknowledgedRevision == 1 })
	if _, err := s.Submit(map[string]string{"q1": "scripted answer", "q2": "second"}, 1); err != nil {
		t.Fatal(err)
	}
	waitView(t, s, func(v View) bool { return v.Phase == "complete" })
	CloseMonitoring(s)
	_ = s.ServiceShutdown()
	time.Sleep(200 * time.Millisecond) // the shutdown's last events reach the server

	var list struct {
		Sessions []struct {
			ID string `json:"session_id"`
		} `json:"sessions"`
	}
	server.admin(t, "GET", "/admin/sessions?exam_id="+exam, nil, &list, 200)
	if len(list.Sessions) != 1 {
		t.Fatalf("%d sessions", len(list.Sessions))
	}
	var timeline struct {
		Items []map[string]any `json:"items"`
	}
	server.admin(t, "GET", "/admin/session/"+list.Sessions[0].ID+"/timeline", nil, &timeline, 200)
	record := make([]any, len(timeline.Items))
	for i, item := range timeline.Items {
		record[i] = normalize("", item)
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

// volatile names the fields that hold a time, an id or a duration, which differ from one run to the next.
var volatile = regexp.MustCompile(`(^|_)(at|id|ids|time|ms|millis|until|cursor|hostname|sequence)$|^(id|occurred_at|client_time|server_time)$`)

func normalize(key string, value any) any {
	switch value := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, v := range value {
			out[k] = normalize(k, v)
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i, v := range value {
			out[i] = normalize(key, v)
		}
		return out
	}
	if volatile.MatchString(key) && value != nil {
		return "<" + key + ">"
	}
	return value
}
