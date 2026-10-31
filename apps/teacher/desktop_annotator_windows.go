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
	modUser32DA = windows.NewLazySystemDLL("user32.dll")
	modGdi32DA  = windows.NewLazySystemDLL("gdi32.dll")

	procRegisterClassExWDA = modUser32DA.NewProc("RegisterClassExW")
	procCreateWindowExWDA  = modUser32DA.NewProc("CreateWindowExW")
	procDestroyWindowDA    = modUser32DA.NewProc("DestroyWindow")
	procShowWindowDA       = modUser32DA.NewProc("ShowWindow")
	procUpdateWindowDA     = modUser32DA.NewProc("UpdateWindow")
	procDefWindowProcWDA   = modUser32DA.NewProc("DefWindowProcW")
	procBeginPaintDA       = modUser32DA.NewProc("BeginPaint")
	procEndPaintDA         = modUser32DA.NewProc("EndPaint")
	procFillRectDA         = modUser32DA.NewProc("FillRect")
	procDrawTextWDA        = modUser32DA.NewProc("DrawTextW")
	procPostMessageWDA     = modUser32DA.NewProc("PostMessageW")
	procPostQuitMessageDA  = modUser32DA.NewProc("PostQuitMessage")
	procGetMessageWDA      = modUser32DA.NewProc("GetMessageW")
	procTranslateMessageDA = modUser32DA.NewProc("TranslateMessage")
	procDispatchMessageWDA = modUser32DA.NewProc("DispatchMessageW")
	procLoadCursorWDA      = modUser32DA.NewProc("LoadCursorW")
	procInvalidateRectDA   = modUser32DA.NewProc("InvalidateRect")
	procGetSystemMetricsDA = modUser32DA.NewProc("GetSystemMetrics")
	procGetDCDA            = modUser32DA.NewProc("GetDC")
	procReleaseDCDA        = modUser32DA.NewProc("ReleaseDC")

	procCreateCompatibleDCDA     = modGdi32DA.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmapDA = modGdi32DA.NewProc("CreateCompatibleBitmap")
	procDeleteDCDA               = modGdi32DA.NewProc("DeleteDC")
	procSelectObjectDA           = modGdi32DA.NewProc("SelectObject")
	procDeleteObjectDA           = modGdi32DA.NewProc("DeleteObject")
	procBitBltDA                 = modGdi32DA.NewProc("BitBlt")
	procCreateSolidBrushDA       = modGdi32DA.NewProc("CreateSolidBrush")
	procCreatePenDA              = modGdi32DA.NewProc("CreatePen")
	procMoveToExDA               = modGdi32DA.NewProc("MoveToEx")
	procLineToDA                 = modGdi32DA.NewProc("LineTo")
	procSetTextColorDA           = modGdi32DA.NewProc("SetTextColor")
	procSetBkModeDA              = modGdi32DA.NewProc("SetBkMode")
	procCreateFontWDA            = modGdi32DA.NewProc("CreateFontW")
)

