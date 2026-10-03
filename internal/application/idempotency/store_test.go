package idempotency

import (
	"sync"
	"testing"
)

func TestStoreReplaysCompletedResult(t *testing.T) {
	s := NewStore()
	if got := s.Begin("key-1", "hash-a"); got.Status != StatusNew {
		t.Fatalf("first Begin() = %s, want %s", got.Status, StatusNew)
	}
	if !s.Complete("key-1", "hash-a", []byte("ok")) {
		t.Fatal("Complete() should accept original fingerprint")
	}
	got := s.Begin("key-1", "hash-a")
	if got.Status != StatusReplayed || string(got.OriginalData) != "ok" {
		t.Fatalf("replay = %+v", got)
	}
}

func TestStoreRejectsFingerprintConflict(t *testing.T) {
	s := NewStore()
	_ = s.Begin("key-1", "hash-a")
	if got := s.Begin("key-1", "hash-b"); got.Status != StatusConflict {
		t.Fatalf("conflict = %s, want %s", got.Status, StatusConflict)
	}
}

func TestStoreConcurrentBeginHasOneOwner(t *testing.T) {
	s := NewStore()
	statuses := make(chan Status, 20)
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			statuses <- s.Begin("key-1", "hash-a").Status
		}()
	}
	wg.Wait()
	close(statuses)
	newCount := 0
	for status := range statuses {
		if status == StatusNew {
			newCount++
		}
	}
	if newCount != 1 {
		t.Fatalf("new owners = %d, want 1", newCount)
	}
}
