package broadcast

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"sync"
	"time"
)

// Magic bytes identifying LocalMesh Broadcast Chunk: "LMBC"
var ChunkMagic = [4]byte{'L', 'M', 'B', 'C'}

const (
	CurrentChunkVersion uint8 = 1

	// Chunk flags
	FlagNone      uint8 = 0
	FlagFECParity uint8 = 0x01

	// Chunk header sizes and network constraints.
	// MTU <= 1400 bytes to prevent IP-level fragmentation across standard LAN/WiFi switches.
	MaxPacketSize     = 1400
	DefaultChunkSize  = 1200
	MaxChunkPayload   = 1300
	FixedHeaderSize   = 47 // 4(magic) + 1(ver) + 1(flags) + 8(seq) + 8(ts) + 4(w) + 4(h) + 4(idx) + 4(tot) + 4(crc) + 1(idLen) + 4(payLen)
	MaxDeviceIDLength = 64
)

var (
	ErrEmptyPayload        = errors.New("frame payload is empty")
	ErrChunkTooLarge       = errors.New("chunk size exceeds maximum MTU-safe payload size")
	ErrInvalidChunkSize    = errors.New("chunk size must be positive")
	ErrInvalidDimensions   = errors.New("frame dimensions must be positive")
	ErrEmptySourceDeviceID = errors.New("source device ID cannot be empty")
	ErrSourceDeviceTooLong = errors.New("source device ID exceeds maximum length")
	ErrChecksumMismatch    = errors.New("chunk CRC32 checksum mismatch")
	ErrInvalidChunk        = errors.New("invalid chunk index or total chunks")
	ErrInvalidMagic        = errors.New("invalid chunk magic bytes")
	ErrInvalidHeaderSize   = errors.New("packet data too small for chunk header")
	ErrPacketTooLarge      = errors.New("marshaled packet exceeds MTU limit")
	ErrFrameExpired        = errors.New("frame assembly timed out or sequence superseded")
)

// ScreenFrame represents a single captured screen frame for broadcasting.
type ScreenFrame struct {
	Sequence       uint64    `json:"sequence"`
	Timestamp      time.Time `json:"timestamp"`
	SourceDeviceID string    `json:"source_device_id"`
	Width          int       `json:"width"`
	Height         int       `json:"height"`
	Payload        []byte    `json:"payload"`
}

// PacketChunk represents a single MTU-safe slice of a ScreenFrame.
// Designed for UDP multicast/broadcast delivery where MTU <= 1400 bytes.
type PacketChunk struct {
	Sequence       uint64    `json:"sequence"`
	Timestamp      time.Time `json:"timestamp"`
	SourceDeviceID string    `json:"source_device_id"`
	Width          int       `json:"width"`
	Height         int       `json:"height"`
	ChunkIndex     int       `json:"chunk_index"`
	TotalChunks    int       `json:"total_chunks"`
	Flags          uint8     `json:"flags,omitempty"`
	CRC32          uint32    `json:"crc32"`
	Payload        []byte    `json:"payload"`
}

// VerifyCRC returns true if the chunk payload's IEEE CRC32 matches chunk.CRC32.
func (c *PacketChunk) VerifyCRC() bool {
	return crc32.ChecksumIEEE(c.Payload) == c.CRC32
}

// MarshalBinary serializes PacketChunk into a compact binary packet conforming to MTU <= 1400 bytes.
func (c *PacketChunk) MarshalBinary() ([]byte, error) {
	devIDBytes := []byte(c.SourceDeviceID)
	if len(devIDBytes) > MaxDeviceIDLength {
		return nil, ErrSourceDeviceTooLong
	}

	totalLen := FixedHeaderSize + len(devIDBytes) + len(c.Payload)
	if totalLen > MaxPacketSize {
		return nil, fmt.Errorf("%w: total packet size %d > %d", ErrPacketTooLarge, totalLen, MaxPacketSize)
	}

	buf := make([]byte, totalLen)
	copy(buf[0:4], ChunkMagic[:])
	buf[4] = CurrentChunkVersion
	buf[5] = c.Flags

	binary.BigEndian.PutUint64(buf[6:14], c.Sequence)
	binary.BigEndian.PutUint64(buf[14:22], uint64(c.Timestamp.UnixNano()))
	binary.BigEndian.PutUint32(buf[22:26], uint32(c.Width))
	binary.BigEndian.PutUint32(buf[26:30], uint32(c.Height))
	binary.BigEndian.PutUint32(buf[30:34], uint32(c.ChunkIndex))
	binary.BigEndian.PutUint32(buf[34:38], uint32(c.TotalChunks))
	binary.BigEndian.PutUint32(buf[38:42], c.CRC32)

	buf[42] = uint8(len(devIDBytes))
	offset := 43
	copy(buf[offset:offset+len(devIDBytes)], devIDBytes)
	offset += len(devIDBytes)

	binary.BigEndian.PutUint32(buf[offset:offset+4], uint32(len(c.Payload)))
	offset += 4
	copy(buf[offset:], c.Payload)

	return buf, nil
}

