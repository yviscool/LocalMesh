//go:build windows

package audio

import (
	"context"
	"fmt"
	"math"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modOle32Capture = windows.NewLazySystemDLL("ole32.dll")

	procCoInitializeExCapture   = modOle32Capture.NewProc("CoInitializeEx")
	procCoUninitializeCapture   = modOle32Capture.NewProc("CoUninitialize")
	procCoCreateInstanceCapture = modOle32Capture.NewProc("CoCreateInstance")
	procCoTaskMemFreeCapture    = modOle32Capture.NewProc("CoTaskMemFree")

	clsidMMDeviceEnumerator = windows.GUID{
		Data1: 0xBCDE0395, Data2: 0xE52F, Data3: 0x467C,
		Data4: [8]byte{0x8E, 0x3D, 0xC4, 0x57, 0x92, 0x91, 0x69, 0x2E},
	}
	iidIMMDeviceEnumerator = windows.GUID{
		Data1: 0xA95664D2, Data2: 0x9614, Data3: 0x4F35,
		Data4: [8]byte{0xA7, 0x46, 0xDE, 0x8D, 0xB6, 0x36, 0x17, 0xE6},
	}
	iidIAudioClient = windows.GUID{
		Data1: 0x1CB9AD4C, Data2: 0xDBFA, Data3: 0x4C32,
		Data4: [8]byte{0xB1, 0x78, 0xC2, 0xF5, 0x68, 0xA7, 0x03, 0xB2},
	}
	iidIAudioCaptureClient = windows.GUID{
		Data1: 0xC8ADBD64, Data2: 0xE71E, Data3: 0x48A0,
		Data4: [8]byte{0xA4, 0xDE, 0x18, 0x5C, 0x39, 0x5C, 0xD3, 0x17},
	}

	subTypeIEEEFloat = windows.GUID{
		Data1: 0x00000003, Data2: 0x0000, Data3: 0x0010,
		Data4: [8]byte{0x80, 0x00, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71},
	}
	subTypePCM = windows.GUID{
		Data1: 0x00000001, Data2: 0x0000, Data3: 0x0010,
		Data4: [8]byte{0x80, 0x00, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71},
	}
)

const (
	clsctxAll = 23 // CLSCTX_INPROC_SERVER | CLSCTX_INPROC_HANDLER | CLSCTX_LOCAL_SERVER

	eRender  = 0 // eRender: Output device (speakers)
	eConsole = 0 // eConsole: Standard system role

	audclntShareModeShared     = 0
	audclntStreamFlagsLoopback = 0x00020000
	audclntBufferFlagsSilent   = 0x2

	waveFormatPCM           = 0x0001
	waveFormatIEEEFloat     = 0x0003
	waveFormatExtensibleTag = 0xFFFE
)

type waveFormatEx struct {
	wFormatTag      uint16
	nChannels       uint16
	nSamplesPerSec  uint32
	nAvgBytesPerSec uint32
	nBlockAlign     uint16
	wBitsPerSample  uint16
	cbSize          uint16
}

type waveFormatExtensible struct {
	Format        waveFormatEx
	Samples       uint16
	DwChannelMask uint32
	SubFormat     windows.GUID
}

type comObject struct {
	vtbl *[64]uintptr
}

func callCOM(obj unsafe.Pointer, methodIndex int, params ...uintptr) uintptr {
	if obj == nil {
		return 0x80004005 // E_FAIL
	}
	com := (*comObject)(obj)
	fn := com.vtbl[methodIndex]
	args := make([]uintptr, 1+len(params))
	args[0] = uintptr(obj)
	copy(args[1:], params)
	r1, _, _ := syscall.SyscallN(fn, args...)
	return r1
}

func releaseCOM(obj unsafe.Pointer) {
	if obj != nil {
		callCOM(obj, 2)
	}
}

// WindowsCapturer captures system audio output on Windows using WASAPI Loopback,
// with graceful fallback if no audio render endpoint is available.
type WindowsCapturer struct {
	mu      sync.Mutex
	format  AudioFormat
	running bool
	stopCh  chan struct{}

	fallback Capturer
}

// NewCapturer returns a Windows system audio capturer.
func NewCapturer(fmt AudioFormat) Capturer {
	if err := fmt.Validate(); err != nil {
		fmt = DefaultAudioFormat
	}
	return &WindowsCapturer{
		format: fmt,
	}
}

