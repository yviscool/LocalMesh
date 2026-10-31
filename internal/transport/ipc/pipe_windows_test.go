//go:build windows

package ipc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"testing"
	"time"
)

func TestNamedPipeListener_NormalizeName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"my-pipe", `\\.\pipe\my-pipe`},
		{`\\.\pipe\my-pipe`, `\\.\pipe\my-pipe`},
		{`\my-pipe`, `\\.\pipe\my-pipe`},
		{`\\.\my-pipe`, `\\.\pipe\my-pipe`},
	}
	for _, tt := range tests {
		got := normalizePipeName(tt.input)
		if got != tt.want {
			t.Errorf("normalizePipeName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestNamedPipeListener_SequentialConnections(t *testing.T) {
	pipeName := fmt.Sprintf(`\\.\pipe\localmesh-test-seq-%d`, time.Now().UnixNano())
	listener, err := ListenNamedPipe(pipeName)
	if err != nil {
		t.Fatalf("ListenNamedPipe failed: %v", err)
	}
	defer listener.Close()

	for i := 0; i < 5; i++ {
		msg := fmt.Sprintf("hello-seq-%d", i)
		done := make(chan error, 1)

		go func() {
			conn, err := listener.Accept(context.Background())
			if err != nil {
				done <- err
				return
			}
			defer conn.Close()

			buf := make([]byte, len(msg))
			if _, err := io.ReadFull(conn, buf); err != nil {
				done <- err
				return
			}
			if string(buf) != msg {
				done <- fmt.Errorf("server got %q, want %q", string(buf), msg)
				return
			}
			_, err = conn.Write(buf)
			done <- err
		}()

		client, err := os.OpenFile(pipeName, os.O_RDWR, 0)
		if err != nil {
			t.Fatalf("client open failed on iter %d: %v", i, err)
		}

		if _, err := client.Write([]byte(msg)); err != nil {
			t.Fatalf("client write failed on iter %d: %v", i, err)
		}

		resp := make([]byte, len(msg))
		if _, err := io.ReadFull(client, resp); err != nil {
			t.Fatalf("client read failed on iter %d: %v", i, err)
		}
		if string(resp) != msg {
			t.Fatalf("client got %q, want %q on iter %d", string(resp), msg, i)
		}
		client.Close()

		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("server failed on iter %d: %v", i, err)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("server timeout on iter %d", i)
		}
	}
}

func TestNamedPipeListener_Cancellation(t *testing.T) {
	pipeName := fmt.Sprintf(`\\.\pipe\localmesh-test-cancel-%d`, time.Now().UnixNano())
	listener, err := ListenNamedPipe(pipeName)
	if err != nil {
		t.Fatalf("ListenNamedPipe failed: %v", err)
	}
	defer listener.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	conn, err := listener.Accept(ctx)
	elapsed := time.Since(start)

	if conn != nil {
		conn.Close()
		t.Fatal("expected nil conn on cancellation")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got %v (elapsed %v)", err, elapsed)
	}
}

func TestNamedPipeListener_ReacceptAfterCancellation(t *testing.T) {
	pipeName := fmt.Sprintf(`\\.\pipe\localmesh-test-reaccept-%d`, time.Now().UnixNano())
	listener, err := ListenNamedPipe(pipeName)
	if err != nil {
		t.Fatalf("ListenNamedPipe failed: %v", err)
	}
	defer listener.Close()

	// 1. Cancel first accept
	ctx1, cancel1 := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel1()
	_, err = listener.Accept(ctx1)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}

	// 2. Accept again and connect successfully
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

	client, err := os.OpenFile(pipeName, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("client open failed: %v", err)
	}
	defer client.Close()

	if _, err := client.Write([]byte("ping")); err != nil {
		t.Fatalf("client write failed: %v", err)
	}

	resp := make([]byte, 4)
	if _, err := io.ReadFull(client, resp); err != nil {
		t.Fatalf("client read failed: %v", err)
	}
	if string(resp) != "pong" {
		t.Fatalf("client got %q, want pong", string(resp))
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("server failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server timeout")
	}
}

func TestNamedPipeListener_PreConnect(t *testing.T) {
	pipeName := fmt.Sprintf(`\\.\pipe\localmesh-test-preconn-%d`, time.Now().UnixNano())
	listener, err := ListenNamedPipe(pipeName)
	if err != nil {
		t.Fatalf("ListenNamedPipe failed: %v", err)
	}
	defer listener.Close()

	// Client opens pipe before server calls Accept
	client, err := os.OpenFile(pipeName, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("pre-connect client open failed: %v", err)
	}
	defer client.Close()

	conn, err := listener.Accept(context.Background())
	if err != nil {
		t.Fatalf("server Accept failed on pre-connected pipe: %v", err)
	}
	defer conn.Close()

	if _, err := client.Write([]byte("data")); err != nil {
		t.Fatalf("client write failed: %v", err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("server read failed: %v", err)
	}
	if string(buf) != "data" {
		t.Fatalf("server got %q, want data", string(buf))
	}
}

func TestNamedPipeListener_CloseUnblocksAccept(t *testing.T) {
	pipeName := fmt.Sprintf(`\\.\pipe\localmesh-test-close-%d`, time.Now().UnixNano())
	listener, err := ListenNamedPipe(pipeName)
	if err != nil {
		t.Fatalf("ListenNamedPipe failed: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := listener.Accept(context.Background())
		done <- err
	}()

	time.Sleep(30 * time.Millisecond)
	if err := listener.Close(); err != nil {
		t.Fatalf("listener.Close failed: %v", err)
	}

	select {
	case err := <-done:
		if !errors.Is(err, io.EOF) {
			t.Fatalf("expected io.EOF on close, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("listener.Close did not unblock Accept")
	}

	// Subsequent Accept after Close returns EOF immediately
	_, err = listener.Accept(context.Background())
	if !errors.Is(err, io.EOF) {
		t.Fatalf("subsequent Accept did not return io.EOF: %v", err)
	}
}
