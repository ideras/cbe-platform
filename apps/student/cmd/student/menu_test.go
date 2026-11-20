package main

import (
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// The keys of the menu are the ones the Edit menu needs and the application's own (Hide, Quit), and none that acts on the exam's window.
func TestTheMacMenuHasNoKeyThatClosesReloadsOrZoomsTheWindow(t *testing.T) {
	// Wails names the application's items after the application, so it must exist (it is not run).
	application.New(application.Options{Name: appName})
	menu := examMenu(appName)

	keys := map[string]string{} // accelerator -> the item's label
	labels := map[string]bool{}
	var walk func(*application.Menu)
	walk = func(m *application.Menu) {
		for i := 0; m.ItemAt(i) != nil; i++ {
			item := m.ItemAt(i)
			labels[item.Label()] = true
			if key := item.GetAccelerator(); key != "" {
				keys[key] = item.Label()
			}
			if sub := item.GetSubmenu(); sub != nil {
				walk(sub)
			}
		}
	}
	walk(menu)

	// Accelerators are compared as Wails writes them, so each is made the way the default menu makes it.
	key := func(accelerator string) string { return application.NewMenuItem("").SetAccelerator(accelerator).GetAccelerator() }
	for _, forbidden := range []string{"CmdOrCtrl+w", "CmdOrCtrl+r", "CmdOrCtrl+Shift+r", "CmdOrCtrl+0", "CmdOrCtrl+plus", "CmdOrCtrl+-", "Ctrl+Command+F", "F11", "CmdOrCtrl+M"} {
		if label, ok := keys[key(forbidden)]; ok {
			t.Errorf("the menu's %q has the key %s, which acts on the exam's window", label, forbidden)
		}
	}
	for _, needed := range []string{"CmdOrCtrl+x", "CmdOrCtrl+c", "CmdOrCtrl+v", "CmdOrCtrl+z", "CmdOrCtrl+a"} {
		if _, ok := keys[key(needed)]; !ok {
			t.Errorf("no item of the menu has the key %s, which the web view needs to edit text", needed)
		}
	}
	for _, gone := range []string{"Close", "Reload", "Force Reload", "Learn More", "Services", "Toggle Full Screen", "Minimise", "Zoom In"} {
		if labels[gone] {
			t.Errorf("the menu has %q", gone)
		}
	}
	if menu.ItemAt(1) == nil || menu.ItemAt(1).Label() != "Edit" || menu.ItemAt(2) != nil {
		t.Errorf("the menu bar is not the application's menu and Edit")
	}
}
