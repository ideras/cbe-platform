// Command cbe-loadtest makes a class of simulated students take a workspace exam on a running exam server, and reports how long everything took.
// It is the Phase 8 load test (docs/workspace/load-test.md): run it on the machine of the exam server, or on another one, so that the router is
// between the students and the server.
//
//	cbe-loadtest -server http://192.168.1.10:8080 -admin-token-file token -package pkg -students 40 -duration 30m -out results   (makes the class and the exam)
//	cbe-loadtest -server http://192.168.1.10:8080 -pin SECRET -students 40 -duration 30m -out results                          (an exam that is already there)
//
// The exam is the project of testdata/workspace/lab-carga, compiled by examc: `build/examc -json -bundles pkg testdata/workspace/lab-carga > pkg/document.json`.
package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"exam-platform/internal/loadtest"
)

func main() {
	var (
		server     = flag.String("server", "", "the exam server's address, http://host:port (required)")
		pin        = flag.String("pin", "", "the exam's PIN (with -admin-token-file and -package, the one the exam is made with; default: a random one)")
		students   = flag.Int("students", 40, "how many students")
		prefix     = flag.String("accounts-prefix", "LT", "the account numbers are this and 001, 002, ...")
		duration   = flag.Duration("duration", 30*time.Minute, "how long the students work once every workspace is up")
		ramp       = flag.Duration("ramp", 30*time.Second, "how long the logins are spread over")
		storms     = flag.String("storms", "", "moments (from the start of the work, 10m,20m) at which every student builds the whole project at once; default: the middle")
		out        = flag.String("out", "load-test-results", "the folder for the results")
		tokenFile  = flag.String("admin-token-file", "", "the admin token's file: with it the class and the exam are made and published first")
		packageDir = flag.String("package", "", "the folder examc compiled the load test's package into (document.json, starter.tar)")
		seed       = flag.Int64("seed", time.Now().UnixNano(), "the seed of the students' choices")
	)
	flag.Parse()
	if *server == "" {
		fmt.Fprintln(os.Stderr, "cbe-loadtest: -server is required")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	accounts := loadtest.Accounts(*prefix, *students)
	if *tokenFile != "" {
		token, err := os.ReadFile(*tokenFile)
		if err != nil || *packageDir == "" {
			fmt.Fprintln(os.Stderr, "cbe-loadtest: -admin-token-file needs a readable file and -package")
			os.Exit(2)
		}
		if *pin == "" {
			*pin = fmt.Sprintf("load-%d", time.Now().Unix())
		}
		examID, err := loadtest.Setup(ctx, *server, strings.TrimSpace(string(token)), *packageDir, fmt.Sprintf("Load test %s", time.Now().Format("2006-01-02 15:04")), *pin, accounts, *duration+30*time.Minute)
		if err != nil {
			fmt.Fprintln(os.Stderr, "cbe-loadtest:", err)
			os.Exit(1)
		}
		fmt.Printf("the class and the exam %s are made; the PIN is %s\n", examID, *pin)
	}
	if *pin == "" {
		fmt.Fprintln(os.Stderr, "cbe-loadtest: -pin is required for an exam that is already there")
		os.Exit(2)
	}
	cfg := loadtest.Config{Server: strings.TrimRight(*server, "/"), PIN: *pin, Accounts: accounts, Duration: *duration, Ramp: *ramp, Seed: *seed,
		Logf: func(format string, args ...any) { fmt.Printf(time.Now().Format("15:04:05 ")+format+"\n", args...) }}
	if *storms == "" {
		cfg.Storms = []time.Duration{*duration / 2}
	} else {
		for _, field := range strings.Split(*storms, ",") {
			at, err := time.ParseDuration(strings.TrimSpace(field))
			if err != nil {
				fmt.Fprintln(os.Stderr, "cbe-loadtest: -storms:", err)
				os.Exit(2)
			}
			cfg.Storms = append(cfg.Storms, at)
		}
	}
	host := &loadtest.Host{}
	cfg.Sample = host.Sample
	result, err := loadtest.Run(ctx, cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cbe-loadtest:", err)
		os.Exit(1)
	}
	if err := writeResults(*out, result); err != nil {
		fmt.Fprintln(os.Stderr, "cbe-loadtest:", err)
		os.Exit(1)
	}
	var report strings.Builder
	result.Summary.Markdown(&report)
	fmt.Println()
	fmt.Print(report.String())
	fmt.Printf("\n%d of %d students worked for %v. The results are in %s.\n", result.Ready, result.Students, result.Steady.Round(time.Second), *out)
	for _, op := range result.Summary.Ops {
		if op.Op == loadtest.OpSave && op.P95MS > 300 {
			fmt.Printf("The 95th percentile of an autosave is %.0f ms: over the 300 ms target.\n", op.P95MS)
			os.Exit(3)
		}
	}
}

func writeResults(dir string, result *loadtest.Result) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(map[string]any{"students": result.Students, "ready": result.Ready, "steady_seconds": result.Steady.Seconds(), "summary": result.Summary}, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "summary.json"), data, 0o644); err != nil {
		return err
	}
	file, err := os.Create(filepath.Join(dir, "machine.csv"))
	if err != nil {
		return err
	}
	defer file.Close()
	return result.WriteSeries(csv.NewWriter(file))
}
