package main

import (
	_ "embed"
	"log"

	"github.com/egoist/mygo"
)

//go:embed resources/icon.png
var appIconPNG []byte

//go:embed resources/icon-tray.png
var trayIconPNG []byte

// setupTray creates the notification-area icon with its menu and wires the
// JiYu-style close-to-tray behavior: closing the window hides it, quitting
// happens only from the tray menu.
func (c *Console) setupTray() {
	menu := mygo.NewMenu([]*mygo.MenuItem{
		{Label: "显示主窗口", Click: func(*mygo.MenuItem, *mygo.Window) { c.showMainWindow() }},
		{Label: "隐藏到托盘", Click: func(*mygo.MenuItem, *mygo.Window) { c.hideToTray() }},
		mygo.Separator(),
		{Label: "一键解锁", Click: func(*mygo.MenuItem, *mygo.Window) { c.unlockFromTray() }},
		mygo.Separator(),
		{Label: "退出 LocalMesh 课堂", Click: func(*mygo.MenuItem, *mygo.Window) { c.quitFromTray() }},
	})

	tray, err := mygo.NewTray(mygo.TrayOptions{
		ID:      "main",
		Icon:    trayIconPNG,
		ToolTip: "LocalMesh 课堂 — 教师端控制台",
		Menu:    menu,
	})
	if err != nil {
		log.Printf("[Tray] init failed: %v", err)
		return
	}

	// Left click toggles the main window, like classroom software does.
	_ = tray.OnClick(func() { c.toggleMainWindow() })

	// Closing the window hides it to the tray instead of quitting — unless
	// a real quit is under way, whose window teardown must not be blocked.
	c.win.OnClose(func(e *mygo.CloseEvent) {
		if c.quitting {
			return
		}
		e.PreventDefault()
		c.hideToTray()
	})
}

// quitFromTray marks the quit so OnClose lets the window die, then quits.
func (c *Console) quitFromTray() {
	c.quitting = true
	mygo.App.Quit()
}

// hideToTray hides the main window; monitor child windows stay visible.
func (c *Console) hideToTray() {
	c.win.Hide()
}

// unlockFromTray unlocks the screens and syncs the console UI immediately.
func (c *Console) unlockFromTray() {
	if err := c.lockMgr.Unlock(); err != nil {
		return // already unlocked — nothing to sync
	}
	_ = EventLockStatus.Emit(c.win, c.lockMgr.Status())
}

// toggleMainWindow shows and focuses the window, or hides it when visible.
func (c *Console) toggleMainWindow() {
	if c.win == nil {
		return
	}
	if c.win.IsVisible() {
		c.hideToTray()
	} else {
		c.showMainWindow()
	}
}

// showMainWindow brings the window back to the front.
func (c *Console) showMainWindow() {
	if c.win == nil {
		return
	}
	c.win.Restore()
	c.win.Show()
	c.win.Focus()
}
