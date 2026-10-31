//go:build windows

package main

import (
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
	modUser32FB = windows.NewLazySystemDLL("user32.dll")
	modGdi32FB  = windows.NewLazySystemDLL("gdi32.dll")

	procRegisterClassExWFB      = modUser32FB.NewProc("RegisterClassExW")
	procCreateWindowExWFB       = modUser32FB.NewProc("CreateWindowExW")
	procDestroyWindowFB         = modUser32FB.NewProc("DestroyWindow")
	procShowWindowFB            = modUser32FB.NewProc("ShowWindow")
	procUpdateWindowFB          = modUser32FB.NewProc("UpdateWindow")
	procSetWindowPosFB          = modUser32FB.NewProc("SetWindowPos")
	procDefWindowProcWFB        = modUser32FB.NewProc("DefWindowProcW")
	procBeginPaintFB            = modUser32FB.NewProc("BeginPaint")
	procEndPaintFB              = modUser32FB.NewProc("EndPaint")
	procFillRectFB              = modUser32FB.NewProc("FillRect")
	procDrawTextWFB             = modUser32FB.NewProc("DrawTextW")
	procPostMessageWFB          = modUser32FB.NewProc("PostMessageW")
	procPostQuitMessageFB       = modUser32FB.NewProc("PostQuitMessage")
	procGetMessageWFB           = modUser32FB.NewProc("GetMessageW")
	procTranslateMessageFB      = modUser32FB.NewProc("TranslateMessage")
	procDispatchMessageWFB      = modUser32FB.NewProc("DispatchMessageW")
	procLoadCursorWFB           = modUser32FB.NewProc("LoadCursorW")
	procInvalidateRectFB        = modUser32FB.NewProc("InvalidateRect")
	procGetClientRectFB         = modUser32FB.NewProc("GetClientRect")
	procGetSystemMetricsFB      = modUser32FB.NewProc("GetSystemMetrics")
	procSetLayeredWindowAttrsFB = modUser32FB.NewProc("SetLayeredWindowAttributes")
	procTrackMouseEventFB       = modUser32FB.NewProc("TrackMouseEvent")
	procSetWindowRgnFB          = modUser32FB.NewProc("SetWindowRgn")

	procCreateCompatibleDCFB     = modGdi32FB.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmapFB = modGdi32FB.NewProc("CreateCompatibleBitmap")
	procDeleteDCFB               = modGdi32FB.NewProc("DeleteDC")
	procSelectObjectFB           = modGdi32FB.NewProc("SelectObject")
	procDeleteObjectFB           = modGdi32FB.NewProc("DeleteObject")
	procBitBltFB                 = modGdi32FB.NewProc("BitBlt")
	procCreateSolidBrushFB       = modGdi32FB.NewProc("CreateSolidBrush")
	procCreateRoundRectRgnFB     = modGdi32FB.NewProc("CreateRoundRectRgn")
	procSetTextColorFB           = modGdi32FB.NewProc("SetTextColor")
	procSetBkModeFB              = modGdi32FB.NewProc("SetBkMode")
	procCreateFontWFB            = modGdi32FB.NewProc("CreateFontW")
)

const (
	wsExTopmostFB    = 0x00000008
	wsExToolwindowFB = 0x00000080
	wsExLayeredFB    = 0x00080000
	wsPopupFB        = 0x80000000

	swShowFB      = 5
	swHideFB      = 0
	hwndTopmostFB = ^uintptr(0)

	swpNoActivateFB = 0x0010
	swpShowWindowFB = 0x0040

	wmDestroyFB     = 0x0002
	wmPaintFB       = 0x000F
	wmCloseFB       = 0x0010
	wmEraseBkgndFB  = 0x0014
	wmMouseMoveFB   = 0x0200
	wmLButtonDownFB = 0x0201
	wmLButtonUpFB   = 0x0202
	wmMouseLeaveFB  = 0x02A3

	wmUserFB        = 0x0400
	wmBarExpandFB   = wmUserFB + 201
	wmBarCollapseFB = wmUserFB + 202
	wmBarDestroyFB  = wmUserFB + 203

	tmeLeaveFB = 0x00000002
	lwaAlphaFB = 0x00000002

	dtCenterFB     = 0x00000001
	dtVCenterFB    = 0x00000004
	dtSingleLineFB = 0x00000020

	collapsedWidthFB  int32 = 220
	collapsedHeightFB int32 = 22
	expandedWidthFB   int32 = 520
	expandedHeightFB  int32 = 52
	defaultAlphaFB    uint8 = 238
)