// UnmarshalBinary deserializes a binary packet into PacketChunk.
func (c *PacketChunk) UnmarshalBinary(data []byte) error {
	if len(data) < FixedHeaderSize {
		return ErrInvalidHeaderSize
	}

	if data[0] != ChunkMagic[0] || data[1] != ChunkMagic[1] || data[2] != ChunkMagic[2] || data[3] != ChunkMagic[3] {
		return ErrInvalidMagic
	}

	c.Flags = data[5]
	c.Sequence = binary.BigEndian.Uint64(data[6:14])
	tsNano := int64(binary.BigEndian.Uint64(data[14:22]))
	c.Timestamp = time.Unix(0, tsNano)
	c.Width = int(binary.BigEndian.Uint32(data[22:26]))
	c.Height = int(binary.BigEndian.Uint32(data[26:30]))
	c.ChunkIndex = int(binary.BigEndian.Uint32(data[30:34]))
	c.TotalChunks = int(binary.BigEndian.Uint32(data[34:38]))
	c.CRC32 = binary.BigEndian.Uint32(data[38:42])

	devIDLen := int(data[42])
	offset := 43
	if len(data) < offset+devIDLen+4 {
		return ErrInvalidHeaderSize
	}

	c.SourceDeviceID = string(data[offset : offset+devIDLen])
	offset += devIDLen

	payloadLen := int(binary.BigEndian.Uint32(data[offset : offset+4]))
	offset += 4

	if len(data) < offset+payloadLen {
		return ErrInvalidHeaderSize
	}

	c.Payload = make([]byte, payloadLen)
	copy(c.Payload, data[offset:offset+payloadLen])

	return nil
}

// ChunkFrame fragments a ScreenFrame into MTU-safe PacketChunks.
// If chunkSize is <= 0, DefaultChunkSize (1200 bytes) is used.
func ChunkFrame(frame ScreenFrame, chunkSize int) ([]PacketChunk, error) {
	if chunkSize <= 0 {
		chunkSize = DefaultChunkSize
	}
	if chunkSize > MaxChunkPayload {
		return nil, ErrChunkTooLarge
	}
	if frame.Width <= 0 || frame.Height <= 0 {
		return nil, ErrInvalidDimensions
	}
	if frame.SourceDeviceID == "" {
		return nil, ErrEmptySourceDeviceID
	}
	if len(frame.SourceDeviceID) > MaxDeviceIDLength {
		return nil, ErrSourceDeviceTooLong
	}

	payloadLen := len(frame.Payload)
	if payloadLen == 0 {
		// Empty frame payload still yields 1 chunk with 0-length payload.
		chunk := PacketChunk{
			Sequence:       frame.Sequence,
			Timestamp:      frame.Timestamp,
			SourceDeviceID: frame.SourceDeviceID,
			Width:          frame.Width,
			Height:         frame.Height,
			ChunkIndex:     0,
			TotalChunks:    1,
			CRC32:          crc32.ChecksumIEEE(nil),
			Payload:        []byte{},
		}
		return []PacketChunk{chunk}, nil
	}

	totalChunks := (payloadLen + chunkSize - 1) / chunkSize
	chunks := make([]PacketChunk, 0, totalChunks)

	for i := 0; i < totalChunks; i++ {
		start := i * chunkSize
		end := start + chunkSize
		if end > payloadLen {
			end = payloadLen
		}

		chunkPayload := make([]byte, end-start)
		copy(chunkPayload, frame.Payload[start:end])

		chunk := PacketChunk{
			Sequence:       frame.Sequence,
			Timestamp:      frame.Timestamp,
			SourceDeviceID: frame.SourceDeviceID,
			Width:          frame.Width,
			Height:         frame.Height,
			ChunkIndex:     i,
			TotalChunks:    totalChunks,
			CRC32:          crc32.ChecksumIEEE(chunkPayload),
			Payload:        chunkPayload,
		}
		chunks = append(chunks, chunk)
	}

	return chunks, nil
}

