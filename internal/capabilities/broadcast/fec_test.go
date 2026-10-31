package broadcast

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"errors"
	"hash/crc32"
	"net"
	"testing"
	"time"
)

func TestGF28Arithmetic(t *testing.T) {
	t.Parallel()

	// Multiplication by 0 and 1
	for i := 0; i < 256; i++ {
		b := byte(i)
		if gfMul(b, 0) != 0 || gfMul(0, b) != 0 {
			t.Fatalf("gfMul by 0 failed for %d", b)
		}
		if gfMul(b, 1) != b || gfMul(1, b) != b {
			t.Fatalf("gfMul by 1 failed for %d", b)
		}
	}

	// Inverse and Division
	for i := 1; i < 256; i++ {
		b := byte(i)
		inv := gfInv(b)
		if gfMul(b, inv) != 1 {
			t.Fatalf("gfInv failed for %d: %d * %d = %d", b, b, inv, gfMul(b, inv))
		}
		if gfDiv(b, b) != 1 {
			t.Fatalf("gfDiv failed for %d / %d != 1", b, b)
		}
	}

	// Distributive property: a * (b ^ c) == (a * b) ^ (a * c)
	for a := 1; a < 30; a++ {
		for b := 1; b < 30; b++ {
			for c := 1; c < 30; c++ {
				left := gfMul(byte(a), byte(b)^byte(c))
				right := gfMul(byte(a), byte(b)) ^ gfMul(byte(a), byte(c))
				if left != right {
					t.Fatalf("distributive property failed for a=%d, b=%d, c=%d", a, b, c)
				}
			}
		}
	}
}

func TestCauchyMatrixProperties(t *testing.T) {
	t.Parallel()

	matrix := buildCauchyMatrix(8, 2)
	if len(matrix) != 2 || len(matrix[0]) != 8 {
		t.Fatalf("unexpected matrix dimensions: %dx%d", len(matrix), len(matrix[0]))
	}

	// Row 0 must be all 1s (Pure XOR)
	for c := 0; c < 8; c++ {
		if matrix[0][c] != 1 {
			t.Fatalf("matrix[0][%d] = %d, expected 1", c, matrix[0][c])
		}
	}

	// Row 1 elements must be non-zero and pairwise distinct
	seen := make(map[byte]bool)
	for c := 0; c < 8; c++ {
		val := matrix[1][c]
		if val == 0 {
			t.Fatalf("matrix[1][%d] is 0", c)
		}
		if seen[val] {
			t.Fatalf("duplicate coefficient in row 1: %d", val)
		}
		seen[val] = true
	}
}

