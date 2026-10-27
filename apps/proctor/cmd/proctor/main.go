package main

import (
	"flag"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"

	proctor "exam-platform/apps/proctor"
	"exam-platform/apps/proctor/internal/dashboard"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

func main() {
	opts := dashboard.DefaultOptions()
	server := flag.String("server", opts.Server, "isolated-LAN HTTP server address")
	exam := flag.String("exam-id", "", "exam UUID to monitor after connecting; default is the published exam. Selection does not change student admission")
	tokenFile := flag.String("admin-token-file", "", "private credential file; never pass the token itself as an argument")
	configFile := flag.String("config", "", "settings file where the dashboard remembers the server address and the last token file; default is cbe-portal-proctor/config.json in the user's configuration folder")
	flag.Parse()
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	opts.Server, opts.ExamID = *server, *exam
	flag.Visit(func(f *flag.Flag) { opts.ServerSet = opts.ServerSet || f.Name == "server" }) // only an address typed on purpose is connected to at startup
	if *tokenFile != "" {
		token, err := dashboard.ReadCredential(*tokenFile)
		if err != nil {
			logger.Error("invalid proctor credential file")
			os.Exit(1)
		}
		opts.Token = token
		if opts.TokenFile, err = filepath.Abs(*tokenFile); err != nil {
			opts.TokenFile = ""
		}
	}
	if *configFile == "" {
		if path, err := dashboard.DefaultConfigPath(); err == nil {
			*configFile = path
		}
	}
	if *configFile != "" {
		if path, err := filepath.Abs(*configFile); err == nil {
			opts.ConfigFile = path
		}
	}
	var app *application.App
	// quitting is set once the operator has confirmed closing; until then closing the main window only asks them.
	var quitting atomic.Bool
	opts.Quit = func() { quitting.Store(true); app.Quit() }
	// The dialog runs on the application's own UI thread; the app exists before any window can ask for a file.
	opts.Choose = func(request dashboard.FileDialog) (string, error) {
		dialog := app.Dialog.OpenFile().SetTitle(request.Title).CanChooseFiles(true).CanChooseDirectories(false)
		if request.Pattern != "" { // the token file has no extension to filter on
			dialog = dialog.AddFilter(request.FilterName, request.Pattern)
		}
		if request.Directory != "" {
			dialog = dialog.SetDirectory(request.Directory)
		}
		return dialog.PromptForSingleSelection()
	}
	opts.OpenWindow = func(request dashboard.WindowRequest) error {
		if window, ok := app.Window.GetByName(request.Name); ok {
			window.SetTitle(request.Title)
			window.SetURL(request.URL)
			window.Restore()
			window.Show()
			window.Focus()
			return nil
		}
		app.Window.NewWithOptions(application.WebviewWindowOptions{Name: request.Name, Title: request.Title, Width: request.Width, Height: request.Height, URL: request.URL})
		return nil
	}
	opts.Save = func(request dashboard.SaveDialog) (string, error) {
		return app.Dialog.SaveFile().SetFilename(request.Filename).AddFilter(request.FilterName, request.Pattern).PromptForSingleSelection()
	}
	opts.ChooseFolder = func(request dashboard.FolderDialog) (string, error) {
		dialog := app.Dialog.OpenFile().SetTitle(request.Title).CanChooseFiles(false).CanChooseDirectories(true).CanCreateDirectories(true)
		if request.Directory != "" {
			dialog = dialog.SetDirectory(request.Directory)
		}
		return dialog.PromptForSingleSelection()
	}
	// Giving a folder to the browser opens it in the system's file manager and returns at once.
	opts.OpenFolder = func(path string) error { return app.Browser.OpenFile(path) }
	service, err := dashboard.New(opts)
	if err != nil {
		logger.Error("invalid proctor configuration", "error", err)
		os.Exit(1)
	}
	defer service.ServiceShutdown()
	app = application.New(application.Options{Name: "cbe-portal-proctor", Description: "CBE Portal proctor: exam monitoring and review", Services: []application.Service{application.NewService(service)}, Assets: application.AssetOptions{Handler: application.AssetFileServerFS(proctor.Assets)}, Mac: application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: true}})
	dashboardWindow := app.Window.NewWithOptions(application.WebviewWindowOptions{Name: "main", Title: "CBE Portal Proctor", Width: 1280, Height: 850, URL: "/"})
	// The monitoring, review and image windows belong to the main one: closing it closes the dashboard, so it asks first.
	// The page shows the confirmation and calls Quit when the operator agrees.
	dashboardWindow.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		if !quitting.Load() {
			event.Cancel()
			app.Event.Emit("dashboard:close-requested")
		}
	})
	if err := app.Run(); err != nil {
		logger.Error("proctor application failed")
		os.Exit(1)
	}
}
