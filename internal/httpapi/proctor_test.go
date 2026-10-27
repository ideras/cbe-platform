package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"exam-platform/internal/domain"
	"exam-platform/internal/service"
)

func proctorFixture(t *testing.T) (*apiFixture, string) {
	t.Helper()
	f, id := fullFixture(t)
	exams := service.NewExams(f.store, f.clock, f.sessions)
	f.router = New(f.clock, f.sessions, f.logger, Options{AdminToken: testAdminToken, Exams: exams, Proctor: f.sessions})
	return f, id
}
func TestEveryAdminRouteAndNamespaceUsesOnlyAdminCredentials(t *testing.T) {
	f, id := proctorFixture(t)
	student := f.login(t, "0012")
	for _, route := range []struct{ method, path, body string }{
		{"POST", examPath, examBody},
		{"GET", "/admin/exam/" + id, ""},
		{"DELETE", "/admin/exam/" + id, ""},
		{"GET", "/admin/sessions?exam_id=" + id, ""},
		{"GET", "/admin/session/" + student.SessionID + "/timeline", ""},
		{"GET", "/admin/session/" + student.SessionID + "/submission", ""},
		{"POST", "/admin/force-submit/" + student.SessionID, `{"reason":"instructor"}`},
		{"GET", "/admin", ""}, {"GET", "/admin/unknown", ""}, {"DELETE", "/admin/sessions", ""},
		{"GET", "/admin//sessions", ""}, {"GET", "/admin/session/../sessions", ""},
	} {
		for _, auth := range []string{"", "Bearer wrong", "Bearer " + student.SessionToken, "Basic " + testAdminToken} {
			if w := request(f.router, route.method, route.path, route.body, auth); w.Code != 401 || w.Header().Get("WWW-Authenticate") != `Bearer realm="proctor"` {
				t.Fatalf("%s auth = %d", route.path, w.Code)
			}
		}
		r := httptest.NewRequest(route.method, route.path, strings.NewReader(route.body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Add("Authorization", "Bearer "+testAdminToken)
		r.Header.Add("Authorization", "Bearer "+testAdminToken)
		w := httptest.NewRecorder()
		f.router.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatal("multiple authorization headers allowed")
		}
	}
	if w := request(New(f.clock, f.sessions, f.logger, Options{Proctor: f.sessions}), "GET", "/admin/sessions?exam_id="+id, "", "Bearer "+testAdminToken); w.Code != 401 {
		t.Fatal("empty admin token did not fail closed")
	}
}

func TestRealHTTPProctorListTimelineForceAndTerminalReadability(t *testing.T) {
	for _, save := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "saved"}[save], func(t *testing.T) {
			f, id := proctorFixture(t)
			login := f.login(t, "0012")
			server := httptest.NewServer(f.router)
			defer server.Close()
			if save {
				result := realRequest(t, server, "POST", "/autosave", `{"answers":{"q1":"saved 世界"}}`, login.SessionToken)
				if result.Code != 200 {
					t.Fatal("save failed")
				}
			}
			listReply := realRequest(t, server, "GET", "/admin/sessions?exam_id="+id, "", testAdminToken)
			list := decoded[service.SessionList](t, listReply.Body)
			if listReply.Code != 200 || len(list.Sessions) != 1 || list.Sessions[0].AccountNumber != "0012" || list.Sessions[0].Name != "Ada" || len(list.StudentsWithoutSession) != 1 || list.StudentsWithoutSession[0].AccountNumber != "0034" || listReply.Header.Get("Cache-Control") != "no-store" {
				t.Fatalf("list = %s", listReply.Body)
			}
			forcedReply := realRequest(t, server, "POST", "/admin/force-submit/"+login.SessionID, `{"reason":" operator intervention "}`, testAdminToken)
			forced := decoded[submitResponse](t, forcedReply.Body)
			if forcedReply.Code != 200 || forced.Status != domain.ForceSubmitted || forced.Sequence != 1 || forced.FinalVersionID <= 0 {
				t.Fatalf("force = %s", forcedReply.Body)
			}
			for range 3 {
				retry := realRequest(t, server, "POST", "/admin/force-submit/"+login.SessionID, `{"reason":"another reason"}`, testAdminToken)
				if retry.Code != 200 || decoded[submitResponse](t, retry.Body) != forced {
					t.Fatal("force retry changed final result")
				}
			}
			if realRequest(t, server, "POST", "/autosave", `{"answers":{}}`, login.SessionToken).Code != 403 {
				t.Fatal("answer write after force")
			}
			listReply = realRequest(t, server, "GET", "/admin/sessions?exam_id="+id, "", testAdminToken)
			if decoded[service.SessionList](t, listReply.Body).Sessions[0].Final.Reason != "force_submit" {
				t.Fatal("missing final summary")
			}
			var items []service.TimelineItem
			path := "/admin/session/" + login.SessionID + "/timeline?limit=1"
			for {
				reply := realRequest(t, server, "GET", path, "", testAdminToken)
				page := decoded[service.TimelinePage](t, reply.Body)
				if reply.Code != 200 || page.Session.Final == nil || page.Session.Final.VersionID != forced.FinalVersionID || page.Session.Final.OperatorReason != "operator intervention" {
					t.Fatalf("timeline = %s", reply.Body)
				}
				for _, secret := range []string{login.SessionToken, "exam-secret", `"token"`, `"pin"`} {
					if strings.Contains(string(reply.Body), secret) || strings.Contains(string(listReply.Body), secret) {
						t.Fatal("credential field exposed")
					}
				}
				items = append(items, page.Items...)
				if page.NextCursor == nil {
					break
				}
				path = "/admin/session/" + login.SessionID + "/timeline?limit=1&cursor=" + url.QueryEscape(*page.NextCursor)
			}
			versions, finalEvents := 0, 0
			for _, item := range items {
				if item.Version != nil {
					versions++
					if !item.Version.IsFinal || item.Version.ID != forced.FinalVersionID {
						t.Fatal("wrong selected answer")
					}
					if save && item.Version.Answers["q1"] != "saved 世界" || !save && len(item.Version.Answers) != 0 {
						t.Fatal("not latest/empty snapshot")
					}
				}
				if item.Event != nil && item.Event.Type == "force_submitted" {
					finalEvents++
				}
			}
			if versions != 1 || finalEvents != 1 {
				t.Fatal("incomplete/duplicate timeline")
			}
			if strings.Contains(f.logs.String(), testAdminToken) || strings.Contains(f.logs.String(), login.SessionToken) {
				t.Fatal("credentials logged")
			}
		})
	}
}

