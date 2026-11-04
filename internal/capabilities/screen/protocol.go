package screen

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

// Magic bytes identifying LocalMesh Screen Frame: "LMSF"
var FrameMagic = [4]byte{'L', 'M', 'S', 'F'}

const (
	CurrentProtocolVersion uint8 = 1
	HeaderSize                   = 36 // fixed 36-byte binary header
	MaxDimension                 = 16384
	MaxPayloadSize               = 64 * 1024 * 1024 // 64 MB
)

// FrameFormat identifies the pixel/compression format of a screen frame.
type FrameFormat uint8

const (
	FormatRawRGB  FrameFormat = 1
	FormatRawRGBA FrameFormat = 2
	FormatJPEG    FrameFormat = 3
	FormatPNG     FrameFormat = 4
	// FormatH264 reserves the payload format for the H.264 path
	// (Annex-B NALUs; see docs/H264-EVALUATION.md).
	FormatH264 FrameFormat = 5
)

func (f FrameFormat) String() string {
	switch f {
	case FormatRawRGB:
		return "RGB"
	case FormatRawRGBA:
		return "RGBA"
	case FormatJPEG:
		return "JPEG"
	case FormatPNG:
		return "PNG"
	case FormatH264:
		return "H264"
	default:
		return fmt.Sprintf("Unknown(%d)", f)
	}
}

// Frame flags
const (
	FlagKeyFrame uint16 = 1 << 0
)

var (
	ErrInvalidMagic      = errors.New("invalid frame magic header")
	ErrInvalidHeaderSize = errors.New("frame data too small for header")
	ErrPayloadMismatch   = errors.New("frame payload size does not match header")
	ErrInvalidDimensions = errors.New("invalid frame dimensions")
	ErrPayloadTooLarge   = errors.New("frame payload exceeds maximum size")
	ErrUnsupportedFormat = errors.New("unsupported frame format")
)

// FrameHeader represents the binary header prefixing every streaming screen frame.
// Layout (36 bytes, Big Endian):
//   - Magic:       4 bytes  ("LMSF")
//   - Version:     1 byte
//   - Format:      1 byte
//   - Flags:       2 bytes
//   - Sequence:    8 bytes
//   - Timestamp:   8 bytes  (Unix nanoseconds)
//   - Width:       4 bytes
//   - Height:      4 bytes
//   - PayloadSize: 4 bytes
type FrameHeader struct {
	Magic       [4]byte
	Version     uint8
	Format      FrameFormat
	Flags       uint16
	Sequence    uint64
	Timestamp   int64
	Width       uint32
	Height      uint32
	PayloadSize uint32
}

// NewFrameHeader constructs a FrameHeader with default magic, version, and timestamp.
func NewFrameHeader(format FrameFormat, width, height uint32, payloadSize uint32, seq uint64) (FrameHeader, error) {
	if width == 0 || width > MaxDimension || height == 0 || height > MaxDimension {
		return FrameHeader{}, ErrInvalidDimensions
	}
	if payloadSize > MaxPayloadSize {
		return FrameHeader{}, ErrPayloadTooLarge
	}

	return FrameHeader{
		Magic:       FrameMagic,
		Version:     CurrentProtocolVersion,
		Format:      format,
		Flags:       0,
		Sequence:    seq,
		Timestamp:   time.Now().UnixNano(),
		Width:       width,
		Height:      height,
		PayloadSize: payloadSize,
	}, nil
}

// MarshalBinary encodes the FrameHeader into a 36-byte slice.
func (h *FrameHeader) MarshalBinary() ([]byte, error) {
	buf := make([]byte, HeaderSize)
	copy(buf[0:4], h.Magic[:])
	buf[4] = h.Version
	buf[5] = byte(h.Format)
	binary.BigEndian.PutUint16(buf[6:8], h.Flags)
	binary.BigEndian.PutUint64(buf[8:16], h.Sequence)
	binary.BigEndian.PutUint64(buf[16:24], uint64(h.Timestamp))
	binary.BigEndian.PutUint32(buf[24:28], h.Width)
	binary.BigEndian.PutUint32(buf[28:32], h.Height)
	binary.BigEndian.PutUint32(buf[32:36], h.PayloadSize)
	return buf, nil
}

// UnmarshalBinary decodes a 36-byte slice into the FrameHeader and validates fields.
func (h *FrameHeader) UnmarshalBinary(data []byte) error {
	if len(data) < HeaderSize {
		return ErrInvalidHeaderSize
	}

	copy(h.Magic[:], data[0:4])
	if h.Magic != FrameMagic {
		return ErrInvalidMagic
	}

	h.Version = data[4]
	h.Format = FrameFormat(data[5])
	h.Flags = binary.BigEndian.Uint16(data[6:8])
	h.Sequence = binary.BigEndian.Uint64(data[8:16])
	h.Timestamp = int64(binary.BigEndian.Uint64(data[16:24]))
	h.Width = binary.BigEndian.Uint32(data[24:28])
	h.Height = binary.BigEndian.Uint32(data[28:32])
	h.PayloadSize = binary.BigEndian.Uint32(data[32:36])

	if h.Width == 0 || h.Width > MaxDimension || h.Height == 0 || h.Height > MaxDimension {
		return ErrInvalidDimensions
	}
	if h.PayloadSize > MaxPayloadSize {
		return ErrPayloadTooLarge
	}
	return nil
}

// EncodeFrame serializes a FrameHeader and its payload into a single contiguous byte buffer.
func EncodeFrame(header FrameHeader, payload []byte) ([]byte, error) {
	if uint32(len(payload)) != header.PayloadSize {
		return nil, ErrPayloadMismatch
	}

	headerBytes, err := header.MarshalBinary()
	if err != nil {
		return nil, err
	}

	buf := make([]byte, len(headerBytes)+len(payload))
	copy(buf[:len(headerBytes)], headerBytes)
	copy(buf[len(headerBytes):], payload)
	return buf, nil
}

// DecodeFrame deserializes a buffer into a FrameHeader and returns the payload slice.
func DecodeFrame(data []byte) (*FrameHeader, []byte, error) {
	if len(data) < HeaderSize {
		return nil, nil, ErrInvalidHeaderSize
	}

	var header FrameHeader
	if err := header.UnmarshalBinary(data[:HeaderSize]); err != nil {
		return nil, nil, err
	}

	payload := data[HeaderSize:]
	if uint32(len(payload)) != header.PayloadSize {
		return nil, nil, ErrPayloadMismatch
	}

	return &header, payload, nil
}
