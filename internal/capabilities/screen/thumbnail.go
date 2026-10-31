package screen

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"sort"
	"sync"
	"time"
)

var (
	ErrInvalidThumbnailInput = errors.New("invalid thumbnail input parameters")
	ErrDeviceIDRequired      = errors.New("device id is required")
)

// Thumbnail represents a cached preview frame of a device screen.
type Thumbnail struct {
	DeviceID  string      `json:"device_id"`
	Width     int         `json:"width"`
	Height    int         `json:"height"`
	Format    FrameFormat `json:"format"`
	Data      []byte      `json:"data"`
	Timestamp time.Time   `json:"timestamp"`
	Sequence  uint64      `json:"sequence"`
}

// CalculateThumbnailDimensions calculates downscaled dimensions maintaining aspect ratio.
func CalculateThumbnailDimensions(srcW, srcH, maxW, maxH int) (int, int) {
	if srcW <= 0 || srcH <= 0 || maxW <= 0 || maxH <= 0 {
		return 0, 0
	}

	if srcW <= maxW && srcH <= maxH {
		return srcW, srcH
	}

	ratioW := float64(maxW) / float64(srcW)
	ratioH := float64(maxH) / float64(srcH)
	ratio := ratioW
	if ratioH < ratioW {
		ratio = ratioH
	}

	dstW := int(float64(srcW) * ratio)
	dstH := int(float64(srcH) * ratio)
	if dstW < 1 {
		dstW = 1
	}
	if dstH < 1 {
		dstH = 1
	}
	return dstW, dstH
}

// DownscaleRGBA resizes raw RGBA bytes (4 bytes per pixel) using bilinear interpolation.
func DownscaleRGBA(src []byte, srcW, srcH, dstW, dstH int) ([]byte, error) {
	expectedSrcLen := srcW * srcH * 4
	if len(src) < expectedSrcLen || srcW <= 0 || srcH <= 0 || dstW <= 0 || dstH <= 0 {
		return nil, ErrInvalidThumbnailInput
	}

	dst := make([]byte, dstW*dstH*4)

	xRatio := float64(srcW-1) / float64(dstW)
	yRatio := float64(srcH-1) / float64(dstH)
	if dstW == 1 {
		xRatio = 0
	}
	if dstH == 1 {
		yRatio = 0
	}

	for y := 0; y < dstH; y++ {
		srcY := int(float64(y) * yRatio)
		yDiff := (float64(y) * yRatio) - float64(srcY)
		nextY := srcY + 1
		if nextY >= srcH {
			nextY = srcY
		}

		for x := 0; x < dstW; x++ {
			srcX := int(float64(x) * xRatio)
			xDiff := (float64(x) * xRatio) - float64(srcX)
			nextX := srcX + 1
			if nextX >= srcW {
				nextX = srcX
			}

			// Indices for 4 neighboring pixels (A, B, C, D)
			idxA := (srcY*srcW + srcX) * 4
			idxB := (srcY*srcW + nextX) * 4
			idxC := (nextY*srcW + srcX) * 4
			idxD := (nextY*srcW + nextX) * 4

			dstIdx := (y*dstW + x) * 4

			for c := 0; c < 4; c++ {
				valA := float64(src[idxA+c])
				valB := float64(src[idxB+c])
				valC := float64(src[idxC+c])
				valD := float64(src[idxD+c])

				// Bilinear formula:
				// val = A*(1-w)*(1-h) + B*(w)*(1-h) + C*(h)*(1-w) + D*(w*h)
				val := valA*(1-xDiff)*(1-yDiff) +
					valB*(xDiff)*(1-yDiff) +
					valC*(yDiff)*(1-xDiff) +
					valD*(xDiff*yDiff)

				if val < 0 {
					val = 0
				} else if val > 255 {
					val = 255
				}
				dst[dstIdx+c] = byte(val + 0.5)
			}
		}
	}

	return dst, nil
}