func TestFECEncodeDecode8Plus2(t *testing.T) {
	t.Parallel()

	k := 8
	m := 2
	shardSize := 1200

	encoder, err := NewFECEncoder(k, m)
	if err != nil {
		t.Fatalf("NewFECEncoder failed: %v", err)
	}

	decoder, err := NewFECDecoder(k, m)
	if err != nil {
		t.Fatalf("NewFECDecoder failed: %v", err)
	}

	// Generate K distinct data shards
	originalData := make([][]byte, k)
	dataCRCs := make([]uint32, k)
	for i := 0; i < k; i++ {
		originalData[i] = generateRandomPayload(shardSize)
		dataCRCs[i] = crc32.ChecksumIEEE(originalData[i])
	}

	parity, err := encoder.Encode(originalData)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}
	if len(parity) != m {
		t.Fatalf("expected %d parity shards, got %d", m, len(parity))
	}

	// Case 1: 0 shards lost (no-op)
	shards := make([][]byte, k+m)
	for i := 0; i < k; i++ {
		shards[i] = make([]byte, shardSize)
		copy(shards[i], originalData[i])
	}
	for i := 0; i < m; i++ {
		shards[k+i] = make([]byte, shardSize)
		copy(shards[k+i], parity[i])
	}

	if err := decoder.DecodeWithCRC(shards, dataCRCs); err != nil {
		t.Fatalf("DecodeWithCRC with 0 loss failed: %v", err)
	}

	// Case 2: 1 data shard lost (arbitrary index, test all 8 positions)
	for lostIdx := 0; lostIdx < k; lostIdx++ {
		testShards := make([][]byte, k+m)
		for i := 0; i < k; i++ {
			if i != lostIdx {
				testShards[i] = make([]byte, shardSize)
				copy(testShards[i], originalData[i])
			}
		}
		// Include parity 0
		testShards[k] = make([]byte, shardSize)
		copy(testShards[k], parity[0])

		if err := decoder.DecodeWithCRC(testShards, dataCRCs); err != nil {
			t.Fatalf("Decode single lost shard %d failed: %v", lostIdx, err)
		}
		if !bytes.Equal(testShards[lostIdx], originalData[lostIdx]) {
			t.Fatalf("recovered shard %d does not match original", lostIdx)
		}
	}

	// Case 3: 2 data shards lost (arbitrary pairs, e.g. (1, 4), (0, 7), (2, 5))
	pairs := [][2]int{{0, 1}, {1, 4}, {0, 7}, {2, 5}, {3, 6}}
	for _, pair := range pairs {
		l1, l2 := pair[0], pair[1]
		testShards := make([][]byte, k+m)
		for i := 0; i < k; i++ {
			if i != l1 && i != l2 {
				testShards[i] = make([]byte, shardSize)
				copy(testShards[i], originalData[i])
			}
		}
		testShards[k] = make([]byte, shardSize)
		copy(testShards[k], parity[0])
		testShards[k+1] = make([]byte, shardSize)
		copy(testShards[k+1], parity[1])

		if err := decoder.DecodeWithCRC(testShards, dataCRCs); err != nil {
			t.Fatalf("Decode 2 lost shards (%d, %d) failed: %v", l1, l2, err)
		}
		if !bytes.Equal(testShards[l1], originalData[l1]) {
			t.Fatalf("recovered shard %d mismatch in pair (%d, %d)", l1, l1, l2)
		}
		if !bytes.Equal(testShards[l2], originalData[l2]) {
			t.Fatalf("recovered shard %d mismatch in pair (%d, %d)", l2, l1, l2)
		}
	}

	// Case 4: 1 data shard lost and 1 parity shard lost (should recover using 1 remaining parity)
	{
		testShards := make([][]byte, k+m)
		for i := 1; i < k; i++ {
			testShards[i] = make([]byte, shardSize)
			copy(testShards[i], originalData[i])
		}
		// Data shard 0 is lost. Parity 1 is lost. Parity 0 is present.
		testShards[k] = make([]byte, shardSize)
		copy(testShards[k], parity[0])

		if err := decoder.DecodeWithCRC(testShards, dataCRCs); err != nil {
			t.Fatalf("Decode with 1 data + 1 parity lost failed: %v", err)
		}
		if !bytes.Equal(testShards[0], originalData[0]) {
			t.Fatal("recovered shard 0 mismatch")
		}
	}

	// Case 5: 3 data shards lost (> M, must return ErrTooManyLostShards)
	{
		testShards := make([][]byte, k+m)
		for i := 3; i < k; i++ {
			testShards[i] = make([]byte, shardSize)
			copy(testShards[i], originalData[i])
		}
		testShards[k] = make([]byte, shardSize)
		copy(testShards[k], parity[0])
		testShards[k+1] = make([]byte, shardSize)
		copy(testShards[k+1], parity[1])

		err := decoder.Decode(testShards)
		if !errors.Is(err, ErrTooManyLostShards) {
			t.Fatalf("expected ErrTooManyLostShards for 3 losses, got %v", err)
		}
	}
}

