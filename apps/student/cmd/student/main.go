package main

import (
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"sync/atomic"
	"time"

	student "exam-platform/apps/student"
	"exam-platform/apps/student/internal/client"
	"exam-platform/apps/student/internal/remotewatch"
	"exam-platform/apps/student/internal/sessionwatch"
	"exam-platform/apps/student/internal/spike"
	"exam-platform/apps/student/internal/sysvolume"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// version is the release, set by the build: go build -ldflags "-X main.version=v0.15.0".
var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print the version and exit")
	deterrence := flag.Bool("deterrence-spike", false, "test fullscreen and always-on-top; not a security boundary")
	capture := flag.Bool("capture-spike", false, "compatibility option; manual capture controls are available by default")
	worker := flag.Bool("capture-spike-worker", false, "internal one-shot PNG capture worker")
	defaults := client.DefaultOptions()
	server := flag.String("server", defaults.Server, "instructor's server address (http:// on the exam LAN, https:// over the internet)")
	debounce := flag.Duration("autosave-debounce", defaults.Debounce, "edit debounce before a full-snapshot save")
	periodic := flag.Duration("autosave-interval", defaults.Periodic, "periodic save backstop during continuous edits")
	poll := flag.Duration("status-interval", defaults.Poll, "server status polling cadence")
	flag.Parse()
	if *showVersion {
		fmt.Println("cbe-portal-student", version)
		return
	}
	if *worker {
		if !*capture {
			fmt.Fprintln(os.Stderr, "capture worker requires --capture-spike")
			os.Exit(2)
		}
		if err := spike.CaptureDesktopPNG(os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	defaults.Server, defaults.Debounce, defaults.Periodic, defaults.Poll = *server, *debounce, *periodic, *poll
	defaults.Version = version
	defaults.Sound.Control = sysvolume.System{}
	if runtime.GOOS == "linux" {
		// Only Linux (PulseAudio/PipeWire) tells the application at once that the volume changed; Windows and macOS keep to
		// the heartbeat and periodic checks.
		defaults.Sound.Watch = sysvolume.System{}.Watch
	}
	defaults.Sound.OnError = func(operation string, err error) { logger.Warn("system sound", "operation", operation, "error", err) }
	var nativeWindow *application.WebviewWindow
	var nativeApp *application.App
	var examClient *client.Service
	var closing atomic.Bool
	defaults.OnCompletion = func() {
		application.InvokeSync(func() {
			if !closing.Load() {
				nativeWindow.SetAlwaysOnTop(false)
				nativeWindow.UnFullscreen()
			}
		})
	}
	defaults.OnClose = func() { closing.Store(true); nativeWindow.Close() }
	// While the student reconnects after asking for it, the window is an ordinary one: full screen and on top of everything
	// else it hides the computer's network settings, and the student could not change network. It is full screen again when
	// the instructor unlocks the exam, and takes the focus so that the student does not have to find it.
	defaults.Monitoring.OnWindowRelaxed = func(relaxed bool) {
		// Each of these window calls already runs on the main thread by itself, as OnParticipation's do: wrapping them all in
		// one InvokeSync would only hold this goroutine, which also drives the alarm and the screenshots, for the whole sequence.
		if closing.Load() {
			return
		}
		if relaxed {
			nativeWindow.SetAlwaysOnTop(false)
			nativeWindow.UnFullscreen()
			return
		}
		nativeWindow.Fullscreen()
		nativeWindow.SetAlwaysOnTop(true)
		nativeWindow.Focus()
		// Some window managers drop a request for focus made while the window is still changing state, and some never grant
		// it to a window the student did not click: ask again once it has settled. The window shows a note to click it when
		// it is still not the active one.
		time.AfterFunc(500*time.Millisecond, func() {
			if !closing.Load() {
				nativeWindow.Focus()
			}
		})
	}
	defaults.Monitoring.OnParticipation = func() {
		// Fullscreen/topmost are bypassable deterrents, not a security boundary.
		nativeWindow.Fullscreen()
		nativeWindow.SetAlwaysOnTop(true)
		client.ObserveNativeFocus(examClient, nativeWindow.IsFocused)
	}
	nativeCapture := spike.New()
	defaults.Screenshots.Test = func(ctx context.Context) (client.CaptureTestResult, error) {
		result, err := nativeCapture.TestScreenshot(ctx)
		return client.CaptureTestResult{Width: result.Width, Height: result.Height}, err
	}
	defaults.Screenshots.Capture = spike.CaptureExamPNG
	defaults.Screenshots.Permission = func(request bool) client.CapturePermission {
		var result spike.CapturePermission
		if request {
			application.InvokeSync(func() { result = spike.CheckCapturePermission(true) })
		} else {
			result = spike.CheckCapturePermission(false)
		}
		return client.CapturePermission{Supported: result.Supported, Granted: result.Granted, TestRequired: result.TestRequired, Message: result.Message}
	}
	defaults.Monitoring.OnAlert = func(incident client.Incident) { nativeApp.Event.Emit("exam:unfocus-alert", incident) }
	defaults.Monitoring.OnIncident = func(incident client.Incident) { nativeApp.Event.Emit("exam:unfocus-hook", incident) }
	// A workspace exam's terminal output and the server's messages reach the page as events; the page never has a connection of its own.
	defaults.Workspace.OnOutput = func(term uint32, data []byte) {
		nativeApp.Event.Emit("workspace:output", map[string]any{"term": term, "data": base64.StdEncoding.EncodeToString(data)})
	}
	defaults.Workspace.OnMessage = func(message string) { nativeApp.Event.Emit("workspace:message", message) }
	examClient, err := client.New(defaults)
	if err != nil {
		logger.Error("student configuration invalid", "error", err)
		os.Exit(1)
	}
	defer examClient.ServiceShutdown()
	// A student who switches to a text console (Ctrl+Alt+F3) or to another user leaves the exam without the window losing its
	// focus: the session being off screen counts as leaving the window.
	watching, stopWatching := context.WithCancel(context.Background())
	defer stopWatching()
	go sessionwatch.Watch(watching, func(active bool, reason sessionwatch.Reason) {
		client.ReportSessionActive(examClient, active, string(reason))
	})
	// A Remote Desktop session, or remote-control software that the server named at the login, is told to the instructor.
	go remotewatch.Watch(watching, func() []string { return client.RemoteTools(examClient) }, func(found []remotewatch.Finding) {
		findings := make([]client.RemoteFinding, len(found))
		for i, finding := range found {
			// The two types have the same fields: a field added to one and not the other stops this from compiling, where a copy
			// field by field would drop it without a word.
			findings[i] = client.RemoteFinding(finding)
		}
		client.ReportRemote(examClient, findings)
	})
	app := application.New(application.Options{
		Name: "cbe-portal-student", Description: "CBE Portal student application",
		Services: []application.Service{application.NewService(examClient), application.NewService(nativeCapture)},
		Assets:   application.AssetOptions{Handler: application.AssetFileServerFS(student.Assets)},
		Mac:      application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: true},
	})
	nativeApp = app
	options := application.WebviewWindowOptions{Title: "CBE Portal", Width: 1000, Height: 700, URL: "/"}
	if *deterrence {
		// Native window constraints are bypassable deterrents, not a security boundary.
		options.AlwaysOnTop = true
		options.StartState = application.WindowStateFullscreen
	}
	window := app.Window.NewWithOptions(options)
	nativeWindow = window
	reportFocus := func(*application.WindowEvent) { client.ObserveNativeFocus(examClient, window.IsFocused) }
	window.OnWindowEvent(events.Common.WindowFocus, reportFocus)
	window.OnWindowEvent(events.Common.WindowLostFocus, reportFocus)
	window.RegisterHook(events.Common.WindowClosing, func(*application.WindowEvent) { closing.Store(true); client.CloseMonitoring(examClient) })
	app.OnShutdown(func() { closing.Store(true); client.CloseMonitoring(examClient) })
	if err := app.Run(); err != nil {
		logger.Error("student application failed", "error", err)
		os.Exit(1)
	}
}
