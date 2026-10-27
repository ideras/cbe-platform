package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestServerValidation(t *testing.T) {
	for _, input := range []string{"", "localhost:8080", "https://host", "http://user:password@host", "http://host/path", "http://host?token=secret", "http://host#secret", "http://host?", "http://host:", "http://host:0", "http://host:65536", "http://host:bad", "http://ho\nst", "http://[]"} {
		t.Run(input, func(t *testing.T) {
			if _, err := validateServer(input); err == nil {
				t.Fatalf("accepted invalid server %q", input)
			}
		})
	}
	for _, input := range []string{"http://host", "http://127.0.0.1:8080/", "http://[::1]:8080", " http://host:80/ "} {
		if _, err := validateServer(input); err != nil {
			t.Errorf("rejected valid server %q: %v", input, err)
		}
	}
}
func TestRedirectAndEnvironmentProxyCannotForwardCredentials(t *testing.T) {
	received := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { received = true; respond(w, map[string]string{}) }))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	t.Setenv("ALL_PROXY", "http://127.0.0.1:1")
	t.Setenv("NO_PROXY", "")
	h := newHTTPClient(time.Second)
	defer h.CloseIdleConnections()
	var reply any
	err := requestJSON(t.Context(), h, origin.URL, "private", http.MethodPost, "/login", map[string]string{"pin": "sensitive"}, &reply)
	if failureCode(err) != 307 || received {
		t.Fatalf("redirect forwarded credentials or proxy used: %v", err)
	}
}
func TestBoundedResponsesAndSanitizedErrors(t *testing.T) {
	for _, body := range []string{"{invalid", "{} {}", strings.Repeat(" ", 16<<20+1)} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
		var value any
		err := requestJSON(t.Context(), newHTTPClient(time.Second), server.URL, "secret", http.MethodGet, "/exam", nil, &value)
		server.Close()
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal("invalid response accepted or credential leaked")
		}
	}
}
func TestConfigurationAndCancelledBootstrap(t *testing.T) {
	options := DefaultOptions()
	options.Periodic = 0
	if s, err := New(options); err == nil {
		_ = s.ServiceShutdown()
		t.Fatal("zero interval accepted")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	h := newHTTPClient(time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var output any
	if err := requestJSON(ctx, h, server.URL, "", http.MethodGet, "/exam", nil, &output); err == nil {
		t.Fatal("cancelled request accepted")
	}
}