const (
	wsExTopmostDA = 0x00000008
	wsPopupDA     = 0x80000000

	swShowDA = 5
	swHideDA = 0

	wmDestroyDA     = 0x0002
	wmPaintDA       = 0x000F
	wmCloseDA       = 0x0010
	wmEraseBkgndDA  = 0x0014
	wmKeyDownDA     = 0x0100
	wmMouseMoveDA   = 0x0200
	wmLButtonDownDA = 0x0201
	wmLButtonUpDA   = 0x0202
	wmRButtonUpDA   = 0x0205

	vkEscapeDA = 0x1B

	srccopyDA = 0x00CC0020
	psSolidDA = 0

	toolRedPen      = 0 // Red pen (~3px)
	toolHighlighter = 1 // Yellow highlighter (~14px)
	toolEraser      = 2 // Eraser (~24px radius)
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

type rectDA struct {
	left, top, right, bottom int32
}

type paintStructDA struct {
	hdc         uintptr
	fErase      int32
	rcPaint     rectDA
	fRestore    int32
	fIncUpdate  int32
	rgbReserved [32]byte
}

type wndClassExWDA struct {
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

// DesktopAnnotator manages full-screen transparent/frozen snapshot drawing and annotation.
type DesktopAnnotator struct {
	mu        sync.RWMutex
	hwnd      uintptr
	active    atomic.Bool
	readyChan chan struct{}
	doneChan  chan struct{}
	onClose   func()

	// Drawing canvas state
	currentTool   int
	isDrawing     bool
	currentStroke *strokeDA
	strokes       []strokeDA

	// Snapshot resources
	frozenDC  uintptr
	frozenBmp uintptr
	oldFrozen uintptr
	screenX   int32
	screenY   int32
	screenW   int32
	screenH   int32
}

var (
	activeAnnotator        *DesktopAnnotator
	annotatorClassInitOnce sync.Once
	annotatorClassName     = "LocalMeshDesktopAnnotatorOverlay"
	annotatorClassNamePtr  *uint16
)

// NewDesktopAnnotator creates a desktop annotator instance with an optional close callback.
func NewDesktopAnnotator(onClose func()) *DesktopAnnotator {
	return &DesktopAnnotator{
		currentTool: toolRedPen,
		readyChan:   make(chan struct{}),
		doneChan:    make(chan struct{}),
		onClose:     onClose,
	}
}

// IsActive reports whether annotation mode is currently open and active.
func (da *DesktopAnnotator) IsActive() bool {
	return da.active.Load()
}

// Start opens full-screen desktop annotation over a frozen screen snapshot.
func (da *DesktopAnnotator) Start() error {
	da.mu.Lock()
	if da.active.Load() {
		da.mu.Unlock()
		return nil
	}
	da.readyChan = make(chan struct{})
	da.doneChan = make(chan struct{})
	activeAnnotator = da
	da.mu.Unlock()

	go da.uiThread()

	select {
	case <-da.readyChan:
		da.active.Store(true)
		return nil
	case <-time.After(3 * time.Second):
		return fmt.Errorf("timeout waiting for desktop annotator initialization")
	}
}

// Stop closes the desktop annotation window and restores normal desktop interaction.
func (da *DesktopAnnotator) Stop() {
	da.mu.Lock()
	hwnd := da.hwnd
	da.mu.Unlock()

	if hwnd != 0 {
		procPostMessageWDA.Call(hwnd, uintptr(wmCloseDA), 0, 0)
		select {
		case <-da.doneChan:
		case <-time.After(2 * time.Second):
		}
	}
	da.active.Store(false)
}

// Clear removes all active drawing strokes.
func (da *DesktopAnnotator) Clear() {
	da.mu.Lock()
	da.strokes = nil
	da.currentStroke = nil
	hwnd := da.hwnd
	da.mu.Unlock()

	if hwnd != 0 {
		procInvalidateRectDA.Call(hwnd, 0, 0)
	}
}

// SetTool selects the drawing tool (toolRedPen, toolHighlighter, or toolEraser).
func (da *DesktopAnnotator) SetTool(tool int) {
	da.mu.Lock()
	da.currentTool = tool
	hwnd := da.hwnd
	da.mu.Unlock()

	if hwnd != 0 {
		procInvalidateRectDA.Call(hwnd, 0, 0)
	}
}

// AddStroke is a test helper for programmatic stroke injection.
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

func (da *DesktopAnnotator) uiThread() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// 1. Capture screen snapshot before window creation
	da.captureDesktopSnapshot()

	// 2. Register window class
	annotatorClassInitOnce.Do(func() {
		ptr, _ := windows.UTF16PtrFromString(annotatorClassName)
		annotatorClassNamePtr = ptr

		hCursor, _, _ := procLoadCursorWDA.Call(0, uintptr(32512)) // IDC_ARROW
		wc := wndClassExWDA{
			cbSize:        uint32(unsafe.Sizeof(wndClassExWDA{})),
			lpfnWndProc:   syscall.NewCallback(annotatorWndProc),
			hCursor:       hCursor,
			lpszClassName: annotatorClassNamePtr,
		}
		procRegisterClassExWDA.Call(uintptr(unsafe.Pointer(&wc)))
	})

	// 3. Create fullscreen topmost window covering virtual screen
	titlePtr, _ := windows.UTF16PtrFromString("LocalMeshDesktopAnnotator")
	hwnd, _, _ := procCreateWindowExWDA.Call(
		uintptr(wsExTopmostDA),
		uintptr(unsafe.Pointer(annotatorClassNamePtr)),
		uintptr(unsafe.Pointer(titlePtr)),
		uintptr(wsPopupDA),
		uintptr(da.screenX),
		uintptr(da.screenY),
		uintptr(da.screenW),
		uintptr(da.screenH),
		0, 0, 0, 0,
	)

	if hwnd == 0 {
		close(da.readyChan)
		return
	}

	da.mu.Lock()
	da.hwnd = hwnd
	da.mu.Unlock()

	procShowWindowDA.Call(hwnd, uintptr(swShowDA))
	procUpdateWindowDA.Call(hwnd)

	close(da.readyChan)

	// Message pump
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
		ret, _, _ := procGetMessageWDA.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		procTranslateMessageDA.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageWDA.Call(uintptr(unsafe.Pointer(&msg)))
	}

	// Cleanup GDI snapshot resources
	da.cleanupSnapshot()

	da.mu.Lock()
	da.hwnd = 0
	da.active.Store(false)
	da.mu.Unlock()

	if da.onClose != nil {
		da.onClose()
	}

	close(da.doneChan)
}

