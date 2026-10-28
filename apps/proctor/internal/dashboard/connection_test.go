package dashboard

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestConnectChecksTheServerBeforeReportingConnected(t *testing.T) {
	f := newConfigFixture(t, nil)
	if _, err := f.Connect(f.server.URL); err == nil || !strings.Contains(err.Error(), "token file") {
		t.Fatalf("no token chosen = %v", err)
	}
	if _, err := f.Connect("http://"); err == nil || !strings.Contains(err.Error(), "Invalid server address") {
		t.Fatalf("invalid address = %v", err)
	}
	if _, err := f.Connect("ftp://example.invalid"); err == nil || !strings.Contains(err.Error(), "Invalid server address") {
		t.Fatalf("wrong scheme = %v", err)
	}
	if len(f.seen) != 0 {
		t.Fatal("an invalid request reached the server")
	}

	f.chosen[tokenTitle] = writeToken(t, otherSecret, 0o600)
	if _, err := f.ChooseCredential(); err != nil {
		t.Fatal(err)
	}
	view, err := f.Connect(f.server.URL)
	if err == nil || !strings.Contains(err.Error(), "rejected the token") || view.Connected || view.Connecting || view.CatalogLoaded || view.ConnectError != err.Error() {
		t.Fatalf("rejected token: err = %v, view = %+v", err, view)
	}
	if strings.Contains(view.ConnectError, otherSecret) {
		t.Fatal("the token appears in the error")
	}

	// The staged file is kept, so the same attempt can be repeated once the server accepts it.
	f.chosen[tokenTitle] = writeToken(t, secret, 0o600)
	if _, err := f.ChooseCredential(); err != nil {
		t.Fatal(err)
	}
	view, err = f.Connect(f.server.URL)
	if err != nil || !view.Connected || view.ConnectError != "" || !view.CatalogLoaded || view.Server != f.server.URL {
		t.Fatalf("accepted token: err = %v, view = %+v", err, view)
	}
}

func TestAnUnreachableServerIsExplained(t *testing.T) {
	dead := httptest.NewServer(http.NotFoundHandler())
	address := dead.URL
	dead.Close()
	f := newConfigFixture(t, nil)
	f.chosen[tokenTitle] = writeToken(t, secret, 0o600)
	if _, err := f.ChooseCredential(); err != nil {
		t.Fatal(err)
	}
	view, err := f.Connect(address)
	if err == nil || !strings.Contains(err.Error(), "Cannot reach "+strings.TrimPrefix(address, "http://")) || !strings.Contains(err.Error(), "server is running") || view.Connected {
		t.Fatalf("err = %v, view = %+v", err, view)
	}
	if _, err := os.Stat(f.config); err == nil {
		t.Fatalf("an address that never answered was remembered: %+v", f.remembered(t))
	}
}

func TestAnAttemptIsVisibleWhileTheServerThinksAndCannotBeRepeated(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{}, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Count the request without ever blocking on a full buffer, and hold it only until the test releases it or the
		// client goes away; a handler parked on a send nobody drains keeps server.Close waiting forever.
		select {
		case entered <- struct{}{}:
		case <-release:
		case <-r.Context().Done():
			return
		}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		serveAdminState(w, r)
	}))
	t.Cleanup(server.Close)
	opts := DefaultOptions()
	opts.Server, opts.Token, opts.Poll = server.URL, secret, 15*time.Millisecond
	s, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.ServiceShutdown() })
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})

	done := make(chan error, 1)
	go func() { _, err := s.Connect(server.URL); done <- err }()
	<-entered
	if view := s.Snapshot(); !view.Connecting || view.Connected {
		t.Fatalf("view during the attempt = %+v", view)
	}
	if _, err := s.Connect(server.URL); err == nil || !strings.Contains(err.Error(), "already in progress") {
		t.Fatalf("second attempt = %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if view := s.Snapshot(); view.Connecting || !view.Connected {
		t.Fatalf("view after the attempt = %+v", view)
	}
}

