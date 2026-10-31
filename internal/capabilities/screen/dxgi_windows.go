//go:build windows

package screen

import (
	"fmt"
	"image"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modD3D11 = windows.NewLazySystemDLL("d3d11.dll")
	modDXGI  = windows.NewLazySystemDLL("dxgi.dll")

	procD3D11CreateDevice = modD3D11.NewProc("D3D11CreateDevice")
)

type guid struct {
	data1 uint32
	data2 uint16
	data3 uint16
	data4 [8]byte
}

var (
	// IID_IDXGIDevice: 54ec77fa-1377-44e6-8c32-88fd5f44c84c
	iidIDXGIDevice = guid{
		data1: 0x54ec77fa,
		data2: 0x1377,
		data3: 0x44e6,
		data4: [8]byte{0x8c, 0x32, 0x88, 0xfd, 0x5f, 0x44, 0xc8, 0x4c},
	}

	// IID_IDXGIOutput1: 00cddea8-939b-4b83-a340-a685226666cc
	iidIDXGIOutput1 = guid{
		data1: 0x00cddea8,
		data2: 0x939b,
		data3: 0x4b83,
		data4: [8]byte{0xa3, 0x40, 0xa6, 0x85, 0x22, 0x66, 0x66, 0xcc},
	}

	// IID_ID3D11Texture2D: 6f15aaf2-d208-4e89-9ab4-489535d34f9c
	iidID3D11Texture2D = guid{
		data1: 0x6f15aaf2,
		data2: 0xd208,
		data3: 0x4e89,
		data4: [8]byte{0x9a, 0xb4, 0x48, 0x95, 0x35, 0xd3, 0x4f, 0x9c},
	}
)

const (
	d3dDriverTypeHardware = 1
	d3dDriverTypeWARP     = 2

	d3d11CreateDeviceBGRASupport = 0x20
	d3d11SdkVersion              = 7

	d3d11UsageStaging  = 3
	d3d11CPUAccessRead = 0x20000
	d3d11MapRead       = 1

	dxgiFormatB8G8R8A8UNORM = 87

	sOK                          = 0x00000000
	dxgiErrorMoreData            = 0x887A0003
	dxgiErrorUnsupported         = 0x887A0004
	dxgiErrorSessionDisconnected = 0x887A0020
	dxgiErrorNotCurrentlyAvail   = 0x887A0022
	dxgiErrorAccessLost          = 0x887A0026
	dxgiErrorWaitTimeout         = 0x887A0027
	dxgiErrorAccessDenied        = 0x887A002B
	dxgiErrorInvalidCall         = 0x887A0001
	hresultAccessDenied          = 0x80070005
)

type dxgiSampleDesc struct {
	Count   uint32
	Quality uint32
}

type d3d11Texture2DDesc struct {
	Width          uint32
	Height         uint32
	MipLevels      uint32
	ArraySize      uint32
	Format         uint32
	SampleDesc     dxgiSampleDesc
	Usage          uint32
	BindFlags      uint32
	CPUAccessFlags uint32
	MiscFlags      uint32
}

type d3d11MappedSubresource struct {
	pData      unsafe.Pointer
	RowPitch   uint32
	DepthPitch uint32
}

type winRect struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

type dxgiPoint struct {
	X int32
	Y int32
}

type dxgiOutduplPointerPosition struct {
	Position dxgiPoint
	Visible  int32
}

type dxgiOutduplFrameInfo struct {
	LastPresentTime           int64
	LastMouseUpdateTime       int64
	AccumulatedFrames         uint32
	RectsCoalesced            int32
	ProtectedContentMaskedOut int32
	PointerPosition           dxgiOutduplPointerPosition
	TotalMetadataBufferSize   uint32
	PointerShapeBufferSize    uint32
}

type dxgiRational struct {
	Numerator   uint32
	Denominator uint32
}

type dxgiModeDesc struct {
	Width            uint32
	Height           uint32
	RefreshRate      dxgiRational
	Format           uint32
	ScanlineOrdering uint32
	Scaling          uint32
}

