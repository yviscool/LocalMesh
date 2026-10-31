//go:build !windows

package main

import (
	"sync"
	"sync/atomic"
)

const (
	toolRedPen      = 0
	toolHighlighter = 1
	toolEraser      = 2
)

type pointDA struct {
	X, Y int32
}

type strokeDA struct {
	tool   int
	color  uint32
	width  int32
	points []pointDA
}

type DesktopAnnotator struct {
	mu          sync.RWMutex
	active      atomic.Bool
	currentTool int
	strokes     []strokeDA
	onClose     func()
}

func NewDesktopAnnotator(onClose func()) *DesktopAnnotator {
	return &DesktopAnnotator{
		currentTool: toolRedPen,
		onClose:     onClose,
	}
}

func (da *DesktopAnnotator) IsActive() bool {
	return da.active.Load()
}

func (da *DesktopAnnotator) Start() error {
	da.active.Store(true)
	return nil
}

func (da *DesktopAnnotator) Stop() {
	da.active.Store(false)
	if da.onClose != nil {
		da.onClose()
	}
}

func (da *DesktopAnnotator) Clear() {
	da.mu.Lock()
	defer da.mu.Unlock()
	da.strokes = nil
}

func (da *DesktopAnnotator) SetTool(tool int) {
	da.mu.Lock()
	defer da.mu.Unlock()
	da.currentTool = tool
}

// AddStroke is a test helper for cross-platform simulation.
func (da *DesktopAnnotator) AddStroke(tool int, points []pointDA) {
	da.mu.Lock()
	defer da.mu.Unlock()
	da.strokes = append(da.strokes, strokeDA{
		tool:   tool,
		points: points,
	})
}

// StrokeCount returns the number of active strokes.
func (da *DesktopAnnotator) StrokeCount() int {
	da.mu.RLock()
	defer da.mu.RUnlock()
	return len(da.strokes)
}