func TestAFailedAttemptLeavesAnExistingConnectionAlone(t *testing.T) {
	f := newConfigFixture(t, nil)
	f.chosen[tokenTitle] = writeToken(t, secret, 0o600)
	if _, err := f.ChooseCredential(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Connect(f.server.URL); err != nil {
		t.Fatal(err)
	}
	dead := httptest.NewServer(http.NotFoundHandler())
	address := dead.URL
	dead.Close()
	// A token is never carried to another address on its own, so it has to be chosen again for the new one.
	if _, err := f.Connect(address); err == nil || !strings.Contains(err.Error(), "token file") {
		t.Fatalf("another address without choosing the token = %v", err)
	}
	f.chosen[tokenTitle] = writeToken(t, secret, 0o600)
	if _, err := f.ChooseCredential(); err != nil {
		t.Fatal(err)
	}
	view, err := f.Connect(address)
	if err == nil || !view.Connected || view.Server != f.server.URL || view.ConnectError == "" {
		t.Fatalf("err = %v, view = %+v", err, view)
	}
	wait(t, func() bool { return f.Snapshot().ClassesLoaded })
}

func rememberedFixture(t *testing.T, server, token string, configure func(*Options)) *configFixture {
	t.Helper()
	tokenPath := writeToken(t, token, 0o600)
	return newConfigFixture(t, func(opts *Options, config string) {
		remembered := Config{TokenFile: tokenPath}
		if server != "" {
			remembered.Server = opts.Server
		}
		if err := saveConfig(config, remembered); err != nil {
			t.Fatal(err)
		}
		if configure != nil {
			configure(opts)
		}
	})
}

func TestTheRememberedServerIsConnectedToAtStartup(t *testing.T) {
	f := rememberedFixture(t, "remember", secret, nil)
	wait(t, func() bool { return f.Snapshot().Connected && f.Snapshot().ClassesLoaded })
	view := f.Snapshot()
	if view.Server != f.server.URL || view.ConnectError != "" || view.CredentialFile != tokenLabel {
		t.Fatalf("view = %+v", view)
	}
	if len(view.KnownServers) != 1 || view.KnownServers[0] != f.server.URL {
		t.Fatalf("known servers = %v", view.KnownServers)
	}
}

func TestAServerGivenAtLaunchIsConnectedToAndBeatsTheRememberedOne(t *testing.T) {
	other := httptest.NewServer(http.NotFoundHandler())
	other.Close()
	f := newConfigFixture(t, func(opts *Options, config string) {
		if err := saveConfig(config, Config{Server: other.URL}); err != nil {
			t.Fatal(err)
		}
		opts.Token, opts.ServerSet = secret, true
	})
	wait(t, func() bool { return f.Snapshot().Connected })
	if view := f.Snapshot(); view.Server != f.server.URL {
		t.Fatalf("view = %+v", view)
	}
	if got := f.remembered(t).Server; got != f.server.URL {
		t.Fatalf("the address that worked was not remembered over the old one: %q", got)
	}
}

func TestNothingIsConnectedToAtStartupWithoutAServerAndAToken(t *testing.T) {
	// A remembered server but no usable token: nothing to authenticate with, so no attempt is made.
	f := rememberedFixture(t, "remember", secret, nil)
	f.ServiceShutdown()
	noToken := newConfigFixture(t, func(opts *Options, config string) {
		if err := saveConfig(config, Config{Server: opts.Server}); err != nil {
			t.Fatal(err)
		}
	})
	time.Sleep(100 * time.Millisecond)
	if view := noToken.Snapshot(); view.Connected || view.Connecting || view.ConnectError != "" || len(noToken.seen) != 0 {
		t.Fatalf("view = %+v, %d requests", view, len(noToken.seen))
	}
	// A token but no server that anyone chose: the built-in default address is not connected to on its own.
	plain := newConfigFixture(t, func(opts *Options, _ string) { opts.Token = secret })
	time.Sleep(100 * time.Millisecond)
	if view := plain.Snapshot(); view.Connected || view.Connecting || len(plain.seen) != 0 {
		t.Fatalf("view = %+v, %d requests", view, len(plain.seen))
	}
}

func TestAFailedStartupConnectionIsShownAndCanBeRetried(t *testing.T) {
	var up atomic.Bool
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if !up.Load() {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		serveAdminState(w, r)
	}))
	t.Cleanup(server.Close)
	token := writeToken(t, secret, 0o600)
	config := filepath.Join(t.TempDir(), "config.json")
	if err := saveConfig(config, Config{Server: server.URL, TokenFile: token}); err != nil {
		t.Fatal(err)
	}
	opts := DefaultOptions()
	opts.ConfigFile, opts.Poll, opts.Timeout = config, 15*time.Millisecond, time.Second
	s, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.ServiceShutdown)
	wait(t, func() bool { return s.Snapshot().ConnectError != "" })
	view := s.Snapshot()
	if view.Connected || view.Connecting || !strings.Contains(view.ConnectError, "did not answer as expected") || view.Server != server.URL {
		t.Fatalf("view = %+v", view)
	}
	up.Store(true)
	view, err = s.Connect(server.URL) // what the dialog's Connect button does
	if err != nil || !view.Connected || view.ConnectError != "" {
		t.Fatalf("retry: err = %v, view = %+v", err, view)
	}
}

func TestQuitAsksTheApplicationToClose(t *testing.T) {
	var closed atomic.Bool
	f := newConfigFixture(t, func(opts *Options, _ string) { opts.Quit = func() { closed.Store(true) } })
	f.Quit()
	if !closed.Load() {
		t.Fatal("Quit did nothing")
	}
	bare := newConfigFixture(t, nil)
	bare.Quit() // no hook: nothing to do, and no panic
}

func TestAnEstablishedConnectionSaysWhenTheServerStopsAnswering(t *testing.T) {
	var down atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if down.Load() {
			if hijacker, ok := w.(http.Hijacker); ok {
				if conn, _, err := hijacker.Hijack(); err == nil {
					conn.Close() // the connection drops without an answer
					return
				}
			}
		}
		if !serveAdminState(w, r) {
			reply(w, roster())
		}
	}))
	t.Cleanup(server.Close)
	opts := DefaultOptions()
	opts.Server, opts.Token, opts.Poll, opts.Timeout = server.URL, secret, 10*time.Millisecond, 200*time.Millisecond
	s, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.ServiceShutdown)
	if _, err := s.Connect(server.URL); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool { v := s.Snapshot(); return v.ClassesLoaded && !v.Unreachable && !v.Stale })
	down.Store(true)
	wait(t, func() bool { return s.Snapshot().Unreachable })
	if v := s.Snapshot(); !v.Connected || !v.Stale || len(v.Classes) != 2 {
		t.Fatalf("an outage must keep the last observations and the connection: %+v", v)
	}
	down.Store(false)
	wait(t, func() bool { v := s.Snapshot(); return !v.Unreachable && !v.Stale })
}
