package audio

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"net"
	"time"
)

var (
	// ErrInvalidFormat indicates an unsupported or malformed audio format specification.
	ErrInvalidFormat = errors.New("audio: invalid audio format")
	// ErrPacketCorrupt indicates packet header mismatch or CRC32 checksum mismatch.
	ErrPacketCorrupt = errors.New("audio: packet corrupted or checksum mismatch")
	// ErrPacketTooShort indicates raw packet bytes are shorter than the fixed header length.
	ErrPacketTooShort = errors.New("audio: packet too short")
	// ErrSenderClosed indicates an operation was performed on a closed sender.
	ErrSenderClosed = errors.New("audio: sender is closed")
	// ErrReceiverClosed indicates an operation was performed on a closed receiver.
	ErrReceiverClosed = errors.New("audio: receiver is closed")
	// ErrBufferFull indicates the buffer queue has exceeded maximum capacity.
	ErrBufferFull = errors.New("audio: buffer queue full")
	// ErrDeviceNotFound indicates no available audio input or output device was found.
	ErrDeviceNotFound = errors.New("audio: audio device not found")
)

// AudioPacketMagic is the 4-byte header identifier for LocalMesh audio packets ("LMAU").
var AudioPacketMagic = [4]byte{'L', 'M', 'A', 'U'}

// AudioPacketHeaderSize is the fixed size in bytes of an AudioPacket header.
const AudioPacketHeaderSize = 32

// Packet Flags
const (
	FlagNone          uint8 = 0
	FlagSilence       uint8 = 1 << 0
	FlagEndOfStream   uint8 = 1 << 1
	FlagDiscontinuity uint8 = 1 << 2
)

// AudioFormat represents PCM audio stream specifications.
type AudioFormat struct {
	SampleRate    int // Samples per second, e.g. 48000, 44100, 16000
	Channels      int // Number of channels: 1 (mono), 2 (stereo)
	BitsPerSample int // Bits per sample: 16 (int16 PCM)
}

// DefaultAudioFormat returns the standard high-fidelity audio format (48kHz, 16-bit, stereo).
var DefaultAudioFormat = AudioFormat{
	SampleRate:    48000,
	Channels:      2,
	BitsPerSample: 16,
}

// Validate checks if the format parameters are standard and supported.
func (f AudioFormat) Validate() error {
	if f.SampleRate <= 0 || f.SampleRate > 192000 {
		return fmt.Errorf("%w: invalid sample rate %d", ErrInvalidFormat, f.SampleRate)
	}
	if f.Channels <= 0 || f.Channels > 8 {
		return fmt.Errorf("%w: invalid channel count %d", ErrInvalidFormat, f.Channels)
	}
	if f.BitsPerSample != 8 && f.BitsPerSample != 16 && f.BitsPerSample != 24 && f.BitsPerSample != 32 {
		return fmt.Errorf("%w: invalid bits per sample %d", ErrInvalidFormat, f.BitsPerSample)
	}
	return nil
}

// BytesPerSample returns the number of bytes per individual channel sample (e.g. 2 for 16-bit).
func (f AudioFormat) BytesPerSample() int {
	return f.BitsPerSample / 8
}

// FrameSize returns the byte size of one complete audio sample across all channels.
func (f AudioFormat) FrameSize() int {
	return f.Channels * f.BytesPerSample()
}

// BytesPerSecond returns the raw data throughput in bytes per second.
func (f AudioFormat) BytesPerSecond() int {
	return f.SampleRate * f.FrameSize()
}

// DurationToBytes calculates the byte count corresponding to a given duration.
func (f AudioFormat) DurationToBytes(d time.Duration) int {
	samples := int(float64(f.SampleRate) * d.Seconds())
	return samples * f.FrameSize()
}

// BytesToDuration calculates the playback duration for a given byte count.
func (f AudioFormat) BytesToDuration(bytes int) time.Duration {
	frameSize := f.FrameSize()
	if frameSize == 0 || f.SampleRate == 0 {
		return 0
	}
	samples := bytes / frameSize
	return time.Duration(float64(samples) / float64(f.SampleRate) * float64(time.Second))
}

func (f AudioFormat) String() string {
	return fmt.Sprintf("%dHz %d-bit %dch", f.SampleRate, f.BitsPerSample, f.Channels)
}

// AudioPacket represents a transmitted audio network frame.
type AudioPacket struct {
	Magic         [4]byte
	Version       uint8
	Flags         uint8
	Channels      uint8
	BitsPerSample uint8
	SampleRate    uint32
	Sequence      uint32
	Timestamp     uint64 // Presentation timestamp in microseconds or sample offset
	PayloadLen    uint32
	Checksum      uint32 // IEEE CRC32 of payload data
	Data          []byte
}

// NewAudioPacket constructs an AudioPacket with correct headers and payload checksum.
func NewAudioPacket(seq uint32, ts uint64, fmt AudioFormat, flags uint8, data []byte) *AudioPacket {
	var csum uint32
	if len(data) > 0 {
		csum = crc32.ChecksumIEEE(data)
	}
	return &AudioPacket{
		Magic:         AudioPacketMagic,
		Version:       1,
		Flags:         flags,
		Channels:      uint8(fmt.Channels),
		BitsPerSample: uint8(fmt.BitsPerSample),
		SampleRate:    uint32(fmt.SampleRate),
		Sequence:      seq,
		Timestamp:     ts,
		PayloadLen:    uint32(len(data)),
		Checksum:      csum,
		Data:          data,
	}
}