type trackMouseEventFB struct {
	cbSize      uint32
	dwFlags     uint32
	hwndTrack   uintptr
	dwHoverTime uint32
}

type rectFB struct {
	left, top, right, bottom int32
}

type paintStructFB struct {
	hdc         uintptr
	fErase      int32
	rcPaint     rectFB
	fRestore    int32
	fIncUpdate  int32
	rgbReserved [32]byte
}

type wndClassExWFB struct {
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

// FloatingBarCallbacks defines notification handlers for user actions on the capsule toolbar.
type FloatingBarCallbacks struct {
	OnBroadcast   func() error
	OnBlackScreen func() error
	OnAnnotate    func() error
	OnCollapse    func()
}

// FloatingBar represents a Magpie-inspired native topmost capsule toolbar docked to the top-center of the screen.
type FloatingBar struct {
	mu           sync.RWMutex
	hwnd         uintptr
	callbacks    FloatingBarCallbacks
	isExpanded   atomic.Bool
	isVisible    atomic.Bool
	hoveredIndex int // -1: none, 0: broadcast, 1: blackscreen, 2: annotate, 3: collapse
	readyChan    chan struct{}
	doneChan     chan struct{}
}

var (
	activeFloatingBar  *FloatingBar
	barClassRegistered sync.Once
	barClassName       = "LocalMeshFloatingBarCapsule"
	barClassNamePtr    *uint16
)

// NewFloatingBar creates an initialized floating capsule toolbar.
func NewFloatingBar(callbacks FloatingBarCallbacks) *FloatingBar {
	return &FloatingBar{
		callbacks:    callbacks,
		hoveredIndex: -1,
		readyChan:    make(chan struct{}),
		doneChan:     make(chan struct{}),
	}
}

// Start spawns the dedicated OS thread hosting the Win32 window message loop.
func (fb *FloatingBar) Start() error {
	fb.mu.Lock()
	if fb.hwnd != 0 {
		fb.mu.Unlock()
		return nil
	}
	activeFloatingBar = fb
	fb.mu.Unlock()

	go fb.uiThread()

	select {
	case <-fb.readyChan:
		return nil
	case <-time.After(3 * time.Second):
		return fmt.Errorf("timeout waiting for floating bar window initialization")
	}
}

// Show makes the floating bar visible on the screen.
func (fb *FloatingBar) Show() {
	fb.mu.RLock()
	hwnd := fb.hwnd
	fb.mu.RUnlock()

	if hwnd != 0 {
		fb.isVisible.Store(true)
		procShowWindowFB.Call(hwnd, uintptr(swShowFB))
		procUpdateWindowFB.Call(hwnd)
	}
}

// Hide hides the floating bar.
func (fb *FloatingBar) Hide() {
	fb.mu.RLock()
	hwnd := fb.hwnd
	fb.mu.RUnlock()

	if hwnd != 0 {
		fb.isVisible.Store(false)
		procShowWindowFB.Call(hwnd, uintptr(swHideFB))
	}
}

// Destroy closes the floating bar window and message pump.
func (fb *FloatingBar) Destroy() {
	fb.mu.RLock()
	hwnd := fb.hwnd
	fb.mu.RUnlock()

	if hwnd != 0 {
		procPostMessageWFB.Call(hwnd, uintptr(wmBarDestroyFB), 0, 0)
		select {
		case <-fb.doneChan:
		case <-time.After(2 * time.Second):
		}
	}
}

// IsExpanded reports whether the capsule is currently in the expanded state.
func (fb *FloatingBar) IsExpanded() bool {
	return fb.isExpanded.Load()
}

// SetExpanded programmatically changes the capsule expansion state.
func (fb *FloatingBar) SetExpanded(expand bool) {
	fb.mu.RLock()
	hwnd := fb.hwnd
	fb.mu.RUnlock()

	if hwnd != 0 {
		if expand {
			procPostMessageWFB.Call(hwnd, uintptr(wmBarExpandFB), 0, 0)
		} else {
			procPostMessageWFB.Call(hwnd, uintptr(wmBarCollapseFB), 0, 0)
		}
	} else {
		fb.isExpanded.Store(expand)
		if !expand && fb.callbacks.OnCollapse != nil {
			fb.callbacks.OnCollapse()
		}
	}
}

// TriggerAction allows programmatically simulating a capsule button click.
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

func (fb *FloatingBar) uiThread() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// 1. Register window class
	barClassRegistered.Do(func() {
		ptr, _ := windows.UTF16PtrFromString(barClassName)
		barClassNamePtr = ptr

		hCursor, _, _ := procLoadCursorWFB.Call(0, uintptr(32512)) // IDC_ARROW
		wc := wndClassExWFB{
			cbSize:        uint32(unsafe.Sizeof(wndClassExWFB{})),
			lpfnWndProc:   syscall.NewCallback(floatingBarWndProc),
			hCursor:       hCursor,
			lpszClassName: barClassNamePtr,
		}
		procRegisterClassExWFB.Call(uintptr(unsafe.Pointer(&wc)))
	})

	// 2. Compute screen geometry
	rWidth, _, _ := procGetSystemMetricsFB.Call(0) // SM_CXSCREEN
	screenWidth := int32(rWidth)
	if screenWidth <= 0 {
		screenWidth = 1920
	}

	initX := (screenWidth - collapsedWidthFB) / 2
	initY := int32(0)

	titlePtr, _ := windows.UTF16PtrFromString("LocalMeshFloatingBar")

	// 3. Create layered topmost toolwindow
	hwnd, _, _ := procCreateWindowExWFB.Call(
		uintptr(wsExTopmostFB|wsExToolwindowFB|wsExLayeredFB),
		uintptr(unsafe.Pointer(barClassNamePtr)),
		uintptr(unsafe.Pointer(titlePtr)),
		uintptr(wsPopupFB),
		uintptr(initX),
		uintptr(initY),
		uintptr(collapsedWidthFB),
		uintptr(collapsedHeightFB),
		0, 0, 0, 0,
	)

	if hwnd == 0 {
		close(fb.readyChan)
		return
	}

	fb.mu.Lock()
	fb.hwnd = hwnd
	fb.mu.Unlock()

	// Set layered window opacity
	procSetLayeredWindowAttrsFB.Call(hwnd, 0, uintptr(defaultAlphaFB), uintptr(lwaAlphaFB))

	// Clip rounded pill region
	updateWindowPillRegion(hwnd, collapsedWidthFB, collapsedHeightFB)

	// Display collapsed pill
	fb.isVisible.Store(true)
	procShowWindowFB.Call(hwnd, uintptr(swShowFB))
	procUpdateWindowFB.Call(hwnd)

	close(fb.readyChan)

	// Message loop
	var msg struct {
		hwnd     uintptr
		message  uint32
		wParam   uintptr
		lParam   uintptr
		time     uint32
		pt       struct{ x, y int32 }
		lPrivate uint32
	}

	for {
		ret, _, _ := procGetMessageWFB.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		procTranslateMessageFB.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageWFB.Call(uintptr(unsafe.Pointer(&msg)))
	}

	fb.mu.Lock()
	fb.hwnd = 0
	fb.mu.Unlock()
	close(fb.doneChan)
}

