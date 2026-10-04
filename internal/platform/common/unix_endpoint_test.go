//go:build !windows

package common

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUnixEndpointRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.sock")
	endpoint := UnixEndpoint{}
	listener, err := endpoint.Listen(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode = %o, want 600", info.Mode().Perm())
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
		_, err = io.CopyN(io.Discard, conn, 4)
		done <- err
	}()
	conn, err := endpoint.Dial(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("unix endpoint did not accept")
	}
}
