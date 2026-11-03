package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"log"
	"net/http"
	"strconv"
	"time"

	"localmesh/internal/capabilities/screen"
)

const (
	// DefaultScreenHTTPAddr is the on-demand HD screen endpoint. The teacher
	// console derives it from the agent's discovery IP plus this port.
	DefaultScreenHTTPAddr = "0.0.0.0:7450"

	defaultStreamFPS     = 10
	maxStreamFPS         = 30
	defaultFrameQuality  = 70
	defaultMaxFrameWidth = 1920
)

// screenServer serves on-demand full-resolution screen frames:
//
//	GET /frame.jpg          one full-resolution JPEG snapshot
//	GET /stream.mjpg?fps=10 multipart MJPEG live stream
//	GET /screen/info        JSON {width, height}
type screenServer struct {
	srv *http.Server
}

func newScreenServer(addr string) *screenServer {
	mux := http.NewServeMux()
	s := &screenServer{
		srv: &http.Server{Addr: addr, Handler: mux},
	}

	mux.HandleFunc("GET /frame.jpg", s.handleFrame)
	mux.HandleFunc("GET /stream.mjpg", s.handleStream)
	mux.HandleFunc("GET /screen/info", s.handleInfo)
	return s
}

// Start launches the screen endpoint in the background.
func (s *screenServer) Start() {
	go func() {
		log.Printf("[Screen] HD screen endpoint listening (LAN)")
		if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[Screen] server error: %v", err)
		}
	}()
}

// Stop shuts the endpoint down.
func (s *screenServer) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = s.srv.Shutdown(ctx)
}

// captureJPEG grabs one screen frame, downscaled to maxFrameWidth, encoded
// at the requested quality.
func captureJPEG(quality int) ([]byte, int, int, error) {
	img, err := screen.CaptureScreen()
	if err != nil {
		return nil, 0, 0, fmt.Errorf("capture: %w", err)
	}

	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	dstW, dstH := screen.CalculateThumbnailDimensions(w, h, defaultMaxFrameWidth, 0)
	if dstW <= 0 {
		dstW, dstH = w, h
	}
	if dstW != w || dstH != h {
		img = screen.DownscaleImage(img, dstW, dstH)
		bounds = img.Bounds()
		w, h = bounds.Dx(), bounds.Dy()
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, 0, 0, fmt.Errorf("encode: %w", err)
	}
	return buf.Bytes(), w, h, nil
}

func (s *screenServer) handleFrame(w http.ResponseWriter, r *http.Request) {
	quality := queryInt(r, "quality", defaultFrameQuality, 1, 100)
	data, w_, h_, err := captureJPEG(quality)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("X-Frame-Width", strconv.Itoa(w_))
	w.Header().Set("X-Frame-Height", strconv.Itoa(h_))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

func (s *screenServer) handleStream(w http.ResponseWriter, r *http.Request) {
	fps := queryInt(r, "fps", defaultStreamFPS, 1, maxStreamFPS)
	quality := queryInt(r, "quality", defaultFrameQuality, 1, 100)
	interval := time.Second / time.Duration(fps)

	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=localmeshframe")
	w.Header().Set("Cache-Control", "no-store")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	ctx := r.Context()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Send the first frame immediately, then on the cadence.
	for {
		data, fw, fh, err := captureJPEG(quality)
		if err == nil {
			fmt.Fprintf(w, "\r\n--localmeshframe\r\nContent-Type: image/jpeg\r\nContent-Length: %d\r\nX-Frame-Width: %d\r\nX-Frame-Height: %d\r\n\r\n", len(data), fw, fh)
			if _, err := w.Write(data); err != nil {
				return // client went away
			}
			flusher.Flush()
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *screenServer) handleInfo(w http.ResponseWriter, _ *http.Request) {
	img, err := screen.CaptureScreen()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	b := img.Bounds()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"width": b.Dx(), "height": b.Dy()})
}

func queryInt(r *http.Request, key string, def, min, max int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}
