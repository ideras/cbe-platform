package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const otherSecret = "another-private-proctor-credential-for-this-test"

func TestConfigFileRoundTripIsPrivateAndAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "cbe-portal-proctor", "config.json")
	if config, err := loadConfig(path); err != nil || config != (Config{}) {
		t.Fatalf("missing file = %+v, %v", config, err)
	}
	want := Config{Server: "http://192.168.1.10:8080", TokenFile: filepath.Join(t.TempDir(), "keys", "admin-token")}
	if err := saveConfig(path, want); err != nil {
		t.Fatal(err)
	}
	if err := saveConfig(path, want); err != nil { // replacing an existing file works too
		t.Fatal(err)
	}
	if got, err := loadConfig(path); err != nil || got != want {
		t.Fatalf("round trip = %+v, %v", got, err)
	}
	if runtime.GOOS != "windows" {
		for p, mode := range map[string]os.FileMode{path: 0o600, filepath.Dir(path): 0o700} {
			if info, err := os.Stat(p); err != nil || info.Mode().Perm() != mode {
				t.Fatalf("%s mode = %v, %v", p, info.Mode().Perm(), err)
			}
		}
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary files left behind: %v %v", entries, err)
	}
}

func TestConfigFileProblemsAreReportedAndIgnored(t *testing.T) {
	dir := t.TempDir()
	write := func(content string) string {
		path := filepath.Join(dir, "config.json")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	for name, content := range map[string]string{"not JSON": "{token_file:", "wrong type": `{"token_file": 7}`, "too large": `{"token_file":"/` + strings.Repeat("a", maxConfigFile) + `"}`} {
		if config, err := loadConfig(write(content)); err == nil || config != (Config{}) {
			t.Fatalf("%s accepted: %+v, %v", name, config, err)
		}
	}
	config, err := loadConfig(write(`{"token_file":"relative/admin-token","future_option":true}`))
	if err != nil || config != (Config{}) {
		t.Fatalf("relative paths must be dropped and unknown keys tolerated: %+v, %v", config, err)
	}
}

// Earlier versions also remembered the last roster file. A settings file that still has it must keep working: the server and
// the token file are picked up as before, and the key is gone the next time the settings are saved.
func TestASettingsFileThatAlsoKeptARosterStillLoadsAndLosesTheKeyOnTheNextSave(t *testing.T) {
	token := writeToken(t, secret, 0o600)
	f := newConfigFixture(t, func(opts *Options, config string) {
		if err := os.MkdirAll(filepath.Dir(config), 0o700); err != nil {
			t.Fatal(err)
		}
		earlier, err := json.Marshal(map[string]string{"server_url": opts.Server, "token_file": token, "roster_file": "/rosters/class-a.csv"})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(config, earlier, 0o600); err != nil {
			t.Fatal(err)
		}
	})
	if view := f.Snapshot(); view.CredentialFile != tokenLabel || view.Notice != "" {
		t.Fatalf("the remembered token file was not picked up: %+v", view)
	}
	wait(t, func() bool { return f.Snapshot().CatalogLoaded }) // it connects by itself, from the remembered server and token file
	data, err := os.ReadFile(f.config)
	var saved map[string]any
	decodeErr := json.Unmarshal(data, &saved)
	_, hasRoster := saved["roster_file"]
	if err != nil || decodeErr != nil || saved["token_file"] != token || saved["server_url"] != f.server.URL || hasRoster {
		t.Fatalf("settings after the next save: %s, %v", data, err)
	}
}

// configFixture is a dashboard with a configuration file, talking to a server that accepts only `secret`.
type configFixture struct {
	*Service
	server  *httptest.Server
	config  string
	dialogs []FileDialog
	chosen  map[string]string // dialog title -> the path the operator picks
	seen    chan string       // Authorization headers received
}

func newConfigFixture(t *testing.T, configure func(*Options, string)) *configFixture {
	t.Helper()
	f := &configFixture{config: filepath.Join(t.TempDir(), "settings", "config.json"), chosen: map[string]string{}, seen: make(chan string, 1000)}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case f.seen <- r.Header.Get("Authorization"):
		default:
		}
		if r.Header.Get("Authorization") != "Bearer "+secret {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/admin/classes" {
			reply(w, map[string]any{"students": 2, "warning_count": 0, "warnings": []any{}, "applied": false, "class": nil})
			return
		}
		if r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/roster") {
			reply(w, map[string]any{"students": 2, "added": 1, "removed": 0, "warning_count": 0, "warnings": []any{}, "applied": false, "admitting": false, "pruned": 0})
			return
		}
		if serveAdminState(w, r) {
			return
		}
		reply(w, roster())
	}))
	t.Cleanup(f.server.Close)
	opts := DefaultOptions()
	opts.Server, opts.ExamID, opts.Poll, opts.Timeout, opts.ConfigFile = f.server.URL, examID, 15*time.Millisecond, time.Second, f.config
	opts.Choose = func(dialog FileDialog) (string, error) {
		f.dialogs = append(f.dialogs, dialog)
		return f.chosen[dialog.Title], nil
	}
	if configure != nil {
		configure(&opts, f.config)
	}
	var err error
	if f.Service, err = New(opts); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.ServiceShutdown)
	return f
}

