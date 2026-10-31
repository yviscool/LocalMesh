//go:build windows

package screenlock

import (
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	WH_KEYBOARD_LL = 13
	WH_MOUSE_LL    = 14
	HC_ACTION      = 0
	WM_QUIT        = 0x0012
)

var (
	modUser32               = windows.NewLazySystemDLL("user32.dll")
	procSetWindowsHookExW   = modUser32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx = modUser32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx      = modUser32.NewProc("CallNextHookEx")
	procGetMessageW         = modUser32.NewProc("GetMessageW")
	procPostThreadMessageW  = modUser32.NewProc("PostThreadMessageW")
	procBlockInput          = modUser32.NewProc("BlockInput")
	procGetAsyncKeyState    = modUser32.NewProc("GetAsyncKeyState")
)

type winMSG struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      struct{ x, y int32 }
}

type kbdLLHookStruct struct {
	vkCode      uint32
	scanCode    uint32
	flags       uint32
	time        uint32
	dwExtraInfo uintptr
}

// WindowsTrapper installs low-level Windows keyboard and mouse hooks
// to suppress shortcuts and manage input trapping.
type WindowsTrapper struct {
	mu         sync.Mutex
	active     bool
	inputOnly  bool
	fullLock   atomic.Bool
	threadID   uint32
	doneChan   chan struct{}
	hKbdHook   uintptr
	hMouseHook uintptr
}

// Global reference for hook callbacks
var (
	activeTrapperMu sync.RWMutex
	activeTrapper   *WindowsTrapper
)

// NewPlatformTrapper returns the Windows implementation of Trapper.
func NewPlatformTrapper() Trapper {
	return &WindowsTrapper{}
}

func isCtrlPressed() bool {
	ret, _, _ := procGetAsyncKeyState.Call(uintptr(VK_CONTROL))
	return (ret & 0x8000) != 0
}

func keyboardHookProc(nCode int, wParam uintptr, lParam unsafe.Pointer) uintptr {
	if nCode == HC_ACTION && lParam != nil {
		pkh := (*kbdLLHookStruct)(lParam)
		activeTrapperMu.RLock()
		tr := activeTrapper
		activeTrapperMu.RUnlock()

		fullLock := false
		if tr != nil {
			fullLock = tr.fullLock.Load()
		}

		if ShouldTrapKey(pkh.vkCode, pkh.flags, isCtrlPressed(), fullLock) {
			// Non-zero LRESULT blocks the keystroke from reaching target window
			return 1
		}
	}

	activeTrapperMu.RLock()
	var hHook uintptr
	if activeTrapper != nil {
		hHook = activeTrapper.hKbdHook
	}
	activeTrapperMu.RUnlock()

	ret, _, _ := procCallNextHookEx.Call(hHook, uintptr(nCode), wParam, uintptr(lParam))
	return ret
}

func mouseHookProc(nCode int, wParam uintptr, lParam uintptr) uintptr {
	if nCode == HC_ACTION {
		activeTrapperMu.RLock()
		tr := activeTrapper
		activeTrapperMu.RUnlock()

		if tr != nil && tr.fullLock.Load() {
			// Suppress mouse input when fullLock is active
			return 1
		}
	}

	activeTrapperMu.RLock()
	var hHook uintptr
	if activeTrapper != nil {
		hHook = activeTrapper.hMouseHook
	}
	activeTrapperMu.RUnlock()

	ret, _, _ := procCallNextHookEx.Call(hHook, uintptr(nCode), wParam, uintptr(lParam))
	return ret
}

// Trap activates the Windows low-level input hooks.
func (w *WindowsTrapper) Trap(inputOnly bool) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.inputOnly = inputOnly
	w.fullLock.Store(true)

	if w.active {
		return nil
	}

	activeTrapperMu.Lock()
	activeTrapper = w
	activeTrapperMu.Unlock()

	readyChan := make(chan error, 1)
	w.doneChan = make(chan struct{})

	go w.hookLoop(readyChan)

	select {
	case err := <-readyChan:
		if err != nil {
			activeTrapperMu.Lock()
			activeTrapper = nil
			activeTrapperMu.Unlock()
			return err
		}
		w.active = true
		// Attempt BlockInput as an auxiliary security measure (fails safely if unprivileged)
		procBlockInput.Call(1)
		return nil
	case <-time.After(3 * time.Second):
		activeTrapperMu.Lock()
		activeTrapper = nil
		activeTrapperMu.Unlock()
		return errors.New("timeout initializing windows input hooks")
	}
}

// Untrap deactivates Windows hooks and unblocks input.
func (w *WindowsTrapper) Untrap() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.active {
		return nil
	}

	// Release BlockInput
	procBlockInput.Call(0)
	w.fullLock.Store(false)

	if w.threadID != 0 {
		procPostThreadMessageW.Call(uintptr(w.threadID), WM_QUIT, 0, 0)
	}

	if w.doneChan != nil {
		select {
		case <-w.doneChan:
		case <-time.After(2 * time.Second):
		}
	}

	activeTrapperMu.Lock()
	if activeTrapper == w {
		activeTrapper = nil
	}
	activeTrapperMu.Unlock()

	w.active = false
	w.threadID = 0
	w.doneChan = nil
	return nil
}

// IsTrapping reports whether hooks are currently active.
func (w *WindowsTrapper) IsTrapping() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.active
}

func (w *WindowsTrapper) hookLoop(readyChan chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	w.threadID = windows.GetCurrentThreadId()

	kbdCallback := syscall.NewCallback(keyboardHookProc)
	mouseCallback := syscall.NewCallback(mouseHookProc)

	hKbd, _, kbdErr := procSetWindowsHookExW.Call(
		WH_KEYBOARD_LL,
		kbdCallback,
		0,
		0,
	)
	if hKbd == 0 {
		readyChan <- kbdErr
		close(w.doneChan)
		return
	}
	w.hKbdHook = hKbd

	hMouse, _, _ := procSetWindowsHookExW.Call(
		WH_MOUSE_LL,
		mouseCallback,
		0,
		0,
	)
	w.hMouseHook = hMouse

	readyChan <- nil

	// Windows Message Loop to process hook callbacks
	var msg winMSG
	for {
		ret, _, _ := procGetMessageW.Call(
			uintptr(unsafe.Pointer(&msg)),
			0,
			0,
			0,
		)
		if int32(ret) <= 0 {
			break
		}
	}

	if w.hKbdHook != 0 {
		procUnhookWindowsHookEx.Call(w.hKbdHook)
		w.hKbdHook = 0
	}
	if w.hMouseHook != 0 {
		procUnhookWindowsHookEx.Call(w.hMouseHook)
		w.hMouseHook = 0
	}

	close(w.doneChan)
}
