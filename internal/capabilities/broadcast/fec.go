package broadcast

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
)

var (
	ErrInvalidShards     = errors.New("data and parity shards must be positive, sum <= 255")
	ErrTooManyLostShards = errors.New("too many lost shards to reconstruct data")
	ErrShardSizeMismatch = errors.New("all shards must have identical non-zero size")
	ErrEmptyDataShards   = errors.New("data shard slice is empty")
	ErrInvalidFECHeader  = errors.New("invalid FEC header bytes")
)

// Galois Field GF(2^8) arithmetic using polynomial 0x11d (x^8 + x^4 + x^3 + x^2 + 1).
var (
	gfExp      [512]byte
	gfLog      [256]byte
	gfMulTable [256][256]byte
)

func init() {
	x := 1
	for i := 0; i < 255; i++ {
		gfExp[i] = byte(x)
		gfExp[i+255] = byte(x)
		gfLog[x] = byte(i)
		x = x << 1
		if x&0x100 != 0 {
			x ^= 0x11d
		}
	}
	for a := 0; a < 256; a++ {
		for b := 0; b < 256; b++ {
			if a == 0 || b == 0 {
				gfMulTable[a][b] = 0
			} else {
				gfMulTable[a][b] = gfExp[int(gfLog[a])+int(gfLog[b])]
			}
		}
	}
}

// gfMul multiplies two elements in GF(2^8).
func gfMul(a, b byte) byte {
	return gfMulTable[a][b]
}

// gfDiv divides a by b in GF(2^8). Panics if b == 0.
func gfDiv(a, b byte) byte {
	if b == 0 {
		panic("gfDiv: division by zero in GF(2^8)")
	}
	if a == 0 {
		return 0
	}
	logA := int(gfLog[a])
	logB := int(gfLog[b])
	diff := (logA - logB + 255) % 255
	return gfExp[diff]
}

// gfInv computes multiplicative inverse of a in GF(2^8).
func gfInv(a byte) byte {
	if a == 0 {
		panic("gfInv: inverse of zero in GF(2^8)")
	}
	return gfExp[255-int(gfLog[a])]
}

// buildCauchyMatrix creates an M x K parity matrix where every submatrix is invertible.
// Row 0 is normalized to all 1's so Parity Shard 0 is pure XOR of all data shards.
func buildCauchyMatrix(dataShards, parityShards int) [][]byte {
	m := make([][]byte, parityShards)
	for i := range m {
		m[i] = make([]byte, dataShards)
	}

	// X elements: 0 .. parityShards-1
	// Y elements: parityShards .. parityShards+dataShards-1
	for r := 0; r < parityShards; r++ {
		for c := 0; c < dataShards; c++ {
			if r == 0 {
				// Normalized row 0: all 1's (Pure XOR)
				m[0][c] = 1
			} else {
				// Cauchy formula normalized by row 0:
				// P(r, c) = (X0 ^ Yc) / (Xr ^ Yc)
				x0 := byte(0)
				xr := byte(r)
				yc := byte(parityShards + c)
				num := x0 ^ yc
				den := xr ^ yc
				m[r][c] = gfDiv(num, den)
			}
		}
	}
	return m
}

// FECConfig specifies the error correction configuration.
type FECConfig struct {
	Enabled      bool `json:"enabled"`
	DataShards   int  `json:"data_shards"`   // e.g. 8 or 10
	ParityShards int  `json:"parity_shards"` // e.g. 2 or 1
}

// DefaultFECConfig returns standard 8+2 forward error correction (recovers any 2 lost shards).
func DefaultFECConfig() FECConfig {
	return FECConfig{
		Enabled:      true,
		DataShards:   8,
		ParityShards: 2,
	}
}

// FECEncoder generates M parity shards from K data shards.
type FECEncoder struct {
	dataShards   int
	parityShards int
	matrix       [][]byte
}

// NewFECEncoder creates a new FECEncoder.
func NewFECEncoder(dataShards, parityShards int) (*FECEncoder, error) {
	if dataShards <= 0 || parityShards <= 0 || dataShards+parityShards > 255 {
		return nil, ErrInvalidShards
	}
	matrix := buildCauchyMatrix(dataShards, parityShards)
	return &FECEncoder{
		dataShards:   dataShards,
		parityShards: parityShards,
		matrix:       matrix,
	}, nil
}

// DataShards returns the number of data shards K.
func (e *FECEncoder) DataShards() int {
	return e.dataShards
}

// ParityShards returns the number of parity shards M.
func (e *FECEncoder) ParityShards() int {
	return e.parityShards
}