type dxgiOutduplDesc struct {
	ModeDesc                   dxgiModeDesc
	Rotation                   uint32
	DesktopImageInSystemMemory int32
}

type dxgiOutputDesc struct {
	DeviceName         [32]uint16
	DesktopCoordinates winRect
	AttachedToDesktop  int32
	Rotation           uint32
	Monitor            uintptr
}

// Low-level COM interface abstractions

type iUnknownVtbl [100]uintptr

type iUnknown struct {
	lpVtbl *iUnknownVtbl
}

func (u *iUnknown) Release() uint32 {
	if u == nil || u.lpVtbl == nil {
		return 0
	}
	r1, _, _ := syscall.SyscallN(u.lpVtbl[2], uintptr(unsafe.Pointer(u)))
	return uint32(r1)
}

func (u *iUnknown) QueryInterface(riid *guid, ppv unsafe.Pointer) uint32 {
	r1, _, _ := syscall.SyscallN(u.lpVtbl[0], uintptr(unsafe.Pointer(u)), uintptr(unsafe.Pointer(riid)), uintptr(ppv))
	return uint32(r1)
}

type d3d11Device iUnknown

func (d *d3d11Device) Release() uint32 {
	return (*iUnknown)(d).Release()
}

func (d *d3d11Device) QueryInterface(riid *guid, ppv unsafe.Pointer) uint32 {
	return (*iUnknown)(d).QueryInterface(riid, ppv)
}

func (d *d3d11Device) CreateTexture2D(desc *d3d11Texture2DDesc, initialData unsafe.Pointer, ppTexture2D **d3d11Texture2D) uint32 {
	r1, _, _ := syscall.SyscallN(
		d.lpVtbl[5],
		uintptr(unsafe.Pointer(d)),
		uintptr(unsafe.Pointer(desc)),
		uintptr(initialData),
		uintptr(unsafe.Pointer(ppTexture2D)),
	)
	return uint32(r1)
}

type d3d11DeviceContext iUnknown

func (c *d3d11DeviceContext) Release() uint32 {
	return (*iUnknown)(c).Release()
}

func (c *d3d11DeviceContext) Map(resource *iUnknown, subresource uint32, mapType uint32, mapFlags uint32, mapped *d3d11MappedSubresource) uint32 {
	r1, _, _ := syscall.SyscallN(
		c.lpVtbl[14],
		uintptr(unsafe.Pointer(c)),
		uintptr(unsafe.Pointer(resource)),
		uintptr(subresource),
		uintptr(mapType),
		uintptr(mapFlags),
		uintptr(unsafe.Pointer(mapped)),
	)
	return uint32(r1)
}

func (c *d3d11DeviceContext) Unmap(resource *iUnknown, subresource uint32) {
	syscall.SyscallN(
		c.lpVtbl[15],
		uintptr(unsafe.Pointer(c)),
		uintptr(unsafe.Pointer(resource)),
		uintptr(subresource),
	)
}

func (c *d3d11DeviceContext) CopyResource(dst *iUnknown, src *iUnknown) {
	syscall.SyscallN(
		c.lpVtbl[47],
		uintptr(unsafe.Pointer(c)),
		uintptr(unsafe.Pointer(dst)),
		uintptr(unsafe.Pointer(src)),
	)
}

type dxgiDevice iUnknown

func (d *dxgiDevice) Release() uint32 {
	return (*iUnknown)(d).Release()
}

func (d *dxgiDevice) GetAdapter(ppAdapter **dxgiAdapter) uint32 {
	r1, _, _ := syscall.SyscallN(
		d.lpVtbl[7],
		uintptr(unsafe.Pointer(d)),
		uintptr(unsafe.Pointer(ppAdapter)),
	)
	return uint32(r1)
}

type dxgiAdapter iUnknown

func (a *dxgiAdapter) Release() uint32 {
	return (*iUnknown)(a).Release()
}

