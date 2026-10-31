package whiteboard

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"testing"
)

func TestVectorDrawingPrimitives(t *testing.T) {
	t.Parallel()

	wb := NewWhiteboard("class-alpha")

	// 1. Pen Stroke
	penStroke := Stroke{
		Tool:  ToolPen,
		Color: RGB(255, 0, 0),
		Width: 0.005,
		Points: []Point{
			{X: 0.1, Y: 0.1},
			{X: 0.12, Y: 0.15},
			{X: 0.15, Y: 0.2},
		},
	}
	if err := wb.AddStroke(penStroke); err != nil {
		t.Fatalf("AddStroke(pen) failed: %v", err)
	}

	// 2. Highlighter Stroke (translucent)
	highlighterStroke := Stroke{
		Tool:  ToolHighlighter,
		Color: RGBA(255, 255, 0, 100),
		Width: 0.02,
		Points: []Point{
			{X: 0.2, Y: 0.3},
			{X: 0.8, Y: 0.3},
		},
	}
	if err := wb.AddStroke(highlighterStroke); err != nil {
		t.Fatalf("AddStroke(highlighter) failed: %v", err)
	}

	// 3. Arrow Stroke
	arrowStroke := Stroke{
		Tool:  ToolArrow,
		Color: RGB(0, 0, 255),
		Width: 0.008,
		Points: []Point{
			{X: 0.1, Y: 0.1},
			{X: 0.5, Y: 0.5},
		},
	}
	if err := wb.AddStroke(arrowStroke); err != nil {
		t.Fatalf("AddStroke(arrow) failed: %v", err)
	}

	// 4. Rect Stroke
	rectStroke := Stroke{
		Tool:  ToolRect,
		Color: RGB(0, 255, 0),
		Width: 0.004,
		Points: []Point{
			{X: 0.2, Y: 0.2},
			{X: 0.6, Y: 0.6},
		},
	}
	if err := wb.AddStroke(rectStroke); err != nil {
		t.Fatalf("AddStroke(rect) failed: %v", err)
	}

	// 5. Ellipse Stroke
	ellipseStroke := Stroke{
		Tool:  ToolEllipse,
		Color: RGB(128, 0, 128),
		Width: 0.004,
		Points: []Point{
			{X: 0.3, Y: 0.3},
			{X: 0.7, Y: 0.5},
		},
	}
	if err := wb.AddStroke(ellipseStroke); err != nil {
		t.Fatalf("AddStroke(ellipse) failed: %v", err)
	}

	// 6. Text Stroke
	textStroke := Stroke{
		Tool:        ToolText,
		Color:       RGB(0, 0, 0),
		Width:       0.016,
		Points:      []Point{{X: 0.4, Y: 0.4}},
		TextContent: "LocalMesh JiYu Vector Whiteboard",
	}
	if err := wb.AddStroke(textStroke); err != nil {
		t.Fatalf("AddStroke(text) failed: %v", err)
	}

	if wb.StrokeCount() != 6 {
		t.Fatalf("expected 6 strokes, got %d", wb.StrokeCount())
	}

	strokes := wb.ExportStrokes()
	if len(strokes) != 6 {
		t.Fatalf("exported strokes length %d != 6", len(strokes))
	}

	// Verify deep-copy immutability
	strokes[0].Points[0].X = 0.999
	originalFirst := wb.ExportStrokes()[0]
	if originalFirst.Points[0].X == 0.999 {
		t.Fatal("mutating exported stroke leaked into internal whiteboard state")
	}

	snap := wb.Snapshot()
	if snap.StrokeCount != 6 || len(snap.Strokes) != 6 {
		t.Fatalf("snapshot mismatch: count=%d, len=%d", snap.StrokeCount, len(snap.Strokes))
	}
}

