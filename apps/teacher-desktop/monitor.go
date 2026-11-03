package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/egoist/mygo"
)

// ScreenPort is the agent's on-demand HD screen endpoint port. The agent
// announces over UDP from this same host, so the console derives the screen
// URL from the discovery address plus this port.
const ScreenPort = 7450

// screenHTTPTimeout bounds the single-shot snapshot request.
const screenHTTPTimeout = 4 * time.Second

// MonitorService streams on-demand HD screen frames from a student agent.
type MonitorService struct{ c *Console }

// ScreenInfo describes an agent's screen.
type ScreenInfo struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// agentScreenURL resolves the agent's HD screen base URL from the discovery
// registry: the UDP announcement's host plus the fixed screen port.
func (s *MonitorService) agentScreenURL(deviceID string) (string, error) {
	candidates := s.c.discoveryReg.Candidates(time.Now(), 60*time.Second)
	for _, cand := range candidates {
		if string(cand.DeviceID) != deviceID {
			continue
		}
		host := cand.Address.Addr()
		if !host.IsValid() {
			continue
		}
		return fmt.Sprintf("http://%s:%d", host, ScreenPort), nil
	}
	return "", fmt.Errorf("device %s is not online", deviceID)
}

// SnapshotResult is one full-resolution JPEG frame (base64) plus its
// dimensions.
type SnapshotResult struct {
	Frame string     `json:"frame"`
	Info  ScreenInfo `json:"info"`
}

// Snapshot fetches one full-resolution JPEG frame (base64) from the agent.
func (s *MonitorService) Snapshot(_ context.Context, deviceID string) (SnapshotResult, error) {
	base, err := s.agentScreenURL(deviceID)
	if err != nil {
		return SnapshotResult{}, err
	}

	client := &http.Client{Timeout: screenHTTPTimeout}
	resp, err := client.Get(base + "/frame.jpg?quality=80")
	if err != nil {
		return SnapshotResult{}, fmt.Errorf("fetch frame: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return SnapshotResult{}, fmt.Errorf("agent returned %s", resp.Status)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 12<<20))
	if err != nil {
		return SnapshotResult{}, err
	}
	return SnapshotResult{
		Frame: base64.StdEncoding.EncodeToString(data),
		Info: ScreenInfo{
			Width:  atoiDefault(resp.Header.Get("X-Frame-Width")),
			Height: atoiDefault(resp.Header.Get("X-Frame-Height")),
		},
	}, nil
}

// frameSink receives base64 JPEG frames; *mygo.Channel[string] satisfies it.
type frameSink interface {
	Send(frame string) error
}

// Stream relays the agent's MJPEG stream into the page as base64 JPEG
// frames. The channel closes when the page stops iterating (which also
// cancels the upstream request) or the context ends.
func (s *MonitorService) Stream(ctx context.Context, deviceID string, fps int, frames *mygo.Channel[string]) error {
	base, err := s.agentScreenURL(deviceID)
	if err != nil {
		return err
	}
	if fps <= 0 {
		fps = 10
	}
	if fps > 30 {
		fps = 30
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/stream.mjpg?fps=%d&quality=80", base, fps), nil)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return fmt.Errorf("open stream: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("agent returned %s", resp.Status)
	}
	return relayMJPEG(ctx, resp.Body, resp.Header.Get("Content-Type"), frames)
}

// relayMJPEG parses a multipart MJPEG body and forwards each JPEG frame,
// base64-encoded, into sink. Clean stream end and client cancel return nil;
// malformed streams propagate their error.
func relayMJPEG(_ context.Context, body io.Reader, contentType string, sink frameSink) error {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
		return fmt.Errorf("unexpected content type %q", contentType)
	}

	mr := multipart.NewReader(body, params["boundary"])
	for {
		part, err := mr.NextPart()
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, context.Canceled) {
				return nil // stream ended or client went away
			}
			return fmt.Errorf("read part: %w", err)
		}
		data, err := io.ReadAll(io.LimitReader(part, 12<<20))
		part.Close()
		if err != nil {
			// 连接中断或流被截断：保留已收内容，视为正常结束
			return nil
		}
		if len(data) == 0 {
			continue
		}
		if err := sink.Send(base64.StdEncoding.EncodeToString(data)); err != nil {
			return nil // page stopped listening
		}
	}
}

// ScreenInfoFor reports the agent's screen dimensions.
func (s *MonitorService) ScreenInfoFor(ctx context.Context, deviceID string) (ScreenInfo, error) {
	base, err := s.agentScreenURL(deviceID)
	if err != nil {
		return ScreenInfo{}, err
	}
	client := &http.Client{Timeout: screenHTTPTimeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/screen/info", nil)
	if err != nil {
		return ScreenInfo{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return ScreenInfo{}, err
	}
	defer resp.Body.Close()
	info := ScreenInfo{}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return ScreenInfo{}, err
	}
	return info, nil
}

// WindowService exposes window-level controls to the page. They act on the
// window that made the call, so monitor child windows control themselves.
type WindowService struct{ c *Console }

// ToggleFullscreen switches the caller's window in or out of OS fullscreen.
// Returns the resulting fullscreen state.
func (s *WindowService) ToggleFullscreen(ctx context.Context) bool {
	w := mygo.CallerWindow(ctx)
	if w == nil {
		return false
	}
	w.ToggleFullScreen()
	return w.IsFullScreen()
}

// IsFullscreen reports whether the caller's window is OS fullscreen.
func (s *WindowService) IsFullscreen(ctx context.Context) bool {
	w := mygo.CallerWindow(ctx)
	return w != nil && w.IsFullScreen()
}

func atoiDefault(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}
