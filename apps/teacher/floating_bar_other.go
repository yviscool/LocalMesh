//go:build !windows

package main

import (
	"sync"
	"sync/atomic"
)

type FloatingBarCallbacks struct {
	OnBroadcast   func() error
	OnBlackScreen func() error
	OnAnnotate    func() error
	OnCollapse    func()
}

type FloatingBar struct {
	mu         sync.RWMutex
	callbacks  FloatingBarCallbacks
	isExpanded atomic.Bool
	isVisible  atomic.Bool
}

func NewFloatingBar(callbacks FloatingBarCallbacks) *FloatingBar {
	return &FloatingBar{
		callbacks: callbacks,
	}
}

func (fb *FloatingBar) Start() error {
	fb.isVisible.Store(true)
	return nil
}

func (fb *FloatingBar) Show() {
	fb.isVisible.Store(true)
}

func (fb *FloatingBar) Hide() {
	fb.isVisible.Store(false)
}

func (fb *FloatingBar) Destroy() {
	fb.isVisible.Store(false)
}

func (fb *FloatingBar) IsExpanded() bool {
	return fb.isExpanded.Load()
}

func (fb *FloatingBar) SetExpanded(expand bool) {
	fb.isExpanded.Store(expand)
	if !expand && fb.callbacks.OnCollapse != nil {
		fb.callbacks.OnCollapse()
	}
}

// TriggerAction triggers a simulated button click for testing.
func (fb *FloatingBar) TriggerAction(action string) error {
	switch action {
	case "broadcast":
		if fb.callbacks.OnBroadcast != nil {
			return fb.callbacks.OnBroadcast()
		}
	case "blackscreen":
		if fb.callbacks.OnBlackScreen != nil {
			return fb.callbacks.OnBlackScreen()
		}
	case "annotate":
		if fb.callbacks.OnAnnotate != nil {
			return fb.callbacks.OnAnnotate()
		}
	case "collapse":
		fb.SetExpanded(false)
	}
	return nil
}
