package client

import (
	"testing"
	"time"
)

func TestRealServerIntegratedHeartbeatFocusAnswersAndReconnect(t *testing.T) {
	server := setupRealServer(t)
	exam := server.create(t, 60, 10)
	stop := server.start(t, exam, fastPolicyWith(100))
	s := realClient(t, server.base)
	ReportNativeFocus(s, true)
	if _, err := s.Login(t.Context(), server.base, "00123", "synthetic PIN"); err != nil {
		t.Fatal(err)
	}
	initial := waitMonitor(t, s, func(v MonitoringView) bool { return v.HeartbeatAt != nil && v.Queued == 0 })
	if initial.Policy.HeartbeatMillis != 40 || initial.Policy.FocusMillis != 50 || initial.PolicySource != "server" {
		t.Fatal("client did not adopt real server policy")
	}
	ReportNativeFocus(s, false)
	waitMonitor(t, s, func(v MonitoringView) bool { return v.Incident != nil && v.Incident.LossEventID > 0 })
	time.Sleep(70 * time.Millisecond)
	ReportNativeFocus(s, true)
	update(t, s, "answers independent from telemetry", 1)
	waitView(t, s, func(v View) bool { return v.AcknowledgedRevision == 1 })
	waitMonitor(t, s, func(v MonitoringView) bool { return v.Queued == 0 })
	stop()
	waitMonitor(t, s, func(v MonitoringView) bool { return v.HeartbeatError != "" })
	time.Sleep(130 * time.Millisecond)
	stop = server.start(t, exam, fastPolicyWith(100))
	waitMonitor(t, s, func(v MonitoringView) bool { return v.HeartbeatError == "" && v.Queued == 0 })
	var list struct {
		Sessions []struct {
			ID       string `json:"session_id"`
			Presence struct {
				Hostname    string `json:"hostname"`
				EverOverdue bool   `json:"ever_overdue"`
			} `json:"presence"`
		} `json:"sessions"`
	}
	server.admin(t, "GET", "/admin/sessions?exam_id="+exam, nil, &list, 200)
	if len(list.Sessions) != 1 || !list.Sessions[0].Presence.EverOverdue || list.Sessions[0].Presence.Hostname == "" {
		t.Fatal("real server did not see hostname/persistent gap")
	}
	CloseMonitoring(s)
	var history struct {
		Items []struct {
			Event *struct {
				Type   string         `json:"event_type"`
				Detail map[string]any `json:"detail"`
			} `json:"event"`
			Version *struct {
				Answers map[string]string `json:"answers"`
			} `json:"answer_version"`
		} `json:"items"`
	}
	server.admin(t, "GET", "/admin/session/"+list.Sessions[0].ID+"/timeline", nil, &history, 200)
	kinds := map[string]int{}
	answerFound, matched := false, false
	for _, item := range history.Items {
		if item.Event != nil {
			kinds[item.Event.Type]++
			if item.Event.Type == "focus_regained" && item.Event.Detail["focus_lost_event_log_id"] != nil && item.Event.Detail["unfocused_duration_ms"] != nil {
				matched = true
			}
		}
		if item.Version != nil && item.Version.Answers["q1"] == "answers independent from telemetry" {
			answerFound = true
		}
	}
	for _, kind := range []string{"network_connected", "app_started", "focus_lost", "focus_regained", "network_gap", "network_reconnected", "app_closed"} {
		if kinds[kind] == 0 {
			t.Fatalf("real integrated history missing %s", kind)
		}
	}
	if !answerFound || !matched {
		t.Fatal("answers or server-computed matched focus duration missing")
	}
	_ = s.ServiceShutdown()
	stop()
}

func TestRealServerShowsTheStudentsOriginalSoundLevelToTheProctor(t *testing.T) {
	server := setupRealServer(t)
	exam := server.create(t, 60, 10) // no volume given: the server's default applies
	stop := server.start(t, exam, fastPolicyWith(100))
	defer stop()
	control := &fakeSound{volume: 20, muted: true}
	s := realClientWith(t, server.base, func(o *Options) {
		o.Sound = soundOptions(control)
		o.Sound.Interval = time.Minute // the only check after login is the one the login itself triggers
	})
	if _, _, sets := control.state(); len(sets) != 0 {
		t.Fatalf("application changed the sound before login: %v", sets)
	}
	if _, err := s.Login(t.Context(), server.base, "00123", "synthetic PIN"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the exam's default volume applied", func() bool { v, m, _ := control.state(); return v == 80 && !m })
	waitMonitor(t, s, func(v MonitoringView) bool { return v.HeartbeatAt != nil })
	var list struct {
		Sessions []struct {
			Presence struct {
				Volume *int  `json:"volume_percent"`
				Muted  *bool `json:"volume_muted"`
			} `json:"presence"`
		} `json:"sessions"`
	}
	eventually(t, "proctor list shows the student's level", func() bool {
		server.admin(t, "GET", "/admin/sessions?exam_id="+exam, nil, &list, 200)
		return len(list.Sessions) == 1 && list.Sessions[0].Presence.Volume != nil
	})
	if p := list.Sessions[0].Presence; *p.Volume != 20 || p.Muted == nil || !*p.Muted {
		t.Fatalf("proctor sees %d muted=%v instead of the student's own setting", *p.Volume, p.Muted)
	}
}