func (a *dxgiAdapter) EnumOutputs(output uint32, ppOutput **dxgiOutput) uint32 {
	r1, _, _ := syscall.SyscallN(
		a.lpVtbl[7],
		uintptr(unsafe.Pointer(a)),
		uintptr(output),
		uintptr(unsafe.Pointer(ppOutput)),
	)
	return uint32(r1)
}

type dxgiOutput iUnknown

func (o *dxgiOutput) Release() uint32 {
	return (*iUnknown)(o).Release()
}

func (o *dxgiOutput) QueryInterface(riid *guid, ppv unsafe.Pointer) uint32 {
	return (*iUnknown)(o).QueryInterface(riid, ppv)
}

func (o *dxgiOutput) GetDesc(pDesc *dxgiOutputDesc) uint32 {
	r1, _, _ := syscall.SyscallN(
		o.lpVtbl[7],
		uintptr(unsafe.Pointer(o)),
		uintptr(unsafe.Pointer(pDesc)),
	)
	return uint32(r1)
}

type dxgiOutput1 iUnknown

func (o *dxgiOutput1) Release() uint32 {
	return (*iUnknown)(o).Release()
}

func (o *dxgiOutput1) DuplicateOutput(device *iUnknown, ppOutputDuplication **dxgiOutputDuplication) uint32 {
	r1, _, _ := syscall.SyscallN(
		o.lpVtbl[22],
		uintptr(unsafe.Pointer(o)),
		uintptr(unsafe.Pointer(device)),
		uintptr(unsafe.Pointer(ppOutputDuplication)),
	)
	return uint32(r1)
}

type dxgiOutputDuplication iUnknown

func (d *dxgiOutputDuplication) Release() uint32 {
	return (*iUnknown)(d).Release()
}

func (d *dxgiOutputDuplication) GetDesc(pDesc *dxgiOutduplDesc) {
	syscall.SyscallN(
		d.lpVtbl[7],
		uintptr(unsafe.Pointer(d)),
		uintptr(unsafe.Pointer(pDesc)),
	)
}

func (d *dxgiOutputDuplication) AcquireNextFrame(timeout uint32, pFrameInfo *dxgiOutduplFrameInfo, ppDesktopResource **iUnknown) uint32 {
	r1, _, _ := syscall.SyscallN(
		d.lpVtbl[8],
		uintptr(unsafe.Pointer(d)),
		uintptr(timeout),
		uintptr(unsafe.Pointer(pFrameInfo)),
		uintptr(unsafe.Pointer(ppDesktopResource)),
	)
	return uint32(r1)
}

func (d *dxgiOutputDuplication) GetFrameDirtyRects(bufferSize uint32, pBuffer *winRect, pRequiredSize *uint32) uint32 {
	r1, _, _ := syscall.SyscallN(
		d.lpVtbl[9],
		uintptr(unsafe.Pointer(d)),
		uintptr(bufferSize),
		uintptr(unsafe.Pointer(pBuffer)),
		uintptr(unsafe.Pointer(pRequiredSize)),
	)
	return uint32(r1)
}

func (d *dxgiOutputDuplication) ReleaseFrame() uint32 {
	r1, _, _ := syscall.SyscallN(
		d.lpVtbl[14],
		uintptr(unsafe.Pointer(d)),
	)
	return uint32(r1)
}

type d3d11Texture2D iUnknown

func (t *d3d11Texture2D) Release() uint32 {
	return (*iUnknown)(t).Release()
}

// DXGICapturer implements ultra high-speed screen capture via the Windows
// DXGI 1.2 Desktop Duplication API (DDA), supporting dirty rects and 60FPS zero-copy capture.
type DXGICapturer struct {
	mu           sync.Mutex
	displayIndex int
	timeoutMs    uint32

	d3dDevice  *d3d11Device
	d3dContext *d3d11DeviceContext
	outputDupl *dxgiOutputDuplication
	stagingTex *d3d11Texture2D

	width  int
	height int
	bounds image.Rectangle

	cachedFrame  *CapturedFrame
	cachedRGBA   *image.RGBA
	dirtyRectBuf []winRect

	closed     bool
	accessLost bool
}