// pendingFrame tracks chunks being assembled for a single ScreenFrame.
type pendingFrame struct {
	sequence       uint64
	timestamp      time.Time
	sourceDeviceID string
	width          int
	height         int
	totalChunks    int
	receivedCount  int
	chunks         map[int][]byte
	parityChunks   []PacketChunk
	firstSeen      time.Time
	lastUpdated    time.Time
}

// FrameReassembler assembles out-of-order PacketChunks into complete ScreenFrames.
// Evicts expired incomplete frames after a configurable timeout and discards corrupted chunks.
type FrameReassembler struct {
	mu        sync.Mutex
	timeout   time.Duration
	frames    map[uint64]*pendingFrame
	completed map[uint64]time.Time
	clock     func() time.Time
}

// NewFrameReassembler creates a FrameReassembler with the given frame completion timeout.
// If timeout <= 0, a default of 1 second is used.
func NewFrameReassembler(timeout time.Duration) *FrameReassembler {
	if timeout <= 0 {
		timeout = 1 * time.Second
	}
	return &FrameReassembler{
		timeout:   timeout,
		frames:    make(map[uint64]*pendingFrame),
		completed: make(map[uint64]time.Time),
		clock:     time.Now,
	}
}

// AddChunk processes an incoming PacketChunk:
// 1. Validates chunk parameters and verifies CRC32 checksum (discards on mismatch).
// 2. Evicts expired in-progress frames.
// 3. Assembles chunk into its corresponding frame slot.
// 4. If parity chunks are present and data chunks are missing, applies FEC recovery.
// 5. If all chunks for the frame have arrived or been recovered, returns the reassembled ScreenFrame.
func (r *FrameReassembler) AddChunk(chunk PacketChunk) (*ScreenFrame, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 1. Validation & CRC Check
	isParity := chunk.Flags&FlagFECParity != 0
	if chunk.TotalChunks <= 0 || chunk.ChunkIndex < 0 {
		return nil, ErrInvalidChunk
	}
	if !isParity && chunk.ChunkIndex >= chunk.TotalChunks {
		return nil, ErrInvalidChunk
	}
	if !chunk.VerifyCRC() {
		return nil, ErrChecksumMismatch
	}

	now := r.clock()

	// 2. Evict expired frames and old completed markers
	r.evictExpiredLocked(now)

	// Check if this chunk belongs to an already completed sequence
	if _, alreadyCompleted := r.completed[chunk.Sequence]; alreadyCompleted {
		return nil, nil
	}

	// 3. Find or create pending frame
	pf, exists := r.frames[chunk.Sequence]
	if !exists {
		pf = &pendingFrame{
			sequence:       chunk.Sequence,
			timestamp:      chunk.Timestamp,
			sourceDeviceID: chunk.SourceDeviceID,
			width:          chunk.Width,
			height:         chunk.Height,
			totalChunks:    chunk.TotalChunks,
			chunks:         make(map[int][]byte, chunk.TotalChunks),
			parityChunks:   nil,
			firstSeen:      now,
			lastUpdated:    now,
		}
		r.frames[chunk.Sequence] = pf
	} else {
		// Consistency check
		if pf.totalChunks != chunk.TotalChunks {
			return nil, ErrInvalidChunk
		}
		pf.lastUpdated = now
	}

	// Store chunk according to whether it is a data chunk or parity chunk
	if !isParity {
		if _, alreadyHave := pf.chunks[chunk.ChunkIndex]; !alreadyHave {
			pf.chunks[chunk.ChunkIndex] = chunk.Payload
			pf.receivedCount++
		}
	} else {
		alreadyHaveParity := false
		for _, p := range pf.parityChunks {
			if p.ChunkIndex == chunk.ChunkIndex {
				alreadyHaveParity = true
				break
			}
		}
		if !alreadyHaveParity {
			pf.parityChunks = append(pf.parityChunks, chunk)
		}
	}

	// 4. If missing data chunks and parity chunks exist, attempt FEC recovery
	if pf.receivedCount < pf.totalChunks && len(pf.parityChunks) > 0 {
		r.tryFECRecoveryLocked(pf)
	}

	// 5. Check if frame is complete
	if pf.receivedCount == pf.totalChunks {
		totalLen := 0
		for i := 0; i < pf.totalChunks; i++ {
			totalLen += len(pf.chunks[i])
		}

		fullPayload := make([]byte, totalLen)
		offset := 0
		for i := 0; i < pf.totalChunks; i++ {
			chunkData := pf.chunks[i]
			copy(fullPayload[offset:], chunkData)
			offset += len(chunkData)
		}

		frame := &ScreenFrame{
			Sequence:       pf.sequence,
			Timestamp:      pf.timestamp,
			SourceDeviceID: pf.sourceDeviceID,
			Width:          pf.width,
			Height:         pf.height,
			Payload:        fullPayload,
		}

		delete(r.frames, chunk.Sequence)
		r.completed[chunk.Sequence] = now

		return frame, nil
	}

	return nil, nil
}

