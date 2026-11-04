package main

import (
	"bytes"
	"context"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"localmesh/internal/capabilities/broadcast"
)

// TestBroadcastReceiverFeedUpdate exercises the agent-side broadcastFeed:
// put wakes MJPEG writers and snapshot copies the latest frame.
func TestBroadcastReceiverFeedUpdate(t *testing.T) {
	feed := newBroadcastFeed()

	if _, _, _, ok := feed.snapshot(); ok {
		t.Fatal("empty feed must report no frame")
	}

	wake := feed.wakeCh()
	frame1 := []byte{1, 2, 3}
	feed.put(&broadcast.ScreenFrame{Sequence: 1, Payload: frame1, Width: 4, Height: 4})

	got, w, h, ok := feed.snapshot()
	if !ok || !bytes.Equal(got, frame1) || w != 4 || h != 4 {
		t.Fatalf("snapshot mismatch: ok=%v w=%d h=%d", ok, w, h)
	}

	select {
	case <-wake:
	default:
		t.Fatal("put did not wake stream writers")
	}

	wake2 := feed.wakeCh()
	feed.put(&broadcast.ScreenFrame{Sequence: 2, Payload: []byte{9}, Width: 2, Height: 2})
	got, _, _, _ = feed.snapshot()
	if !bytes.Equal(got, []byte{9}) {
		t.Fatal("second frame not stored")
	}
	select {
	case <-wake2:
	default:
		t.Fatal("second put did not wake")
	}
}

// TestBroadcastMJPEGStream is the real deal: it drives the screen server's
// broadcast endpoint over HTTP, parses the multipart stream back, and
// asserts frames pushed into the feed arrive on the wire.
func TestBroadcastMJPEGStream(t *testing.T) {
	feed := newBroadcastFeed()

	srv := newScreenServer("127.0.0.1:0", feed)
	ts := httptest.NewServer(srv.srv.Handler)
	defer ts.Close()
	defer srv.srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/broadcast.mjpg", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	respCh := make(chan *http.Response, 1)
	go func() {
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return
		}
		respCh <- resp
	}()

	// The handler blocks writing until frames arrive; give it a beat, then
	// feed two frames and parse what lands on the wire.
	time.Sleep(300 * time.Millisecond)
	frames := [][]byte{{0xFF, 0xD8, 0x01}, {0xFF, 0xD8, 0x02}}
	for i, f := range frames {
		feed.put(&broadcast.ScreenFrame{Sequence: uint64(i + 1), Payload: f, Width: 8, Height: 8})
	}

	var resp *http.Response
	select {
	case resp = <-respCh:
	case <-ctx.Done():
		t.Fatal("no response")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	mediaType, params, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/x-mixed-replace" {
		t.Fatalf("content type %q", resp.Header.Get("Content-Type"))
	}

	mr := multipart.NewReader(resp.Body, params["boundary"])
	received := make([][]byte, 0, 4)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for len(received) < 3 {
			part, err := mr.NextPart()
			if err != nil {
				return
			}
			data, err := io.ReadAll(io.LimitReader(part, 1<<20))
			part.Close()
			if err != nil || len(data) == 0 {
				continue
			}
			received = append(received, data)
		}
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("timeout: received %d parts", len(received))
	}

	// latest-frame-wins 语义：高速连发时可能跳过中间帧，但每个在网的帧
	// 都必须是真实投喂的帧，且最后一帧必然是 frames[1]。
	for _, got := range received {
		valid := bytes.Equal(got, frames[0]) || bytes.Equal(got, frames[1])
		if !valid {
			t.Fatalf("unexpected payload on the wire: %v", got)
		}
	}
	if !bytes.Equal(received[len(received)-1], frames[1]) {
		t.Fatalf("latest frame must eventually arrive, got %v", received)
	}
}
