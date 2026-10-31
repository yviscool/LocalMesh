//go:build windows

package screenlock

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procRegisterClassExW    = modUser32.NewProc("RegisterClassExW")
	procCreateWindowExW     = modUser32.NewProc("CreateWindowExW")
	procDestroyWindow       = modUser32.NewProc("DestroyWindow")
	procShowWindow          = modUser32.NewProc("ShowWindow")
	procUpdateWindow        = modUser32.NewProc("UpdateWindow")
	procSetWindowPos        = modUser32.NewProc("SetWindowPos")
	procSetForegroundWindow = modUser32.NewProc("SetForegroundWindow")
	procDefWindowProcW      = modUser32.NewProc("DefWindowProcW")
	procBeginPaint          = modUser32.NewProc("BeginPaint")
	procEndPaint            = modUser32.NewProc("EndPaint")
	procFillRect            = modUser32.NewProc("FillRect")
	procDrawTextW           = modUser32.NewProc("DrawTextW")
	procPostQuitMessage     = modUser32.NewProc("PostQuitMessage")
	procPostMessageW        = modUser32.NewProc("PostMessageW")
	procTranslateMessage    = modUser32.NewProc("TranslateMessage")
	procDispatchMessageW    = modUser32.NewProc("DispatchMessageW")
	procLoadCursorW         = modUser32.NewProc("LoadCursorW")
	procInvalidateRect      = modUser32.NewProc("InvalidateRect")
	procGetClientRect       = modUser32.NewProc("GetClientRect")
	procGetSystemMetrics    = modUser32.NewProc("GetSystemMetrics")
	procPeekMessageW        = modUser32.NewProc("PeekMessageW")

	modGdi32Overlay    = windows.NewLazySystemDLL("gdi32.dll")
	procCreateFontW    = modGdi32Overlay.NewProc("CreateFontW")
	procSelectObject   = modGdi32Overlay.NewProc("SelectObject")
	procDeleteObject   = modGdi32Overlay.NewProc("DeleteObject")
	procSetTextColor   = modGdi32Overlay.NewProc("SetTextColor")
	procSetBkMode      = modGdi32Overlay.NewProc("SetBkMode")
	procGetStockObject = modGdi32Overlay.NewProc("GetStockObject")

	procGetModuleHandleW = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetModuleHandleW")
)

const (
	WS_EX_TOPMOST    = 0x00000008
	WS_EX_TOOLWINDOW = 0x00000080
	WS_POPUP         = 0x80000000
	WS_VISIBLE       = 0x10000000

	SW_SHOW        = 5
	SW_HIDE        = 0
	HWND_TOPMOST   = ^uintptr(0) // -1
	SWP_SHOWWINDOW = 0x0040

	WM_DESTROY    = 0x0002
	WM_PAINT      = 0x000F
	WM_CLOSE      = 0x0010
	WM_ERASEBKGND = 0x0014
	WM_SYSCOMMAND = 0x0112

	WM_USER           = 0x0400
	WM_CLOSE_OVERLAY  = WM_USER + 101
	WM_UPDATE_OVERLAY = WM_USER + 102

	SC_MINIMIZE = 0xF020
	SC_RESTORE  = 0xF120
	SC_CLOSE    = 0xF060
	SC_MAXIMIZE = 0xF030

	SM_CXSCREEN        = 0
	SM_CYSCREEN        = 1
	SM_XVIRTUALSCREEN  = 76
	SM_YVIRTUALSCREEN  = 77
	SM_CXVIRTUALSCREEN = 78
	SM_CYVIRTUALSCREEN = 79

	DT_CENTER     = 0x00000001
	DT_VCENTER    = 0x00000004
	DT_WORDBREAK  = 0x00000010
	DT_SINGLELINE = 0x00000020
	DT_NOPREFIX   = 0x00000800

	FW_NORMAL           = 400
	FW_BOLD             = 700
	DEFAULT_CHARSET     = 1
	OUT_DEFAULT_PRECIS  = 0
	CLIP_DEFAULT_PRECIS = 0
	CLEARTYPE_QUALITY   = 5
	DEFAULT_PITCH       = 0
	FF_DONTCARE         = 0

	TRANSPARENT = 1
	BLACK_BRUSH = 4
	IDC_ARROW   = 32512
)