func TestFECEncodeDecode10Plus1(t *testing.T) {
	t.Parallel()

	k := 10
	m := 1
	shardSize := 1024

	encoder, err := NewFECEncoder(k, m)
	if err != nil {
		t.Fatalf("NewFECEncoder failed: %v", err)
	}
	decoder, err := NewFECDecoder(k, m)
	if err != nil {
		t.Fatalf("NewFECDecoder failed: %v", err)
	}

	data := make([][]byte, k)
	crcs := make([]uint32, k)
	for i := 0; i < k; i++ {
		data[i] = generateRandomPayload(shardSize)
		crcs[i] = crc32.ChecksumIEEE(data[i])
	}

	parity, err := encoder.Encode(data)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	// Verify Parity 0 is pure XOR
	xorBuf := make([]byte, shardSize)
	for _, d := range data {
		for j := 0; j < shardSize; j++ {
			xorBuf[j] ^= d[j]
		}
	}
	if !bytes.Equal(parity[0], xorBuf) {
		t.Fatal("10+1 parity shard is not pure XOR of data shards")
	}

	// Drop shard 5
	shards := make([][]byte, k+m)
	for i := 0; i < k; i++ {
		if i != 5 {
			shards[i] = make([]byte, shardSize)
			copy(shards[i], data[i])
		}
	}
	shards[k] = make([]byte, shardSize)
	copy(shards[k], parity[0])

	if err := decoder.DecodeWithCRC(shards, crcs); err != nil {
		t.Fatalf("DecodeWithCRC failed: %v", err)
	}
	if !bytes.Equal(shards[5], data[5]) {
		t.Fatal("recovered shard 5 mismatch")
	}
}

func TestCRCVerificationCatchesCorruption(t *testing.T) {
	t.Parallel()

	k := 4
	m := 2
	shardSize := 512

	encoder, _ := NewFECEncoder(k, m)
	decoder, _ := NewFECDecoder(k, m)

	data := make([][]byte, k)
	crcs := make([]uint32, k)
	for i := 0; i < k; i++ {
		data[i] = generateRandomPayload(shardSize)
		crcs[i] = crc32.ChecksumIEEE(data[i])
	}

	parity, _ := encoder.Encode(data)

	// Corrupt expected CRC of shard 2
	tamperedCRCs := make([]uint32, k)
	copy(tamperedCRCs, crcs)
	tamperedCRCs[2] ^= 0xDEADBEEF

	shards := make([][]byte, k+m)
	for i := 0; i < k; i++ {
		if i != 2 {
			shards[i] = make([]byte, shardSize)
			copy(shards[i], data[i])
		}
	}
	shards[k] = make([]byte, shardSize)
	copy(shards[k], parity[0])
	shards[k+1] = make([]byte, shardSize)
	copy(shards[k+1], parity[1])

	err := decoder.DecodeWithCRC(shards, tamperedCRCs)
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("expected ErrChecksumMismatch, got %v", err)
	}
}

func TestFECHeaderSerialization(t *testing.T) {
	t.Parallel()

	hdr := FECHeader{
		GroupIndex:  42,
		ParityIndex: 1,
		TotalParity: 2,
		StartChunk:  16,
		DataCount:   8,
		MaxChunkLen: 1200,
		DataCRCs:    []uint32{0x12345678, 0x87654321, 0xAABBCCDD, 0x00112233, 0x44556677, 0x8899AABB, 0xCCDDEEFF, 0xFEDCBA98},
		ChunkSizes:  []uint16{1200, 1200, 1180, 1200, 1050, 1200, 1200, 990},
	}

	bin := hdr.MarshalBinary()
	if len(bin) == 0 {
		t.Fatal("MarshalBinary returned empty slice")
	}

	var decoded FECHeader
	if err := decoded.UnmarshalBinary(bin); err != nil {
		t.Fatalf("UnmarshalBinary failed: %v", err)
	}

	if decoded.GroupIndex != hdr.GroupIndex || decoded.ParityIndex != hdr.ParityIndex || decoded.TotalParity != hdr.TotalParity {
		t.Fatalf("header scalar mismatch: %+v vs %+v", decoded, hdr)
	}
	if decoded.StartChunk != hdr.StartChunk || decoded.DataCount != hdr.DataCount || decoded.MaxChunkLen != hdr.MaxChunkLen {
		t.Fatalf("header chunk parameters mismatch")
	}
	for i := range hdr.DataCRCs {
		if decoded.DataCRCs[i] != hdr.DataCRCs[i] {
			t.Fatalf("crc %d mismatch", i)
		}
		if decoded.ChunkSizes[i] != hdr.ChunkSizes[i] {
			t.Fatalf("chunk size %d mismatch", i)
		}
	}
}

