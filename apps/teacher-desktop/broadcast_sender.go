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
	"sync"
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

// screenStream is the live broadcast producer: one capture loop feeding one
// or more multicast senders (main stream + per-group fan-out).
type screenStream struct {
	cancel   context.CancelFunc
	loop     *broadcast.CaptureSendLoop
	sender   *broadcast.MulticastSender
	provider *hybridProvider
	source   string
	groupMu  sync.Mutex
	groupTX  map[string]*broadcast.MulticastSender // groupID -> sender
}

// fanOutGroup creates a group multicast sender that receives the same
// encoded frames as the main stream (encoded once, sent N times).
func (s *screenStream) fanOutGroup(groupID string, port int) error {
	s.groupMu.Lock()
	defer s.groupMu.Unlock()
	if _, ok := s.groupTX[groupID]; ok {
		return nil
	}
	addr := groupBroadcastAddr(port)
	tx, err := broadcast.NewMulticastSender(broadcast.MulticastSenderConfig{
		TargetAddr: addr,
		FEC:        broadcast.DefaultFECConfig(),
	})
	if err != nil {
		return err
	}
	s.groupTX[groupID] = tx
	// 把组发送器挂进循环：循环每帧同时写主地址与组地址
	if s.loop != nil {
		s.loop.AttachSender(tx)
	}
	log.Printf("[Broadcast] group %s fan-out on %s", groupID, addr)
	return nil
}

func (s *screenStream) stopGroups() {
	s.groupMu.Lock()
	defer s.groupMu.Unlock()
	for id, tx := range s.groupTX {
		_ = tx.Close()
		if s.loop != nil {
			s.loop.DetachSender(tx)
		}
		delete(s.groupTX, id)
	}
}

// groupBroadcastAddr derives the per-group multicast endpoint.
func groupBroadcastAddr(port int) string {
	return fmt.Sprintf("239.255.42.1:%d", port)
}

// stats
type streamStats struct {
	frames uint64
}

func (s *screenStream) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	if s.provider != nil {
		_ = s.provider.capturer.Close()
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
	stream := &screenStream{cancel: cancel, sender: sender, source: source, groupTX: map[string]*broadcast.MulticastSender{}}

	c.mu.Lock()
	c.screenStream = stream
	c.mu.Unlock()

	if source == "teacher" {
		provider, err := newHybridProvider()
		if err != nil {
			cancel()
			_ = sender.Close()
			c.mu.Lock()
			c.screenStream = nil
			c.mu.Unlock()
			return fmt.Errorf("init capturer: %w", err)
		}
		stream.provider = provider
		stream.loop = broadcast.NewCaptureSendLoop(broadcast.LoopOptions{
			SourceID: "teacher",
			FPS:      c.cfg.Broadcast.FPS,
			Quality:  c.cfg.Broadcast.Quality,
		}, provider, sender, c.cfg.Broadcast.BudgetMBPerSec*(1<<20))
		go stream.loop.Run(streamCtx)
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
		stream.stopGroups()
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

// hybridProvider adapts the DXGI/GDI hybrid capturer to the shared
// broadcast.FrameProvider interface.
type hybridProvider struct {
	capturer screen.HybridCapturer
}

func newHybridProvider() (*hybridProvider, error) {
	cap, err := screen.NewHybridCapturer()
	if err != nil {
		return nil, err
	}
	return &hybridProvider{capturer: cap}, nil
}

func (p *hybridProvider) CaptureJPEG(quality int) ([]byte, int, int, bool, error) {
	frame, err := p.capturer.Capture()
	if err != nil {
		if err == screen.ErrNoNewFrame {
			return nil, 0, 0, false, nil
		}
		return nil, 0, 0, false, err
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, frame.Image, &jpeg.Options{Quality: quality}); err != nil {
		return nil, 0, 0, false, err
	}
	return buf.Bytes(), frame.Width, frame.Height, true, nil
}

// broadcastBudgetBytes 默认推流带宽预算（4 MB/s，令牌桶按半桶突发）。
const broadcastBudgetBytes = 4 << 20

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

// fanOutToGroup creates the group sender on the running stream.
func (c *Console) fanOutToGroup(groupID string, port int) error {
	c.mu.Lock()
	stream := c.screenStream
	c.mu.Unlock()
	if stream == nil {
		return fmt.Errorf("no active screen stream")
	}
	return stream.fanOutGroup(groupID, port)
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