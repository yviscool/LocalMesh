package main

import (
	_ "embed"
	"log"

	"github.com/egoist/mygo"
)

//go:embed resources/icon-tray.png
var trayIconPNG []byte

// setupTray creates the student tray with quick actions.
func (ui *UI) setupTray() {
	menu := mygo.NewMenu([]*mygo.MenuItem{
		{Label: "显示悬浮条", Click: func(*mygo.MenuItem, *mygo.Window) { ui.openBar() }},
		{Label: "举手", Click: func(*mygo.MenuItem, *mygo.Window) { _ = ui.raiseHand("通过托盘举手") }},
		mygo.Separator(),
		{Label: "退出", Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }},
	})

	tray, err := mygo.NewTray(mygo.TrayOptions{
		ID:      "student",
		Icon:    trayIconPNG,
		ToolTip: "LocalMesh 学生端",
		Menu:    menu,
	})
	if err != nil {
		log.Printf("[Tray] init failed: %v", err)
		return
	}
	ui.tray = tray
}

// openBar shows (or creates) the always-on-top floating capsule bar.
func (ui *UI) openBar() {
	if ui.bar != nil {
		ui.bar.Show()
		ui.bar.Focus()
		return
	}

	work := mygo.Screen.PrimaryDisplay().WorkArea
	ui.bar = mygo.NewWindow(mygo.WindowOptions{
		Title:         "LocalMesh 学生端",
		Width:         250,
		Height:        58,
		X:             work.X + work.Width - 270,
		Y:             work.Y + work.Height - 78,
		Frameless:     true,
		AlwaysOnTop:   true,
		SkipTaskbar:   true,
		DisableResize: true,
		BackgroundColor: "#101623",
		URL:           "/bar.html",
	})
	ui.bar.OnClosed(func() { ui.bar = nil })
}

// openViewer opens the fullscreen broadcast viewing window.
func (ui *UI) openViewer() {
	if ui.viewer != nil {
		ui.viewer.Show()
		ui.viewer.Focus()
		return
	}
	ui.viewer = mygo.NewWindow(mygo.WindowOptions{
		Title:           "屏幕广播",
		FullScreen:      true,
		Frameless:       true,
		AlwaysOnTop:     true,
		BackgroundColor: "#000000",
		URL:             "/viewer.html",
	})
	ui.viewer.OnClosed(func() { ui.viewer = nil })
}

// closeViewer exits broadcast viewing.
func (ui *UI) closeViewer() {
	if ui.viewer != nil {
		w := ui.viewer
		ui.viewer = nil
		w.Close()
	}
}

// notifyPage relays an event to whichever page is alive.
func (ui *UI) notifyPage(event *mygo.Event[UiNotification], n UiNotification) {
	targets := make([]*mygo.Window, 0, 2)
	if ui.bar != nil {
		targets = append(targets, ui.bar)
	}
	if ui.viewer != nil {
		targets = append(targets, ui.viewer)
	}
	for _, w := range targets {
		_ = event.Emit(w, n)
	}
}