// Encode generates M parity shards for K input data shards.
func (e *FECEncoder) Encode(data [][]byte) ([][]byte, error) {
	if len(data) != e.dataShards {
		return nil, fmt.Errorf("%w: expected %d data shards, got %d", ErrInvalidShards, e.dataShards, len(data))
	}
	if len(data) == 0 || len(data[0]) == 0 {
		return nil, ErrEmptyDataShards
	}
	shardSize := len(data[0])
	for i := 1; i < len(data); i++ {
		if len(data[i]) != shardSize {
			return nil, ErrShardSizeMismatch
		}
	}

	parity := make([][]byte, e.parityShards)
	for i := range parity {
		parity[i] = make([]byte, shardSize)
	}

	// Row 0 is pure XOR
	for _, d := range data {
		p0 := parity[0]
		for i := 0; i < shardSize; i++ {
			p0[i] ^= d[i]
		}
	}

	// Subsequent parity rows using Cauchy coefficients
	for r := 1; r < e.parityShards; r++ {
		pr := parity[r]
		rowCoeffs := e.matrix[r]
		for c, d := range data {
			coeff := rowCoeffs[c]
			if coeff == 0 {
				continue
			}
			if coeff == 1 {
				for i := 0; i < shardSize; i++ {
					pr[i] ^= d[i]
				}
			} else {
				mulRow := gfMulTable[coeff]
				for i := 0; i < shardSize; i++ {
					pr[i] ^= mulRow[d[i]]
				}
			}
		}
	}

	return parity, nil
}

// FECDecoder reconstructs missing shards from received shards.
type FECDecoder struct {
	dataShards   int
	parityShards int
	matrix       [][]byte
}

// NewFECDecoder creates an FECDecoder.
func NewFECDecoder(dataShards, parityShards int) (*FECDecoder, error) {
	if dataShards <= 0 || parityShards <= 0 || dataShards+parityShards > 255 {
		return nil, ErrInvalidShards
	}
	matrix := buildCauchyMatrix(dataShards, parityShards)
	return &FECDecoder{
		dataShards:   dataShards,
		parityShards: parityShards,
		matrix:       matrix,
	}, nil
}

// Decode reconstructs any missing data shards in shards[0..dataShards-1].
// shards must have length K+M. Missing shards are nil (or empty).
// Requires at least K shards present in total.
func (d *FECDecoder) Decode(shards [][]byte) error {
	totalExpected := d.dataShards + d.parityShards
	if len(shards) != totalExpected {
		return fmt.Errorf("%w: expected %d shards, got %d", ErrInvalidShards, totalExpected, len(shards))
	}

	var shardSize int
	var lostData []int
	var availData []int
	var availParity []int

	for i := 0; i < d.dataShards; i++ {
		if len(shards[i]) == 0 {
			lostData = append(lostData, i)
		} else {
			availData = append(availData, i)
			if shardSize == 0 {
				shardSize = len(shards[i])
			}
		}
	}

	for i := 0; i < d.parityShards; i++ {
		idx := d.dataShards + i
		if len(shards[idx]) > 0 {
			availParity = append(availParity, i)
			if shardSize == 0 {
				shardSize = len(shards[idx])
			}
		}
	}

	if len(lostData) == 0 {
		return nil // No missing data shards
	}

	if len(availData)+len(availParity) < d.dataShards {
		return ErrTooManyLostShards
	}
	if len(lostData) > len(availParity) {
		return ErrTooManyLostShards
	}
	if shardSize == 0 {
		return ErrEmptyDataShards
	}

	// Allocate space for missing data shards
	for _, lostIdx := range lostData {
		shards[lostIdx] = make([]byte, shardSize)
	}

	// FAST PATH: Single data shard loss and Parity 0 (Pure XOR) is available
	if len(lostData) == 1 && len(shards[d.dataShards]) > 0 {
		lostIdx := lostData[0]
		recovered := shards[lostIdx]
		// Start with Parity 0
		copy(recovered, shards[d.dataShards])
		// XOR all available data shards
		for _, idx := range availData {
			dataShard := shards[idx]
			for i := 0; i < shardSize; i++ {
				recovered[i] ^= dataShard[i]
			}
		}
		return nil
	}

	// GENERAL PATH: Solve linear equations over GF(2^8)
	// Pick first len(lostData) available parity shards
	e := len(lostData)
	chosenParities := availParity[:e]

	// Build e x e submatrix A
	subA := make([][]byte, e)
	for r := 0; r < e; r++ {
		subA[r] = make([]byte, e)
		parityIdx := chosenParities[r]
		for c := 0; c < e; c++ {
			dataIdx := lostData[c]
			subA[r][c] = d.matrix[parityIdx][dataIdx]
		}
	}

	// Build RHS vectors S[r] = P[parityIdx] ^ sum(d.matrix[parityIdx][avail] * shards[avail])
	rhs := make([][]byte, e)
	for r := 0; r < e; r++ {
		rhs[r] = make([]byte, shardSize)
		parityIdx := chosenParities[r]
		copy(rhs[r], shards[d.dataShards+parityIdx])

		rowCoeffs := d.matrix[parityIdx]
		for _, dataIdx := range availData {
			coeff := rowCoeffs[dataIdx]
			if coeff == 0 {
				continue
			}
			src := shards[dataIdx]
			if coeff == 1 {
				for i := 0; i < shardSize; i++ {
					rhs[r][i] ^= src[i]
				}
			} else {
				mulRow := gfMulTable[coeff]
				for i := 0; i < shardSize; i++ {
					rhs[r][i] ^= mulRow[src[i]]
				}
			}
		}
	}

	// Solve subA * X = rhs using Gaussian-Jordan elimination
	for col := 0; col < e; col++ {
		// Find pivot
		pivotRow := -1
		for r := col; r < e; r++ {
			if subA[r][col] != 0 {
				pivotRow = r
				break
			}
		}
		if pivotRow == -1 {
			return ErrTooManyLostShards // Singular submatrix (should not happen with Cauchy)
		}

		// Swap rows
		if pivotRow != col {
			subA[col], subA[pivotRow] = subA[pivotRow], subA[col]
			rhs[col], rhs[pivotRow] = rhs[pivotRow], rhs[col]
		}

		// Scale pivot row so subA[col][col] == 1
		pivotVal := subA[col][col]
		if pivotVal != 1 {
			inv := gfInv(pivotVal)
			mulRow := gfMulTable[inv]
			for c := col; c < e; c++ {
				subA[col][c] = gfMulTable[inv][subA[col][c]]
			}
			for i := 0; i < shardSize; i++ {
				rhs[col][i] = mulRow[rhs[col][i]]
			}
		}

		// Eliminate all other rows (above and below)
		for r := 0; r < e; r++ {
			if r == col {
				continue
			}
			factor := subA[r][col]
			if factor == 0 {
				continue
			}

			if factor == 1 {
				for c := col; c < e; c++ {
					subA[r][c] ^= subA[col][c]
				}
				for i := 0; i < shardSize; i++ {
					rhs[r][i] ^= rhs[col][i]
				}
			} else {
				mulRow := gfMulTable[factor]
				for c := col; c < e; c++ {
					subA[r][c] ^= mulRow[subA[col][c]]
				}
				for i := 0; i < shardSize; i++ {
					rhs[r][i] ^= mulRow[rhs[col][i]]
				}
			}
		}
	}

	// Copy solved solutions to reconstructed data shards
	for c := 0; c < e; c++ {
		lostIdx := lostData[c]
		copy(shards[lostIdx], rhs[c])
	}

	return nil
}

