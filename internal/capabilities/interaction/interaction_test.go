package interaction

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestChatMessageValidation(t *testing.T) {
	tests := []struct {
		name    string
		msg     ChatMessage
		wantErr error
	}{
		{
			name: "valid broadcast message",
			msg: ChatMessage{
				ClassroomID: "room-101",
				SenderID:    "teacher-01",
				SenderName:  "Prof Smith",
				SenderRole:  RoleTeacher,
				Content:     "Welcome to class!",
			},
			wantErr: nil,
		},
		{
			name: "missing classroom id",
			msg: ChatMessage{
				SenderID: "teacher-01",
				Content:  "Hello",
			},
			wantErr: ErrClassroomIDMissing,
		},
		{
			name: "missing sender id",
			msg: ChatMessage{
				ClassroomID: "room-101",
				Content:     "Hello",
			},
			wantErr: ErrSenderIDMissing,
		},
		{
			name: "empty content",
			msg: ChatMessage{
				ClassroomID: "room-101",
				SenderID:    "teacher-01",
				Content:     "   ",
			},
			wantErr: ErrEmptyContent,
		},
		{
			name: "content exceeds limit",
			msg: ChatMessage{
				ClassroomID: "room-101",
				SenderID:    "teacher-01",
				Content:     strings.Repeat("C", MaxContentLength+1),
			},
			wantErr: ErrContentTooLong,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.msg.Validate()
			if err != tt.wantErr {
				t.Fatalf("Validate() = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestChatRoutingAndPrivacy(t *testing.T) {
	cm := NewChatManager()

	room := "room-cs1"
	subTeacher, cancelT := cm.Subscribe(room, "teacher-1")
	defer cancelT()
	subStudentA, cancelA := cm.Subscribe(room, "student-A")
	defer cancelA()
	subStudentB, cancelB := cm.Subscribe(room, "student-B")
	defer cancelB()

	// 1. Broadcast test: Teacher sends public message
	broadcastMsg := ChatMessage{
		ClassroomID: room,
		SenderID:    "teacher-1",
		SenderName:  "Teacher",
		SenderRole:  RoleTeacher,
		Content:     "Quiet please",
	}

	sent, err := cm.SendMessage(broadcastMsg)
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}
	if sent.IsWhisper {
		t.Fatalf("expected broadcast, not whisper")
	}

	// Verify all 3 received the broadcast
	select {
	case m := <-subTeacher:
		if m.Content != "Quiet please" {
			t.Fatalf("teacher received unexpected content: %s", m.Content)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("teacher did not receive broadcast")
	}

	select {
	case m := <-subStudentA:
		if m.Content != "Quiet please" {
			t.Fatalf("student A received unexpected content: %s", m.Content)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("student A did not receive broadcast")
	}

	select {
	case m := <-subStudentB:
		if m.Content != "Quiet please" {
			t.Fatalf("student B received unexpected content: %s", m.Content)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("student B did not receive broadcast")
	}

	// 2. Whisper test: Student A whispers to Teacher
	whisperMsg := ChatMessage{
		ClassroomID: room,
		SenderID:    "student-A",
		SenderName:  "Alice",
		SenderRole:  RoleStudent,
		TargetID:    "teacher-1",
		Content:     "I have a private question",
	}

	sentWhisper, err := cm.SendMessage(whisperMsg)
	if err != nil {
		t.Fatalf("SendMessage whisper failed: %v", err)
	}
	if !sentWhisper.IsWhisper {
		t.Fatalf("expected IsWhisper to be true")
	}

	// Student A (sender) receives it
	select {
	case m := <-subStudentA:
		if m.Content != "I have a private question" {
			t.Fatalf("student A received wrong content: %s", m.Content)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("student A did not receive own whisper")
	}

	// Teacher (target) receives it
	select {
	case m := <-subTeacher:
		if m.Content != "I have a private question" {
			t.Fatalf("teacher received wrong content: %s", m.Content)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("teacher did not receive whisper")
	}

	// Student B (third party) MUST NOT receive it
	select {
	case m := <-subStudentB:
		t.Fatalf("privacy violation: student B received private whisper: %+v", m)
	case <-time.After(100 * time.Millisecond):
		// Expected: nothing received
	}

	// 3. History privacy check
	histB := cm.GetHistory(ChatFilter{
		ClassroomID: room,
		UserID:      "student-B",
	})
	for _, m := range histB {
		if m.IsWhisper {
			t.Fatalf("privacy violation: student B has whisper in history: %+v", m)
		}
	}
	if len(histB) != 1 {
		t.Fatalf("student B should only have 1 public message in history, got %d", len(histB))
	}

	histA := cm.GetHistory(ChatFilter{
		ClassroomID: room,
		UserID:      "student-A",
	})
	if len(histA) != 2 {
		t.Fatalf("student A should have 2 messages in history, got %d", len(histA))
	}
}

func TestRaiseHandLifecycle(t *testing.T) {
	queue := NewRaiseHandQueue()
	room := "lab-3"

	sub, cancel := queue.Subscribe(room)
	defer cancel()

	// Initial count
	if queue.ActiveCount(room) != 0 {
		t.Fatalf("expected 0 active hands")
	}

	// 1. Student raises hand
	rec, err := queue.Raise(room, "dev-01", "Bob", "Need help with compiler error")
	if err != nil {
		t.Fatalf("Raise failed: %v", err)
	}
	if rec.Status != HandStatusRaised {
		t.Fatalf("expected HandStatusRaised, got %s", rec.Status)
	}

	// Verify subscriber notification
	select {
	case event := <-sub:
		if event.DeviceID != "dev-01" || event.Status != HandStatusRaised {
			t.Fatalf("unexpected subscriber event: %+v", event)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("subscriber did not receive raise notification")
	}

	// Second student raises hand slightly later
	time.Sleep(10 * time.Millisecond)
	_, err = queue.Raise(room, "dev-02", "Charlie", "Question 2")
	if err != nil {
		t.Fatalf("Raise for dev-02 failed: %v", err)
	}
	<-sub

	// Check FIFO list order
	list := queue.ListActive(room)
	if len(list) != 2 {
		t.Fatalf("expected 2 active hands, got %d", len(list))
	}
	if list[0].DeviceID != "dev-01" || list[1].DeviceID != "dev-02" {
		t.Fatalf("FIFO order violated: [%s, %s]", list[0].DeviceID, list[1].DeviceID)
	}

	// 2. Teacher acknowledges dev-01
	ackRec, err := queue.Acknowledge(room, "dev-01", "teacher-prof")
	if err != nil {
		t.Fatalf("Acknowledge failed: %v", err)
	}
	if ackRec.Status != HandStatusAcknowledged || ackRec.AcknowledgedBy != "teacher-prof" {
		t.Fatalf("unexpected ack state: %+v", ackRec)
	}
	select {
	case event := <-sub:
		if event.Status != HandStatusAcknowledged {
			t.Fatalf("expected acknowledged status in subscriber, got %s", event.Status)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("subscriber did not receive ack notification")
	}

	// Cannot acknowledge unknown device
	_, err = queue.Acknowledge(room, "dev-nonexistent", "teacher-prof")
	if err != ErrHandNotRaised {
		t.Fatalf("expected ErrHandNotRaised, got %v", err)
	}

	// 3. Clear dev-01
	if err := queue.Clear(room, "dev-01"); err != nil {
		t.Fatalf("Clear failed: %v", err)
	}
	select {
	case event := <-sub:
		if event.Status != HandStatusCleared {
			t.Fatalf("expected cleared status, got %s", event.Status)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("subscriber did not receive clear notification")
	}

	if queue.ActiveCount(room) != 1 {
		t.Fatalf("expected 1 remaining hand, got %d", queue.ActiveCount(room))
	}

	// 4. ClearAll remaining hands
	cleared := queue.ClearAll(room)
	if cleared != 1 {
		t.Fatalf("expected 1 hand cleared by ClearAll, got %d", cleared)
	}
	if queue.ActiveCount(room) != 0 {
		t.Fatalf("expected 0 hands after ClearAll")
	}
}

func TestSeatingChart(t *testing.T) {
	t.Run("dimensions validation", func(t *testing.T) {
		_, err := NewSeatingChart("room-1", "Lab A", 0, 10)
		if err != ErrInvalidDimensions {
			t.Fatalf("expected ErrInvalidDimensions for row 0, got %v", err)
		}

		_, err = NewSeatingChart("room-1", "Lab A", 10, MaxGridDimension+1)
		if err != ErrInvalidDimensions {
			t.Fatalf("expected ErrInvalidDimensions for oversized cols, got %v", err)
		}

		_, err = NewSeatingChart("", "Lab A", 5, 5)
		if err != ErrClassroomIDMissing {
			t.Fatalf("expected ErrClassroomIDMissing, got %v", err)
		}
	})

	t.Run("seat assignment and re-assignment", func(t *testing.T) {
		chart, err := NewSeatingChart("room-101", "Math Lab", 4, 4)
		if err != nil {
			t.Fatalf("NewSeatingChart failed: %v", err)
		}

		// Out of bounds
		if err := chart.AssignSeat(4, 2, "dev-1", "Alice"); err != ErrSeatOutOfBounds {
			t.Fatalf("expected ErrSeatOutOfBounds, got %v", err)
		}

		// Assign Alice at (1, 2)
		if err := chart.AssignSeat(1, 2, "dev-1", "Alice"); err != nil {
			t.Fatalf("AssignSeat failed: %v", err)
		}

		seat, ok := chart.GetSeat(1, 2)
		if !ok || !seat.Occupied || seat.StudentName != "Alice" {
			t.Fatalf("seat (1,2) not properly assigned: %+v", seat)
		}
		if chart.OccupiedCount() != 1 {
			t.Fatalf("expected 1 occupied seat, got %d", chart.OccupiedCount())
		}

		// Re-assign Alice to (2, 3) -> previous seat (1, 2) should be freed!
		if err := chart.AssignSeat(2, 3, "dev-1", "Alice"); err != nil {
			t.Fatalf("AssignSeat re-assignment failed: %v", err)
		}

		prevSeat, ok := chart.GetSeat(1, 2)
		if ok && prevSeat.Occupied {
			t.Fatalf("previous seat (1,2) was not freed upon reassignment!")
		}
		newSeat, ok := chart.GetSeat(2, 3)
		if !ok || !newSeat.Occupied || newSeat.StudentName != "Alice" {
			t.Fatalf("new seat (2,3) not properly assigned")
		}
		if chart.OccupiedCount() != 1 {
			t.Fatalf("expected still 1 occupied seat after reassignment, got %d", chart.OccupiedCount())
		}

		// Lookup by device
		byDev, ok := chart.GetSeatByDevice("dev-1")
		if !ok || byDev.Row != 2 || byDev.Col != 3 {
			t.Fatalf("GetSeatByDevice failed: %+v", byDev)
		}

		// Unassign device
		if !chart.UnassignDevice("dev-1") {
			t.Fatalf("UnassignDevice returned false")
		}
		if chart.OccupiedCount() != 0 {
			t.Fatalf("expected 0 occupied after UnassignDevice")
		}
	})

	t.Run("swap seats and grid conversion", func(t *testing.T) {
		chart, _ := NewSeatingChart("room-101", "Lab", 2, 2)
		_ = chart.AssignSeat(0, 0, "dev-A", "Student A")
		_ = chart.AssignSeat(1, 1, "dev-B", "Student B")

		if err := chart.SwapSeats(0, 0, 1, 1); err != nil {
			t.Fatalf("SwapSeats failed: %v", err)
		}

		s00, _ := chart.GetSeat(0, 0)
		s11, _ := chart.GetSeat(1, 1)

		if s00.DeviceID != "dev-B" || s11.DeviceID != "dev-A" {
			t.Fatalf("seats not swapped properly: (0,0)=%s, (1,1)=%s", s00.DeviceID, s11.DeviceID)
		}

		// Grid export
		grid := chart.ToGrid()
		if len(grid) != 2 || len(grid[0]) != 2 {
			t.Fatalf("grid dimensions mismatch: %dx%d", len(grid), len(grid[0]))
		}
		if !grid[0][0].Occupied || grid[0][0].DeviceID != "dev-B" {
			t.Fatalf("grid[0][0] mismatch: %+v", grid[0][0])
		}
		if grid[0][1].Occupied {
			t.Fatalf("grid[0][1] should be unoccupied")
		}

		// Clear
		chart.Clear()
		if chart.OccupiedCount() != 0 {
			t.Fatalf("expected 0 seats after Clear")
		}
	})
}

func TestInteractionConcurrency(t *testing.T) {
	cm := NewChatManager()
	queue := NewRaiseHandQueue()
	chart, _ := NewSeatingChart("room-c", "Concurrent Lab", 10, 10)

	var wg sync.WaitGroup

	// Concurrently send chat messages
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, _ = cm.SendMessage(ChatMessage{
				ClassroomID: "room-c",
				SenderID:    "student-x",
				Content:     "Hello from concurrent test",
			})
			_ = cm.GetHistory(ChatFilter{ClassroomID: "room-c"})
		}(i)
	}

	// Concurrently raise and clear hands
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			devID := "dev-conc"
			_, _ = queue.Raise("room-c", devID, "Student", "Need help")
			_, _ = queue.Acknowledge("room-c", devID, "teacher")
			_ = queue.Clear("room-c", devID)
			_ = queue.ListActive("room-c")
		}(i)
	}

	// Concurrently assign and swap seats
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			row := idx % 10
			col := (idx * 2) % 10
			_ = chart.AssignSeat(row, col, "dev-c", "Student")
			_ = chart.SwapSeats(row, col, 0, 0)
			_ = chart.ToGrid()
		}(i)
	}

	wg.Wait()
}