type paintStruct struct {
	hdc         uintptr
	fErase      int32
	rcPaint     rect
	fRestore    int32
	fIncUpdate  int32
	rgbReserved [32]byte
}

type rect struct {
	left, top, right, bottom int32
}

type winMsg struct {
	hwnd     uintptr
	message  uint32
	wParam   uintptr
	lParam   uintptr
	time     uint32
	pt       struct{ x, y int32 }
	lPrivate uint32
}

type wndClassExW struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

// WindowsOverlay manages the native topmost lock screen window.
type WindowsOverlay struct {
	mu       sync.Mutex
	hwnd     uintptr
	threadID uint32
	visible  atomic.Bool
	doneChan chan struct{}

	textMu  sync.RWMutex
	title   string
	message string
}

var (
	defaultOverlay      = &WindowsOverlay{}
	registerClassOnce   sync.Once
	overlayClassNamePtr *uint16
	overlayClassName    = "LocalMeshLockOverlay"
)

// NewPlatformOverlay creates a platform-native lock screen overlay.
func NewPlatformOverlay() Overlay {
	return defaultOverlay
}

func getSystemMetric(n int) int32 {
	r, _, _ := procGetSystemMetrics.Call(uintptr(n))
	return int32(r)
}

func createFont(size int32, weight int32, faceName string) uintptr {
	faceUTF16 := windows.StringToUTF16(faceName)
	hFont, _, _ := procCreateFontW.Call(
		uintptr(-size),
		0,
		0,
		0,
		uintptr(weight),
		0,
		0,
		0,
		DEFAULT_CHARSET,
		OUT_DEFAULT_PRECIS,
		CLIP_DEFAULT_PRECIS,
		CLEARTYPE_QUALITY,
		DEFAULT_PITCH|FF_DONTCARE,
		uintptr(unsafe.Pointer(&faceUTF16[0])),
	)
	return hFont
}

func registerOverlayClass(hInstance uintptr) error {
	var regErr error
	registerClassOnce.Do(func() {
		hCursor, _, _ := procLoadCursorW.Call(0, uintptr(IDC_ARROW))
		hBlackBrush, _, _ := procGetStockObject.Call(BLACK_BRUSH)
		ptr, err := windows.UTF16PtrFromString(overlayClassName)
		if err != nil {
			regErr = err
			return
		}
		overlayClassNamePtr = ptr

		wndProcCallback := syscall.NewCallback(overlayWndProc)

		wc := wndClassExW{
			cbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
			style:         0,
			lpfnWndProc:   wndProcCallback,
			hInstance:     hInstance,
			hCursor:       hCursor,
			hbrBackground: hBlackBrush,
			lpszClassName: overlayClassNamePtr,
		}

		ret, _, lastErr := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
		if ret == 0 {
			if errno, ok := lastErr.(windows.Errno); ok && errno == 1410 {
				// ERROR_CLASS_ALREADY_EXISTS is fine
				return
			}
			regErr = fmt.Errorf("RegisterClassExW failed: %w", lastErr)
		}
	})
	return regErr
}

func overlayWndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case WM_ERASEBKGND:
		return 1

	case WM_PAINT:
		var ps paintStruct
		hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		if hdc != 0 {
			var cr rect
			procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&cr)))

			// 1. Fill background with solid black
			hBlackBrush, _, _ := procGetStockObject.Call(BLACK_BRUSH)
			procFillRect.Call(hdc, uintptr(unsafe.Pointer(&cr)), hBlackBrush)

			// 2. Set DC background mode to transparent
			procSetBkMode.Call(hdc, uintptr(TRANSPARENT))

			// 3. Retrieve title and message
			defaultOverlay.textMu.RLock()
			title := defaultOverlay.title
			message := defaultOverlay.message
			defaultOverlay.textMu.RUnlock()

			if title == "" {
				title = "Screen Locked by Teacher"
			}
			if message == "" {
				message = "Please pay attention to the classroom presentation."
			}

			centerY := (cr.bottom - cr.top) / 2

			// 4. Render Title in Gold (RGB 255, 215, 0 -> 0x0000D7FF)
			titleFont := createFont(42, FW_BOLD, "Segoe UI")
			hOldFont, _, _ := procSelectObject.Call(hdc, titleFont)
			procSetTextColor.Call(hdc, 0x0000D7FF)

			titleRect := rect{
				left:   cr.left + 40,
				top:    centerY - 120,
				right:  cr.right - 40,
				bottom: centerY - 30,
			}
			titleU16 := windows.StringToUTF16(title)
			procDrawTextW.Call(
				hdc,
				uintptr(unsafe.Pointer(&titleU16[0])),
				uintptr(len(titleU16)-1),
				uintptr(unsafe.Pointer(&titleRect)),
				DT_CENTER|DT_SINGLELINE|DT_NOPREFIX,
			)

			// 5. Render Message in Clean White (RGB 255, 255, 255 -> 0x00FFFFFF)
			msgFont := createFont(22, FW_NORMAL, "Segoe UI")
			procSelectObject.Call(hdc, msgFont)
			procSetTextColor.Call(hdc, 0x00FFFFFF)

			msgRect := rect{
				left:   cr.left + 80,
				top:    centerY - 10,
				right:  cr.right - 80,
				bottom: centerY + 160,
			}
			msgU16 := windows.StringToUTF16(message)
			procDrawTextW.Call(
				hdc,
				uintptr(unsafe.Pointer(&msgU16[0])),
				uintptr(len(msgU16)-1),
				uintptr(unsafe.Pointer(&msgRect)),
				DT_CENTER|DT_WORDBREAK|DT_NOPREFIX,
			)

			// 6. Cleanup custom fonts to avoid GDI leaks
			procSelectObject.Call(hdc, hOldFont)
			procDeleteObject.Call(titleFont)
			procDeleteObject.Call(msgFont)

			procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		}
		return 0

	case WM_CLOSE:
		// Ignore close attempts completely
		return 0

	case WM_SYSCOMMAND:
		cmd := wParam & 0xFFF0
		if cmd == SC_MINIMIZE || cmd == SC_RESTORE || cmd == SC_CLOSE || cmd == SC_MAXIMIZE {
			return 0
		}
		r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
		return r

	case WM_CLOSE_OVERLAY:
		// Destroy overlay on the thread that created it
		procDestroyWindow.Call(hwnd)
		return 0

	case WM_UPDATE_OVERLAY:
		procInvalidateRect.Call(hwnd, 0, 1)
		procUpdateWindow.Call(hwnd)
		return 0

	case WM_DESTROY:
		procPostQuitMessage.Call(0)
		return 0

	default:
		r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
		return r
	}
}

// Show displays the native full-screen lock overlay.
func (o *WindowsOverlay) Show(title, message string) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.textMu.Lock()
	o.title = title
	o.message = message
	o.textMu.Unlock()

	if o.visible.Load() {
		if o.hwnd != 0 {
			procPostMessageW.Call(o.hwnd, WM_UPDATE_OVERLAY, 0, 0)
		}
		return nil
	}

	readyChan := make(chan error, 1)
	o.doneChan = make(chan struct{})

	go o.runWindowLoop(readyChan)

	select {
	case err := <-readyChan:
		if err != nil {
			o.visible.Store(false)
			return err
		}
		o.visible.Store(true)
		return nil
	case <-time.After(3 * time.Second):
		o.visible.Store(false)
		return errors.New("timeout creating lock screen overlay window")
	}
}