// DecodeWithCRC reconstructs missing shards and verifies them against expected CRC32s.
func (d *FECDecoder) DecodeWithCRC(shards [][]byte, crcs []uint32) error {
	var lostData []int
	for i := 0; i < d.dataShards; i++ {
		if len(shards[i]) == 0 {
			lostData = append(lostData, i)
		}
	}

	if err := d.Decode(shards); err != nil {
		return err
	}

	// Verify CRC of all recovered shards
	for _, idx := range lostData {
		if crcs != nil && idx < len(crcs) {
			actual := crc32.ChecksumIEEE(shards[idx])
			if actual != crcs[idx] {
				return fmt.Errorf("%w: shard %d expected %08x got %08x", ErrChecksumMismatch, idx, crcs[idx], actual)
			}
		}
	}

	return nil
}

// FECHeader prefix inside an FEC parity chunk payload.
type FECHeader struct {
	GroupIndex  uint16   `json:"group_index"`
	ParityIndex uint8    `json:"parity_index"`
	TotalParity uint8    `json:"total_parity"`
	StartChunk  uint16   `json:"start_chunk"`
	DataCount   uint16   `json:"data_count"`
	MaxChunkLen uint16   `json:"max_chunk_len"`
	DataCRCs    []uint32 `json:"data_crcs"`
	ChunkSizes  []uint16 `json:"chunk_sizes"`
}

const FECHeaderFixedSize = 10 // 2(grp) + 1(pIdx) + 1(totP) + 2(start) + 2(dCount) + 2(maxLen)

// MarshalBinary encodes FECHeader into binary bytes.
func (h *FECHeader) MarshalBinary() []byte {
	size := FECHeaderFixedSize + len(h.DataCRCs)*4 + len(h.ChunkSizes)*2
	buf := make([]byte, size)
	binary.BigEndian.PutUint16(buf[0:2], h.GroupIndex)
	buf[2] = h.ParityIndex
	buf[3] = h.TotalParity
	binary.BigEndian.PutUint16(buf[4:6], h.StartChunk)
	binary.BigEndian.PutUint16(buf[6:8], h.DataCount)
	binary.BigEndian.PutUint16(buf[8:10], h.MaxChunkLen)

	offset := FECHeaderFixedSize
	for _, c := range h.DataCRCs {
		binary.BigEndian.PutUint32(buf[offset:offset+4], c)
		offset += 4
	}
	for _, s := range h.ChunkSizes {
		binary.BigEndian.PutUint16(buf[offset:offset+2], s)
		offset += 2
	}
	return buf
}

