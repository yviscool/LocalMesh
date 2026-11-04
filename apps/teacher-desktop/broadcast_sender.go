package main

import (
	"bytes"
	"context"
	"fmt"
	"image/jpeg"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"time"

	"localmesh/internal/capabilities/broadcast"
	"localmesh/internal/capabilities/screen"
	"localmesh/internal/classroom"
)

const (
	broadcastFPS          = 15
	broadcastJPEGQuality  = 70
	relayConnectTimeout   = 4 * time.Second
	broadcastLogThrottle  = 10 * time.Second
)

// screenStream is the live teacher-screen (or student-demo relay) producer.
type screenStream struct {
	cancel   context.CancelFunc
	sender   *broadcast.MulticastSender
	capturer screen.HybridCapturer
	source   string
}

// stats
type streamStats struct {
	frames uint64
}

func (s *screenStream) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	if s.capturer != nil {
		_ = s.capturer.Close()
	}
	if s.sender != nil {
		_ = s.sender.Close()
	}
}

// startScreenStream begins multicasting the given source ("teacher" for the
// local screen, or a device id whose MJPEG endpoint is relayed) on the
// classroom's channel-derived multicast address.
func (c *Console) startScreenStream(source string) error {
	c.mu.Lock()
	if c.screenStream != nil {
		c.mu.Unlock()
		return fmt.Errorf("screen stream already running")
	}
	c.mu.Unlock()

	addr := c.currentScreenMulticast()
	sender, err := broadcast.NewMulticastSender(broadcast.MulticastSenderConfig{
		TargetAddr: addr,
		FEC:        broadcast.DefaultFECConfig(),
	})
	if err != nil {
		return fmt.Errorf("init multicast sender: %w", err)
	}

	streamCtx, cancel := context.WithCancel(context.Background())
	stream := &screenStream{cancel: cancel, sender: sender, source: source}

	c.mu.Lock()
	c.screenStream = stream
	c.mu.Unlock()

	if source == "teacher" {
		go runCaptureLoop(streamCtx, stream)
	} else {
		go runRelayLoop(streamCtx, stream, c)
	}
	log.Printf("[Broadcast] screen stream started (source=%s, target=%s)", source, addr)
	return nil
}

// stopScreenStream tears the stream down; safe when not running.
func (c *Console) stopScreenStream() {
	c.mu.Lock()
	stream := c.screenStream
	c.screenStream = nil
	c.mu.Unlock()
	if stream != nil {
		stream.Stop()
		log.Printf("[Broadcast] screen stream stopped")
	}
}

func (c *Console) currentScreenMulticast() string {
	c.mu.Lock()
	ch := c.channelID
	c.mu.Unlock()
	return classroom.DeriveMulticastAddr(ch, broadcast.DefaultMulticastAddr)
}

// runCaptureLoop captures the local screen at a fixed rate and multicasts
// JPEG frames with FEC.
func runCaptureLoop(ctx context.Context, stream *screenStream) {
	capturer, err := screen.NewHybridCapturer()
	if err != nil {
		log.Printf("[Broadcast] capturer init failed: %v", err)
		return
	}
	stream.capturer = capturer

	ticker := time.NewTicker(time.Second / broadcastFPS)
	defer ticker.Stop()

	var seq uint64
	var lastErrLog time.Time

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		frame, err := capturer.Capture()
		if err != nil {
			if err == screen.ErrNoNewFrame {
				continue // nothing changed — reuse of cached frame not needed
			}
			if time.Since(lastErrLog) >= broadcastLogThrottle {
				log.Printf("[Broadcast] capture error: %v", err)
				lastErrLog = time.Now()
			}
			continue
		}

		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, frame.Image, &jpeg.Options{Quality: broadcastJPEGQuality}); err != nil {
			continue
		}

		seq++
		sf := broadcast.ScreenFrame{
			Sequence:       seq,
			Timestamp:      time.Now(),
			SourceDeviceID: "teacher",
			Width:          frame.Width,
			Height:         frame.Height,
			Payload:        buf.Bytes(),
		}
		if err := stream.sender.SendFrame(ctx, sf); err != nil {
			if ctx.Err() != nil {
				return
			}
			if time.Since(lastErrLog) >= broadcastLogThrottle {
				log.Printf("[Broadcast] send error: %v", err)
				lastErrLog = time.Now()
			}
		}
	}
}

// runRelayLoop relays a student agent's MJPEG stream onto the multicast
// group for 学生演示 — JPEG payloads pass through without re-encoding.
func runRelayLoop(ctx context.Context, stream *screenStream, c *Console) {
	deviceID := stream.source
	client := &http.Client{Timeout: relayConnectTimeout}
	resp, err := client.Get(fmt.Sprintf("http://%s/stream.mjpg?fps=%d", resolveAgentHost(c, deviceID), broadcastFPS))
	if err != nil {
		log.Printf("[Broadcast] relay connect to %s failed: %v", deviceID, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("[Broadcast] relay: agent returned %s", resp.Status)
		return
	}

	mediaType, params, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/x-mixed-replace" {
		log.Printf("[Broadcast] relay: unexpected content type %q", resp.Header.Get("Content-Type"))
		return
	}

	mr := multipart.NewReader(resp.Body, params["boundary"])
	var seq uint64

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		part, err := mr.NextPart()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
				// agent stream ended — reconnect loop
				go runRelayLoop(ctx, stream, c)
				return
			}
		}
		payload, err := io.ReadAll(io.LimitReader(part, 12<<20))
		part.Close()
		if err != nil || len(payload) == 0 {
			continue
		}

		w := part.Header.Get("X-Frame-Width")
		h := part.Header.Get("X-Frame-Height")
		seq++
		sf := broadcast.ScreenFrame{
			Sequence:       seq,
			Timestamp:      time.Now(),
			SourceDeviceID: deviceID,
			Width:          atoiSafe(w),
			Height:         atoiSafe(h),
			Payload:        payload,
		}
		if err := stream.sender.SendFrame(ctx, sf); err != nil {
			if ctx.Err() != nil {
				return
			}
		}
	}
}

// broadcastFeed mux-guard shared state lives in main (mutex-guarded fields).
func atoiSafe(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// resolveAgentHost finds the agent's discovery host for deviceID.
func resolveAgentHost(c *Console, deviceID string) string {
	for _, cand := range c.discoveryReg.Candidates(time.Now(), 60*time.Second) {
		if string(cand.DeviceID) == deviceID && cand.Address.IsValid() {
			return fmt.Sprintf("%s:%d", cand.Address.Addr(), ScreenPort)
		}
	}
	return fmt.Sprintf("127.0.0.1:%d", ScreenPort)
}