package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"

	student "exam-platform/apps/student"
	"exam-platform/apps/student/internal/client"
	"exam-platform/apps/student/internal/spike"
	"exam-platform/apps/student/internal/sysvolume"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

func main() {
	deterrence := flag.Bool("deterrence-spike", false, "test fullscreen and always-on-top; not a security boundary")
	capture := flag.Bool("capture-spike", false, "compatibility option; manual capture controls are available by default")
	worker := flag.Bool("capture-spike-worker", false, "internal one-shot PNG capture worker")
	defaults := client.DefaultOptions()
	server := flag.String("server", defaults.Server, "instructor's isolated-LAN HTTP server address")
	debounce := flag.Duration("autosave-debounce", defaults.Debounce, "edit debounce before a full-snapshot save")
	periodic := flag.Duration("autosave-interval", defaults.Periodic, "periodic save backstop during continuous edits")
	poll := flag.Duration("status-interval", defaults.Poll, "server status polling cadence")
	flag.Parse()
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
	defaults.Sound.Control = sysvolume.System{}
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
	examClient, err := client.New(defaults)
	if err != nil {
		logger.Error("student configuration invalid", "error", err)
		os.Exit(1)
	}
	defer examClient.ServiceShutdown()
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
