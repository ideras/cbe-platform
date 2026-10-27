package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// tokenLabel is how the window names a file written by writeToken: its folder and name.
var tokenLabel = filepath.Join("keys", "admin-token")

func writeToken(t *testing.T, token string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "keys", "admin-token")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(token+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

// tokenFixture is a dashboard started without any credential, whose file dialog returns whatever *chosen holds.
type tokenFixture struct {
	*Service
	server  *httptest.Server
	chosen  *string
	seen    *atomic.Value // the Authorization header the server last received
	dialogs []FileDialog  // every token file dialog the window opened
}

func newTokenFixture(t *testing.T, launchToken string) *tokenFixture {
	t.Helper()
	f := &tokenFixture{chosen: new(string), seen: new(atomic.Value)}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.seen.Store(r.Header.Get("Authorization"))
		if serveAdminState(w, r) {
			return
		}
		reply(w, roster())
	}))
	t.Cleanup(f.server.Close)
	opts := DefaultOptions()
	opts.Server, opts.Token, opts.ExamID, opts.Poll, opts.Timeout = f.server.URL, launchToken, examID, 15*time.Millisecond, time.Second
	opts.Choose = func(dialog FileDialog) (string, error) {
		if dialog.Title != "Choose the proctor token file" || dialog.Pattern != "" {
			t.Errorf("dialog = %+v", dialog)
		}
		f.dialogs = append(f.dialogs, dialog)
		return *f.chosen, nil
	}
	var err error
	if f.Service, err = New(opts); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.ServiceShutdown)
	return f
}

func TestChosenTokenFileConnectsWithoutTheTokenEverReachingTheWindow(t *testing.T) {
	f := newTokenFixture(t, "")
	if _, err := f.Connect(f.server.URL); err == nil || !strings.Contains(err.Error(), "token file") {
		t.Fatalf("connect with no credential = %v", err)
	}
	*f.chosen = writeToken(t, secret, 0600)
	choice, err := f.ChooseCredential() // works before connecting
	if err != nil || choice.Cancelled || choice.File != tokenLabel {
		t.Fatalf("choice = %+v, %v", choice, err)
	}
	if f.Snapshot().CredentialFile != tokenLabel || f.Snapshot().Connected {
		t.Fatalf("staged view = %+v", f.Snapshot())
	}
	if _, err := f.Connect(f.server.URL); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool { return f.Snapshot().Loaded })
	if got := f.seen.Load(); got != "Bearer "+secret {
		t.Fatalf("server saw %v", got)
	}
	view := f.Snapshot()
	if view.CredentialFile != tokenLabel || !view.Connected {
		t.Fatalf("connected view = %+v", view)
	}
	for _, value := range []any{choice, view} {
		data, err := json.Marshal(value)
		if err != nil || strings.Contains(string(data), secret) {
			t.Fatalf("token reached the window: %s %v", data, err)
		}
	}
	// A reconnect to the same server reuses it without asking again.
	if _, err := f.Connect(f.server.URL); err != nil {
		t.Fatal(err)
	}
}

func TestCancellingTheDialogChoosesNothing(t *testing.T) {
	f := newTokenFixture(t, "")
	choice, err := f.ChooseCredential()
	if err != nil || !choice.Cancelled || choice.File != "" || f.Snapshot().CredentialFile != "" {
		t.Fatalf("cancel = %+v, %v", choice, err)
	}
	if _, err := f.Connect(f.server.URL); err == nil {
		t.Fatal("connected without any credential")
	}
}

func TestUnsafeTokenFilesAreRefusedAndKeepTheEarlierChoice(t *testing.T) {
	f := newTokenFixture(t, "")
	*f.chosen = writeToken(t, secret, 0600)
	if _, err := f.ChooseCredential(); err != nil {
		t.Fatal(err)
	}
	bad := map[string]string{"too short": writeToken(t, "short", 0600), "directory": t.TempDir(), "missing": filepath.Join(t.TempDir(), "nope")}
	if runtime.GOOS != "windows" {
		bad["readable by others"] = writeToken(t, secret, 0644)
		link := filepath.Join(t.TempDir(), "link")
		if err := os.Symlink(writeToken(t, secret, 0600), link); err != nil {
			t.Fatal(err)
		}
		bad["symlink"] = link
	}
	for name, path := range bad {
		*f.chosen = path
		choice, err := f.ChooseCredential()
		if err == nil || strings.Contains(err.Error(), secret) {
			t.Fatalf("%s: err = %v", name, err)
		}
		if choice.File != credentialLabel(path) {
			t.Fatalf("%s: choice = %+v", name, choice)
		}
		if f.Snapshot().CredentialFile != tokenLabel {
			t.Fatalf("%s replaced the earlier choice: %q", name, f.Snapshot().CredentialFile)
		}
	}
	if _, err := f.Connect(f.server.URL); err != nil {
		t.Fatal("the earlier valid choice was lost:", err)
	}
}

func TestChosenFileReplacesTheLaunchCredentialAndShutdownForgetsIt(t *testing.T) {
	const other = "another-private-proctor-credential-for-this-test"
	f := newTokenFixture(t, secret)
	*f.chosen = writeToken(t, other, 0600)
	if _, err := f.ChooseCredential(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Connect(f.server.URL); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool { return f.seen.Load() == "Bearer "+other })
	f.ServiceShutdown()
	if _, err := f.ChooseCredential(); err == nil {
		t.Fatal("chose a credential after shutdown")
	}
	if f.Snapshot().CredentialFile != "" {
		t.Fatal("shutdown kept the credential file")
	}
}

func TestLaunchCredentialStillWorksWhenNothingIsChosen(t *testing.T) {
	f := newTokenFixture(t, secret)
	if _, err := f.Connect(f.server.URL); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool { return f.Snapshot().Loaded })
	if f.Snapshot().CredentialFile != "" || f.seen.Load() != "Bearer "+secret {
		t.Fatalf("view = %+v seen %v", f.Snapshot(), f.seen.Load())
	}
}

func TestCredentialDialogNeedsABuildThatCanChooseFiles(t *testing.T) {
	f := newTokenFixture(t, "")
	f.Service.opts.Choose = nil
	if _, err := f.ChooseCredential(); err == nil {
		t.Fatal("chose a file without a dialog")
	}
}
