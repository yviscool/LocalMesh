package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/egoist/mygo"
)

// monitorWindows tracks open HD monitor child windows by device id so a
// second double-click focuses the existing window instead of stacking.
var monitorWindows = struct {
	mu   sync.Mutex
	wins map[string]*mygo.Window
}{wins: map[string]*mygo.Window{}}

// OpenWindow opens a dedicated monitor window for the device, like JiYu's
// double-click monitor. An already-open window for the device is focused.
func (s *MonitorService) OpenWindow(_ context.Context, deviceID string) (string, error) {
	if _, err := s.agentScreenURL(deviceID); err != nil {
		return "", err
	}

	monitorWindows.mu.Lock()
	defer monitorWindows.mu.Unlock()

	if w, ok := monitorWindows.wins[deviceID]; ok {
		w.Restore()
		w.Show()
		w.Focus()
		return "focused", nil
	}

	w := mygo.NewWindow(mygo.WindowOptions{
		Title:           fmt.Sprintf("实时监控 — %s", deviceID),
		Width:           960,
		Height:          580,
		MinWidth:        480,
		MinHeight:       320,
		TitleBarStyle:   mygo.TitleBarHidden,
		TitleBarHeight:  36,
		BackgroundColor: "#07090e",
		URL:             "/monitor.html?device=" + deviceID,
	})
	monitorWindows.wins[deviceID] = w
	w.OnClosed(func() {
		monitorWindows.mu.Lock()
		delete(monitorWindows.wins, deviceID)
		monitorWindows.mu.Unlock()
	})
	return "opened", nil
}
