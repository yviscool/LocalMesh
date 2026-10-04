package network

import (
	"context"
	"testing"
)

func TestScenarioIsDeterministicForLossAndDuplication(t *testing.T) {
	messages := make([][]byte, 100)
	for i := range messages {
		messages[i] = []byte("command")
	}
	a, dropA, dupA, err := RunScenario(context.Background(), Config{Loss: .15, Duplicates: .2, Seed: 42}, messages)
	if err != nil {
		t.Fatal(err)
	}
	b, dropB, dupB, err := RunScenario(context.Background(), Config{Loss: .15, Duplicates: .2, Seed: 42}, messages)
	if err != nil {
		t.Fatal(err)
	}
	if a != b || dropA != dropB || dupA != dupB {
		t.Fatalf("a=%d/%d/%d b=%d/%d/%d", a, dropA, dupA, b, dropB, dupB)
	}
}
