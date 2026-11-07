package remotewatch

import (
	"context"
	"flag"
	"strings"
	"testing"
	"time"
)

var (
	live      = flag.Duration("live", 0, "watch the real computer for this long, printing what is found (TestLiveWatch)")
	liveTools = flag.String("tools", "teamviewer*,anydesk*,rustdesk*,mstsc,msra,quickassist,winvnc*,tvnserver,x11vnc,xvnc*,screensharingd,notepad", "comma-separated program names for TestLiveWatch")
)

// Watching the real computer by hand, on any platform: start it, connect with a remote desktop client or start a program of the
// list, and read what is found, with the times. It does nothing unless -live is given.
//
//	go test ./internal/remotewatch -run TestLiveWatch -v -live 3m
func TestLiveWatch(t *testing.T) {
	if *live <= 0 {
		t.Skip("give -live <duration> to watch the real computer")
	}
	ctx, cancel := context.WithTimeout(t.Context(), *live)
	defer cancel()
	var tools []string
	for _, name := range strings.Split(*liveTools, ",") {
		if name = strings.TrimSpace(name); name != "" {
			tools = append(tools, name)
		}
	}
	stamp := func() string { return time.Now().Format("15:04:05.000") }
	t.Logf("%s watching for %v for: %v", stamp(), *live, tools)
	Watch(ctx, func() []string { return tools }, func(findings []Finding) {
		if len(findings) == 0 {
			t.Logf("%s nothing found any more", stamp())
			return
		}
		for _, finding := range findings {
			t.Logf("%s found: %s %q %q", stamp(), finding.Kind, finding.Name, finding.Detail)
		}
	})
	t.Logf("%s done", stamp())
}