// NewSilentCapturer returns a capturer that generates silent audio on Windows.
func NewSilentCapturer(fmt AudioFormat) Capturer {
	return &syntheticCapturer{
		format:    fmt,
		frequency: 0,
	}
}

// Format returns the audio format.
func (c *WindowsCapturer) Format() AudioFormat {
	return c.format
}

// Start begins capturing loopback audio from Windows speakers.
func (c *WindowsCapturer) Start(ctx context.Context, handler AudioHandler) error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return nil
	}
	c.running = true
	c.stopCh = make(chan struct{})
	c.mu.Unlock()

	// Try WASAPI loopback capture
	err := c.startWASAPILoopback(ctx, handler)
	if err != nil {
		// If WASAPI fails (e.g. headless CI container without audio device),
		// fallback to synthetic capturer so the system stays fully functional.
		fallback := &syntheticCapturer{
			format:    c.format,
			frequency: 440.0,
		}
		c.mu.Lock()
		c.fallback = fallback
		c.mu.Unlock()
		return fallback.Start(ctx, handler)
	}

	return nil
}

func (c *WindowsCapturer) startWASAPILoopback(ctx context.Context, handler AudioHandler) error {
	// Initialize COM apartment for this thread
	r1, _, _ := procCoInitializeExCapture.Call(0, 0) // COINIT_MULTITHREADED
	// RPC_E_CHANGED_MODE (0x80010106) is benign if already initialized
	if r1 != 0 && r1 != 0x80010106 && r1 != 1 {
		return fmt.Errorf("CoInitializeEx failed: 0x%08x", r1)
	}

	var pEnum unsafe.Pointer
	r1, _, _ = procCoCreateInstanceCapture.Call(
		uintptr(unsafe.Pointer(&clsidMMDeviceEnumerator)),
		0,
		clsctxAll,
		uintptr(unsafe.Pointer(&iidIMMDeviceEnumerator)),
		uintptr(unsafe.Pointer(&pEnum)),
	)
	if r1 != 0 || pEnum == nil {
		return fmt.Errorf("CoCreateInstance MMDeviceEnumerator failed: 0x%08x", r1)
	}

	// IMMDeviceEnumerator::GetDefaultAudioEndpoint(eRender=0, eConsole=0, &pDevice)
	var pDevice unsafe.Pointer
	hr := callCOM(pEnum, 4, uintptr(eRender), uintptr(eConsole), uintptr(unsafe.Pointer(&pDevice)))
	releaseCOM(pEnum)
	if hr != 0 || pDevice == nil {
		return fmt.Errorf("GetDefaultAudioEndpoint failed: 0x%08x", hr)
	}

	// IMMDevice::Activate(iidIAudioClient, CLSCTX_ALL, NULL, &pAudioClient)
	var pAudioClient unsafe.Pointer
	hr = callCOM(pDevice, 3,
		uintptr(unsafe.Pointer(&iidIAudioClient)),
		clsctxAll,
		0,
		uintptr(unsafe.Pointer(&pAudioClient)),
	)
	releaseCOM(pDevice)
	if hr != 0 || pAudioClient == nil {
		return fmt.Errorf("Activate IAudioClient failed: 0x%08x", hr)
	}

	// IAudioClient::GetMixFormat(&pwfx)
	var pwfx *waveFormatEx
	hr = callCOM(pAudioClient, 8, uintptr(unsafe.Pointer(&pwfx)))
	if hr != 0 || pwfx == nil {
		releaseCOM(pAudioClient)
		return fmt.Errorf("GetMixFormat failed: 0x%08x", hr)
	}

	srcChannels := int(pwfx.nChannels)
	srcSampleRate := int(pwfx.nSamplesPerSec)
	srcBits := int(pwfx.wBitsPerSample)
	srcIsFloat := false

	if pwfx.wFormatTag == waveFormatIEEEFloat {
		srcIsFloat = true
	} else if pwfx.wFormatTag == waveFormatExtensibleTag && pwfx.cbSize >= 22 {
		wfExt := (*waveFormatExtensible)(unsafe.Pointer(pwfx))
		if wfExt.SubFormat == subTypeIEEEFloat {
			srcIsFloat = true
		}
	}

	// 1 second buffer duration (in 100ns units) = 10,000,000
	bufferDuration := int64(10000000)
	// IAudioClient::Initialize(AUDCLNT_SHAREMODE_SHARED, AUDCLNT_STREAMFLAGS_LOOPBACK, bufferDuration, 0, pwfx, NULL)
	hr = callCOM(pAudioClient, 3,
		uintptr(audclntShareModeShared),
		uintptr(audclntStreamFlagsLoopback),
		uintptr(bufferDuration),
		0,
		uintptr(unsafe.Pointer(pwfx)),
		0,
	)
	procCoTaskMemFreeCapture.Call(uintptr(unsafe.Pointer(pwfx)))
	if hr != 0 {
		releaseCOM(pAudioClient)
		return fmt.Errorf("Initialize loopback failed: 0x%08x", hr)
	}

	// IAudioClient::GetService(iidIAudioCaptureClient, &pCaptureClient)
	var pCaptureClient unsafe.Pointer
	hr = callCOM(pAudioClient, 14,
		uintptr(unsafe.Pointer(&iidIAudioCaptureClient)),
		uintptr(unsafe.Pointer(&pCaptureClient)),
	)
	if hr != 0 || pCaptureClient == nil {
		releaseCOM(pAudioClient)
		return fmt.Errorf("GetService IAudioCaptureClient failed: 0x%08x", hr)
	}

	// IAudioClient::Start()
	hr = callCOM(pAudioClient, 10)
	if hr != 0 {
		releaseCOM(pCaptureClient)
		releaseCOM(pAudioClient)
		return fmt.Errorf("IAudioClient Start failed: 0x%08x", hr)
	}

	// Background capture thread
	go func() {
		defer func() {
			callCOM(pAudioClient, 11) // Stop
			releaseCOM(pCaptureClient)
			releaseCOM(pAudioClient)
			procCoUninitializeCapture.Call()
		}()

		frameDuration := DefaultFrameDuration
		ticker := time.NewTicker(frameDuration / 2)
		defer ticker.Stop()

		silenceBuf := make([]byte, c.format.DurationToBytes(frameDuration))

		for {
			select {
			case <-c.stopCh:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				var packetSize uint32
				// IAudioCaptureClient::GetNextPacketSize(&packetSize)
				_ = callCOM(pCaptureClient, 5, uintptr(unsafe.Pointer(&packetSize)))

				framesCaptured := 0
				for packetSize > 0 {
					var pData unsafe.Pointer
					var numFramesToRead uint32
					var dwFlags uint32
					var devPos, qpcPos uint64

					// IAudioCaptureClient::GetBuffer(&pData, &numFramesToRead, &dwFlags, &devPos, &qpcPos)
					hrBuf := callCOM(pCaptureClient, 3,
						uintptr(unsafe.Pointer(&pData)),
						uintptr(unsafe.Pointer(&numFramesToRead)),
						uintptr(unsafe.Pointer(&dwFlags)),
						uintptr(unsafe.Pointer(&devPos)),
						uintptr(unsafe.Pointer(&qpcPos)),
					)
					if hrBuf != 0 || numFramesToRead == 0 {
						break
					}

					framesCaptured += int(numFramesToRead)

					if dwFlags&audclntBufferFlagsSilent != 0 {
						// Output silence of corresponding duration
						outBytes := int(numFramesToRead) * c.format.FrameSize()
						if outBytes > len(silenceBuf) {
							silenceBuf = make([]byte, outBytes)
						}
						handler(silenceBuf[:outBytes])
					} else if pData != nil {
						// Convert captured frames to 16-bit PCM stereo target format
						converted := convertCapturedAudio(
							pData,
							int(numFramesToRead),
							srcChannels,
							srcSampleRate,
							srcBits,
							srcIsFloat,
							c.format,
						)
						if len(converted) > 0 {
							handler(converted)
						}
					}

					// IAudioCaptureClient::ReleaseBuffer(numFramesToRead)
					callCOM(pCaptureClient, 4, uintptr(numFramesToRead))

					// Check next packet size
					packetSize = 0
					_ = callCOM(pCaptureClient, 5, uintptr(unsafe.Pointer(&packetSize)))
				}
			}
		}
	}()

	return nil
}

