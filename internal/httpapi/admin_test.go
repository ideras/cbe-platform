package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAdminAuth(t *testing.T) {
	token := strings.Repeat("a", 64)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	for _, header := range []string{"", "Bearer student-session-token", "Basic " + token, "Bearer " + token + " ", "Bearer " + token, "bearer " + token} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest("GET", "/admin/test", nil)
		request.Header.Set("Authorization", header)
		AdminAuth(token, next).ServeHTTP(recorder, request)
		expected := http.StatusUnauthorized
		if header == "Bearer "+token || header == "bearer "+token {
			expected = http.StatusNoContent
		}
		if recorder.Code != expected {
			t.Fatalf("status = %d; expected %d", recorder.Code, expected)
		}
	}
	recorder := httptest.NewRecorder()
	AdminAuth("", next).ServeHTTP(recorder, httptest.NewRequest("GET", "/admin/test", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatal("empty configuration did not fail closed")
	}
}
