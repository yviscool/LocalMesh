package screen

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

var (
	ErrInvalidEventType      = errors.New("invalid input event type")
	ErrCoordinatesOutOfRange = errors.New("input coordinates out of range")
	ErrInvalidMouseButton    = errors.New("invalid mouse button")
	ErrKeyNotSpecified       = errors.New("keycode or text must be specified for key events")
	ErrInvalidEventData      = errors.New("insufficient data for input event")
)

// InputEventType indicates the kind of input action.
type InputEventType uint8

const (
	EventMouseMove  InputEventType = 1
	EventMouseDown  InputEventType = 2
	EventMouseUp    InputEventType = 3
	EventMouseWheel InputEventType = 4
	EventKeyDown    InputEventType = 5
	EventKeyUp      InputEventType = 6
)

func (t InputEventType) String() string {
	switch t {
	case EventMouseMove:
		return "MouseMove"
	case EventMouseDown:
		return "MouseDown"
	case EventMouseUp:
		return "MouseUp"
	case EventMouseWheel:
		return "MouseWheel"
	case EventKeyDown:
		return "KeyDown"
	case EventKeyUp:
		return "KeyUp"
	default:
		return fmt.Sprintf("Unknown(%d)", t)
	}
}

// MouseButton identifies the mouse button in click events.
type MouseButton uint8

const (
	ButtonNone   MouseButton = 0
	ButtonLeft   MouseButton = 1
	ButtonRight  MouseButton = 2
	ButtonMiddle MouseButton = 3
)

func (b MouseButton) String() string {
	switch b {
	case ButtonNone:
		return "None"
	case ButtonLeft:
		return "Left"
	case ButtonRight:
		return "Right"
	case ButtonMiddle:
		return "Middle"
	default:
		return fmt.Sprintf("Button(%d)", b)
	}
}

// InputModifiers represents bitflags of modifier keys.
type InputModifiers uint16

const (
	ModShift InputModifiers = 1 << 0
	ModCtrl  InputModifiers = 1 << 1
	ModAlt   InputModifiers = 1 << 2
	ModMeta  InputModifiers = 1 << 3
)

// InputEvent represents an injected mouse or keyboard action.
type InputEvent struct {
	Type       InputEventType `json:"type"`
	X          float64        `json:"x"`
	Y          float64        `json:"y"`
	Button     MouseButton    `json:"button"`
	WheelDelta int32          `json:"wheel_delta"`
	KeyCode    uint32         `json:"key_code"`
	Modifiers  InputModifiers `json:"modifiers"`
	Text       string         `json:"text,omitempty"`
	Normalized bool           `json:"normalized"`
}

// Validate checks event integrity and bounds.
func (e *InputEvent) Validate() error {
	switch e.Type {
	case EventMouseMove:
		return e.validateCoordinates()
	case EventMouseDown, EventMouseUp:
		if e.Button != ButtonLeft && e.Button != ButtonRight && e.Button != ButtonMiddle {
			return ErrInvalidMouseButton
		}
		return e.validateCoordinates()
	case EventMouseWheel:
		return nil
	case EventKeyDown, EventKeyUp:
		if e.KeyCode == 0 && len(e.Text) == 0 {
			return ErrKeyNotSpecified
		}
		return nil
	default:
		return ErrInvalidEventType
	}
}

func (e *InputEvent) validateCoordinates() error {
	if e.Normalized {
		if e.X < 0.0 || e.X > 1.0 || e.Y < 0.0 || e.Y > 1.0 {
			return ErrCoordinatesOutOfRange
		}
	} else {
		if e.X < 0.0 || e.Y < 0.0 || e.X > float64(MaxDimension) || e.Y > float64(MaxDimension) {
			return ErrCoordinatesOutOfRange
		}
	}
	return nil
}

// MarshalBinary serializes InputEvent to binary for low-latency UDP/QUIC streams.
// Binary format:
// [0] Type (1 byte)
// [1] Flags (bit 0: Normalized) (1 byte)
// [2] Button (1 byte)
// [3] Reserved (1 byte)
// [4:12] X (float64)
// [12:20] Y (float64)
// [20:24] WheelDelta (int32)
// [24:28] KeyCode (uint32)
// [28:30] Modifiers (uint16)
// [30:32] TextLen (uint16)
// [32:32+TextLen] Text bytes
func (e *InputEvent) MarshalBinary() ([]byte, error) {
	textBytes := []byte(e.Text)
	textLen := len(textBytes)
	buf := make([]byte, 32+textLen)

	buf[0] = byte(e.Type)
	var flags byte
	if e.Normalized {
		flags |= 1
	}
	buf[1] = flags
	buf[2] = byte(e.Button)
	buf[3] = 0 // reserved

	binary.BigEndian.PutUint64(buf[4:12], math.Float64bits(e.X))
	binary.BigEndian.PutUint64(buf[12:20], math.Float64bits(e.Y))
	binary.BigEndian.PutUint32(buf[20:24], uint32(e.WheelDelta))
	binary.BigEndian.PutUint32(buf[24:28], e.KeyCode)
	binary.BigEndian.PutUint16(buf[28:30], uint16(e.Modifiers))
	binary.BigEndian.PutUint16(buf[30:32], uint16(textLen))
	if textLen > 0 {
		copy(buf[32:], textBytes)
	}

	return buf, nil
}

// UnmarshalBinary parses binary stream bytes into InputEvent.
func (e *InputEvent) UnmarshalBinary(data []byte) error {
	if len(data) < 32 {
		return ErrInvalidEventData
	}

	e.Type = InputEventType(data[0])
	e.Normalized = (data[1] & 1) != 0
	e.Button = MouseButton(data[2])

	e.X = math.Float64frombits(binary.BigEndian.Uint64(data[4:12]))
	e.Y = math.Float64frombits(binary.BigEndian.Uint64(data[12:20]))
	e.WheelDelta = int32(binary.BigEndian.Uint32(data[20:24]))
	e.KeyCode = binary.BigEndian.Uint32(data[24:28])
	e.Modifiers = InputModifiers(binary.BigEndian.Uint16(data[28:30]))

	textLen := int(binary.BigEndian.Uint16(data[30:32]))
	if len(data) < 32+textLen {
		return ErrInvalidEventData
	}
	if textLen > 0 {
		e.Text = string(data[32 : 32+textLen])
	} else {
		e.Text = ""
	}

	return e.Validate()
}

// Injector defines the interface for injecting remote input events on the student workstation.
type Injector interface {
	Inject(event InputEvent) error
}
