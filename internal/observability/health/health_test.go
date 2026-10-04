package health

import (
	"context"
	"testing"
)

type probe Status

func (p probe) Check(context.Context) Check { return Check{Name: "test", Status: Status(p)} }
func TestOverallHealth(t *testing.T) {
	if Overall([]Check{{Status: StatusHealthy}, {Status: StatusDegraded}}) != StatusDegraded {
		t.Fatal("degraded status lost")
	}
	if Overall([]Check{{Status: StatusFailed}}) != StatusFailed {
		t.Fatal("failed status lost")
	}
}
