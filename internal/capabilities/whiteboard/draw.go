package whiteboard

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalidToolType    = errors.New("invalid whiteboard tool type")
	ErrInvalidStrokeWidth = errors.New("stroke width must be positive")
	ErrInsufficientPoints = errors.New("insufficient points for specified tool")
	ErrPointOutOfBounds   = errors.New("point coordinates must be normalized within [0.0, 1.0]")
	ErrEmptyTextContent   = errors.New("text stroke must contain non-empty text content")
	ErrInvalidHexColor    = errors.New("invalid hex color string")
)

// Point represents a normalized 2D coordinate where (0.0, 0.0) is the top-left
// and (1.0, 1.0) is the bottom-right corner of the canvas.
// This enables resolution-independent cross-platform drawing across heterogeneous displays.
type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Validate verifies that coordinates are within normalized bounds [0.0, 1.0] and not NaN/Inf.
func (p Point) Validate() error {
	if math.IsNaN(p.X) || math.IsNaN(p.Y) || math.IsInf(p.X, 0) || math.IsInf(p.Y, 0) {
		return ErrPointOutOfBounds
	}
	if p.X < 0.0 || p.X > 1.0 || p.Y < 0.0 || p.Y > 1.0 {
		return fmt.Errorf("%w: (%.4f, %.4f)", ErrPointOutOfBounds, p.X, p.Y)
	}
	return nil
}

// Clamp clamps point coordinates into [0.0, 1.0].
func (p Point) Clamp() Point {
	x := p.X
	y := p.Y
	if math.IsNaN(x) || x < 0.0 {
		x = 0.0
	} else if x > 1.0 {
		x = 1.0
	}
	if math.IsNaN(y) || y < 0.0 {
		y = 0.0
	} else if y > 1.0 {
		y = 1.0
	}
	return Point{X: x, Y: y}
}

// ToolType denotes the drawing or command tool.
type ToolType string

const (
	ToolPen         ToolType = "pen"
	ToolHighlighter ToolType = "highlighter"
	ToolArrow       ToolType = "arrow"
	ToolRect        ToolType = "rect"
	ToolEllipse     ToolType = "ellipse"
	ToolText        ToolType = "text"
	ToolClear       ToolType = "clear"
	ToolUndo        ToolType = "undo"
)

// IsValid checks whether ToolType is a recognized drawing or command tool.
func (t ToolType) IsValid() bool {
	switch ToolType(strings.ToLower(string(t))) {
	case ToolPen, ToolHighlighter, ToolArrow, ToolRect, ToolEllipse, ToolText, ToolClear, ToolUndo:
		return true
	default:
		return false
	}
}

// Normalized returns the canonical lowercase ToolType.
func (t ToolType) Normalized() ToolType {
	return ToolType(strings.ToLower(string(t)))
}

// IsDrawingTool returns true for graphical stroke primitives.
func (t ToolType) IsDrawingTool() bool {
	switch t.Normalized() {
	case ToolPen, ToolHighlighter, ToolArrow, ToolRect, ToolEllipse, ToolText:
		return true
	default:
		return false
	}
}

// IsCommand returns true for canvas control actions (Clear, Undo).
func (t ToolType) IsCommand() bool {
	switch t.Normalized() {
	case ToolClear, ToolUndo:
		return true
	default:
		return false
	}
}

// Color represents an RGBA color with 8 bits per channel.
type Color struct {
	R uint8 `json:"r"`
	G uint8 `json:"g"`
	B uint8 `json:"b"`
	A uint8 `json:"a"`
}

// RGBA creates a Color with explicit alpha.
func RGBA(r, g, b, a uint8) Color {
	return Color{R: r, G: g, B: b, A: a}
}

// RGB creates an opaque Color (alpha = 255).
func RGB(r, g, b uint8) Color {
	return Color{R: r, G: g, B: b, A: 255}
}

