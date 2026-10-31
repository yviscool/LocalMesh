//go:build windows

package audio

import (
	"context"
	"fmt"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modWinmm = windows.NewLazySystemDLL("winmm.dll")

	procWaveOutOpen            = modWinmm.NewProc("waveOutOpen")
	procWaveOutClose           = modWinmm.NewProc("waveOutClose")
	procWaveOutPrepareHeader   = modWinmm.NewProc("waveOutPrepareHeader")
	procWaveOutUnprepareHeader = modWinmm.NewProc("waveOutUnprepareHeader")
	procWaveOutWrite           = modWinmm.NewProc("waveOutWrite")
	procWaveOutReset           = modWinmm.NewProc("waveOutReset")
)

const (
	waveMapper   = 0xFFFFFFFF
	callbackNull = 0x00000000

	mmsyserrNoError     = 0
	mmsyserrBadDeviceID = 2
	mmsyserrNoDriver    = 6

	whdrDone     = 0x00000001
	whdrPrepared = 0x00000002

	// Number of ring buffers. 4 buffers of 10ms = 40ms total capacity, ensuring < 50ms latency.
	numRingBuffers = 4
)

type waveFormatExPlayer struct {
	wFormatTag      uint16
	nChannels       uint16
	nSamplesPerSec  uint32
	nAvgBytesPerSec uint32
	nBlockAlign     uint16
	wBitsPerSample  uint16
	cbSize          uint16
}

type waveHdr struct {
	lpData          *byte
	dwBufferLength  uint32
	dwBytesRecorded uint32
	dwUser          uintptr
	dwFlags         uint32
	dwLoops         uint32
	lpNext          uintptr
	reserved        uintptr
}

type ringSlot struct {
	header   waveHdr
	data     []byte
	prepared bool
}

// WindowsPlayer implements the Player interface for Windows using winmm waveOut,
// rendering PCM frames into speaker output through a ring buffer with < 50ms latency.
type WindowsPlayer struct {
	mu           sync.Mutex
	format       AudioFormat
	hWaveOut     uintptr
	running      bool
	headless     bool
	slots        [numRingBuffers]ringSlot
	slotIndex    int
	slotBytes    int
	partialBuf   []byte
	bytesWritten uint64
}

// NewPlayer returns a Windows native audio player.
func NewPlayer(fmt AudioFormat) Player {
	if err := fmt.Validate(); err != nil {
		fmt = DefaultAudioFormat
	}
	return &WindowsPlayer{
		format: fmt,
	}
}

// Format returns the audio format.
func (p *WindowsPlayer) Format() AudioFormat {
	return p.format
}

// Start opens the waveOut device and allocates the ring buffers.
func (p *WindowsPlayer) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.running {
		return nil
	}

	frameSize := p.format.FrameSize()
	slotBytes := p.format.DurationToBytes(DefaultFrameDuration)
	if slotBytes <= 0 {
		slotBytes = frameSize * 480
	}
	if rem := slotBytes % frameSize; rem != 0 {
		slotBytes += frameSize - rem
	}
	p.slotBytes = slotBytes

	wfx := waveFormatExPlayer{
		wFormatTag:      waveFormatPCM,
		nChannels:       uint16(p.format.Channels),
		nSamplesPerSec:  uint32(p.format.SampleRate),
		nAvgBytesPerSec: uint32(p.format.BytesPerSecond()),
		nBlockAlign:     uint16(frameSize),
		wBitsPerSample:  uint16(p.format.BitsPerSample),
		cbSize:          0,
	}

	var hWave uintptr
	r1, _, _ := procWaveOutOpen.Call(
		uintptr(unsafe.Pointer(&hWave)),
		uintptr(waveMapper),
		uintptr(unsafe.Pointer(&wfx)),
		0,
		0,
		callbackNull,
	)

	if r1 != mmsyserrNoError {
		// Headless fallback if no audio device / driver is installed in container or VM
		p.headless = true
		p.running = true
		return nil
	}

	p.hWaveOut = hWave
	p.headless = false
	p.running = true
	p.slotIndex = 0

	// Initialize ring buffer slots
	for i := 0; i < numRingBuffers; i++ {
		p.slots[i].data = make([]byte, slotBytes)
		p.slots[i].header = waveHdr{
			lpData:         &p.slots[i].data[0],
			dwBufferLength: uint32(slotBytes),
			dwFlags:        0,
		}
		p.slots[i].prepared = false
	}

	return nil
}

