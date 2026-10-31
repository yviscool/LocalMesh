package main

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFloatingBar_Initialization(t *testing.T) {
	fb := NewFloatingBar(FloatingBarCallbacks{})
	if fb == nil {
		t.Fatalf("expected non-nil floating bar")
	}
	if fb.IsExpanded() {
		t.Errorf("expected initially collapsed")
	}
}

func TestFloatingBar_Callbacks(t *testing.T) {
	var (
		broadcastCount   atomic.Int32
		blackscreenCount atomic.Int32
		annotateCount    atomic.Int32
		collapseCount    atomic.Int32
	)

	callbacks := FloatingBarCallbacks{
		OnBroadcast: func() error {
			broadcastCount.Add(1)
			return nil
		},
		OnBlackScreen: func() error {
			blackscreenCount.Add(1)
			return errors.New("simulated lock error")
		},
		OnAnnotate: func() error {
			annotateCount.Add(1)
			return nil
		},
		OnCollapse: func() {
			collapseCount.Add(1)
		},
	}

	fb := NewFloatingBar(callbacks)

	// Test broadcast trigger
	if err := fb.TriggerAction("broadcast"); err != nil {
		t.Errorf("unexpected broadcast error: %v", err)
	}
	if broadcastCount.Load() != 1 {
		t.Errorf("expected broadcast count 1, got %d", broadcastCount.Load())
	}

	// Test blackscreen trigger
	if err := fb.TriggerAction("blackscreen"); err == nil {
		t.Errorf("expected error from blackscreen callback")
	}
	if blackscreenCount.Load() != 1 {
		t.Errorf("expected blackscreen count 1, got %d", blackscreenCount.Load())
	}

	// Test annotate trigger
	if err := fb.TriggerAction("annotate"); err != nil {
		t.Errorf("unexpected annotate error: %v", err)
	}
	if annotateCount.Load() != 1 {
		t.Errorf("expected annotate count 1, got %d", annotateCount.Load())
	}

	// Test collapse trigger
	fb.SetExpanded(true)
	if !fb.IsExpanded() {
		t.Errorf("expected expanded")
	}

	if err := fb.TriggerAction("collapse"); err != nil {
		t.Errorf("unexpected collapse error: %v", err)
	}
	if fb.IsExpanded() {
		t.Errorf("expected collapsed after action")
	}
	if collapseCount.Load() != 1 {
		t.Errorf("expected collapse count 1, got %d", collapseCount.Load())
	}
}

func TestFloatingBar_ConcurrentExpansion(t *testing.T) {
	var collapseCount atomic.Int32
	fb := NewFloatingBar(FloatingBarCallbacks{
		OnCollapse: func() {
			collapseCount.Add(1)
		},
	})

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				fb.SetExpanded(j%2 == 0)
				_ = fb.IsExpanded()
			}
		}(i)
	}
	wg.Wait()
}

func TestFloatingBar_Lifecycle(t *testing.T) {
	fb := NewFloatingBar(FloatingBarCallbacks{})
	if err := fb.Start(); err != nil {
		t.Fatalf("fb.Start failed: %v", err)
	}

	fb.Show()
	fb.Hide()
	fb.Show()
	fb.SetExpanded(true)
	time.Sleep(50 * time.Millisecond)

	fb.SetExpanded(false)
	time.Sleep(50 * time.Millisecond)

	fb.Destroy()
}