func TestUndoRedo(t *testing.T) {
	t.Parallel()

	wb := NewWhiteboard("class-undoredo")

	// Empty undo/redo errors
	if err := wb.Undo(); !errors.Is(err, ErrNothingToUndo) {
		t.Fatalf("expected ErrNothingToUndo, got %v", err)
	}
	if err := wb.Redo(); !errors.Is(err, ErrNothingToRedo) {
		t.Fatalf("expected ErrNothingToRedo, got %v", err)
	}

	makeStroke := func(text string) Stroke {
		return Stroke{
			Tool:        ToolText,
			Color:       RGB(0, 0, 0),
			Width:       0.01,
			Points:      []Point{{X: 0.5, Y: 0.5}},
			TextContent: text,
		}
	}

	// Add 3 strokes
	_ = wb.AddStroke(makeStroke("stroke-1"))
	_ = wb.AddStroke(makeStroke("stroke-2"))
	_ = wb.AddStroke(makeStroke("stroke-3"))

	if wb.StrokeCount() != 3 || wb.UndoCount() != 0 {
		t.Fatalf("initial state mismatch: active=%d undo=%d", wb.StrokeCount(), wb.UndoCount())
	}

	// Undo 1
	undone, err := wb.UndoStroke()
	if err != nil {
		t.Fatalf("UndoStroke failed: %v", err)
	}
	if undone.TextContent != "stroke-3" {
		t.Errorf("expected stroke-3 undone, got %s", undone.TextContent)
	}
	if wb.StrokeCount() != 2 || wb.UndoCount() != 1 {
		t.Fatalf("after 1 undo: active=%d undo=%d", wb.StrokeCount(), wb.UndoCount())
	}

	// Undo 2
	undone2, err := wb.UndoStroke()
	if err != nil {
		t.Fatalf("UndoStroke 2 failed: %v", err)
	}
	if undone2.TextContent != "stroke-2" {
		t.Errorf("expected stroke-2 undone, got %s", undone2.TextContent)
	}
	if wb.StrokeCount() != 1 || wb.UndoCount() != 2 {
		t.Fatalf("after 2 undo: active=%d undo=%d", wb.StrokeCount(), wb.UndoCount())
	}

	// Redo 1
	redone, err := wb.RedoStroke()
	if err != nil {
		t.Fatalf("RedoStroke failed: %v", err)
	}
	if redone.TextContent != "stroke-2" {
		t.Errorf("expected stroke-2 redone, got %s", redone.TextContent)
	}
	if wb.StrokeCount() != 2 || wb.UndoCount() != 1 {
		t.Fatalf("after 1 redo: active=%d undo=%d", wb.StrokeCount(), wb.UndoCount())
	}

	// Adding a new stroke invalidates redo stack
	_ = wb.AddStroke(makeStroke("stroke-4"))
	if wb.StrokeCount() != 3 {
		t.Fatalf("expected 3 active strokes, got %d", wb.StrokeCount())
	}
	if wb.UndoCount() != 0 {
		t.Fatalf("expected redo stack cleared, got %d", wb.UndoCount())
	}
	if err := wb.Redo(); !errors.Is(err, ErrNothingToRedo) {
		t.Fatalf("expected ErrNothingToRedo after new stroke, got %v", err)
	}

	// Test Command strokes: ToolUndo via AddStroke
	_ = wb.AddStroke(Stroke{Tool: ToolUndo})
	if wb.StrokeCount() != 2 {
		t.Fatalf("expected 2 active strokes after ToolUndo, got %d", wb.StrokeCount())
	}
}

func TestClear(t *testing.T) {
	t.Parallel()

	wb := NewWhiteboard("class-clear")

	stroke := Stroke{
		Tool:   ToolPen,
		Color:  RGB(0, 0, 0),
		Width:  0.01,
		Points: []Point{{X: 0.1, Y: 0.1}},
	}
	_ = wb.AddStroke(stroke)
	_ = wb.AddStroke(stroke)

	if wb.StrokeCount() != 2 {
		t.Fatalf("expected 2 strokes, got %d", wb.StrokeCount())
	}

	wb.Clear()
	if wb.StrokeCount() != 0 || wb.UndoCount() != 0 {
		t.Fatalf("expected empty board after Clear(), got active=%d undo=%d", wb.StrokeCount(), wb.UndoCount())
	}

	// Test ToolClear via AddStroke
	_ = wb.AddStroke(stroke)
	if wb.StrokeCount() != 1 {
		t.Fatalf("expected 1 stroke, got %d", wb.StrokeCount())
	}
	_ = wb.AddStroke(Stroke{Tool: ToolClear})
	if wb.StrokeCount() != 0 {
		t.Fatalf("expected 0 strokes after ToolClear stroke, got %d", wb.StrokeCount())
	}
}

