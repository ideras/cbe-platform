package clientwire

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestServerOriginAndTransportBoundaries(t *testing.T) {
	for _, input := range []string{"https://host", "http://user:secret@host", "http://host/path", "http://host?x", "http://host#x", "http://host:", "http://host:0", "http://host:65536", "http://ho\nst/"} {
		if _, err := ValidateServer(input); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
	for _, input := range []string{"http://127.0.0.1:8080", "http://[::1]:8080/", " http://instructor.local "} {
		if _, err := ValidateServer(input); err != nil {
			t.Fatal(err)
		}
	}
	var forwarded atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Add(1) }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }))
	defer source.Close()
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("ALL_PROXY", "http://127.0.0.1:1")
	client := NewHTTPClient(time.Second)
	defer client.CloseIdleConnections()
	resp, err := client.Get(source.URL)
	if err != nil {
		t.Fatal("environment proxy used", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 302 || forwarded.Load() != 0 {
		t.Fatal("redirect followed")
	}
}
