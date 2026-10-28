package config

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDefaultsAndValidation(t *testing.T) {
	cfg, err := Parse(nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Bind != ":8080" || cfg.WorkerInterval.String() != "1s" {
		t.Fatalf("defaults = %+v", cfg)
	}
	for _, args := range [][]string{
		{"-worker-interval=0s"},
		{"-bind=localhost"}, {"-bind=:0"}, {"-bind=:70000"},
		{"unexpected"}, {"-max-json-bytes=0"},
		{"-max-answer-bytes=-1"}, {"-max-answers-bytes=1"}, {"-max-json-bytes=100"},
	} {
		if _, err := Parse(args, io.Discard); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}

// scratch is a regular file next to the data directory that tests turn into every kind of wrong path.
func scratch(cfg Config) string {
	return filepath.Join(filepath.Dir(filepath.Dir(cfg.Database)), "scratch.txt")
}

func testConfig(t *testing.T) Config {
	t.Helper()
	dir := t.TempDir()
	cfg, err := Parse([]string{"-database=" + filepath.Join(dir, "data", "exam.db"), "-screenshots=" + filepath.Join(dir, "images"), "-admin-token-file=" + filepath.Join(dir, "token")}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scratch(cfg), []byte("csv"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.AdminTokenFile, []byte(strings.Repeat("a", 64)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := restrictTokenACL(cfg.AdminTokenFile); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestPreparePathsAndSecret(t *testing.T) {
	cfg := testConfig(t)
	if err := cfg.Prepare(); err != nil {
		t.Fatal(err)
	}
	if cfg.AdminToken != strings.Repeat("a", 64) {
		t.Fatal("token not trimmed")
	}
	if err := os.WriteFile(cfg.Database, []byte("keep existing"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Prepare(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(cfg.Database)
	if err != nil || string(data) != "keep existing" {
		t.Fatalf("database truncated: %q %v", data, err)
	}
	for _, path := range []string{cfg.Database, cfg.Screenshots, filepath.Dir(cfg.Database)} {
		info, err := os.Stat(path)
		if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0) {
			t.Fatalf("path not private: %s %v", path, err)
		}
	}
}

func TestPrepareRejectsInvalidPathsAndTokens(t *testing.T) {
	for _, scenario := range []string{"symlink database", "database directory", "short token", "token whitespace", "public token", "screenshots file"} {
		t.Run(scenario, func(t *testing.T) {
			cfg := testConfig(t)
			switch scenario {
			case "symlink database":
				cfg.Database = filepath.Join(filepath.Dir(scratch(cfg)), "link.db")
				if err := os.Symlink(scratch(cfg), cfg.Database); err != nil {
					if runtime.GOOS == "windows" {
						t.Skipf("symlink creation unavailable: %v", err)
					}
					t.Fatal(err)
				}
			case "database directory":
				cfg.Database = filepath.Dir(scratch(cfg))
			case "short token":
				if err := os.WriteFile(cfg.AdminTokenFile, []byte("short"), 0600); err != nil {
					t.Fatal(err)
				}
			case "token whitespace":
				if err := os.WriteFile(cfg.AdminTokenFile, []byte(strings.Repeat("a", 32)+" space"), 0600); err != nil {
					t.Fatal(err)
				}
			case "public token":
				if err := makeTokenPublic(cfg.AdminTokenFile); err != nil {
					t.Fatal(err)
				}
			case "screenshots file":
				cfg.Screenshots = scratch(cfg)
			}
			if err := cfg.Prepare(); err == nil {
				t.Fatal("expected preflight failure")
			}
		})
	}
}

func TestTheStartupRosterIsNoLongerAServerOption(t *testing.T) {
	var output strings.Builder
	if _, err := Parse([]string{"-roster=students.csv"}, &output); err == nil || !strings.Contains(output.String(), "flag provided but not defined: -roster") {
		t.Fatalf("err = %v, output = %q", err, output.String())
	}
}

func TestTheCurrentExamIsNoLongerAServerOption(t *testing.T) {
	var output strings.Builder
	if _, err := Parse([]string{"-current-exam=00000000-0000-4000-8000-000000000001"}, &output); err == nil || !strings.Contains(output.String(), "flag provided but not defined: -current-exam") {
		t.Fatalf("err = %v, output = %q", err, output.String())
	}
}
