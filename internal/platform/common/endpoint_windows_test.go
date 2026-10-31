//go:build windows

package common

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"
)

func TestWindowsEndpointRoundTrip(t *testing.T) {
	pipeName := fmt.Sprintf(`\\.\pipe\localmesh-endpoint-test-%d`, time.Now().UnixNano())
	endpoint := defaultEndpoint()

	listener, err := endpoint.Listen(context.Background(), pipeName)
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	defer listener.Close()

	done := make(chan error, 1)
	go func() {
		conn, err := listener.Accept(context.Background())
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()

		buf := make([]byte, 4)
		if _, err := io.ReadFull(conn, buf); err != nil {
			done <- err
			return
		}
		_, err = conn.Write([]byte("pong"))
		done <- err
	}()

	conn, err := endpoint.Dial(context.Background(), pipeName)
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	resp := make([]byte, 4)
	if _, err := io.ReadFull(conn, resp); err != nil {
		t.Fatalf("ReadFull failed: %v", err)
	}
	if string(resp) != "pong" {
		t.Fatalf("got %q, want pong", string(resp))
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("server failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("endpoint communication timed out")
	}
}