// DownscaleImage scales an image.Image into an RGBA thumbnail.
func DownscaleImage(src image.Image, maxW, maxH int) *image.RGBA {
	bounds := src.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()

	dstW, dstH := CalculateThumbnailDimensions(srcW, srcH, maxW, maxH)
	if dstW == 0 || dstH == 0 {
		return image.NewRGBA(image.Rect(0, 0, 1, 1))
	}

	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))

	// Fast path: if dimensions match, draw directly
	if srcW == dstW && srcH == dstH {
		draw.Draw(dst, dst.Bounds(), src, bounds.Min, draw.Src)
		return dst
	}

	// Bilinear resampling
	xRatio := float64(srcW-1) / float64(dstW)
	yRatio := float64(srcH-1) / float64(dstH)
	if dstW == 1 {
		xRatio = 0
	}
	if dstH == 1 {
		yRatio = 0
	}

	for y := 0; y < dstH; y++ {
		sy := int(float64(y) * yRatio)
		yDiff := (float64(y) * yRatio) - float64(sy)
		nextY := sy + 1
		if nextY >= srcH {
			nextY = sy
		}

		for x := 0; x < dstW; x++ {
			sx := int(float64(x) * xRatio)
			xDiff := (float64(x) * xRatio) - float64(sx)
			nextX := sx + 1
			if nextX >= srcW {
				nextX = sx
			}

			rA, gA, bA, aA := src.At(bounds.Min.X+sx, bounds.Min.Y+sy).RGBA()
			rB, gB, bB, aB := src.At(bounds.Min.X+nextX, bounds.Min.Y+sy).RGBA()
			rC, gC, bC, aC := src.At(bounds.Min.X+sx, bounds.Min.Y+nextY).RGBA()
			rD, gD, bD, aD := src.At(bounds.Min.X+nextX, bounds.Min.Y+nextY).RGBA()

			interp := func(a, b, c, d uint32) uint8 {
				fA := float64(a >> 8)
				fB := float64(b >> 8)
				fC := float64(c >> 8)
				fD := float64(d >> 8)
				v := fA*(1-xDiff)*(1-yDiff) +
					fB*(xDiff)*(1-yDiff) +
					fC*(yDiff)*(1-xDiff) +
					fD*(xDiff*yDiff)
				if v < 0 {
					return 0
				}
				if v > 255 {
					return 255
				}
				return uint8(v + 0.5)
			}

			dst.SetRGBA(x, y, color.RGBA{
				R: interp(rA, rB, rC, rD),
				G: interp(gA, gB, gC, gD),
				B: interp(bA, bB, bC, bD),
				A: interp(aA, aB, aC, aD),
			})
		}
	}

	return dst
}

// ThumbnailCache collects and caches student desktop previews for teacher grid monitoring.
type ThumbnailCache struct {
	mu           sync.RWMutex
	maxW         int
	maxH         int
	ttl          time.Duration
	items        map[string]*Thumbnail
	maxCacheSize int
}

// NewThumbnailCache creates a collector with maximum thumbnail bounding dimensions and TTL.
func NewThumbnailCache(maxWidth, maxHeight int, ttl time.Duration) *ThumbnailCache {
	if maxWidth <= 0 {
		maxWidth = 320
	}
	if maxHeight <= 0 {
		maxHeight = 180
	}
	if ttl <= 0 {
		ttl = 30 * time.Second
	}

	return &ThumbnailCache{
		maxW:         maxWidth,
		maxH:         maxHeight,
		ttl:          ttl,
		items:        make(map[string]*Thumbnail),
		maxCacheSize: 500,
	}
}

// Set stores a pre-computed thumbnail for a device.
func (c *ThumbnailCache) Set(thumb *Thumbnail) error {
	if thumb == nil || thumb.DeviceID == "" {
		return ErrDeviceIDRequired
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.items[thumb.DeviceID] = thumb
	return nil
}

// Get returns the thumbnail for a device if present and unexpired.
func (c *ThumbnailCache) Get(deviceID string) (*Thumbnail, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	thumb, exists := c.items[deviceID]
	if !exists {
		return nil, false
	}
	if c.ttl > 0 && time.Since(thumb.Timestamp) > c.ttl {
		return nil, false
	}
	return thumb, true
}

// Delete removes a device's thumbnail.
func (c *ThumbnailCache) Delete(deviceID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, deviceID)
}