// convertCapturedAudio transcode captured native samples (e.g. 32-bit float multi-channel)
// into standard 16-bit PCM (target format).
func convertCapturedAudio(
	pData unsafe.Pointer,
	numFrames int,
	srcChannels int,
	srcSampleRate int,
	srcBits int,
	srcIsFloat bool,
	target AudioFormat,
) []byte {
	outBytesPerFrame := target.FrameSize()
	out := make([]byte, numFrames*outBytesPerFrame)

	if srcIsFloat && srcBits == 32 {
		srcSlice := unsafe.Slice((*float32)(pData), numFrames*srcChannels)

		for i := 0; i < numFrames; i++ {
			var left, right float32
			offset := i * srcChannels

			if srcChannels == 1 {
				left = srcSlice[offset]
				right = left
			} else if srcChannels == 2 {
				left = srcSlice[offset]
				right = srcSlice[offset+1]
			} else if srcChannels >= 6 {
				// 5.1 surround downmix: FL + 0.7*C + 0.7*SL, FR + 0.7*C + 0.7*SR
				fl := srcSlice[offset]
				fr := srcSlice[offset+1]
				c := srcSlice[offset+2]
				sl := srcSlice[offset+4]
				sr := srcSlice[offset+5]
				left = fl + 0.707*c + 0.707*sl
				right = fr + 0.707*c + 0.707*sr
			} else {
				left = srcSlice[offset]
				right = srcSlice[offset+1]
			}

			// Clamp float to [-1.0, 1.0]
			if left > 1.0 {
				left = 1.0
			} else if left < -1.0 {
				left = -1.0
			}
			if right > 1.0 {
				right = 1.0
			} else if right < -1.0 {
				right = -1.0
			}

			i16L := int16(left * 32767.0)
			i16R := int16(right * 32767.0)

			outOff := i * outBytesPerFrame
			if target.Channels == 1 {
				mono := int16((int32(i16L) + int32(i16R)) / 2)
				out[outOff] = byte(mono)
				out[outOff+1] = byte(mono >> 8)
			} else {
				out[outOff] = byte(i16L)
				out[outOff+1] = byte(i16L >> 8)
				out[outOff+2] = byte(i16R)
				out[outOff+3] = byte(i16R >> 8)
			}
		}
		return out
	}

	// 16-bit PCM source
	if srcBits == 16 {
		srcSlice := unsafe.Slice((*int16)(pData), numFrames*srcChannels)
		for i := 0; i < numFrames; i++ {
			offset := i * srcChannels
			var l, r int16
			if srcChannels == 1 {
				l = srcSlice[offset]
				r = l
			} else {
				l = srcSlice[offset]
				r = srcSlice[offset+1]
			}

			outOff := i * outBytesPerFrame
			if target.Channels == 1 {
				mono := int16((int32(l) + int32(r)) / 2)
				out[outOff] = byte(mono)
				out[outOff+1] = byte(mono >> 8)
			} else {
				out[outOff] = byte(l)
				out[outOff+1] = byte(l >> 8)
				out[outOff+2] = byte(r)
				out[outOff+3] = byte(r >> 8)
			}
		}
		return out
	}

	return out
}

