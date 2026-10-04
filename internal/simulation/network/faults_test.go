package network

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestFaultInjectorIsDeterministicAndCopiesPayload(t *testing.T) {
	config := Config{Latency: 100 * time.Millisecond, Duplicates: 1, Seed: 7}
	first, _ := New(config)
	second, _ := New(config)
	a, err := first.Transmit(context.Background(), []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := second.Transmit(context.Background(), []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != len(b) || len(a) != 2 || string(a[0].Payload) != string(b[0].Payload) {
		t.Fatalf("a=%#v b=%#v", a, b)
	}
	a[0].Payload[0] = 'x'
	if string(a[1].Payload) != "hello" {
		t.Fatal("duplicate delivery shares payload storage")
	}
}

func TestFaultInjectorReportsLossAndCancellation(t *testing.T) {
	dropped, _ := New(Config{Loss: 1, Seed: 1})
	if _, err := dropped.Transmit(context.Background(), []byte("x")); !errors.Is(err, ErrDropped) {
		t.Fatalf("loss error = %v", err)
	}
	cancelled, _ := New(Config{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := cancelled.Transmit(ctx, []byte("x")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error = %v", err)
	}
}