func TestBoundaryAndValidation(t *testing.T) {
	t.Parallel()

	// 1. ToolType validity
	validTools := []ToolType{ToolPen, ToolHighlighter, ToolArrow, ToolRect, ToolEllipse, ToolText, ToolClear, ToolUndo}
	for _, tool := range validTools {
		if !tool.IsValid() {
			t.Errorf("tool %s should be valid", tool)
		}
	}
	if ToolType("laser").IsValid() {
		t.Error("laser tool should be invalid")
	}

	// 2. Stroke width validation
	invalidWidthStroke := Stroke{
		Tool:   ToolPen,
		Color:  RGB(0, 0, 0),
		Width:  0,
		Points: []Point{{X: 0.5, Y: 0.5}},
	}
	if err := invalidWidthStroke.Validate(); !errors.Is(err, ErrInvalidStrokeWidth) {
		t.Fatalf("expected ErrInvalidStrokeWidth for 0 width, got %v", err)
	}

	nanWidthStroke := invalidWidthStroke
	nanWidthStroke.Width = math.NaN()
	if err := nanWidthStroke.Validate(); !errors.Is(err, ErrInvalidStrokeWidth) {
		t.Fatalf("expected ErrInvalidStrokeWidth for NaN width, got %v", err)
	}

	// 3. Point coordinate normalization validation
	tests := []struct {
		name    string
		point   Point
		wantErr bool
	}{
		{"Valid min", Point{X: 0.0, Y: 0.0}, false},
		{"Valid max", Point{X: 1.0, Y: 1.0}, false},
		{"Valid mid", Point{X: 0.5, Y: 0.5}, false},
		{"Negative X", Point{X: -0.01, Y: 0.5}, true},
		{"Negative Y", Point{X: 0.5, Y: -0.01}, true},
		{"Excessive X", Point{X: 1.01, Y: 0.5}, true},
		{"Excessive Y", Point{X: 0.5, Y: 1.01}, true},
		{"NaN X", Point{X: math.NaN(), Y: 0.5}, true},
		{"Inf Y", Point{X: 0.5, Y: math.Inf(1)}, true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.point.Validate()
			if tc.wantErr && !errors.Is(err, ErrPointOutOfBounds) {
				t.Fatalf("expected ErrPointOutOfBounds for %v, got %v", tc.point, err)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected valid point for %v, got %v", tc.point, err)
			}
		})
	}

	// 4. Insufficient points
	penNoPoints := Stroke{
		Tool:   ToolPen,
		Color:  RGB(0, 0, 0),
		Width:  0.01,
		Points: nil,
	}
	if err := penNoPoints.Validate(); !errors.Is(err, ErrInsufficientPoints) {
		t.Fatalf("expected ErrInsufficientPoints for pen, got %v", err)
	}

	rectOnePoint := Stroke{
		Tool:   ToolRect,
		Color:  RGB(0, 0, 0),
		Width:  0.01,
		Points: []Point{{X: 0.1, Y: 0.1}},
	}
	if err := rectOnePoint.Validate(); !errors.Is(err, ErrInsufficientPoints) {
		t.Fatalf("expected ErrInsufficientPoints for rect with 1 point, got %v", err)
	}

	textNoContent := Stroke{
		Tool:        ToolText,
		Color:       RGB(0, 0, 0),
		Width:       0.01,
		Points:      []Point{{X: 0.1, Y: 0.1}},
		TextContent: "   ",
	}
	if err := textNoContent.Validate(); !errors.Is(err, ErrEmptyTextContent) {
		t.Fatalf("expected ErrEmptyTextContent for text, got %v", err)
	}

	// 5. Hex Color Parsing
	c1, err := Hex("#FF0000")
	if err != nil || c1 != RGB(255, 0, 0) {
		t.Errorf("Hex #FF0000 parse failed: %v, got %v", err, c1)
	}
	c2, err := Hex("#00FF0080")
	if err != nil || c2 != RGBA(0, 255, 0, 128) {
		t.Errorf("Hex #00FF0080 parse failed: %v, got %v", err, c2)
	}
	c3, err := Hex("#F00")
	if err != nil || c3 != RGB(255, 0, 0) {
		t.Errorf("Hex #F00 parse failed: %v, got %v", err, c3)
	}
	if _, err := Hex("invalid-hex"); !errors.Is(err, ErrInvalidHexColor) {
		t.Fatalf("expected ErrInvalidHexColor, got %v", err)
	}
}