func TestBuildFECParityChunksAndReassembleWithLoss(t *testing.T) {
	t.Parallel()

	originalPayload := generateRandomPayload(20 * 1024) // 20 KB -> ~17 chunks
	frame := ScreenFrame{
		Sequence:       777,
		Timestamp:      time.Now(),
		SourceDeviceID: "teacher-room-a",
		Width:          1920,
		Height:         1080,
		Payload:        originalPayload,
	}

	chunks, err := ChunkFrame(frame, 1200)
	if err != nil {
		t.Fatalf("ChunkFrame failed: %v", err)
	}
	if len(chunks) < 10 {
		t.Fatalf("expected at least 10 chunks, got %d", len(chunks))
	}

	// Generate 8+2 FEC parity chunks
	parityChunks, err := BuildFECParityChunks(chunks, 8, 2)
	if err != nil {
		t.Fatalf("BuildFECParityChunks failed: %v", err)
	}
	if len(parityChunks) == 0 {
		t.Fatal("expected non-empty parity chunks")
	}

	// Verify parity chunks have FlagFECParity and valid CRC
	for _, pc := range parityChunks {
		if pc.Flags&FlagFECParity == 0 {
			t.Errorf("parity chunk %d missing FlagFECParity", pc.ChunkIndex)
		}
		if !pc.VerifyCRC() {
			t.Errorf("parity chunk %d failed CRC verification", pc.ChunkIndex)
		}
	}

	// Scenario: Network drops chunk 2 and chunk 5 (2 lost data chunks in group 0)
	reassembler := NewFrameReassembler(2 * time.Second)

	var reassembled *ScreenFrame
	// Feed all data chunks EXCEPT 2 and 5
	for _, c := range chunks {
		if c.ChunkIndex == 2 || c.ChunkIndex == 5 {
			continue // Simulated packet drop
		}
		res, err := reassembler.AddChunk(c)
		if err != nil {
			t.Fatalf("AddChunk failed: %v", err)
		}
		if res != nil {
			t.Fatal("frame should not reassemble before missing chunks are recovered")
		}
	}

	// Feed parity chunks: this should trigger FEC recovery for chunks 2 and 5!
	for _, pc := range parityChunks {
		res, err := reassembler.AddChunk(pc)
		if err != nil {
			t.Fatalf("AddChunk for parity failed: %v", err)
		}
		if res != nil {
			reassembled = res
		}
	}

	if reassembled == nil {
		t.Fatal("FrameReassembler failed to reconstruct frame with FEC")
	}
	if reassembled.Sequence != frame.Sequence {
		t.Errorf("sequence mismatch: got %d, want %d", reassembled.Sequence, frame.Sequence)
	}
	if !bytes.Equal(reassembled.Payload, frame.Payload) {
		t.Fatal("reconstructed frame payload does not match original frame payload")
	}
}

