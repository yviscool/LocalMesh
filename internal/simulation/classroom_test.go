package simulation

import "testing"

func TestRunOneHundredAgents(t *testing.T) {
	report, err := Run(100)
	if err != nil {
		t.Fatal(err)
	}
	if report.Agents != 100 || report.Discovered != 100 || report.ActiveSessions != 100 || report.Reconnected != 100 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if !report.StableDeviceIDs {
		t.Fatal("device IDs must survive address changes")
	}
	if report.DuplicateSessions {
		t.Fatal("reconnect must not create duplicate active session IDs")
	}
}

func TestRunRejectsEmptySimulation(t *testing.T) {
	if _, err := Run(0); err == nil {
		t.Fatal("Run(0) should fail")
	}
}