func updateWindowPillRegion(hwnd uintptr, width, height int32) {
	rgn, _, _ := procCreateRoundRectRgnFB.Call(0, 0, uintptr(width), uintptr(height), 16, 16)
	if rgn != 0 {
		procSetWindowRgnFB.Call(hwnd, rgn, 1)
	}
}

func repositionBar(hwnd uintptr, expand bool) {
	rWidth, _, _ := procGetSystemMetricsFB.Call(0)
	screenWidth := int32(rWidth)
	if screenWidth <= 0 {
		screenWidth = 1920
	}

	var w, h int32
	if expand {
		w, h = expandedWidthFB, expandedHeightFB
	} else {
		w, h = collapsedWidthFB, collapsedHeightFB
	}

	x := (screenWidth - w) / 2
	y := int32(0)

	procSetWindowPosFB.Call(
		hwnd,
		hwndTopmostFB,
		uintptr(x),
		uintptr(y),
		uintptr(w),
		uintptr(h),
		uintptr(swpShowWindowFB|swpNoActivateFB),
	)
	updateWindowPillRegion(hwnd, w, h)
	procInvalidateRectFB.Call(hwnd, 0, 1)
}

func floatingBarWndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	fb := activeFloatingBar
	if fb == nil {
		ret, _, _ := procDefWindowProcWFB.Call(hwnd, uintptr(msg), wParam, lParam)
		return ret
	}

	switch msg {
	case wmEraseBkgndFB:
		return 1

	case wmBarDestroyFB:
		procDestroyWindowFB.Call(hwnd)
		return 0

	case wmDestroyFB:
		procPostQuitMessageFB.Call(0)
		return 0

	case wmBarExpandFB:
		fb.isExpanded.Store(true)
		repositionBar(hwnd, true)
		return 0

	case wmBarCollapseFB:
		fb.isExpanded.Store(false)
		fb.hoveredIndex = -1
		repositionBar(hwnd, false)
		return 0

	case wmMouseMoveFB:
		x := int32(int16(lParam & 0xFFFF))
		y := int32(int16((lParam >> 16) & 0xFFFF))

		// If collapsed, expand immediately on hover
		if !fb.isExpanded.Load() {
			fb.isExpanded.Store(true)
			repositionBar(hwnd, true)
		}

		// Track mouse leave
		tme := trackMouseEventFB{
			cbSize:    uint32(unsafe.Sizeof(trackMouseEventFB{})),
			dwFlags:   tmeLeaveFB,
			hwndTrack: hwnd,
		}
		procTrackMouseEventFB.Call(uintptr(unsafe.Pointer(&tme)))

		// Check which button is hovered
		btnIdx := getHoveredButton(x, y)
		if btnIdx != fb.hoveredIndex {
			fb.hoveredIndex = btnIdx
			procInvalidateRectFB.Call(hwnd, 0, 0)
		}
		return 0

	case wmMouseLeaveFB:
		// Mouse exited capsule -> collapse back to slim bar
		if fb.isExpanded.Load() {
			fb.isExpanded.Store(false)
			fb.hoveredIndex = -1
			repositionBar(hwnd, false)
			if fb.callbacks.OnCollapse != nil {
				fb.callbacks.OnCollapse()
			}
		}
		return 0

	case wmLButtonUpFB:
		x := int32(int16(lParam & 0xFFFF))
		y := int32(int16((lParam >> 16) & 0xFFFF))

		if !fb.isExpanded.Load() {
			fb.isExpanded.Store(true)
			repositionBar(hwnd, true)
			return 0
		}

		btnIdx := getHoveredButton(x, y)
		switch btnIdx {
		case 0: // 一键广播
			if fb.callbacks.OnBroadcast != nil {
				go fb.callbacks.OnBroadcast()
			}
		case 1: // 一键黑屏
			if fb.callbacks.OnBlackScreen != nil {
				go fb.callbacks.OnBlackScreen()
			}
		case 2: // 一键批注
			if fb.callbacks.OnAnnotate != nil {
				go fb.callbacks.OnAnnotate()
			}
		case 3: // 一键收拢
			fb.isExpanded.Store(false)
			fb.hoveredIndex = -1
			repositionBar(hwnd, false)
			if fb.callbacks.OnCollapse != nil {
				fb.callbacks.OnCollapse()
			}
		}
		procInvalidateRectFB.Call(hwnd, 0, 0)
		return 0

	case wmPaintFB:
		var ps paintStructFB
		hdc, _, _ := procBeginPaintFB.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		if hdc != 0 {
			var cr rectFB
			procGetClientRectFB.Call(hwnd, uintptr(unsafe.Pointer(&cr)))
			width := cr.right - cr.left
			height := cr.bottom - cr.top

			// Double buffer
			memDC, _, _ := procCreateCompatibleDCFB.Call(hdc)
			memBmp, _, _ := procCreateCompatibleBitmapFB.Call(hdc, uintptr(width), uintptr(height))
			oldBmp, _, _ := procSelectObjectFB.Call(memDC, memBmp)

			// 1. Fill dark acrylic background
			// RGB(24, 26, 32) -> 0x00201A18
			hDarkBrush, _, _ := procCreateSolidBrushFB.Call(0x00201A18)
			procFillRectFB.Call(memDC, uintptr(unsafe.Pointer(&cr)), hDarkBrush)
			procDeleteObjectFB.Call(hDarkBrush)

			procSetBkModeFB.Call(memDC, 1) // TRANSPARENT

			if !fb.isExpanded.Load() {
				// Render collapsed minimal pill
				hFont := createCapsuleFont(12, 600)
				oldFont, _, _ := procSelectObjectFB.Call(memDC, hFont)
				procSetTextColorFB.Call(memDC, 0x00CCCCCC) // Light gray text

				txtPtr, _ := windows.UTF16PtrFromString("━━ LocalMesh 控制台 ▾ ━━")
				procDrawTextWFB.Call(
					memDC,
					uintptr(unsafe.Pointer(txtPtr)),
					uintptr(len("━━ LocalMesh 控制台 ▾ ━━")),
					uintptr(unsafe.Pointer(&cr)),
					uintptr(dtCenterFB|dtVCenterFB|dtSingleLineFB),
				)

				procSelectObjectFB.Call(memDC, oldFont)
				procDeleteObjectFB.Call(hFont)
			} else {
				// Render expanded 4 buttons
				renderCapsuleButtons(memDC, width, height, fb.hoveredIndex)
			}

			// Blit to screen
			procBitBltFB.Call(hdc, 0, 0, uintptr(width), uintptr(height), memDC, 0, 0, 0x00CC0020)

			procSelectObjectFB.Call(memDC, oldBmp)
			procDeleteObjectFB.Call(memBmp)
			procDeleteDCFB.Call(memDC)

			procEndPaintFB.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		}
		return 0
	}

	ret, _, _ := procDefWindowProcWFB.Call(hwnd, uintptr(msg), wParam, lParam)
	return ret
}