func TestMulticastSenderAndReceiverFECWithLoss(t *testing.T) {
	t.Parallel()

	rConn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to bind receiver: %v", err)
	}
	defer rConn.Close()

	sConn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to bind sender: %v", err)
	}
	defer sConn.Close()

	receiver, err := NewMulticastReceiver(MulticastReceiverConfig{
		Conn:               rConn,
		ReassemblerTimeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewMulticastReceiver failed: %v", err)
	}
	defer receiver.Close()

	sender, err := NewMulticastSender(MulticastSenderConfig{
		TargetAddr: rConn.LocalAddr().String(),
		Conn:       sConn,
		ChunkSize:  1200,
		FEC: FECConfig{
			Enabled:      true,
			DataShards:   8,
			ParityShards: 2,
		},
	})
	if err != nil {
		t.Fatalf("NewMulticastSender failed: %v", err)
	}
	defer sender.Close()

	if !sender.FECConfig().Enabled {
		t.Fatal("sender FEC should be enabled")
	}

	originalPayload := generateRandomPayload(12 * 1024)
	frame := ScreenFrame{
		Sequence:       888,
		Timestamp:      time.Now().Truncate(time.Millisecond),
		SourceDeviceID: "teacher-fec-node",
		Width:          1920,
		Height:         1080,
		Payload:        originalPayload,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	frameCh := make(chan *ScreenFrame, 1)

	// Custom packet proxy / filter: drop data chunk index 1 and 3 to simulate loss
	// Since rConn is 127.0.0.1:0, we intercept via custom sender loop or test directly
	chunks, err := ChunkFrame(frame, 1200)
	if err != nil {
		t.Fatalf("ChunkFrame failed: %v", err)
	}
	parityChunks, err := BuildFECParityChunks(chunks, 8, 2)
	if err != nil {
		t.Fatalf("BuildFECParityChunks failed: %v", err)
	}

	go func() {
		recv, err := receiver.ReceiveFrame(ctx)
		if err != nil {
			errCh <- err
			return
		}
		frameCh <- recv
	}()

	// Send all chunks except chunk 1 and chunk 3
	for _, c := range chunks {
		if c.ChunkIndex == 1 || c.ChunkIndex == 3 {
			continue // Drop chunk 1 and 3
		}
		b, _ := c.MarshalBinary()
		_, _ = sConn.WriteTo(b, rConn.LocalAddr())
	}
	// Send parity chunks
	for _, pc := range parityChunks {
		b, _ := pc.MarshalBinary()
		_, _ = sConn.WriteTo(b, rConn.LocalAddr())
	}

	select {
	case err := <-errCh:
		t.Fatalf("ReceiveFrame failed: %v", err)
	case recv := <-frameCh:
		if recv.Sequence != frame.Sequence {
			t.Errorf("sequence mismatch: got %d, want %d", recv.Sequence, frame.Sequence)
		}
		if !bytes.Equal(recv.Payload, frame.Payload) {
			t.Fatal("received payload mismatch after FEC reconstruction over network")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for FEC reassembled frame over network")
	}
}

func BenchmarkFECEncode8_2(b *testing.B) {
	encoder, _ := NewFECEncoder(8, 2)
	shards := make([][]byte, 8)
	for i := 0; i < 8; i++ {
		shards[i] = make([]byte, 1200)
		_, _ = crand.Read(shards[i])
	}
	b.ResetTimer()
	b.SetBytes(8 * 1200)
	for i := 0; i < b.N; i++ {
		_, _ = encoder.Encode(shards)
	}
}

func BenchmarkFECDecode8_2_Lost2(b *testing.B) {
	encoder, _ := NewFECEncoder(8, 2)
	decoder, _ := NewFECDecoder(8, 2)
	orig := make([][]byte, 8)
	for i := 0; i < 8; i++ {
		orig[i] = make([]byte, 1200)
		_, _ = crand.Read(orig[i])
	}
	parity, _ := encoder.Encode(orig)

	b.ResetTimer()
	b.SetBytes(8 * 1200)
	for i := 0; i < b.N; i++ {
		shards := make([][]byte, 10)
		for j := 0; j < 8; j++ {
			if j != 1 && j != 4 {
				shards[j] = orig[j]
			}
		}
		shards[8] = parity[0]
		shards[9] = parity[1]
		_ = decoder.Decode(shards)
	}
}
