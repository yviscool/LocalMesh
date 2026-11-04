package main

import (
	"bytes"
	"context"
	"fmt"
	"image/jpeg"
	"log"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"localmesh/internal/capabilities/screen"
)

const (
	thumbnailPushInterval = 2 * time.Second
	thumbnailMaxWidth     = 320
	thumbnailMaxHeight    = 180
	thumbnailJPEGQuality  = 70
	thumbnailLogThrottle  = 30 * time.Second
)

// thumbnailController holds the runtime-tunable sampling parameters that
// the teacher console pushes via the command channel.
type thumbnailController struct {
	intervalMS atomic.Int64
	quality    atomic.Int64
}

func (t *thumbnailController) interval() time.Duration {
	if v := t.intervalMS.Load(); v >= 500 {
		return time.Duration(v) * time.Millisecond
	}
	return thumbnailPushInterval
}

func (t *thumbnailController) jpegQuality() int {
	if v := int(t.quality.Load()); v >= 20 && v <= 95 {
		return v
	}
	return thumbnailJPEGQuality
}

// startThumbnailPusher periodically captures the local screen, downscales it to
// a thumbnail, and pushes the JPEG frame to the teacher console HTTP endpoint.
// Capture or push failures are logged at most once per throttle window and the
// loop keeps running so the feed recovers automatically.
func (a *AgentApp) startThumbnailPusher(ctx context.Context, teacherURL string) {
	url := fmt.Sprintf("%s/api/v1/thumbnails/push", strings.TrimSuffix(teacherURL, "/"))
	client := &http.Client{Timeout: 3 * time.Second}

	var seq uint64
	var lastLog time.Time

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var lastPush time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-a.discoveryStop:
			return
		case <-ticker.C:
		}
		if time.Since(lastPush) < a.thumbCtl.interval() {
			continue
		}

		if err := a.pushThumbnailFrame(client, url, &seq); err != nil {
			if time.Since(lastLog) >= thumbnailLogThrottle {
				log.Printf("[Thumbnail] push to %s failed: %v (further logs throttled)", url, err)
				lastLog = time.Now()
			}
		} else {
			lastPush = time.Now()
		}
	}
}

// pushThumbnailFrame captures one frame and uploads it as JPEG.
func (a *AgentApp) pushThumbnailFrame(client *http.Client, url string, seq *uint64) error {
	img, err := screen.CaptureScreen()
	if err != nil {
		return fmt.Errorf("capture screen: %w", err)
	}

	thumb := screen.DownscaleImage(img, thumbnailMaxWidth, thumbnailMaxHeight)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, thumb, &jpeg.Options{Quality: thumbnailJPEGQuality}); err != nil {
		return fmt.Errorf("encode jpeg: %w", err)
	}

	*seq++
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(buf.Bytes()))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "image/jpeg")
	req.Header.Set("X-Device-ID", string(a.deviceID))
	req.Header.Set("X-Frame-Seq", fmt.Sprintf("%d", *seq))

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("post frame: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("teacher console returned %s", resp.Status)
	}
	return nil
}