func (da *DesktopAnnotator) captureDesktopSnapshot() {
	rx, _, _ := procGetSystemMetricsDA.Call(76) // SM_XVIRTUALSCREEN
	ry, _, _ := procGetSystemMetricsDA.Call(77) // SM_YVIRTUALSCREEN
	rw, _, _ := procGetSystemMetricsDA.Call(78) // SM_CXVIRTUALSCREEN
	rh, _, _ := procGetSystemMetricsDA.Call(79) // SM_CYVIRTUALSCREEN

	da.screenX = int32(rx)
	da.screenY = int32(ry)
	da.screenW = int32(rw)
	da.screenH = int32(rh)

	if da.screenW <= 0 {
		da.screenW = 1920
	}
	if da.screenH <= 0 {
		da.screenH = 1080
	}

	hDeskDC, _, _ := procGetDCDA.Call(0)
	if hDeskDC != 0 {
		hMemDC, _, _ := procCreateCompatibleDCDA.Call(hDeskDC)
		hBmp, _, _ := procCreateCompatibleBitmapDA.Call(hDeskDC, uintptr(da.screenW), uintptr(da.screenH))
		oldBmp, _, _ := procSelectObjectDA.Call(hMemDC, hBmp)

		procBitBltDA.Call(
			hMemDC, 0, 0, uintptr(da.screenW), uintptr(da.screenH),
			hDeskDC, uintptr(da.screenX), uintptr(da.screenY),
			uintptr(srccopyDA),
		)

		procReleaseDCDA.Call(0, hDeskDC)

		da.frozenDC = hMemDC
		da.frozenBmp = hBmp
		da.oldFrozen = oldBmp
	}
}

func (da *DesktopAnnotator) cleanupSnapshot() {
	if da.frozenDC != 0 {
		if da.oldFrozen != 0 {
			procSelectObjectDA.Call(da.frozenDC, da.oldFrozen)
		}
		if da.frozenBmp != 0 {
			procDeleteObjectDA.Call(da.frozenBmp)
		}
		procDeleteDCDA.Call(da.frozenDC)
		da.frozenDC = 0
		da.frozenBmp = 0
	}
}

func annotatorWndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	da := activeAnnotator
	if da == nil {
		ret, _, _ := procDefWindowProcWDA.Call(hwnd, uintptr(msg), wParam, lParam)
		return ret
	}

	switch msg {
	case wmEraseBkgndDA:
		return 1

	case wmCloseDA:
		procDestroyWindowDA.Call(hwnd)
		return 0

	case wmDestroyDA:
		procPostQuitMessageDA.Call(0)
		return 0

	case wmKeyDownDA:
		switch wParam {
		case vkEscapeDA, 'Q', 'q':
			// ESC or Q exits annotation immediately
			procDestroyWindowDA.Call(hwnd)
			return 0
		case 'P', 'p', '1':
			da.SetTool(toolRedPen)
		case 'H', 'h', '2':
			da.SetTool(toolHighlighter)
		case 'E', 'e', '3':
			da.SetTool(toolEraser)
		case 'C', 'c':
			da.Clear()
		}
		return 0

	case wmRButtonUpDA:
		// Right-click exits annotation smoothly
		procDestroyWindowDA.Call(hwnd)
		return 0

	case wmLButtonDownDA:
		x := int32(int16(lParam & 0xFFFF))
		y := int32(int16((lParam >> 16) & 0xFFFF))

		// Check if click was on the toolbar
		tbIdx := getToolbarButtonIndex(x, y, da.screenW)
		if tbIdx >= 0 {
			switch tbIdx {
			case 0:
				da.SetTool(toolRedPen)
			case 1:
				da.SetTool(toolHighlighter)
			case 2:
				da.SetTool(toolEraser)
			case 3:
				da.Clear()
			case 4:
				procDestroyWindowDA.Call(hwnd)
			}
			return 0
		}

		// Start drawing on canvas
		da.mu.Lock()
		da.isDrawing = true
		if da.currentTool == toolEraser {
			da.eraseNearPoint(x, y, 24)
		} else {
			color := uint32(0x002020DC) // Red: RGB(220, 32, 32)
			width := int32(3)
			if da.currentTool == toolHighlighter {
				color = uint32(0x004FD5FF) // Yellow: RGB(255, 213, 79)
				width = 14
			}
			da.currentStroke = &strokeDA{
				tool:   da.currentTool,
				color:  color,
				width:  width,
				points: []pointDA{{X: x, Y: y}},
			}
		}
		da.mu.Unlock()
		procInvalidateRectDA.Call(hwnd, 0, 0)
		return 0

	case wmMouseMoveDA:
		if da.isDrawing {
			x := int32(int16(lParam & 0xFFFF))
			y := int32(int16((lParam >> 16) & 0xFFFF))

			da.mu.Lock()
			if da.currentTool == toolEraser {
				da.eraseNearPoint(x, y, 24)
			} else if da.currentStroke != nil {
				da.currentStroke.points = append(da.currentStroke.points, pointDA{X: x, Y: y})
			}
			da.mu.Unlock()
			procInvalidateRectDA.Call(hwnd, 0, 0)
		}
		return 0

	case wmLButtonUpDA:
		if da.isDrawing {
			da.mu.Lock()
			da.isDrawing = false
			if da.currentStroke != nil && len(da.currentStroke.points) > 0 {
				da.strokes = append(da.strokes, *da.currentStroke)
				da.currentStroke = nil
			}
			da.mu.Unlock()
			procInvalidateRectDA.Call(hwnd, 0, 0)
		}
		return 0

	case wmPaintDA:
		var ps paintStructDA
		hdc, _, _ := procBeginPaintDA.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		if hdc != 0 {
			memDC, _, _ := procCreateCompatibleDCDA.Call(hdc)
			memBmp, _, _ := procCreateCompatibleBitmapDA.Call(hdc, uintptr(da.screenW), uintptr(da.screenH))
			oldBmp, _, _ := procSelectObjectDA.Call(memDC, memBmp)

			// 1. Draw frozen desktop snapshot
			if da.frozenDC != 0 {
				procBitBltDA.Call(
					memDC, 0, 0, uintptr(da.screenW), uintptr(da.screenH),
					da.frozenDC, 0, 0,
					uintptr(srccopyDA),
				)
			}

			// 2. Render all strokes
			da.mu.RLock()
			for _, st := range da.strokes {
				renderStroke(memDC, st)
			}
			if da.currentStroke != nil {
				renderStroke(memDC, *da.currentStroke)
			}
			curTool := da.currentTool
			da.mu.RUnlock()

			// 3. Render floating annotator toolbar at top center
			renderAnnotatorToolbar(memDC, da.screenW, curTool)

			// 4. Blit to window
			procBitBltDA.Call(hdc, 0, 0, uintptr(da.screenW), uintptr(da.screenH), memDC, 0, 0, uintptr(srccopyDA))

			procSelectObjectDA.Call(memDC, oldBmp)
			procDeleteObjectDA.Call(memBmp)
			procDeleteDCDA.Call(memDC)

			procEndPaintDA.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		}
		return 0
	}

	ret, _, _ := procDefWindowProcWDA.Call(hwnd, uintptr(msg), wParam, lParam)
	return ret
}

func (da *DesktopAnnotator) eraseNearPoint(x, y, radius int32) {
	filtered := da.strokes[:0]
	rSq := float64(radius * radius)

	for _, st := range da.strokes {
		keep := true
		for _, p := range st.points {
			dx := float64(p.X - x)
			dy := float64(p.Y - y)
			if dx*dx+dy*dy <= rSq {
				keep = false
				break
			}
		}
		if keep {
			filtered = append(filtered, st)
		}
	}
	da.strokes = filtered
}