// NewDXGICapturer initializes a DXGI Desktop Duplication capture session.
// displayIndex specifies the monitor index (0 = primary).
// timeout specifies the wait duration in AcquireNextFrame before returning timeout.
func NewDXGICapturer(displayIndex int, timeout time.Duration) (*DXGICapturer, error) {
	if procD3D11CreateDevice.Find() != nil {
		return nil, ErrDXGIUnsupported
	}

	timeoutMs := uint32(timeout.Milliseconds())
	if timeoutMs == 0 {
		timeoutMs = 16 // default ~60fps frame wait
	}

	// 1. Create D3D11 device and immediate context (hardware driver first, then WARP)
	var (
		d3dDevice    *d3d11Device
		featureLevel uint32
		d3dContext   *d3d11DeviceContext
	)

	hr, _, _ := procD3D11CreateDevice.Call(
		0,
		uintptr(d3dDriverTypeHardware),
		0,
		uintptr(d3d11CreateDeviceBGRASupport),
		0, 0,
		d3d11SdkVersion,
		uintptr(unsafe.Pointer(&d3dDevice)),
		uintptr(unsafe.Pointer(&featureLevel)),
		uintptr(unsafe.Pointer(&d3dContext)),
	)

	if uint32(hr) != sOK {
		hr, _, _ = procD3D11CreateDevice.Call(
			0,
			uintptr(d3dDriverTypeWARP),
			0,
			uintptr(d3d11CreateDeviceBGRASupport),
			0, 0,
			d3d11SdkVersion,
			uintptr(unsafe.Pointer(&d3dDevice)),
			uintptr(unsafe.Pointer(&featureLevel)),
			uintptr(unsafe.Pointer(&d3dContext)),
		)
		if uint32(hr) != sOK {
			return nil, fmt.Errorf("%w: D3D11CreateDevice failed: %s", ErrDXGIUnsupported, formatHRESULT(uint32(hr)))
		}
	}

	cleanup := func() {
		if d3dContext != nil {
			d3dContext.Release()
		}
		if d3dDevice != nil {
			d3dDevice.Release()
		}
	}

	// 2. Query IDXGIDevice
	var dxgiDev *dxgiDevice
	hrQuery := d3dDevice.QueryInterface(&iidIDXGIDevice, unsafe.Pointer(&dxgiDev))
	if hrQuery != sOK {
		cleanup()
		return nil, fmt.Errorf("%w: query IDXGIDevice failed: %s", ErrDXGIUnsupported, formatHRESULT(hrQuery))
	}
	defer dxgiDev.Release()

	// 3. Get IDXGIAdapter
	var adapter *dxgiAdapter
	hrAdapter := dxgiDev.GetAdapter(&adapter)
	if hrAdapter != sOK {
		cleanup()
		return nil, fmt.Errorf("%w: GetAdapter failed: %s", ErrDXGIUnsupported, formatHRESULT(hrAdapter))
	}
	defer adapter.Release()

	// 4. Enumerate specified output
	var output *dxgiOutput
	hrEnum := adapter.EnumOutputs(uint32(displayIndex), &output)
	if hrEnum != sOK {
		cleanup()
		return nil, fmt.Errorf("%w: EnumOutputs(%d) failed: %s", ErrDXGIUnsupported, displayIndex, formatHRESULT(hrEnum))
	}
	defer output.Release()

	// 5. Query IDXGIOutput1
	var output1 *dxgiOutput1
	hrOut1 := output.QueryInterface(&iidIDXGIOutput1, unsafe.Pointer(&output1))
	if hrOut1 != sOK {
		cleanup()
		return nil, fmt.Errorf("%w: query IDXGIOutput1 failed: %s", ErrDXGIUnsupported, formatHRESULT(hrOut1))
	}
	defer output1.Release()

	// 6. DuplicateOutput
	var outputDupl *dxgiOutputDuplication
	hrDupl := output1.DuplicateOutput((*iUnknown)(unsafe.Pointer(d3dDevice)), &outputDupl)
	if hrDupl != sOK {
		cleanup()
		if isAccessLost(hrDupl) {
			return nil, fmt.Errorf("%w: DuplicateOutput: %s", ErrDXGIAccessLost, formatHRESULT(hrDupl))
		}
		return nil, fmt.Errorf("%w: DuplicateOutput failed: %s", ErrDXGIUnsupported, formatHRESULT(hrDupl))
	}

	// 7. Get display dimensions
	var duplDesc dxgiOutduplDesc
	outputDupl.GetDesc(&duplDesc)
	width := int(duplDesc.ModeDesc.Width)
	height := int(duplDesc.ModeDesc.Height)

	if width <= 0 || height <= 0 {
		var outDesc dxgiOutputDesc
		if output.GetDesc(&outDesc) == sOK {
			width = int(outDesc.DesktopCoordinates.Right - outDesc.DesktopCoordinates.Left)
			height = int(outDesc.DesktopCoordinates.Bottom - outDesc.DesktopCoordinates.Top)
		}
	}

	if width <= 0 || height <= 0 {
		outputDupl.Release()
		cleanup()
		return nil, fmt.Errorf("%w: invalid desktop dimensions: %dx%d", ErrDXGIUnsupported, width, height)
	}

	// 8. Create CPU-readable staging texture
	stagingDesc := d3d11Texture2DDesc{
		Width:     uint32(width),
		Height:    uint32(height),
		MipLevels: 1,
		ArraySize: 1,
		Format:    dxgiFormatB8G8R8A8UNORM,
		SampleDesc: dxgiSampleDesc{
			Count:   1,
			Quality: 0,
		},
		Usage:          d3d11UsageStaging,
		BindFlags:      0,
		CPUAccessFlags: d3d11CPUAccessRead,
		MiscFlags:      0,
	}

	var stagingTex *d3d11Texture2D
	hrTex := d3dDevice.CreateTexture2D(&stagingDesc, nil, &stagingTex)
	if hrTex != sOK {
		outputDupl.Release()
		cleanup()
		return nil, fmt.Errorf("CreateTexture2D staging failed: %s", formatHRESULT(hrTex))
	}

	bounds := image.Rect(0, 0, width, height)
	capturer := &DXGICapturer{
		displayIndex: displayIndex,
		timeoutMs:    timeoutMs,
		d3dDevice:    d3dDevice,
		d3dContext:   d3dContext,
		outputDupl:   outputDupl,
		stagingTex:   stagingTex,
		width:        width,
		height:       height,
		bounds:       bounds,
		cachedRGBA:   image.NewRGBA(bounds),
		dirtyRectBuf: make([]winRect, 16),
	}

	return capturer, nil
}