func getHoveredButton(x, y int32) int {
	if y < 6 || y > 46 {
		return -1
	}
	// Expanded buttons bounds: 4 buttons across width 520
	// Btn 0: 12..132, Btn 1: 138..258, Btn 2: 264..384, Btn 3: 390..508
	if x >= 12 && x <= 132 {
		return 0
	}
	if x >= 138 && x <= 258 {
		return 1
	}
	if x >= 264 && x <= 384 {
		return 2
	}
	if x >= 390 && x <= 508 {
		return 3
	}
	return -1
}

func renderCapsuleButtons(hdc uintptr, width, height int32, hoveredIdx int) {
	buttons := []struct {
		text  string
		left  int32
		right int32
		color uint32
	}{
		{text: "📢 一键广播", left: 12, right: 132, color: 0x00D97706},   // Amber
		{text: "🖥️ 一键黑屏", left: 138, right: 258, color: 0x00DC2626}, // Red
		{text: "✏️ 桌面批注", left: 264, right: 384, color: 0x00059669}, // Emerald
		{text: "🔼 一键收拢", left: 390, right: 508, color: 0x004B5563},  // Gray
	}

	hFont := createCapsuleFont(13, 600)
	oldFont, _, _ := procSelectObjectFB.Call(hdc, hFont)
	defer func() {
		procSelectObjectFB.Call(hdc, oldFont)
		procDeleteObjectFB.Call(hFont)
	}()

	for i, btn := range buttons {
		btnRect := rectFB{left: btn.left, top: 8, right: btn.right, bottom: 44}

		// Button background
		var brushColor uint32 = 0x002D2422 // Dark button base
		if i == hoveredIdx {
			brushColor = 0x003F3530 // Hover highlight
		}

		hBrush, _, _ := procCreateSolidBrushFB.Call(uintptr(brushColor))
		procFillRectFB.Call(hdc, uintptr(unsafe.Pointer(&btnRect)), hBrush)
		procDeleteObjectFB.Call(hBrush)

		// Button text
		textColor := uint32(0x00FFFFFF)
		if i == hoveredIdx {
			textColor = 0x006EE7B7 // Greenish / bright on hover
		}
		procSetTextColorFB.Call(hdc, uintptr(textColor))

		txtPtr, _ := windows.UTF16PtrFromString(btn.text)
		procDrawTextWFB.Call(
			hdc,
			uintptr(unsafe.Pointer(txtPtr)),
			uintptr(len([]rune(btn.text))),
			uintptr(unsafe.Pointer(&btnRect)),
			uintptr(dtCenterFB|dtVCenterFB|dtSingleLineFB),
		)
	}
}

func createCapsuleFont(size int32, weight int32) uintptr {
	faceUTF16 := windows.StringToUTF16("Segoe UI")
	hFont, _, _ := procCreateFontWFB.Call(
		uintptr(-size),
		0, 0, 0,
		uintptr(weight),
		0, 0, 0,
		1, 0, 0, 5, 0,
		uintptr(unsafe.Pointer(&faceUTF16[0])),
	)
	return hFont
}