func TestAdminQueriesIDsAndForceBodyValidation(t *testing.T) {
	f, id := proctorFixture(t)
	login := f.login(t, "0012")
	unknown := "00000000-0000-4000-8000-000000000099"
	for _, path := range []string{"/admin/sessions", "/admin/sessions?exam_id=", "/admin/sessions?exam_id=bad", "/admin/sessions?exam_id=" + id + "&exam_id=" + id, "/admin/sessions?exam_id=" + id + "&account_number=0012", "/admin/session/bad/timeline", "/admin/session/" + login.SessionID + "/timeline?limit=0", "/admin/session/" + login.SessionID + "/timeline?limit=201", "/admin/session/" + login.SessionID + "/timeline?limit=-1", "/admin/session/" + login.SessionID + "/timeline?limit=oops", "/admin/session/" + login.SessionID + "/timeline?limit=1&limit=2", "/admin/session/" + login.SessionID + "/timeline?cursor=bad", "/admin/session/" + login.SessionID + "/timeline?cursor=" + strings.Repeat("a", 1025), "/admin/session/" + login.SessionID + "/timeline?exam_id=" + id} {
		if w := request(f.router, "GET", path, "", "Bearer "+testAdminToken); w.Code != 400 {
			t.Fatalf("%s = %d %s", path, w.Code, w.Body.String())
		}
	}
	for _, path := range []string{"/admin/sessions?exam_id=" + unknown, "/admin/session/" + unknown + "/timeline"} {
		if w := request(f.router, "GET", path, "", "Bearer "+testAdminToken); w.Code != 404 {
			t.Fatalf("unknown = %d", w.Code)
		}
	}
	for _, body := range []string{`{}`, `null`, `[]`, `{`, `{"reason":""}`, `{"reason":"   "}`, `{"reason":12}`, `{"reason":"x","session_id":"other"}`, `{"reason":"x"} {}`, `{"reason":"` + strings.Repeat("x", 1025) + `"}`} {
		if w := request(f.router, "POST", "/admin/force-submit/"+login.SessionID, body, "Bearer "+testAdminToken); w.Code != 400 {
			t.Fatalf("body validation = %d", w.Code)
		}
	}
	if w := request(f.router, "POST", "/admin/force-submit/"+unknown, `{"reason":"test"}`, "Bearer "+testAdminToken); w.Code != 404 {
		t.Fatal("unknown force target")
	}
	if w := request(f.router, "POST", "/admin/force-submit/bad", `{"reason":"test"}`, "Bearer "+testAdminToken); w.Code != 400 {
		t.Fatal("malformed force target")
	}
	if w := request(f.router, "POST", "/admin/force-submit/"+login.SessionID, `{"reason":"`+strings.Repeat("x", 9000)+`"}`, "Bearer "+testAdminToken); w.Code != 413 {
		t.Fatal("unbounded force body")
	}
	r := httptest.NewRequest("POST", "/admin/force-submit/"+login.SessionID, strings.NewReader(`{"reason":"test"}`))
	r.Header.Set("Authorization", "Bearer "+testAdminToken)
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, r)
	if w.Code != 415 {
		t.Fatal("missing content type accepted")
	}
}

func TestAdminForceCannotRelabelSubmittedOrExpired(t *testing.T) {
	for _, expiry := range []bool{false, true} {
		f, _ := proctorFixture(t)
		login := f.login(t, "0012")
		if expiry {
			f.clock.Set(login.DeadlineAt.Add(30 * time.Second))
		} else {
			if w := request(f.router, "POST", "/submit", `{"answers":{"q1":"student final"}}`, "Bearer "+login.SessionToken); w.Code != 200 {
				t.Fatal("student submit")
			}
		}
		w := request(f.router, "POST", "/admin/force-submit/"+login.SessionID, `{"reason":"late instructor"}`, "Bearer "+testAdminToken)
		if w.Code != 200 || decoded[submitResponse](t, w.Body.Bytes()).Status != domain.Submitted {
			t.Fatal("relabelled final state")
		}
		page := decoded[service.TimelinePage](t, request(f.router, "GET", "/admin/session/"+login.SessionID+"/timeline", "", "Bearer "+testAdminToken).Body.Bytes())
		want := "explicit_submit"
		if expiry {
			want = "grace_expired"
		}
		if page.Session.Final.Reason != want {
			t.Fatal("original reason lost")
		}
	}
}

func TestAdminDTOsDoNotMarshalInternalCredentials(t *testing.T) {
	f, _ := proctorFixture(t)
	login := f.login(t, "0012")
	page, err := f.sessions.ProctorTimeline(t.Context(), login.SessionID, 100, "")
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), login.SessionToken) || strings.Contains(string(body), "exam-secret") {
		t.Fatal("internal record was marshalled")
	}
}
