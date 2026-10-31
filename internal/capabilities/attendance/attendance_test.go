package attendance_test

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"localmesh/internal/capabilities/attendance"
)

func TestSessionLifecycle(t *testing.T) {
	roster := []attendance.ExpectedStudent{
		{StudentID: "S001", StudentName: "Alice"},
		{StudentID: "S002", StudentName: "Bob"},
	}

	session, err := attendance.NewAttendanceSession("sess_01", "room_101", "Morning Math Roll-Call", 60, roster)
	if err != nil {
		t.Fatalf("failed to create attendance session: %v", err)
	}

	// 1. Initial state must be Pending
	if state := session.GetState(); state != attendance.SessionPending {
		t.Fatalf("expected state %s, got %s", attendance.SessionPending, state)
	}

	// 2. Cannot check in while Pending
	err = session.CheckIn(attendance.CheckInRequest{
		StudentID: "S001",
		DeviceID:  "dev_001",
		SeatRow:   1,
		SeatCol:   1,
	})
	if !errors.Is(err, attendance.ErrSessionNotActive) {
		t.Fatalf("expected ErrSessionNotActive when pending, got %v", err)
	}

	// 3. Start session -> Active
	if err := session.Start(); err != nil {
		t.Fatalf("failed to start session: %v", err)
	}
	if state := session.GetState(); state != attendance.SessionActive {
		t.Fatalf("expected state %s, got %s", attendance.SessionActive, state)
	}

	// Cannot start again
	if err := session.Start(); !errors.Is(err, attendance.ErrSessionNotPending) {
		t.Fatalf("expected ErrSessionNotPending on repeated start, got %v", err)
	}

	// 4. Valid check in while Active
	err = session.CheckIn(attendance.CheckInRequest{
		StudentID:   "S001",
		DeviceID:    "dev_001",
		StudentName: "Alice",
		SeatRow:     1,
		SeatCol:     1,
	})
	if err != nil {
		t.Fatalf("expected successful check-in, got %v", err)
	}

	// 5. Close session -> Closed
	if err := session.Close(); err != nil {
		t.Fatalf("failed to close session: %v", err)
	}
	if state := session.GetState(); state != attendance.SessionClosed {
		t.Fatalf("expected state %s, got %s", attendance.SessionClosed, state)
	}

	// 6. Cannot check in while Closed
	err = session.CheckIn(attendance.CheckInRequest{
		StudentID: "S002",
		DeviceID:  "dev_002",
		SeatRow:   1,
		SeatCol:   2,
	})
	if !errors.Is(err, attendance.ErrSessionClosed) {
		t.Fatalf("expected ErrSessionClosed when closed, got %v", err)
	}
}

func TestDuplicateCheckInDefense(t *testing.T) {
	roster := []attendance.ExpectedStudent{
		{StudentID: "S001", StudentName: "Alice"},
		{StudentID: "S002", StudentName: "Bob"},
	}

	session, err := attendance.NewAttendanceSession("sess_02", "room_101", "Physics Roll-Call", 60, roster)
	if err != nil {
		t.Fatalf("NewAttendanceSession failed: %v", err)
	}

	if err := session.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// First check-in succeeds
	err = session.CheckIn(attendance.CheckInRequest{
		StudentID:   "S001",
		DeviceID:    "dev_pc_01",
		StudentName: "Alice",
		SeatRow:     2,
		SeatCol:     3,
	})
	if err != nil {
		t.Fatalf("first check-in failed: %v", err)
	}

	// Duplicate StudentID check-in must fail
	err = session.CheckIn(attendance.CheckInRequest{
		StudentID:   "S001",
		DeviceID:    "dev_pc_02",
		StudentName: "Alice",
		SeatRow:     2,
		SeatCol:     4,
	})
	if !errors.Is(err, attendance.ErrDuplicateCheckIn) {
		t.Fatalf("expected ErrDuplicateCheckIn, got %v", err)
	}

	// Duplicate DeviceID check-in by another student must fail
	err = session.CheckIn(attendance.CheckInRequest{
		StudentID:   "S002",
		DeviceID:    "dev_pc_01", // same device as S001
		StudentName: "Bob",
		SeatRow:     2,
		SeatCol:     3,
	})
	if !errors.Is(err, attendance.ErrDeviceAlreadyUsed) {
		t.Fatalf("expected ErrDeviceAlreadyUsed, got %v", err)
	}
}