func renderStroke(hdc uintptr, st strokeDA) {
	if len(st.points) == 0 {
		return
	}

	hPen, _, _ := procCreatePenDA.Call(uintptr(psSolidDA), uintptr(st.width), uintptr(st.color))
	oldPen, _, _ := procSelectObjectDA.Call(hdc, hPen)

	first := st.points[0]
	procMoveToExDA.Call(hdc, uintptr(first.X), uintptr(first.Y), 0)

	for _, pt := range st.points[1:] {
		procLineToDA.Call(hdc, uintptr(pt.X), uintptr(pt.Y))
	}

	procSelectObjectDA.Call(hdc, oldPen)
	procDeleteObjectDA.Call(hPen)
}

func getToolbarButtonIndex(x, y, screenW int32) int {
	tbWidth := int32(460)
	tbHeight := int32(44)
	tbTop := int32(14)
	tbLeft := (screenW - tbWidth) / 2

	if y < tbTop || y > tbTop+tbHeight || x < tbLeft || x > tbLeft+tbWidth {
		return -1
	}

	relX := x - tbLeft
	btnWidth := tbWidth / 5 // 5 buttons
	idx := int(relX / btnWidth)
	if idx > 4 {
		idx = 4
	}
	return idx
}

func renderAnnotatorToolbar(hdc uintptr, screenW int32, activeTool int) {
	tbWidth := int32(460)
	tbHeight := int32(44)
	tbTop := int32(14)
	tbLeft := (screenW - tbWidth) / 2

	tbRect := rectDA{left: tbLeft, top: tbTop, right: tbLeft + tbWidth, bottom: tbTop + tbHeight}

	// Toolbar background pill
	hBgBrush, _, _ := procCreateSolidBrushDA.Call(0x001B1A20) // Deep dark gray
	procFillRectDA.Call(hdc, uintptr(unsafe.Pointer(&tbRect)), hBgBrush)
	procDeleteObjectDA.Call(hBgBrush)

	buttons := []struct {
		text   string
		toolID int
	}{
		{text: "✏️ 红笔(P)", toolID: toolRedPen},
		{text: "🖍️ 高亮(H)", toolID: toolHighlighter},
		{text: "🧹 橡皮(E)", toolID: toolEraser},
		{text: "🗑️ 清屏(C)", toolID: -1},
		{text: "❌ 退出(ESC)", toolID: -2},
	}

	btnWidth := tbWidth / 5

	hFont := createCapsuleFontDA(12, 600)
	oldFont, _, _ := procSelectObjectDA.Call(hdc, hFont)
	defer func() {
		procSelectObjectDA.Call(hdc, oldFont)
		procDeleteObjectDA.Call(hFont)
	}()

	procSetBkModeDA.Call(hdc, 1) // TRANSPARENT

	for i, btn := range buttons {
		bLeft := tbLeft + int32(i)*btnWidth + 2
		bRight := bLeft + btnWidth - 4
		bRect := rectDA{left: bLeft, top: tbTop + 4, right: bRight, bottom: tbTop + tbHeight - 4}

		// Highlight active tool button
		if btn.toolID == activeTool {
			hActiveBrush, _, _ := procCreateSolidBrushDA.Call(0x007C3AED) // Vibrant purple/accent
			procFillRectDA.Call(hdc, uintptr(unsafe.Pointer(&bRect)), hActiveBrush)
			procDeleteObjectDA.Call(hActiveBrush)
			procSetTextColorDA.Call(hdc, 0x00FFFFFF)
		} else {
			procSetTextColorDA.Call(hdc, 0x00D1D5DB)
		}

		txtPtr, _ := windows.UTF16PtrFromString(btn.text)
		procDrawTextWDA.Call(
			hdc,
			uintptr(unsafe.Pointer(txtPtr)),
			uintptr(len([]rune(btn.text))),
			uintptr(unsafe.Pointer(&bRect)),
			uintptr(0x00000001|0x00000004|0x00000020), // DT_CENTER | DT_VCENTER | DT_SINGLELINE
		)
	}
}

func createCapsuleFontDA(size int32, weight int32) uintptr {
	faceUTF16 := windows.StringToUTF16("Segoe UI")
	hFont, _, _ := procCreateFontWDA.Call(
		uintptr(-size),
		0, 0, 0,
		uintptr(weight),
		0, 0, 0,
		1, 0, 0, 5, 0,
		uintptr(unsafe.Pointer(&faceUTF16[0])),
	)
	return hFont
}