func (o *WindowsOverlay) runWindowLoop(readyChan chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	doneChan := o.doneChan
	defer close(doneChan)

	o.threadID = windows.GetCurrentThreadId()

	hInstance, _, _ := procGetModuleHandleW.Call(0)
	if err := registerOverlayClass(hInstance); err != nil {
		readyChan <- err
		return
	}

	w := getSystemMetric(SM_CXVIRTUALSCREEN)
	h := getSystemMetric(SM_CYVIRTUALSCREEN)
	x := getSystemMetric(SM_XVIRTUALSCREEN)
	y := getSystemMetric(SM_YVIRTUALSCREEN)

	if w <= 0 || h <= 0 {
		w = getSystemMetric(SM_CXSCREEN)
		h = getSystemMetric(SM_CYSCREEN)
		x = 0
		y = 0
	}
	if w <= 0 || h <= 0 {
		w = 1920
		h = 1080
	}

	windowNamePtr, _ := windows.UTF16PtrFromString("LocalMesh Overlay")

	tB := time.Now()
	hwnd, _, lastErr := procCreateWindowExW.Call(
		WS_EX_TOPMOST|WS_EX_TOOLWINDOW,
		uintptr(unsafe.Pointer(overlayClassNamePtr)),
		uintptr(unsafe.Pointer(windowNamePtr)),
		WS_POPUP|WS_VISIBLE,
		uintptr(x),
		uintptr(y),
		uintptr(w),
		uintptr(h),
		0,
		0,
		hInstance,
		0,
	)
	createDur := time.Since(tB)
	if hwnd == 0 {
		readyChan <- fmt.Errorf("CreateWindowExW failed (took %v): %w", createDur, lastErr)
		return
	}

	o.hwnd = hwnd

	procShowWindow.Call(hwnd, SW_SHOW)
	procSetForegroundWindow.Call(hwnd)
	procSetWindowPos.Call(
		hwnd,
		HWND_TOPMOST,
		uintptr(x),
		uintptr(y),
		uintptr(w),
		uintptr(h),
		SWP_SHOWWINDOW,
	)
	procUpdateWindow.Call(hwnd)

	// Force message queue initialization before signaling readiness
	var dummyMsg winMsg
	procPeekMessageW.Call(uintptr(unsafe.Pointer(&dummyMsg)), 0, 0, 0, 0)

	readyChan <- nil

	// Message Loop (0 as hwnd processes all messages for this thread, including WM_QUIT)
	var msg winMsg
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
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}

	if hwnd != 0 {
		procDestroyWindow.Call(hwnd)
	}

	o.hwnd = 0
	o.threadID = 0
}

// Hide closes and destroys the lock screen overlay window.
func (o *WindowsOverlay) Hide() error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if !o.visible.Load() {
		return nil
	}

	if o.hwnd != 0 {
		procPostMessageW.Call(o.hwnd, WM_CLOSE_OVERLAY, 0, 0)
	}
	if o.threadID != 0 {
		procPostThreadMessageW.Call(uintptr(o.threadID), WM_QUIT, 0, 0)
	}

	if o.doneChan != nil {
		select {
		case <-o.doneChan:
		case <-time.After(2 * time.Second):
		}
	}

	o.visible.Store(false)
	o.hwnd = 0
	o.threadID = 0
	o.doneChan = nil
	return nil
}

// Destroy destroys the lock screen overlay.
func (o *WindowsOverlay) Destroy() error {
	return o.Hide()
}

// IsVisible returns whether the overlay is currently displayed.
func (o *WindowsOverlay) IsVisible() bool {
	return o.visible.Load()
}

// Update updates the overlay title and message while displayed.
func (o *WindowsOverlay) Update(title, message string) error {
	o.textMu.Lock()
	o.title = title
	o.message = message
	o.textMu.Unlock()

	o.mu.Lock()
	hwnd := o.hwnd
	o.mu.Unlock()

	if hwnd != 0 {
		procPostMessageW.Call(hwnd, WM_UPDATE_OVERLAY, 0, 0)
	}
	return nil
}

// ShowOverlay displays the full-screen topmost lock window.
func ShowOverlay(title, message string) error {
	return defaultOverlay.Show(title, message)
}

// HideOverlay hides and destroys the full-screen lock window.
func HideOverlay() error {
	return defaultOverlay.Hide()
}

// DestroyOverlay destroys the full-screen lock window.
func DestroyOverlay() error {
	return defaultOverlay.Destroy()
}

// IsOverlayVisible reports whether the overlay window is currently displayed.
func IsOverlayVisible() bool {
	return defaultOverlay.IsVisible()
}

// UpdateOverlay updates the title and message of the displayed overlay.
func UpdateOverlay(title, message string) error {
	return defaultOverlay.Update(title, message)
}