// Capture grabs the next desktop frame from VRAM, extracts dirty rects, and maps to memory.
func (c *DXGICapturer) Capture() (*CapturedFrame, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil, ErrCapturerClosed
	}
	if c.accessLost {
		return nil, ErrDXGIAccessLost
	}

	var (
		frameInfo  dxgiOutduplFrameInfo
		desktopRes *iUnknown
	)

	hr := c.outputDupl.AcquireNextFrame(c.timeoutMs, &frameInfo, &desktopRes)
	if hr == dxgiErrorWaitTimeout {
		// Desktop has not changed since last frame.
		if c.cachedFrame != nil {
			return c.cachedFrame, nil
		}
		// First frame wait timeout: try once with a slightly longer wait
		hr = c.outputDupl.AcquireNextFrame(100, &frameInfo, &desktopRes)
		if hr == dxgiErrorWaitTimeout {
			return nil, ErrNoNewFrame
		}
	}

	if isAccessLost(hr) {
		c.accessLost = true
		c.releaseDXGI()
		return nil, fmt.Errorf("%w: %s", ErrDXGIAccessLost, formatHRESULT(hr))
	}

	if hr != sOK {
		return nil, fmt.Errorf("AcquireNextFrame failed: %s", formatHRESULT(hr))
	}

	// 1. Extract Dirty Rectangles
	var dirtyRects []image.Rectangle
	if frameInfo.TotalMetadataBufferSize > 0 {
		var reqSize uint32
		bufBytes := uint32(len(c.dirtyRectBuf) * int(unsafe.Sizeof(winRect{})))
		dirtyHr := c.outputDupl.GetFrameDirtyRects(bufBytes, &c.dirtyRectBuf[0], &reqSize)
		if dirtyHr == dxgiErrorMoreData || reqSize > bufBytes {
			c.dirtyRectBuf = make([]winRect, (reqSize+15)/16)
			bufBytes = uint32(len(c.dirtyRectBuf) * 16)
			dirtyHr = c.outputDupl.GetFrameDirtyRects(bufBytes, &c.dirtyRectBuf[0], &reqSize)
		}

		if dirtyHr == sOK && reqSize > 0 {
			numRects := int(reqSize / 16)
			dirtyRects = make([]image.Rectangle, 0, numRects)
			for i := 0; i < numRects; i++ {
				r := c.dirtyRectBuf[i]
				rect := image.Rect(int(r.Left), int(r.Top), int(r.Right), int(r.Bottom)).Intersect(c.bounds)
				if !rect.Empty() {
					dirtyRects = append(dirtyRects, rect)
				}
			}
		}
	}

	if len(dirtyRects) == 0 {
		dirtyRects = []image.Rectangle{c.bounds}
	}

	// 2. Copy desktop surface from GPU to staging texture
	if desktopRes != nil {
		var tex2D *d3d11Texture2D
		texHr := desktopRes.QueryInterface(&iidID3D11Texture2D, unsafe.Pointer(&tex2D))
		if texHr == sOK && tex2D != nil {
			c.d3dContext.CopyResource((*iUnknown)(unsafe.Pointer(c.stagingTex)), (*iUnknown)(unsafe.Pointer(tex2D)))
			tex2D.Release()
		}
		desktopRes.Release()
	}

	// 3. Immediately release frame back to DXGI so DWM is not blocked
	_ = c.outputDupl.ReleaseFrame()

	// 4. Map staging texture into CPU address space
	var mapped d3d11MappedSubresource
	mapHr := c.d3dContext.Map((*iUnknown)(unsafe.Pointer(c.stagingTex)), 0, d3d11MapRead, 0, &mapped)
	if mapHr != sOK {
		if isAccessLost(mapHr) {
			c.accessLost = true
			c.releaseDXGI()
			return nil, fmt.Errorf("%w: Map failed: %s", ErrDXGIAccessLost, formatHRESULT(mapHr))
		}
		return nil, fmt.Errorf("Map staging texture failed: %s", formatHRESULT(mapHr))
	}
	defer c.d3dContext.Unmap((*iUnknown)(unsafe.Pointer(c.stagingTex)), 0)

	// 5. Convert mapped BGRA surface to *image.RGBA
	copyBGRAtoRGBA(c.cachedRGBA, mapped.pData, mapped.RowPitch, dirtyRects)

	// 6. Snapshot current image for caller safety
	outImg := cloneRGBA(c.cachedRGBA)
	c.cachedFrame = &CapturedFrame{
		Image:      outImg,
		DirtyRects: dirtyRects,
		Width:      c.width,
		Height:     c.height,
		Timestamp:  time.Now(),
		Backend:    BackendDXGI,
	}

	return c.cachedFrame, nil
}