// Write streams PCM data into the ring buffer and triggers immediate playback.
func (p *WindowsPlayer) Write(pcm []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.running {
		return 0, ErrReceiverClosed
	}

	p.bytesWritten += uint64(len(pcm))

	if p.headless {
		return len(pcm), nil
	}

	p.partialBuf = append(p.partialBuf, pcm...)

	for len(p.partialBuf) >= p.slotBytes {
		chunk := p.partialBuf[:p.slotBytes]
		if err := p.playSlotLocked(chunk); err != nil {
			return 0, err
		}
		p.partialBuf = p.partialBuf[p.slotBytes:]
	}

	return len(pcm), nil
}

// playSlotLocked writes a filled chunk to the next ring buffer slot. Caller must hold p.mu.
func (p *WindowsPlayer) playSlotLocked(chunk []byte) error {
	slot := &p.slots[p.slotIndex]

	// If slot was previously prepared, check if done or wait briefly
	if slot.prepared {
		if slot.header.dwFlags&whdrDone == 0 {
			// Buffer still busy playing; poll up to 2ms
			start := time.Now()
			for slot.header.dwFlags&whdrDone == 0 && time.Since(start) < 2*time.Millisecond {
				time.Sleep(200 * time.Microsecond)
			}
		}

		if slot.header.dwFlags&whdrDone != 0 {
			procWaveOutUnprepareHeader.Call(
				p.hWaveOut,
				uintptr(unsafe.Pointer(&slot.header)),
				unsafe.Sizeof(slot.header),
			)
			slot.prepared = false
		} else {
			// Still busy; drop frame to maintain < 50ms latency without crashing
			p.slotIndex = (p.slotIndex + 1) % numRingBuffers
			return nil
		}
	}

	copy(slot.data, chunk)
	slot.header.dwBufferLength = uint32(len(chunk))
	slot.header.dwFlags = 0

	r1, _, _ := procWaveOutPrepareHeader.Call(
		p.hWaveOut,
		uintptr(unsafe.Pointer(&slot.header)),
		unsafe.Sizeof(slot.header),
	)
	if r1 != mmsyserrNoError {
		return fmt.Errorf("waveOutPrepareHeader failed: %d", r1)
	}
	slot.prepared = true

	r1, _, _ = procWaveOutWrite.Call(
		p.hWaveOut,
		uintptr(unsafe.Pointer(&slot.header)),
		unsafe.Sizeof(slot.header),
	)
	if r1 != mmsyserrNoError {
		return fmt.Errorf("waveOutWrite failed: %d", r1)
	}

	p.slotIndex = (p.slotIndex + 1) % numRingBuffers
	return nil
}

// BufferLatency returns the estimated latency of audio queued in ring buffers.
func (p *WindowsPlayer) BufferLatency() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.headless || !p.running {
		return 10 * time.Millisecond
	}

	activeSlots := 0
	for i := 0; i < numRingBuffers; i++ {
		if p.slots[i].prepared && (p.slots[i].header.dwFlags&whdrDone == 0) {
			activeSlots++
		}
	}

	return time.Duration(activeSlots) * DefaultFrameDuration
}

// Stop resets the waveOut device and frees all allocated ring buffers.
func (p *WindowsPlayer) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.running {
		return nil
	}
	p.running = false

	if p.headless {
		return nil
	}

	// Reset device to stop playing queued headers immediately
	procWaveOutReset.Call(p.hWaveOut)

	// Unprepare all slots
	for i := 0; i < numRingBuffers; i++ {
		if p.slots[i].prepared {
			procWaveOutUnprepareHeader.Call(
				p.hWaveOut,
				uintptr(unsafe.Pointer(&p.slots[i].header)),
				unsafe.Sizeof(p.slots[i].header),
			)
			p.slots[i].prepared = false
		}
	}

	procWaveOutClose.Call(p.hWaveOut)
	p.hWaveOut = 0
	return nil
}