// tryFECRecoveryLocked inspects received parity chunks and recovers missing data chunks in each FEC group.
func (r *FrameReassembler) tryFECRecoveryLocked(pf *pendingFrame) {
	for _, p := range pf.parityChunks {
		var hdr FECHeader
		if err := hdr.UnmarshalBinary(p.Payload); err != nil {
			continue
		}

		start := int(hdr.StartChunk)
		k := int(hdr.DataCount)
		m := int(hdr.TotalParity)

		var missingIndices []int
		for i := 0; i < k; i++ {
			idx := start + i
			if _, ok := pf.chunks[idx]; !ok {
				missingIndices = append(missingIndices, i)
			}
		}

		if len(missingIndices) == 0 {
			continue // All data chunks in this group are already present
		}

		// Collect all parity shards for this group
		groupParities := make(map[int][]byte)
		for _, pc := range pf.parityChunks {
			var curHdr FECHeader
			if err := curHdr.UnmarshalBinary(pc.Payload); err == nil && curHdr.GroupIndex == hdr.GroupIndex {
				hdrSize := FECHeaderFixedSize + int(curHdr.DataCount)*4 + int(curHdr.DataCount)*2
				if len(pc.Payload) >= hdrSize {
					groupParities[int(curHdr.ParityIndex)] = pc.Payload[hdrSize:]
				}
			}
		}

		if len(missingIndices) > len(groupParities) {
			continue // Not enough parity shards to reconstruct this group
		}

		// Prepare shards array for FECDecoder
		shards := make([][]byte, k+m)
		for i := 0; i < k; i++ {
			idx := start + i
			if data, ok := pf.chunks[idx]; ok {
				padded := make([]byte, hdr.MaxChunkLen)
				copy(padded, data)
				shards[i] = padded
			}
		}
		for pIdx, pData := range groupParities {
			if pIdx < m {
				padded := make([]byte, hdr.MaxChunkLen)
				copy(padded, pData)
				shards[k+pIdx] = padded
			}
		}

		decoder, err := NewFECDecoder(k, m)
		if err != nil {
			continue
		}

		if err := decoder.DecodeWithCRC(shards, hdr.DataCRCs); err != nil {
			continue
		}

		// Reconstructed successfully: put back into pf.chunks
		for _, missingIdx := range missingIndices {
			origLen := int(hdr.ChunkSizes[missingIdx])
			if origLen > len(shards[missingIdx]) {
				origLen = len(shards[missingIdx])
			}
			data := make([]byte, origLen)
			copy(data, shards[missingIdx][:origLen])

			pf.chunks[start+missingIdx] = data
			pf.receivedCount++
		}
	}
}

// EvictExpired removes in-progress frames that have exceeded the timeout window.
// Returns the number of evicted frames.
func (r *FrameReassembler) EvictExpired() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.evictExpiredLocked(r.clock())
}

func (r *FrameReassembler) evictExpiredLocked(now time.Time) int {
	evicted := 0
	for seq, pf := range r.frames {
		if now.Sub(pf.firstSeen) > r.timeout {
			delete(r.frames, seq)
			evicted++
		}
	}
	for seq, completedAt := range r.completed {
		if now.Sub(completedAt) > r.timeout*2 {
			delete(r.completed, seq)
		}
	}
	return evicted
}

// ActiveFrames returns the count of currently in-progress frames.
func (r *FrameReassembler) ActiveFrames() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.frames)
}

// Reset clears all in-progress frames.
func (r *FrameReassembler) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.frames = make(map[uint64]*pendingFrame)
	r.completed = make(map[uint64]time.Time)
}