func TestTimeoutRejection(t *testing.T) {
	roster := []attendance.ExpectedStudent{
		{StudentID: "S001", StudentName: "Alice"},
	}

	// 1 second time limit
	session, err := attendance.NewAttendanceSession("sess_03", "room_101", "Quick Roll-Call", 1, roster)
	if err != nil {
		t.Fatalf("NewAttendanceSession failed: %v", err)
	}

	if err := session.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Wait for time limit to elapse
	time.Sleep(1100 * time.Millisecond)

	// Check-in should now be rejected due to timeout
	err = session.CheckIn(attendance.CheckInRequest{
		StudentID: "S001",
		DeviceID:  "dev_01",
		SeatRow:   1,
		SeatCol:   1,
	})
	if !errors.Is(err, attendance.ErrAttendanceTimeout) && !errors.Is(err, attendance.ErrSessionClosed) {
		t.Fatalf("expected timeout or session closed error, got %v", err)
	}

	// State should automatically reflect Closed
	if state := session.GetState(); state != attendance.SessionClosed {
		t.Fatalf("expected state %s after timeout, got %s", attendance.SessionClosed, state)
	}
}

func TestCSVExportCorrectness(t *testing.T) {
	roster := []attendance.ExpectedStudent{
		{StudentID: "S001", StudentName: "Alice"},
		{StudentID: "S002", StudentName: "Bob"},
		{StudentID: "S003", StudentName: "Charlie"},
	}

	session, err := attendance.NewAttendanceSession("sess_04", "room_101", "English Roll-Call", 60, roster)
	if err != nil {
		t.Fatalf("NewAttendanceSession failed: %v", err)
	}
	session.SetLateThreshold(1) // 1 second threshold for late demonstration

	if err := session.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// S001 checks in on time
	err = session.CheckIn(attendance.CheckInRequest{
		StudentID:   "S001",
		DeviceID:    "dev_001",
		StudentName: "Alice",
		SeatRow:     1,
		SeatCol:     1,
	})
	if err != nil {
		t.Fatalf("S001 check-in failed: %v", err)
	}

	// S002 does not check in (absent)
	// S003 checks in
	err = session.CheckIn(attendance.CheckInRequest{
		StudentID:   "S003",
		DeviceID:    "dev_003",
		StudentName: "Charlie",
		SeatRow:     1,
		SeatCol:     3,
	})
	if err != nil {
		t.Fatalf("S003 check-in failed: %v", err)
	}

	csvBytes, err := session.ExportCSV()
	if err != nil {
		t.Fatalf("ExportCSV failed: %v", err)
	}

	// Verify UTF-8 BOM (\xEF\xBB\xBF)
	if len(csvBytes) < 3 {
		t.Fatalf("CSV output too short: %d bytes", len(csvBytes))
	}
	expectedBOM := []byte{0xEF, 0xBB, 0xBF}
	if !bytes.Equal(csvBytes[:3], expectedBOM) {
		t.Fatalf("expected UTF-8 BOM header, got %x", csvBytes[:3])
	}

	// Parse CSV body without BOM
	reader := csv.NewReader(bytes.NewReader(csvBytes[3:]))
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("failed to parse generated CSV: %v", err)
	}

	// Header + 2 checked in + 1 absent = 4 rows
	if len(records) != 4 {
		t.Fatalf("expected 4 CSV rows, got %d", len(records))
	}

	// Verify header
	expectedHeader := []string{"序号", "学号", "姓名", "设备ID", "座位行", "座位列", "签到时间", "状态"}
	for i, col := range expectedHeader {
		if records[0][i] != col {
			t.Errorf("header column %d: expected %q, got %q", i, col, records[0][i])
		}
	}

	// Verify checked in rows
	row1 := records[1]
	if row1[1] != "S001" || row1[2] != "Alice" || row1[3] != "dev_001" || row1[7] != "已签到" {
		t.Errorf("row 1 unexpected: %+v", row1)
	}

	row2 := records[2]
	if row2[1] != "S003" || row2[2] != "Charlie" || row2[3] != "dev_003" || row2[7] != "已签到" {
		t.Errorf("row 2 unexpected: %+v", row2)
	}

	// Verify absent row
	row3 := records[3]
	if row3[1] != "S002" || row3[2] != "Bob" || row3[7] != "缺勤" {
		t.Errorf("row 3 unexpected: %+v", row3)
	}

	// Verify stats
	stats := session.GetStats()
	if stats.TotalExpected != 3 {
		t.Errorf("expected 3 expected students, got %d", stats.TotalExpected)
	}
	if stats.TotalCheckedIn != 2 {
		t.Errorf("expected 2 checked in students, got %d", stats.TotalCheckedIn)
	}
	expectedRate := 66.67
	if stats.AttendanceRate != expectedRate {
		t.Errorf("expected rate %f, got %f", expectedRate, stats.AttendanceRate)
	}
	if len(stats.UncheckedStudents) != 1 || stats.UncheckedStudents[0] != "Bob (S002)" {
		t.Errorf("expected Bob unchecked, got %+v", stats.UncheckedStudents)
	}
}