// Count returns the number of active thumbnails.
func (c *ThumbnailCache) Count() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

// List returns all active thumbnails sorted by DeviceID.
func (c *ThumbnailCache) List() []*Thumbnail {
	c.mu.RLock()
	defer c.mu.RUnlock()

	now := time.Now()
	out := make([]*Thumbnail, 0, len(c.items))
	for _, thumb := range c.items {
		if c.ttl > 0 && now.Sub(thumb.Timestamp) > c.ttl {
			continue
		}
		out = append(out, thumb)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].DeviceID < out[j].DeviceID
	})
	return out
}

// CleanExpired purges thumbnails older than maxAge.
func (c *ThumbnailCache) CleanExpired(maxAge time.Duration) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	cleared := 0
	for id, thumb := range c.items {
		if now.Sub(thumb.Timestamp) > maxAge {
			delete(c.items, id)
			cleared++
		}
	}
	return cleared
}

// UpdateImage decodes or resamples an image.Image, encodes it to JPEG, and updates the cache.
func (c *ThumbnailCache) UpdateImage(deviceID string, img image.Image, seq uint64) (*Thumbnail, error) {
	if deviceID == "" {
		return nil, ErrDeviceIDRequired
	}
	if img == nil {
		return nil, ErrInvalidThumbnailInput
	}

	scaled := DownscaleImage(img, c.maxW, c.maxH)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, scaled, &jpeg.Options{Quality: 65}); err != nil {
		return nil, err
	}

	thumb := &Thumbnail{
		DeviceID:  deviceID,
		Width:     scaled.Rect.Dx(),
		Height:    scaled.Rect.Dy(),
		Format:    FormatJPEG,
		Data:      buf.Bytes(),
		Timestamp: time.Now(),
		Sequence:  seq,
	}

	c.mu.Lock()
	c.items[deviceID] = thumb
	c.mu.Unlock()

	return thumb, nil
}

// UpdateRaw processes raw RGBA or JPEG/PNG frame bytes and stores the thumbnail.
func (c *ThumbnailCache) UpdateRaw(deviceID string, rawData []byte, format FrameFormat, srcW, srcH int, seq uint64) (*Thumbnail, error) {
	if deviceID == "" {
		return nil, ErrDeviceIDRequired
	}
	if len(rawData) == 0 {
		return nil, ErrInvalidThumbnailInput
	}

	switch format {
	case FormatRawRGBA:
		dstW, dstH := CalculateThumbnailDimensions(srcW, srcH, c.maxW, c.maxH)
		downscaled, err := DownscaleRGBA(rawData, srcW, srcH, dstW, dstH)
		if err != nil {
			return nil, err
		}

		// Convert downscaled RGBA to JPEG thumbnail for bandwidth efficiency
		rgbaImg := &image.RGBA{
			Pix:    downscaled,
			Stride: dstW * 4,
			Rect:   image.Rect(0, 0, dstW, dstH),
		}
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, rgbaImg, &jpeg.Options{Quality: 65}); err != nil {
			return nil, err
		}

		thumb := &Thumbnail{
			DeviceID:  deviceID,
			Width:     dstW,
			Height:    dstH,
			Format:    FormatJPEG,
			Data:      buf.Bytes(),
			Timestamp: time.Now(),
			Sequence:  seq,
		}

		c.mu.Lock()
		c.items[deviceID] = thumb
		c.mu.Unlock()
		return thumb, nil

	case FormatJPEG, FormatPNG:
		reader := bytes.NewReader(rawData)
		var img image.Image
		var err error
		if format == FormatJPEG {
			img, err = jpeg.Decode(reader)
		} else {
			img, err = png.Decode(reader)
		}
		if err != nil {
			return nil, err
		}
		return c.UpdateImage(deviceID, img, seq)

	default:
		return nil, ErrUnsupportedFormat
	}
}
