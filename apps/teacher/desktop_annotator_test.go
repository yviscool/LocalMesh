package main

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDesktopAnnotator_Initialization(t *testing.T) {
	da := NewDesktopAnnotator(nil)
	if da == nil {
		t.Fatalf("expected non-nil annotator")
	}
	if da.IsActive() {
		t.Errorf("expected initially inactive")
	}
	if da.currentTool != toolRedPen {
		t.Errorf("expected initial tool RedPen (0), got %d", da.currentTool)
	}
}

func TestDesktopAnnotator_ToolSwitching(t *testing.T) {
	da := NewDesktopAnnotator(nil)

	da.SetTool(toolHighlighter)
	if da.currentTool != toolHighlighter {
		t.Errorf("expected toolHighlighter (1), got %d", da.currentTool)
	}

	da.SetTool(toolEraser)
	if da.currentTool != toolEraser {
		t.Errorf("expected toolEraser (2), got %d", da.currentTool)
	}

	da.SetTool(toolRedPen)
	if da.currentTool != toolRedPen {
		t.Errorf("expected toolRedPen (0), got %d", da.currentTool)
	}
}

func TestDesktopAnnotator_StrokesAndClear(t *testing.T) {
	da := NewDesktopAnnotator(nil)

	pts1 := []pointDA{{X: 10, Y: 10}, {X: 20, Y: 20}, {X: 30, Y: 30}}
	da.AddStroke(toolRedPen, pts1)

	pts2 := []pointDA{{X: 100, Y: 100}, {X: 200, Y: 100}}
	da.AddStroke(toolHighlighter, pts2)

	if da.StrokeCount() != 2 {
		t.Errorf("expected 2 strokes, got %d", da.StrokeCount())
	}

	da.Clear()

	if da.StrokeCount() != 0 {
		t.Errorf("expected 0 strokes after clear, got %d", da.StrokeCount())
	}
}

func TestDesktopAnnotator_LifecycleAndCloseCallback(t *testing.T) {
	var closed atomic.Bool
	da := NewDesktopAnnotator(func() {
		closed.Store(true)
	})

	if err := da.Start(); err != nil {
		t.Fatalf("da.Start failed: %v", err)
	}

	if !da.IsActive() {
		t.Errorf("expected active after Start()")
	}

	time.Sleep(50 * time.Millisecond)
	da.Stop()

	if da.IsActive() {
		t.Errorf("expected inactive after Stop()")
	}

	// Wait up to 500ms for close callback
	start := time.Now()
	for !closed.Load() && time.Since(start) < 500*time.Millisecond {
		time.Sleep(10 * time.Millisecond)
	}

	if !closed.Load() {
		t.Errorf("expected onClose callback to be invoked")
	}
}

func TestDesktopAnnotator_ConcurrentAccess(t *testing.T) {
	da := NewDesktopAnnotator(nil)

	var wg sync.WaitGroup
	for i := 0; i < 15; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				tool := j % 3
				da.SetTool(tool)
				da.AddStroke(tool, []pointDA{{X: int32(j), Y: int32(j)}})
				if j%10 == 0 {
					da.Clear()
				}
				_ = da.IsActive()
				_ = da.StrokeCount()
			}
		}(i)
	}
	wg.Wait()
}