func (f *configFixture) remembered(t *testing.T) Config {
	t.Helper()
	config, err := loadConfig(f.config)
	if err != nil {
		t.Fatal(err)
	}
	return config
}

func (f *configFixture) lastDialog(t *testing.T) FileDialog {
	t.Helper()
	if len(f.dialogs) == 0 {
		t.Fatal("no dialog was opened")
	}
	return f.dialogs[len(f.dialogs)-1]
}

const tokenTitle, rosterTitle, newRosterTitle = "Choose the proctor token file", "Choose the class roster file", "Choose the new roster file"

func TestTokenFileAndServerAreRememberedOnlyOnceTheServerAcceptsThem(t *testing.T) {
	f := newConfigFixture(t, nil)
	wrong := writeToken(t, otherSecret, 0o600)
	f.chosen[tokenTitle] = wrong
	if _, err := f.ChooseCredential(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Connect(f.server.URL); err == nil || !strings.Contains(err.Error(), "rejected the token") {
		t.Fatalf("connect with a rejected token = %v", err)
	}
	if _, err := os.Stat(f.config); err == nil {
		t.Fatal("a token file or server the server rejected was remembered")
	}

	right := writeToken(t, secret, 0o600)
	f.chosen[tokenTitle] = right
	if _, err := f.ChooseCredential(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Connect(f.server.URL); err != nil {
		t.Fatal(err)
	}
	if got := f.remembered(t); got.TokenFile != right || got.Server != f.server.URL {
		t.Fatalf("remembered %+v, want %s and %s", got, right, f.server.URL)
	}
	if data, _ := os.ReadFile(f.config); strings.Contains(string(data), secret) {
		t.Fatal("the token itself was written to the configuration file")
	}
}

func TestRememberedTokenFileConnectsAtTheNextStart(t *testing.T) {
	token := writeToken(t, secret, 0o600)
	f := newConfigFixture(t, func(_ *Options, config string) {
		if err := saveConfig(config, Config{TokenFile: token}); err != nil {
			t.Fatal(err)
		}
	})
	if view := f.Snapshot(); view.CredentialFile != tokenLabel || view.Notice != "" {
		t.Fatalf("view = %+v", view)
	}
	if _, err := f.Connect(f.server.URL); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool { return f.Snapshot().CatalogLoaded })
	if got := <-f.seen; got != "Bearer "+secret {
		t.Fatalf("server saw %q", got)
	}
	f.chosen[tokenTitle] = ""
	if _, err := f.ChooseCredential(); err != nil {
		t.Fatal(err)
	}
	if dialog := f.lastDialog(t); dialog.Directory != filepath.Dir(token) {
		t.Fatalf("the dialog opens in %q, want the remembered token's folder %q", dialog.Directory, filepath.Dir(token))
	}
}

func TestTokenFileGivenAtLaunchBeatsTheRememberedOne(t *testing.T) {
	remembered := writeToken(t, otherSecret, 0o600)
	launched := writeToken(t, secret, 0o600)
	f := newConfigFixture(t, func(opts *Options, config string) {
		if err := saveConfig(config, Config{TokenFile: remembered}); err != nil {
			t.Fatal(err)
		}
		opts.Token, opts.TokenFile = secret, launched
	})
	if _, err := f.Connect(f.server.URL); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool { return f.Snapshot().CatalogLoaded })
	if f.Snapshot().CredentialFile != "" || f.remembered(t).TokenFile != remembered {
		t.Fatalf("launch credential should win without touching the remembered one: %+v", f.Snapshot())
	}
	if _, err := f.ChooseCredential(); err != nil {
		t.Fatal(err)
	}
	if dialog := f.lastDialog(t); dialog.Directory != filepath.Dir(launched) {
		t.Fatalf("the dialog opens in %q, want the launch token's folder %q", dialog.Directory, filepath.Dir(launched))
	}
}

func TestAnUnusableRememberedTokenFileIsReportedNotUsed(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "gone", "admin-token")
	f := newConfigFixture(t, func(_ *Options, config string) {
		if err := saveConfig(config, Config{TokenFile: missing}); err != nil {
			t.Fatal(err)
		}
	})
	view := f.Snapshot()
	if view.CredentialFile != "" || !strings.Contains(view.Notice, filepath.Join("gone", "admin-token")) || !strings.Contains(view.Notice, "Choose it again") {
		t.Fatalf("view = %+v", view)
	}
	if _, err := f.Connect(f.server.URL); err == nil {
		t.Fatal("connected with a token file that no longer exists")
	}
	f.chosen[tokenTitle] = writeToken(t, secret, 0o600)
	if _, err := f.ChooseCredential(); err != nil {
		t.Fatal(err)
	}
	if f.Snapshot().Notice != "" {
		t.Fatalf("notice survived a new choice: %q", f.Snapshot().Notice)
	}
}

