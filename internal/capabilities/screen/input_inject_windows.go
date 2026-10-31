//go:build windows

package screen

import (
	"fmt"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	inputMouse    = 0
	inputKeyboard = 1

	mouseEventFMove       = 0x0001
	mouseEventFLeftDown   = 0x0002
	mouseEventFLeftUp     = 0x0004
	mouseEventFRightDown  = 0x0008
	mouseEventFRightUp    = 0x0010
	mouseEventFMiddleDown = 0x0020
	mouseEventFMiddleUp   = 0x0040
	mouseEventFWheel      = 0x0800
	mouseEventFAbsolute   = 0x8000

	keyEventFKeyUp   = 0x0002
	keyEventFUnicode = 0x0004

	smCxScreen = 0
	smCyScreen = 1
)

var (
	user32               = windows.NewLazySystemDLL("user32.dll")
	procSendInput        = user32.NewProc("SendInput")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
)

type mouseInput struct {
	dx          int32
	dy          int32
	mouseData   uint32
	dwFlags     uint32
	time        uint32
	padding     uint32
	dwExtraInfo uintptr
}

type keybdInput struct {
	wVk         uint16
	wScan       uint16
	dwFlags     uint32
	time        uint32
	padding     uint32
	dwExtraInfo uintptr
}

type winInput struct {
	inputType uint32
	padding   uint32
	// 32-byte union space for mouseInput or keybdInput
	union [32]byte
}

// WindowsInjector executes input injection using Win32 SendInput.
type WindowsInjector struct {
	mu sync.Mutex
}

// NewPlatformInjector returns the Windows SendInput implementation.
func NewPlatformInjector() Injector {
	return &WindowsInjector{}
}

// Inject executes the specified InputEvent via SendInput.
func (w *WindowsInjector) Inject(event InputEvent) error {
	if err := event.Validate(); err != nil {
		return err
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	var input winInput

	switch event.Type {
	case EventMouseMove, EventMouseDown, EventMouseUp, EventMouseWheel:
		input.inputType = inputMouse
		mi := (*mouseInput)(unsafe.Pointer(&input.union[0]))

		var dwFlags uint32
		var normX, normY float64

		if event.Normalized {
			normX = event.X
			normY = event.Y
		} else {
			cx, _, _ := procGetSystemMetrics.Call(smCxScreen)
			cy, _, _ := procGetSystemMetrics.Call(smCyScreen)
			if cx == 0 {
				cx = 1920
			}
			if cy == 0 {
				cy = 1080
			}
			normX = event.X / float64(cx)
			normY = event.Y / float64(cy)
		}

		dwFlags |= mouseEventFAbsolute
		mi.dx = int32(normX * 65535.0)
		mi.dy = int32(normY * 65535.0)

		switch event.Type {
		case EventMouseMove:
			dwFlags |= mouseEventFMove

		case EventMouseDown:
			dwFlags |= mouseEventFMove
			switch event.Button {
			case ButtonLeft:
				dwFlags |= mouseEventFLeftDown
			case ButtonRight:
				dwFlags |= mouseEventFRightDown
			case ButtonMiddle:
				dwFlags |= mouseEventFMiddleDown
			}

		case EventMouseUp:
			dwFlags |= mouseEventFMove
			switch event.Button {
			case ButtonLeft:
				dwFlags |= mouseEventFLeftUp
			case ButtonRight:
				dwFlags |= mouseEventFRightUp
			case ButtonMiddle:
				dwFlags |= mouseEventFMiddleUp
			}

		case EventMouseWheel:
			dwFlags |= mouseEventFWheel
			mi.mouseData = uint32(event.WheelDelta)
		}

		mi.dwFlags = dwFlags

	case EventKeyDown, EventKeyUp:
		input.inputType = inputKeyboard
		ki := (*keybdInput)(unsafe.Pointer(&input.union[0]))

		if len(event.Text) > 0 && event.KeyCode == 0 {
			// Unicode injection
			runes := []rune(event.Text)
			if len(runes) > 0 {
				ki.wScan = uint16(runes[0])
				ki.dwFlags = keyEventFUnicode
				if event.Type == EventKeyUp {
					ki.dwFlags |= keyEventFKeyUp
				}
			}
		} else {
			ki.wVk = uint16(event.KeyCode)
			if event.Type == EventKeyUp {
				ki.dwFlags = keyEventFKeyUp
			}
		}

	default:
		return ErrInvalidEventType
	}

	ret, _, err := procSendInput.Call(
		1,
		uintptr(unsafe.Pointer(&input)),
		unsafe.Sizeof(input),
	)
	if ret == 0 {
		return fmt.Errorf("SendInput failed: %w", err)
	}

	return nil
}