// UnmarshalBinary decodes binary bytes into FECHeader.
func (h *FECHeader) UnmarshalBinary(data []byte) error {
	if len(data) < FECHeaderFixedSize {
		return ErrInvalidFECHeader
	}
	h.GroupIndex = binary.BigEndian.Uint16(data[0:2])
	h.ParityIndex = data[2]
	h.TotalParity = data[3]
	h.StartChunk = binary.BigEndian.Uint16(data[4:6])
	h.DataCount = binary.BigEndian.Uint16(data[6:8])
	h.MaxChunkLen = binary.BigEndian.Uint16(data[8:10])

	expectedLen := FECHeaderFixedSize + int(h.DataCount)*4 + int(h.DataCount)*2
	if len(data) < expectedLen {
		return ErrInvalidFECHeader
	}

	offset := FECHeaderFixedSize
	h.DataCRCs = make([]uint32, h.DataCount)
	for i := 0; i < int(h.DataCount); i++ {
		h.DataCRCs[i] = binary.BigEndian.Uint32(data[offset : offset+4])
		offset += 4
	}

	h.ChunkSizes = make([]uint16, h.DataCount)
	for i := 0; i < int(h.DataCount); i++ {
		h.ChunkSizes[i] = binary.BigEndian.Uint16(data[offset : offset+2])
		offset += 2
	}

	return nil
}

// BuildFECParityChunks generates parity PacketChunks for a slice of data PacketChunks.
// Groups data chunks into blocks of up to dataShards, and produces parityShards for each group.
func BuildFECParityChunks(chunks []PacketChunk, dataShards, parityShards int) ([]PacketChunk, error) {
	if len(chunks) == 0 {
		return nil, nil
	}
	if dataShards <= 0 || parityShards <= 0 {
		return nil, ErrInvalidShards
	}

	var parityChunks []PacketChunk
	totalDataChunks := len(chunks)
	groupCounter := uint16(0)

	for start := 0; start < totalDataChunks; start += dataShards {
		end := start + dataShards
		if end > totalDataChunks {
			end = totalDataChunks
		}
		groupChunks := chunks[start:end]
		k := len(groupChunks)

		// Determine max payload length across chunks in this group
		maxLen := 0
		for _, c := range groupChunks {
			if len(c.Payload) > maxLen {
				maxLen = len(c.Payload)
			}
		}
		if maxLen == 0 {
			maxLen = 1 // Ensure non-zero shard size
		}

		// Prepare padded shards, CRCs, and original sizes
		dataShardsBytes := make([][]byte, k)
		dataCRCs := make([]uint32, k)
		chunkSizes := make([]uint16, k)

		for i, c := range groupChunks {
			padded := make([]byte, maxLen)
			copy(padded, c.Payload)
			dataShardsBytes[i] = padded
			dataCRCs[i] = c.CRC32
			chunkSizes[i] = uint16(len(c.Payload))
		}

		encoder, err := NewFECEncoder(k, parityShards)
		if err != nil {
			return nil, err
		}

		parityShardsBytes, err := encoder.Encode(dataShardsBytes)
		if err != nil {
			return nil, err
		}

		refChunk := groupChunks[0]
		for pIdx, pData := range parityShardsBytes {
			hdr := FECHeader{
				GroupIndex:  groupCounter,
				ParityIndex: uint8(pIdx),
				TotalParity: uint8(parityShards),
				StartChunk:  uint16(start),
				DataCount:   uint16(k),
				MaxChunkLen: uint16(maxLen),
				DataCRCs:    dataCRCs,
				ChunkSizes:  chunkSizes,
			}
			hdrBytes := hdr.MarshalBinary()

			fullPayload := make([]byte, len(hdrBytes)+len(pData))
			copy(fullPayload, hdrBytes)
			copy(fullPayload[len(hdrBytes):], pData)

			parityChunk := PacketChunk{
				Sequence:       refChunk.Sequence,
				Timestamp:      refChunk.Timestamp,
				SourceDeviceID: refChunk.SourceDeviceID,
				Width:          refChunk.Width,
				Height:         refChunk.Height,
				ChunkIndex:     totalDataChunks + len(parityChunks),
				TotalChunks:    totalDataChunks,
				Flags:          FlagFECParity,
				CRC32:          crc32.ChecksumIEEE(fullPayload),
				Payload:        fullPayload,
			}
			parityChunks = append(parityChunks, parityChunk)
		}

		groupCounter++
	}

	return parityChunks, nil
}