// Rosters belong to classes, so nothing about them is remembered: the roster dialogs open in the system's default folder and the
// settings hold no roster, however many files were chosen.
func TestRosterDialogsOpenInTheDefaultFolderAndTheSettingsNeverHoldARoster(t *testing.T) {
	f := newConfigFixture(t, nil)
	f.chosen[tokenTitle] = writeToken(t, secret, 0o600)
	if _, err := f.ChooseCredential(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Connect(f.server.URL); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool { return f.Snapshot().CatalogLoaded })

	roster := filepath.Join(t.TempDir(), "class-a.csv")
	if err := os.WriteFile(roster, []byte("canvas_id,account_number,name,email,sis_user_id\n1,0001,Ada,a@x.invalid,1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for title, choose := range map[string]func() (bool, error){
		rosterTitle:    func() (bool, error) { preview, err := f.ChooseClassFile(); return preview.Valid, err },
		newRosterTitle: func() (bool, error) { preview, err := f.ChooseRosterFile(classID); return preview.Valid, err },
	} {
		f.chosen[title] = roster
		if valid, err := choose(); err != nil || !valid {
			t.Fatalf("%s: the roster was not accepted: %v, %v", title, valid, err)
		}
		f.chosen[title] = "" // the next time the operator closes the dialog: where does it open?
		if _, err := choose(); err != nil {
			t.Fatal(err)
		}
		if dialog := f.lastDialog(t); dialog.Title != title || dialog.Directory != "" || dialog.Pattern != "*.csv" {
			t.Fatalf("%s dialog = %+v: it must not open in the folder of an earlier roster", title, dialog)
		}
	}
	data, err := os.ReadFile(f.config)
	if err != nil || strings.Contains(string(data), "roster") || strings.Contains(string(data), filepath.Dir(roster)) {
		t.Fatalf("a roster ended up in the settings: %s, %v", data, err)
	}
}

func TestTokenDialogFollowsTheLastFileEvenIfItWasRefused(t *testing.T) {
	f := newConfigFixture(t, nil)
	tooShort := writeToken(t, "short", 0o600)
	f.chosen[tokenTitle] = tooShort
	if _, err := f.ChooseCredential(); err == nil {
		t.Fatal("a too-short token was accepted")
	}
	f.chosen[tokenTitle] = ""
	if _, err := f.ChooseCredential(); err != nil {
		t.Fatal(err)
	}
	if dialog := f.lastDialog(t); dialog.Directory != filepath.Dir(tooShort) {
		t.Fatalf("dialog opens in %q, want %q", dialog.Directory, filepath.Dir(tooShort))
	}
	if _, err := os.Stat(f.config); err == nil {
		t.Fatal("a refused file was remembered")
	}
}

func TestSettingsThatCannotBeSavedAreReportedWithoutFailingTheAction(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	f := newConfigFixture(t, func(opts *Options, _ string) { opts.ConfigFile = filepath.Join(blocker, "config.json") })
	f.chosen[tokenTitle] = writeToken(t, secret, 0o600)
	if _, err := f.ChooseCredential(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Connect(f.server.URL); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool { return f.Snapshot().CatalogLoaded })
	if notice := f.Snapshot().Notice; !strings.Contains(notice, "could not be saved") {
		t.Fatalf("notice = %q", notice)
	}
}

func TestUnreadableSettingsStartTheDashboardEmptyWithANotice(t *testing.T) {
	f := newConfigFixture(t, func(_ *Options, config string) {
		if err := os.MkdirAll(filepath.Dir(config), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(config, []byte("{ not json"), 0o600); err != nil {
			t.Fatal(err)
		}
	})
	if view := f.Snapshot(); view.CredentialFile != "" || !strings.Contains(view.Notice, "not valid JSON") {
		t.Fatalf("view = %+v", view)
	}
	data, err := json.Marshal(f.Snapshot())
	if err != nil || strings.Contains(string(data), f.config) {
		t.Fatalf("the settings path leaked into the window: %s", data)
	}
}

func TestNoConfigFileRemembersNothingOnDisk(t *testing.T) {
	f := newConfigFixture(t, func(opts *Options, _ string) { opts.ConfigFile = "" })
	f.chosen[tokenTitle] = writeToken(t, secret, 0o600)
	if _, err := f.ChooseCredential(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Connect(f.server.URL); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool { return f.Snapshot().CatalogLoaded })
	if _, err := os.Stat(f.config); err == nil {
		t.Fatal("a configuration file was written although none was configured")
	}
}
