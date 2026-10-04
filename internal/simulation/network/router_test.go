package network

import "testing"

func TestRouterKeepsClassroomsIsolated(t *testing.T) {
	router := NewRouter()
	router.Add("class-a", "device-a")
	router.Add("class-b", "device-b")
	if !router.Route("class-a", "device-a") || !router.Route("class-b", "device-b") {
		t.Fatal("valid classroom route rejected")
	}
	if router.Route("class-a", "device-b") {
		t.Fatal("cross-classroom route accepted")
	}
	if router.IsolationViolations() != 1 {
		t.Fatalf("violations = %d, want 1", router.IsolationViolations())
	}
}