func TestHighConcurrencyCheckIn(t *testing.T) {
	const studentCount = 100
	roster := make([]attendance.ExpectedStudent, studentCount)
	for i := 0; i < studentCount; i++ {
		roster[i] = attendance.ExpectedStudent{
			StudentID:   fmt.Sprintf("STU_%04d", i+1),
			StudentName: fmt.Sprintf("Student_%04d", i+1),
		}
	}

	session, err := attendance.NewAttendanceSession("sess_concurrent", "room_huge", "Concurrency Test", 30, roster)
	if err != nil {
		t.Fatalf("NewAttendanceSession failed: %v", err)
	}

	if err := session.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, studentCount)

	for i := 0; i < studentCount; i++ {
		wg.Add(1)
		idx := i
		go func() {
			defer wg.Done()
			req := attendance.CheckInRequest{
				StudentID:   roster[idx].StudentID,
				DeviceID:    fmt.Sprintf("DEV_%04d", idx+1),
				StudentName: roster[idx].StudentName,
				SeatRow:     idx / 10,
				SeatCol:     idx % 10,
			}
			if err := session.CheckIn(req); err != nil {
				errCh <- err
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent check-in error: %v", err)
	}

	stats := session.GetStats()
	if stats.TotalCheckedIn != studentCount {
		t.Fatalf("expected %d checked in students, got %d", studentCount, stats.TotalCheckedIn)
	}
	if stats.TotalExpected != studentCount {
		t.Fatalf("expected %d expected students, got %d", studentCount, stats.TotalExpected)
	}
	if stats.AttendanceRate != 100.0 {
		t.Fatalf("expected 100.0%% attendance rate, got %f", stats.AttendanceRate)
	}
	if len(stats.UncheckedStudents) != 0 {
		t.Fatalf("expected 0 unchecked students, got %d", len(stats.UncheckedStudents))
	}

	records := session.GetRecords()
	if len(records) != studentCount {
		t.Fatalf("expected %d records, got %d", studentCount, len(records))
	}
}

func TestManagerOperations(t *testing.T) {
	mgr := attendance.NewManager()

	roster := []attendance.ExpectedStudent{
		{StudentID: "S101", StudentName: "Dave"},
	}

	sess, err := mgr.CreateSession("sess_m1", "room_a", "Chemistry", 60, roster)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	found, err := mgr.GetSession("sess_m1")
	if err != nil || found != sess {
		t.Fatalf("GetSession failed: %v, found: %v", err, found)
	}

	_, err = mgr.GetSession("sess_nonexistent")
	if !errors.Is(err, attendance.ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}

	listA := mgr.ListSessions("room_a")
	if len(listA) != 1 {
		t.Fatalf("expected 1 session in room_a, got %d", len(listA))
	}

	listB := mgr.ListSessions("room_b")
	if len(listB) != 0 {
		t.Fatalf("expected 0 sessions in room_b, got %d", len(listB))
	}

	if err := sess.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if err := mgr.CloseSession("sess_m1"); err != nil {
		t.Fatalf("CloseSession failed: %v", err)
	}
	if sess.GetState() != attendance.SessionClosed {
		t.Fatalf("expected SessionClosed, got %v", sess.GetState())
	}
}

func TestInputValidations(t *testing.T) {
	_, err := attendance.NewAttendanceSession("s1", "", "Title", 10, nil)
	if !errors.Is(err, attendance.ErrInvalidClassroomID) {
		t.Errorf("expected ErrInvalidClassroomID, got %v", err)
	}

	_, err = attendance.NewAttendanceSession("s1", "room1", "", 10, nil)
	if !errors.Is(err, attendance.ErrInvalidSessionTitle) {
		t.Errorf("expected ErrInvalidSessionTitle, got %v", err)
	}

	sess, err := attendance.NewAttendanceSession("s1", "room1", "Title", 10, nil)
	if err != nil {
		t.Fatalf("NewAttendanceSession failed: %v", err)
	}
	_ = sess.Start()

	err = sess.CheckIn(attendance.CheckInRequest{StudentID: "", DeviceID: "d1"})
	if !errors.Is(err, attendance.ErrInvalidStudentID) {
		t.Errorf("expected ErrInvalidStudentID, got %v", err)
	}

	err = sess.CheckIn(attendance.CheckInRequest{StudentID: "s1", DeviceID: ""})
	if !errors.Is(err, attendance.ErrInvalidDeviceID) {
		t.Errorf("expected ErrInvalidDeviceID, got %v", err)
	}

	err = sess.CheckIn(attendance.CheckInRequest{StudentID: "s1", DeviceID: "d1", SeatRow: -1, SeatCol: 0})
	if !errors.Is(err, attendance.ErrInvalidSeatPosition) {
		t.Errorf("expected ErrInvalidSeatPosition, got %v", err)
	}
}