// CaptureImage acquires only the image representation of the desktop.
func (c *DXGICapturer) CaptureImage() (image.Image, error) {
	frame, err := c.Capture()
	if err != nil {
		return nil, err
	}
	return frame.Image, nil
}

// Close releases all Direct3D 11 and DXGI resources.
func (c *DXGICapturer) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true
	c.releaseDXGI()
	return nil
}

// Width returns the display width of the captured screen.
func (c *DXGICapturer) Width() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.width
}

// Height returns the display height of the captured screen.
func (c *DXGICapturer) Height() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.height
}

// Bounds returns the rectangle bounds of the captured screen.
func (c *DXGICapturer) Bounds() image.Rectangle {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.bounds
}

func (c *DXGICapturer) releaseDXGI() {
	if c.stagingTex != nil {
		c.stagingTex.Release()
		c.stagingTex = nil
	}
	if c.outputDupl != nil {
		c.outputDupl.Release()
		c.outputDupl = nil
	}
	if c.d3dContext != nil {
		c.d3dContext.Release()
		c.d3dContext = nil
	}
	if c.d3dDevice != nil {
		c.d3dDevice.Release()
		c.d3dDevice = nil
	}
}

func copyBGRAtoRGBA(dst *image.RGBA, pData unsafe.Pointer, rowPitch uint32, rects []image.Rectangle) {
	if len(rects) == 0 {
		rects = []image.Rectangle{dst.Rect}
	}

	for _, r := range rects {
		rect := r.Intersect(dst.Rect)
		if rect.Empty() {
			continue
		}

		minX := rect.Min.X
		minY := rect.Min.Y
		maxX := rect.Max.X
		maxY := rect.Max.Y
		w := maxX - minX

		for y := minY; y < maxY; y++ {
			srcOffset := uintptr(y)*uintptr(rowPitch) + uintptr(minX)*4
			dstOffset := y*dst.Stride + minX*4
			srcRow := unsafe.Slice((*byte)(unsafe.Add(pData, srcOffset)), w*4)
			dstRow := dst.Pix[dstOffset : dstOffset+w*4]

			for i := 0; i < w*4; i += 4 {
				dstRow[i] = srcRow[i+2]   // Red
				dstRow[i+1] = srcRow[i+1] // Green
				dstRow[i+2] = srcRow[i]   // Blue
				dstRow[i+3] = 255         // Alpha = 255 (fully opaque)
			}
		}
	}
}