// MarshalBinary serializes the AudioPacket into network byte order binary representation.
func (p *AudioPacket) MarshalBinary() ([]byte, error) {
	buf := make([]byte, AudioPacketHeaderSize+len(p.Data))
	copy(buf[0:4], p.Magic[:])
	buf[4] = p.Version
	buf[5] = p.Flags
	buf[6] = p.Channels
	buf[7] = p.BitsPerSample
	binary.BigEndian.PutUint32(buf[8:12], p.SampleRate)
	binary.BigEndian.PutUint32(buf[12:16], p.Sequence)
	binary.BigEndian.PutUint64(buf[16:24], p.Timestamp)
	binary.BigEndian.PutUint32(buf[24:28], uint32(len(p.Data)))
	binary.BigEndian.PutUint32(buf[28:32], p.Checksum)
	if len(p.Data) > 0 {
		copy(buf[AudioPacketHeaderSize:], p.Data)
	}
	return buf, nil
}

// UnmarshalBinary decodes raw bytes into AudioPacket, validating the magic and CRC32 checksum.
func (p *AudioPacket) UnmarshalBinary(buf []byte) error {
	if len(buf) < AudioPacketHeaderSize {
		return ErrPacketTooShort
	}
	copy(p.Magic[:], buf[0:4])
	if p.Magic != AudioPacketMagic {
		return fmt.Errorf("%w: invalid magic %v", ErrPacketCorrupt, p.Magic)
	}
	p.Version = buf[4]
	p.Flags = buf[5]
	p.Channels = buf[6]
	p.BitsPerSample = buf[7]
	p.SampleRate = binary.BigEndian.Uint32(buf[8:12])
	p.Sequence = binary.BigEndian.Uint32(buf[12:16])
	p.Timestamp = binary.BigEndian.Uint64(buf[16:24])
	p.PayloadLen = binary.BigEndian.Uint32(buf[24:28])
	p.Checksum = binary.BigEndian.Uint32(buf[28:32])

	expectedLen := int(p.PayloadLen)
	actualPayload := buf[AudioPacketHeaderSize:]
	if len(actualPayload) < expectedLen {
		return fmt.Errorf("%w: payload truncated (expected %d, got %d)", ErrPacketCorrupt, expectedLen, len(actualPayload))
	}
	p.Data = make([]byte, expectedLen)
	copy(p.Data, actualPayload[:expectedLen])

	if expectedLen > 0 {
		calcCsum := crc32.ChecksumIEEE(p.Data)
		if calcCsum != p.Checksum {
			return fmt.Errorf("%w: checksum mismatch (calc %08x != pkt %08x)", ErrPacketCorrupt, calcCsum, p.Checksum)
		}
	}
	return nil
}

// Format returns the AudioFormat described by this packet's headers.
func (p *AudioPacket) Format() AudioFormat {
	return AudioFormat{
		SampleRate:    int(p.SampleRate),
		Channels:      int(p.Channels),
		BitsPerSample: int(p.BitsPerSample),
	}
}

// AudioHandler is a callback invoked when audio data is captured or ready to play.
type AudioHandler func(pcm []byte)

// Capturer captures system or microphone audio stream.
type Capturer interface {
	// Start begins capturing audio and delivers raw PCM chunks to the handler.
	Start(ctx context.Context, handler AudioHandler) error
	// Stop halts audio capture and releases resources.
	Stop() error
	// Format returns the negotiated audio format.
	Format() AudioFormat
}

// Player plays audio stream through speakers or sound devices.
type Player interface {
	// Start initializes the playback pipeline.
	Start(ctx context.Context) error
	// Write feeds PCM audio samples into the player buffer.
	Write(pcm []byte) (int, error)
	// Stop pauses/halts playback and flushes buffers.
	Stop() error
	// Format returns the player's audio format.
	Format() AudioFormat
	// BufferLatency returns the estimated latency of buffered, unplayed audio.
	BufferLatency() time.Duration
}

// SenderConfig configures an audio streaming sender.
type SenderConfig struct {
	TargetAddr    string         // Destination address, e.g. "239.255.42.99:8900" or "127.0.0.1:8900"
	Conn          net.PacketConn // Injected PacketConn for testing or virtual network
	Format        AudioFormat    // Audio format specification
	FrameDuration time.Duration  // Duration per audio packet (e.g. 10ms or 20ms, default: 10ms)
	MaxPacketSize int            // Maximum UDP packet size (default: 1400)
}

// ReceiverConfig configures an audio streaming receiver.
type ReceiverConfig struct {
	ListenAddr    string         // Listen address, e.g. "239.255.42.99:8900" or ":8900"
	Conn          net.PacketConn // Injected PacketConn for testing or virtual network
	Format        AudioFormat    // Expected audio format
	JitterDelay   time.Duration  // Playout buffer delay (e.g. 20ms-50ms, default: 30ms)
	MaxJitterSize int            // Max packets to hold in jitter buffer (default: 64)
}