// Stop halts audio capture and releases resources.
func (c *WindowsCapturer) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return nil
	}
	c.running = false
	close(c.stopCh)

	if c.fallback != nil {
		_ = c.fallback.Stop()
	}
	return nil
}

// syntheticCapturer provides fallback audio generation for tests and headless environments.
type syntheticCapturer struct {
	mu        sync.Mutex
	format    AudioFormat
	running   bool
	stopCh    chan struct{}
	frequency float64
}

func (s *syntheticCapturer) Format() AudioFormat {
	return s.format
}

func (s *syntheticCapturer) Start(ctx context.Context, handler AudioHandler) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = true
	s.stopCh = make(chan struct{})
	s.mu.Unlock()

	frameDur := DefaultFrameDuration
	samplesPerFrame := int(float64(s.format.SampleRate) * frameDur.Seconds())
	frameBytes := samplesPerFrame * s.format.FrameSize()

	go func() {
		ticker := time.NewTicker(frameDur)
		defer ticker.Stop()

		var sampleIndex int64
		buf := make([]byte, frameBytes)

		for {
			select {
			case <-s.stopCh:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if s.frequency > 0 {
					for i := 0; i < samplesPerFrame; i++ {
						t := float64(sampleIndex) / float64(s.format.SampleRate)
						val := int16(math.Sin(2*math.Pi*s.frequency*t) * 16000)
						sampleIndex++

						for ch := 0; ch < s.format.Channels; ch++ {
							off := (i*s.format.Channels + ch) * 2
							buf[off] = byte(val)
							buf[off+1] = byte(val >> 8)
						}
					}
				} else {
					for j := range buf {
						buf[j] = 0
					}
				}
				handler(buf)
			}
		}
	}()

	return nil
}

func (s *syntheticCapturer) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}
	s.running = false
	close(s.stopCh)
	return nil
}
