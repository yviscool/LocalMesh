package command

import (
	"testing"
	"time"
)

func TestCommandAggregate(t *testing.T) {
	tests := []struct {
		name    string
		results []Result
		want    ResultStatus
	}{
		{name: "all succeed", results: []Result{{Status: ResultSucceeded}, {Status: ResultSucceeded}}, want: ResultSucceeded},
		{name: "partial success", results: []Result{{Status: ResultSucceeded}, {Status: ResultFailed}}, want: ResultStatus("partially_succeeded")},
		{name: "all fail", results: []Result{{Status: ResultTimedOut}}, want: ResultFailed},
		{name: "empty", want: ResultFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Aggregate(tt.results); got != tt.want {
				t.Fatalf("Aggregate() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestNewCommandRequiresPolicyAndTarget(t *testing.T) {
	_, err := New("cmd-1", "process.launch", "launch", Target{Kind: TargetGroup, ID: "group-a"}, time.Now().Add(time.Minute), RetryPolicy{MaxAttempts: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New("", "process.launch", "launch", Target{Kind: TargetGroup, ID: "group-a"}, time.Now().Add(time.Minute), RetryPolicy{MaxAttempts: 1}); err != ErrInvalidCommand {
		t.Fatalf("New() error = %v, want %v", err, ErrInvalidCommand)
	}
}
