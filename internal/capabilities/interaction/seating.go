package interaction

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrInvalidDimensions = errors.New("rows and cols must be between 1 and 50")
	ErrSeatOutOfBounds   = errors.New("seat position is out of bounds")
	ErrSeatEmpty         = errors.New("seat is empty")
)

const (
	MaxGridDimension = 50
)

// Seat represents a single seat position in a classroom grid.
type Seat struct {
	Row         int       `json:"row"`
	Col         int       `json:"col"`
	DeviceID    string    `json:"device_id,omitempty"`
	StudentName string    `json:"student_name,omitempty"`
	Occupied    bool      `json:"occupied"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// SeatingChart represents the physical layout grid of student computers in a classroom.
type SeatingChart struct {
	mu          sync.RWMutex
	ClassroomID string           `json:"classroom_id"`
	Name        string           `json:"name"`
	Rows        int              `json:"rows"`
	Cols        int              `json:"cols"`
	seats       map[string]*Seat // key: "row:col"
}

// NewSeatingChart constructs a new seating chart grid.
func NewSeatingChart(classroomID, name string, rows, cols int) (*SeatingChart, error) {
	if rows <= 0 || rows > MaxGridDimension || cols <= 0 || cols > MaxGridDimension {
		return nil, ErrInvalidDimensions
	}
	if classroomID == "" {
		return nil, ErrClassroomIDMissing
	}

	return &SeatingChart{
		ClassroomID: classroomID,
		Name:        name,
		Rows:        rows,
		Cols:        cols,
		seats:       make(map[string]*Seat),
	}, nil
}

func seatKey(row, col int) string {
	return fmt.Sprintf("%d:%d", row, col)
}

func (s *SeatingChart) isOutOfBounds(row, col int) bool {
	return row < 0 || row >= s.Rows || col < 0 || col >= s.Cols
}

// AssignSeat places a student computer at the specified row and column.
// If the device is already assigned elsewhere in this chart, its previous seat is cleared.
func (s *SeatingChart) AssignSeat(row, col int, deviceID, studentName string) error {
	if s.isOutOfBounds(row, col) {
		return ErrSeatOutOfBounds
	}
	if deviceID == "" {
		return ErrInvalidDeviceID
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Free any previous seat for this device
	for key, existing := range s.seats {
		if existing.DeviceID == deviceID {
			delete(s.seats, key)
		}
	}

	seat := &Seat{
		Row:         row,
		Col:         col,
		DeviceID:    deviceID,
		StudentName: studentName,
		Occupied:    true,
		UpdatedAt:   time.Now(),
	}

	s.seats[seatKey(row, col)] = seat
	return nil
}

// UnassignSeat clears the seat at row and column.
func (s *SeatingChart) UnassignSeat(row, col int) error {
	if s.isOutOfBounds(row, col) {
		return ErrSeatOutOfBounds
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.seats, seatKey(row, col))
	return nil
}

// UnassignDevice removes whatever seat is assigned to the device.
func (s *SeatingChart) UnassignDevice(deviceID string) bool {
	if deviceID == "" {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for key, seat := range s.seats {
		if seat.DeviceID == deviceID {
			delete(s.seats, key)
			return true
		}
	}
	return false
}

// GetSeat returns the seat at row and col.
func (s *SeatingChart) GetSeat(row, col int) (*Seat, bool) {
	if s.isOutOfBounds(row, col) {
		return nil, false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	seat, exists := s.seats[seatKey(row, col)]
	if !exists {
		return &Seat{Row: row, Col: col, Occupied: false}, false
	}
	return seat, true
}

// GetSeatByDevice finds the seat currently bound to a device ID.
func (s *SeatingChart) GetSeatByDevice(deviceID string) (*Seat, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, seat := range s.seats {
		if seat.DeviceID == deviceID {
			return seat, true
		}
	}
	return nil, false
}

// SwapSeats exchanges the positions of two seats.
func (s *SeatingChart) SwapSeats(r1, c1, r2, c2 int) error {
	if s.isOutOfBounds(r1, c1) || s.isOutOfBounds(r2, c2) {
		return ErrSeatOutOfBounds
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	k1 := seatKey(r1, c1)
	k2 := seatKey(r2, c2)

	seat1, has1 := s.seats[k1]
	seat2, has2 := s.seats[k2]

	if !has1 && !has2 {
		return nil // both empty
	}

	delete(s.seats, k1)
	delete(s.seats, k2)

	if has1 {
		seat1.Row = r2
		seat1.Col = c2
		seat1.UpdatedAt = time.Now()
		s.seats[k2] = seat1
	}
	if has2 {
		seat2.Row = r1
		seat2.Col = c1
		seat2.UpdatedAt = time.Now()
		s.seats[k1] = seat2
	}

	return nil
}

// ToGrid exports the complete Rows x Cols matrix of seats.
func (s *SeatingChart) ToGrid() [][]*Seat {
	s.mu.RLock()
	defer s.mu.RUnlock()

	grid := make([][]*Seat, s.Rows)
	for r := 0; r < s.Rows; r++ {
		grid[r] = make([]*Seat, s.Cols)
		for c := 0; c < s.Cols; c++ {
			if seat, exists := s.seats[seatKey(r, c)]; exists {
				grid[r][c] = seat
			} else {
				grid[r][c] = &Seat{
					Row:      r,
					Col:      c,
					Occupied: false,
				}
			}
		}
	}
	return grid
}

// OccupiedCount returns how many seats are currently occupied.
func (s *SeatingChart) OccupiedCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.seats)
}

// Clear unassigns all seats in the chart.
func (s *SeatingChart) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seats = make(map[string]*Seat)
}
