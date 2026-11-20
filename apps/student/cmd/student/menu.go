package main

import "github.com/wailsapp/wails/v3/pkg/application"

// examMenu is the student application's menu bar on macOS, the one system where Wails shows a menu without being given one. It is made
// once the application exists, because Wails names the application's items after it.
//
// The default menu Wails would show has keys that act on the exam's window, not on the page: Cmd+W closes the window, Cmd+R and
// Cmd+Shift+R load the page again, Cmd+0, Cmd+plus and Cmd+minus zoom it, Ctrl+Cmd+F leaves full screen, Cmd+M minimises the window,
// and Help › Learn More loads a web page in place of the exam. This menu keeps the application's own items (About, Hide, Quit) and the
// Edit menu, which the web view needs for Cmd+X, Cmd+C, Cmd+V, Cmd+Z and Cmd+A, and nothing else, so those keys reach the page (the
// workspace closes the file in front on Cmd+W) or do nothing. It has no Services menu, which hands the selected text to other applications.
func examMenu(name string) *application.Menu {
	menu := application.NewMenu()
	app := menu.AddSubmenu(name)
	app.AddRole(application.About)
	app.AddSeparator()
	app.AddRole(application.Hide)
	app.AddRole(application.HideOthers)
	app.AddRole(application.UnHide)
	app.AddSeparator()
	app.AddRole(application.Quit)
	menu.AddRole(application.EditMenu)
	return menu
}