func cloneRGBA(src *image.RGBA) *image.RGBA {
	if src == nil {
		return nil
	}
	dst := image.NewRGBA(src.Rect)
	copy(dst.Pix, src.Pix)
	return dst
}

func isAccessLost(hr uint32) bool {
	switch hr {
	case dxgiErrorAccessLost,
		dxgiErrorAccessDenied,
		dxgiErrorSessionDisconnected,
		dxgiErrorInvalidCall,
		hresultAccessDenied:
		return true
	default:
		return false
	}
}

func formatHRESULT(hr uint32) string {
	switch hr {
	case sOK:
		return "S_OK (0x0)"
	case dxgiErrorAccessLost:
		return "DXGI_ERROR_ACCESS_LOST (0x887A0026)"
	case dxgiErrorWaitTimeout:
		return "DXGI_ERROR_WAIT_TIMEOUT (0x887A0027)"
	case dxgiErrorUnsupported:
		return "DXGI_ERROR_UNSUPPORTED (0x887A0004)"
	case dxgiErrorSessionDisconnected:
		return "DXGI_ERROR_SESSION_DISCONNECTED (0x887A0020)"
	case dxgiErrorNotCurrentlyAvail:
		return "DXGI_ERROR_NOT_CURRENTLY_AVAILABLE (0x887A0022)"
	case dxgiErrorInvalidCall:
		return "DXGI_ERROR_INVALID_CALL (0x887A0001)"
	case dxgiErrorAccessDenied:
		return "DXGI_ERROR_ACCESS_DENIED (0x887A002B)"
	case hresultAccessDenied:
		return "E_ACCESSDENIED (0x80070005)"
	default:
		return fmt.Sprintf("0x%08X", hr)
	}
}
