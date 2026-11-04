package main

import (
	"context"
	"log"
	"sync"
	"time"

	"localmesh/internal/capabilities/broadcast"
	"localmesh/internal/classroom"
)

// broadcastFeed holds the latest received broadcast frame and a change
// signal so the local MJPEG endpoint can stream it to the student UI.
type broadcastFeed struct {
	mu       sync.RWMutex
	jpeg     []byte
	width    int
	height   int
	seq      uint64
	updated  time.Time
	wake     chan struct{} // closed+replaced on every new frame
	sourceID string

	// UI notification state: silent → active → silent transitions are
	// pushed to the student UI over the local IPC pipe.
	uiActive     bool
	notify       func(map[string]any)
	lastNotified time.Time
}

func newBroadcastFeed() *broadcastFeed {
	return &broadcastFeed{wake: make(chan struct{})}
}

// setNotifier wires the best-effort student UI notifier (agent-only).
func (f *broadcastFeed) setNotifier(fn func(map[string]any)) {
	f.mu.Lock()
	f.notify = fn
	f.mu.Unlock()
}

// uiBroadcastState tracks broadcast liveness for UI notifications: a frame
// marks it active; 5s of silence marks it inactive.
func (f *broadcastFeed) uiBroadcastState() (active bool, changed bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now()
	live := now.Sub(f.updated) < 5*time.Second && f.jpeg != nil
	if live != f.uiActive {
		f.uiActive = live
		return live, true
	}
	return f.uiActive, false
}

// put stores a freshly reassembled frame and wakes all MJPEG writers.
func (f *broadcastFeed) put(frame *broadcast.ScreenFrame) {
	f.mu.Lock()
	f.jpeg = frame.Payload
	f.width, f.height = frame.Width, frame.Height
	f.seq = frame.Sequence
	f.sourceID = frame.SourceDeviceID
	f.updated = time.Now()
	close(f.wake)
	f.wake = make(chan struct{})
	f.mu.Unlock()
}

// snapshot copies the latest frame; ok=false when none has arrived yet.
func (f *broadcastFeed) snapshot() (jpeg []byte, w, h int, ok bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if f.jpeg == nil {
		return nil, 0, 0, false
	}
	out := make([]byte, len(f.jpeg))
	copy(out, f.jpeg)
	return out, f.width, f.height, true
}

// wakeCh returns the current change signal channel.
func (f *broadcastFeed) wakeCh() <-chan struct{} {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.wake
}

// broadcastReceiver joins the classroom multicast group and feeds the feed.
type broadcastReceiver struct {
	mu     sync.Mutex
	cancel context.CancelFunc
	feed   *broadcastFeed
}

func newBroadcastReceiver(feed *broadcastFeed) *broadcastReceiver {
	return &broadcastReceiver{feed: feed}
}

// retune (re)joins the multicast group for the given channel; called on
// start and on channel follow.
func (r *broadcastReceiver) retune(channelID int) {
	r.retuneAddr(classroom.DeriveMulticastAddr(channelID, broadcast.DefaultMulticastAddr))
}

// retuneAddr (re)joins an explicit multicast endpoint (groups use
// port-offset addresses on the same channel octet).
func (r *broadcastReceiver) retuneAddr(addr string) {
	r.mu.Lock()
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.mu.Unlock()

	go func() {
		receiver, err := broadcast.NewMulticastReceiver(broadcast.MulticastReceiverConfig{
			ListenAddr:         addr,
			ReassemblerTimeout: 700 * time.Millisecond,
		})
		if err != nil {
			log.Printf("[BroadcastRX] listen %s failed: %v", addr, err)
			return
		}
		defer receiver.Close()
		log.Printf("[BroadcastRX] joined %s", addr)

		frameCh, errCh := receiver.Start(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case err := <-errCh:
				if err != nil {
					log.Printf("[BroadcastRX] receiver error: %v", err)
					return
				}
			case frame := <-frameCh:
				if frame != nil {
					r.feed.put(frame)
				}
			}
		}
	}()

	// UI 状态监视：开播/停播转换推给学生 UI
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if active, changed := r.feed.uiBroadcastState(); changed {
					r.feed.notifyState(active)
				}
			}
		}
	}()
}

// stop tears the receiver down.
func (r *broadcastReceiver) stop() {
	r.mu.Lock()
	cancel := r.cancel
	r.cancel = nil
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// notifyState pushes a broadcast start/stop notification to the student UI.
func (f *broadcastFeed) notifyState(active bool) {
	f.mu.RLock()
	notify := f.notify
	f.mu.RUnlock()
	if notify == nil {
		return
	}
	title := "屏幕广播已开始"
	if !active {
		title = "屏幕广播已结束"
	}
	notify(map[string]any{"type": "broadcast", "active": active, "title": title})
}