// Hex parses a hex color string like "#RGB", "#RGBA", "#RRGGBB", or "#RRGGBBAA".
func Hex(hex string) (Color, error) {
	s := strings.TrimPrefix(hex, "#")
	switch len(s) {
	case 3: // RGB
		r, err1 := strconv.ParseUint(string(s[0])+string(s[0]), 16, 8)
		g, err2 := strconv.ParseUint(string(s[1])+string(s[1]), 16, 8)
		b, err3 := strconv.ParseUint(string(s[2])+string(s[2]), 16, 8)
		if err1 != nil || err2 != nil || err3 != nil {
			return Color{}, ErrInvalidHexColor
		}
		return Color{R: uint8(r), G: uint8(g), B: uint8(b), A: 255}, nil
	case 4: // RGBA
		r, err1 := strconv.ParseUint(string(s[0])+string(s[0]), 16, 8)
		g, err2 := strconv.ParseUint(string(s[1])+string(s[1]), 16, 8)
		b, err3 := strconv.ParseUint(string(s[2])+string(s[2]), 16, 8)
		a, err4 := strconv.ParseUint(string(s[3])+string(s[3]), 16, 8)
		if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
			return Color{}, ErrInvalidHexColor
		}
		return Color{R: uint8(r), G: uint8(g), B: uint8(b), A: uint8(a)}, nil
	case 6: // RRGGBB
		val, err := strconv.ParseUint(s, 16, 32)
		if err != nil {
			return Color{}, ErrInvalidHexColor
		}
		return Color{
			R: uint8(val >> 16),
			G: uint8(val >> 8),
			B: uint8(val),
			A: 255,
		}, nil
	case 8: // RRGGBBAA
		val, err := strconv.ParseUint(s, 16, 32)
		if err != nil {
			return Color{}, ErrInvalidHexColor
		}
		return Color{
			R: uint8(val >> 24),
			G: uint8(val >> 16),
			B: uint8(val >> 8),
			A: uint8(val),
		}, nil
	default:
		return Color{}, ErrInvalidHexColor
	}
}

// HexString returns the CSS hex representation of the color.
func (c Color) HexString() string {
	if c.A == 255 {
		return fmt.Sprintf("#%02X%02X%02X", c.R, c.G, c.B)
	}
	return fmt.Sprintf("#%02X%02X%02X%02X", c.R, c.G, c.B, c.A)
}

// Stroke represents a vector drawing command or path on the interactive whiteboard.
type Stroke struct {
	ID          string    `json:"id,omitempty"`
	Tool        ToolType  `json:"tool"`
	Color       Color     `json:"color"`
	Width       float64   `json:"width"`
	Points      []Point   `json:"points"`
	TextContent string    `json:"text_content,omitempty"`
	CreatedAt   time.Time `json:"created_at,omitempty"`
}

// Validate ensures all stroke properties are syntactically and semantically valid.
func (s *Stroke) Validate() error {
	normTool := s.Tool.Normalized()
	if !normTool.IsValid() {
		return ErrInvalidToolType
	}

	// Commands (Clear, Undo) require no geometric coordinates
	if normTool.IsCommand() {
		return nil
	}

	if s.Width <= 0 || math.IsNaN(s.Width) || math.IsInf(s.Width, 0) {
		return ErrInvalidStrokeWidth
	}

	switch normTool {
	case ToolPen, ToolHighlighter:
		if len(s.Points) < 1 {
			return ErrInsufficientPoints
		}
	case ToolArrow, ToolRect, ToolEllipse:
		if len(s.Points) < 2 {
			return ErrInsufficientPoints
		}
	case ToolText:
		if len(s.Points) < 1 {
			return ErrInsufficientPoints
		}
		if strings.TrimSpace(s.TextContent) == "" {
			return ErrEmptyTextContent
		}
	}

	for _, pt := range s.Points {
		if err := pt.Validate(); err != nil {
			return err
		}
	}

	return nil
}
