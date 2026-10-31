package whiteboard

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var (
	ErrNothingToUndo = errors.New("whiteboard: nothing to undo")
	ErrNothingToRedo = errors.New("whiteboard: nothing to redo")
)

// Snapshot represents an immutable point-in-time state of the Whiteboard.
type Snapshot struct {
	ClassroomID  string    `json:"classroom_id"`
	Version      uint64    `json:"version"`
	StrokeCount  int       `json:"stroke_count"`
	Strokes      []Stroke  `json:"strokes"`
	LastModified time.Time `json:"last_modified"`
}

// Whiteboard manages interactive whiteboard state with full undo/redo stacks,
// vector serialization, and concurrent access safety.
type Whiteboard struct {
	mu           sync.RWMutex
	classroomID  string
	strokes      []Stroke // Active visible strokes in display order
	undoStack    []Stroke // Undone strokes available for redo
	version      uint64   // Monotonically increasing revision counter
	lastModified time.Time
}

// NewWhiteboard initializes a new Whiteboard instance.
func NewWhiteboard(classroomID string) *Whiteboard {
	return &Whiteboard{
		classroomID:  strings.TrimSpace(classroomID),
		strokes:      make([]Stroke, 0),
		undoStack:    make([]Stroke, 0),
		lastModified: time.Now(),
	}
}

// AddStroke appends a new stroke to the canvas.
// If stroke is ToolClear or ToolUndo, executes the respective action.
// Adding a drawing stroke clears the redo stack.
func (w *Whiteboard) AddStroke(s Stroke) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	normTool := s.Tool.Normalized()

	if normTool == ToolClear {
		w.clearLocked()
		return nil
	}

	if normTool == ToolUndo {
		return w.undoLocked()
	}

	if err := s.Validate(); err != nil {
		return err
	}

	// Normalize tool to canonical form
	s.Tool = normTool

	// Assign ID and timestamp if empty
	w.version++
	if s.ID == "" {
		s.ID = fmt.Sprintf("stroke-%d-%d", w.version, time.Now().UnixNano())
	}
	if s.CreatedAt.IsZero() {
		s.CreatedAt = time.Now()
	}

	// Deep copy points to protect against external mutation
	cloned := s
	if s.Points != nil {
		cloned.Points = make([]Point, len(s.Points))
		copy(cloned.Points, s.Points)
	}

	w.strokes = append(w.strokes, cloned)
	w.undoStack = nil // New stroke invalidates future redos
	w.lastModified = time.Now()

	return nil
}

// Undo removes the most recent stroke and pushes it onto the redo stack.
func (w *Whiteboard) Undo() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.undoLocked()
}

func (w *Whiteboard) undoLocked() error {
	if len(w.strokes) == 0 {
		return ErrNothingToUndo
	}

	lastIdx := len(w.strokes) - 1
	popped := w.strokes[lastIdx]
	w.strokes = w.strokes[:lastIdx]
	w.undoStack = append(w.undoStack, popped)

	w.version++
	w.lastModified = time.Now()
	return nil
}

// UndoStroke removes the most recent stroke, pushes it onto the redo stack,
// and returns the undone stroke.
func (w *Whiteboard) UndoStroke() (Stroke, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.strokes) == 0 {
		return Stroke{}, ErrNothingToUndo
	}

	lastIdx := len(w.strokes) - 1
	popped := w.strokes[lastIdx]
	w.strokes = w.strokes[:lastIdx]
	w.undoStack = append(w.undoStack, popped)

	w.version++
	w.lastModified = time.Now()
	return popped, nil
}

// Redo restores the most recently undone stroke.
func (w *Whiteboard) Redo() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.redoLocked()
}

func (w *Whiteboard) redoLocked() error {
	if len(w.undoStack) == 0 {
		return ErrNothingToRedo
	}

	lastIdx := len(w.undoStack) - 1
	restored := w.undoStack[lastIdx]
	w.undoStack = w.undoStack[:lastIdx]
	w.strokes = append(w.strokes, restored)

	w.version++
	w.lastModified = time.Now()
	return nil
}

// RedoStroke restores the most recently undone stroke and returns it.
func (w *Whiteboard) RedoStroke() (Stroke, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.undoStack) == 0 {
		return Stroke{}, ErrNothingToRedo
	}

	lastIdx := len(w.undoStack) - 1
	restored := w.undoStack[lastIdx]
	w.undoStack = w.undoStack[:lastIdx]
	w.strokes = append(w.strokes, restored)

	w.version++
	w.lastModified = time.Now()
	return restored, nil
}

// Clear removes all active strokes and clears the redo stack.
func (w *Whiteboard) Clear() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.clearLocked()
}

func (w *Whiteboard) clearLocked() {
	if len(w.strokes) == 0 && len(w.undoStack) == 0 {
		return
	}
	w.strokes = make([]Stroke, 0)
	w.undoStack = make([]Stroke, 0)
	w.version++
	w.lastModified = time.Now()
}

// ExportStrokes returns a deep copy of all current active strokes.
func (w *Whiteboard) ExportStrokes() []Stroke {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return cloneStrokes(w.strokes)
}

// ImportStrokes replaces current strokes with the provided slice after validation.
// Clears redo history and increments the version counter.
func (w *Whiteboard) ImportStrokes(strokes []Stroke) error {
	// Pre-validate all strokes before applying any changes
	for _, s := range strokes {
		if err := s.Validate(); err != nil {
			return fmt.Errorf("invalid stroke in import: %w", err)
		}
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	w.strokes = cloneStrokes(strokes)
	w.undoStack = make([]Stroke, 0)
	w.version++
	w.lastModified = time.Now()
	return nil
}

// Snapshot returns an immutable point-in-time snapshot of the whiteboard state.
func (w *Whiteboard) Snapshot() Snapshot {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return Snapshot{
		ClassroomID:  w.classroomID,
		Version:      w.version,
		StrokeCount:  len(w.strokes),
		Strokes:      cloneStrokes(w.strokes),
		LastModified: w.lastModified,
	}
}

// StrokeCount returns the number of active visible strokes.
func (w *Whiteboard) StrokeCount() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return len(w.strokes)
}

// UndoCount returns the number of strokes that can currently be redone.
func (w *Whiteboard) UndoCount() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return len(w.undoStack)
}

// Version returns the current revision number.
func (w *Whiteboard) Version() uint64 {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.version
}

// ClassroomID returns the classroom identifier.
func (w *Whiteboard) ClassroomID() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.classroomID
}

// cloneStrokes creates an isolated deep copy of a stroke slice.
func cloneStrokes(strokes []Stroke) []Stroke {
	if strokes == nil {
		return make([]Stroke, 0)
	}
	res := make([]Stroke, len(strokes))
	for i, s := range strokes {
		res[i] = s
		if s.Points != nil {
			res[i].Points = make([]Point, len(s.Points))
			copy(res[i].Points, s.Points)
		}
	}
	return res
}