func TestImportExport(t *testing.T) {
	t.Parallel()

	wb1 := NewWhiteboard("room-import")
	_ = wb1.AddStroke(Stroke{
		Tool:   ToolPen,
		Color:  RGB(10, 20, 30),
		Width:  0.005,
		Points: []Point{{X: 0.1, Y: 0.1}, {X: 0.2, Y: 0.2}},
	})
	_ = wb1.AddStroke(Stroke{
		Tool:        ToolText,
		Color:       RGB(40, 50, 60),
		Width:       0.012,
		Points:      []Point{{X: 0.3, Y: 0.3}},
		TextContent: "Export/Import Test",
	})

	exported := wb1.ExportStrokes()
	wb2 := NewWhiteboard("room-export")
	if err := wb2.ImportStrokes(exported); err != nil {
		t.Fatalf("ImportStrokes failed: %v", err)
	}

	if wb2.StrokeCount() != 2 {
		t.Fatalf("wb2 stroke count expected 2, got %d", wb2.StrokeCount())
	}

	// Invalid stroke in import must reject without corrupting state
	badStrokes := []Stroke{
		{Tool: ToolPen, Width: -1, Points: []Point{{X: 0.1, Y: 0.1}}},
	}
	if err := wb2.ImportStrokes(badStrokes); err == nil {
		t.Fatal("expected error importing invalid stroke")
	}
	if wb2.StrokeCount() != 2 {
		t.Fatalf("wb2 stroke count should remain 2 after failed import, got %d", wb2.StrokeCount())
	}
}

func Test100ConcurrentDrawingRace(t *testing.T) {
	t.Parallel()

	const numWorkers = 100
	wb := NewWhiteboard("room-race")

	var wg sync.WaitGroup
	wg.Add(numWorkers)

	for i := 0; i < numWorkers; i++ {
		workerID := i
		go func() {
			defer wg.Done()

			normX := float64(workerID%100) / 100.0
			stroke := Stroke{
				ID:    fmt.Sprintf("worker-stroke-%d", workerID),
				Tool:  ToolPen,
				Color: RGB(uint8(workerID%255), 100, 200),
				Width: 0.005,
				Points: []Point{
					{X: normX, Y: 0.1},
					{X: normX, Y: 0.9},
				},
			}

			// Add stroke
			if err := wb.AddStroke(stroke); err != nil {
				t.Errorf("worker %d AddStroke failed: %v", workerID, err)
				return
			}

			// Concurrently read state
			_ = wb.ExportStrokes()
			_ = wb.Snapshot()
			_ = wb.StrokeCount()
			_ = wb.UndoCount()
			_ = wb.Version()

			// Concurrently undo/redo
			if workerID%5 == 0 {
				_ = wb.Undo()
				_ = wb.Redo()
			}
		}()
	}

	wg.Wait()

	if wb.StrokeCount() == 0 {
		t.Fatal("expected positive stroke count after concurrent drawing")
	}
}
