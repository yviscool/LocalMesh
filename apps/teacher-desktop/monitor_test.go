package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeMJPEGAgent serves an MJPEG stream of n distinct frames.
func fakeMJPEGAgent(t *testing.T, n int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=frame")
		flusher := w.(http.Flusher)
		for i := 0; i < n; i++ {
			img := image.NewRGBA(image.Rect(0, 0, 8, 8))
			img.Set(i, 0, color.RGBA{R: 255, A: 255})
			var buf bytes.Buffer
			if err := jpeg.Encode(&buf, img, nil); err != nil {
				t.Fatalf("encode: %v", err)
			}
			fmt.Fprintf(w, "\r\n--frame\r\nContent-Type: image/jpeg\r\nContent-Length: %d\r\n\r\n", buf.Len())
			if _, err := w.Write(buf.Bytes()); err != nil {
				return
			}
			flusher.Flush()
		}
		fmt.Fprint(w, "\r\n--frame--\r\n")
	}))
}

type collectingSink struct {
	frames []string
}

func (s *collectingSink) Send(frame string) error {
	s.frames = append(s.frames, frame)
	return nil
}

// TestRelayMJPEG verifies the multipart parser feeds every frame into the
// sink and terminates cleanly at stream end.
func TestRelayMJPEG(t *testing.T) {
	const wantFrames = 3
	srv := fakeMJPEGAgent(t, wantFrames)
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("get stream: %v", err)
	}
	defer resp.Body.Close()

	sink := &collectingSink{}
	if err := relayMJPEG(context.Background(), resp.Body, resp.Header.Get("Content-Type"), sink); err != nil {
		t.Fatalf("relay returned error: %v", err)
	}

	if len(sink.frames) != wantFrames {
		t.Fatalf("expected %d frames, got %d", wantFrames, len(sink.frames))
	}
	for i, frame := range sink.frames {
		if frame == "" {
			t.Fatalf("frame %d is empty", i)
		}
	}
}

// TestRelayMJPEGRejectsNonMultipart guards against silently consuming a
// non-streaming response.
func TestRelayMJPEGRejectsNonMultipart(t *testing.T) {
	sink := &collectingSink{}
	err := relayMJPEG(context.Background(), bytes.NewReader(nil), "image/jpeg", sink)
	if err == nil {
		t.Fatal("expected error for non-multipart content type")
	}
	if len(sink.frames) != 0 {
		t.Fatalf("expected no frames, got %d", len(sink.frames))
	}
}
