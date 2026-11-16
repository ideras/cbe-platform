//go:build pprof

package main

import (
	"log"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
)

// A build made with -tags pprof serves Go's profiler on the address in CBE_PPROF (a loopback address) and on nothing else: the load test uses it to find
// where the server's time goes (deploy/workspace/test-load.sh, CBE_LOAD_PROFILE=1). A normal build does not contain this file.
func init() {
	addr := os.Getenv("CBE_PPROF")
	if addr == "" {
		return
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil || (host != "127.0.0.1" && host != "::1" && host != "localhost") {
		log.Fatalf("CBE_PPROF must be a loopback address, not %q", addr)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/heap", pprof.Handler("heap").ServeHTTP)
	mux.HandleFunc("/debug/pprof/goroutine", pprof.Handler("goroutine").ServeHTTP)
	go func() { log.Printf("profiler on %s", addr); log.Println(http.ListenAndServe(addr, mux)) }()
}
