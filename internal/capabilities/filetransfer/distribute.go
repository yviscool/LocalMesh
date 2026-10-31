package filetransfer

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	ErrPathTraversal      = errors.New("path traversal detected")
	ErrHashMismatch       = errors.New("SHA-256 checksum mismatch")
	ErrInvalidChunk       = errors.New("invalid chunk data or index")
	ErrFileExists         = errors.New("destination file already exists")
	ErrInvalidManifest    = errors.New("invalid file manifest")
	ErrTransferNotFound   = errors.New("file transfer not found")
	ErrIncompleteTransfer = errors.New("transfer incomplete: missing chunks")
)

type OverwritePolicy string

const (
	OverwriteAlways OverwritePolicy = "overwrite"
	OverwriteNever  OverwritePolicy = "skip"
	OverwriteRename OverwritePolicy = "rename"
)

type TargetRoot string

const (
	TargetRootDesktop   TargetRoot = "desktop"
	TargetRootDocuments TargetRoot = "documents"
	TargetRootTemp      TargetRoot = "temp"
	TargetRootCustom    TargetRoot = "custom"
)

// FileManifest defines the metadata and integrity expectations for a distributed file.
type FileManifest struct {
	FileID          string          `json:"file_id"`
	FileName        string          `json:"file_name"`
	RelativePath    string          `json:"relative_path"`
	TargetRoot      TargetRoot      `json:"target_root"`
	CustomRootPath  string          `json:"custom_root_path,omitempty"`
	TotalBytes      int64           `json:"total_bytes"`
	SHA256          string          `json:"sha256"`
	ChunkSize       int             `json:"chunk_size"`
	TotalChunks     int             `json:"total_chunks"`
	OverwritePolicy OverwritePolicy `json:"overwrite_policy"`
}

// Validate ensures the manifest attributes are structurally valid.
func (m FileManifest) Validate() error {
	if strings.TrimSpace(m.FileID) == "" {
		return fmt.Errorf("%w: missing file_id", ErrInvalidManifest)
	}
	if strings.TrimSpace(m.RelativePath) == "" && strings.TrimSpace(m.FileName) == "" {
		return fmt.Errorf("%w: missing relative_path or file_name", ErrInvalidManifest)
	}
	if m.TotalBytes < 0 {
		return fmt.Errorf("%w: negative total_bytes", ErrInvalidManifest)
	}
	if m.TotalBytes > 0 && len(m.SHA256) != 64 {
		return fmt.Errorf("%w: invalid sha256 hash length", ErrInvalidManifest)
	}
	if m.TotalChunks < 0 {
		return fmt.Errorf("%w: negative total_chunks", ErrInvalidManifest)
	}

	switch m.OverwritePolicy {
	case "", OverwriteAlways, OverwriteNever, OverwriteRename:
	default:
		return fmt.Errorf("%w: invalid overwrite policy %q", ErrInvalidManifest, m.OverwritePolicy)
	}

	return nil
}

// ResolveSafePath ensures that the destination file path stays within baseDir
// and strictly prevents directory traversal attacks.
func ResolveSafePath(baseDir, relativePath string) (string, error) {
	if strings.Contains(relativePath, "\x00") || strings.Contains(baseDir, "\x00") {
		return "", ErrPathTraversal
	}

	// Reject paths containing Windows volume specifiers (e.g. C: or \\)
	if filepath.VolumeName(relativePath) != "" {
		return "", ErrPathTraversal
	}

	// Reject paths with leading directory separators
	if strings.HasPrefix(relativePath, "/") || strings.HasPrefix(relativePath, `\`) {
		return "", ErrPathTraversal
	}

	cleanBase := filepath.Clean(baseDir)
	target := filepath.Clean(filepath.Join(cleanBase, relativePath))

	rel, err := filepath.Rel(cleanBase, target)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrPathTraversal, err)
	}

	// If the relative path starts with ".." or is absolute, it escapes cleanBase!
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", ErrPathTraversal
	}

	return target, nil
}

// FileChunk represents a segment of a distributed file.
type FileChunk struct {
	FileID      string `json:"file_id"`
	Index       int    `json:"index"`
	Offset      int64  `json:"offset"`
	Data        []byte `json:"data"`
	ChunkSHA256 string `json:"chunk_sha256"`
}

// ComputeSHA256 returns the hex-encoded SHA-256 digest of the given data.
func ComputeSHA256(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// SplitIntoChunks reads from r and produces chunks of chunkSize bytes,
// returning the chunks and the overall SHA-256 digest.
func SplitIntoChunks(r io.Reader, fileID string, chunkSize int) ([]FileChunk, string, error) {
	if chunkSize <= 0 {
		chunkSize = 64 * 1024 // 64 KB default
	}

	var chunks []FileChunk
	fullHasher := sha256.New()
	buf := make([]byte, chunkSize)
	var offset int64
	index := 0

	for {
		n, err := io.ReadFull(r, buf)
		if n > 0 {
			chunkData := make([]byte, n)
			copy(chunkData, buf[:n])
			fullHasher.Write(chunkData)

			chunkHash := ComputeSHA256(chunkData)
			chunks = append(chunks, FileChunk{
				FileID:      fileID,
				Index:       index,
				Offset:      offset,
				Data:        chunkData,
				ChunkSHA256: chunkHash,
			})
			offset += int64(n)
			index++
		}
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			return nil, "", err
		}
	}

	return chunks, hex.EncodeToString(fullHasher.Sum(nil)), nil
}

type transferSession struct {
	manifest   FileManifest
	targetPath string
	partPath   string
	file       *os.File
	received   map[int]bool
}

// Receiver manages incoming file distribution streams and integrity validation.
type Receiver struct {
	mu       sync.Mutex
	sessions map[string]*transferSession
}

// NewReceiver creates a new Receiver.
func NewReceiver() *Receiver {
	return &Receiver{
		sessions: make(map[string]*transferSession),
	}
}

// StartTransfer initializes a new incoming transfer session.
func (r *Receiver) StartTransfer(manifest FileManifest, baseDir string) error {
	if err := manifest.Validate(); err != nil {
		return err
	}

	relPath := manifest.RelativePath
	if relPath == "" {
		relPath = manifest.FileName
	}

	targetPath, err := ResolveSafePath(baseDir, relPath)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return fmt.Errorf("create target directory: %w", err)
	}

	partPath := targetPath + ".part." + manifest.FileID
	file, err := os.OpenFile(partPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("create part file: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Clean up existing session if any
	if old, ok := r.sessions[manifest.FileID]; ok {
		_ = old.file.Close()
		_ = os.Remove(old.partPath)
	}

	r.sessions[manifest.FileID] = &transferSession{
		manifest:   manifest,
		targetPath: targetPath,
		partPath:   partPath,
		file:       file,
		received:   make(map[int]bool),
	}

	return nil
}

// WriteChunk writes a verified chunk into the transfer session part file.
func (r *Receiver) WriteChunk(chunk FileChunk) error {
	r.mu.Lock()
	sess, ok := r.sessions[chunk.FileID]
	r.mu.Unlock()

	if !ok {
		return ErrTransferNotFound
	}

	// Verify chunk hash
	if chunk.ChunkSHA256 != "" {
		actualHash := ComputeSHA256(chunk.Data)
		if actualHash != chunk.ChunkSHA256 {
			return fmt.Errorf("%w: chunk %d expected %s, got %s", ErrHashMismatch, chunk.Index, chunk.ChunkSHA256, actualHash)
		}
	}

	if _, err := sess.file.WriteAt(chunk.Data, chunk.Offset); err != nil {
		return fmt.Errorf("write chunk: %w", err)
	}

	r.mu.Lock()
	sess.received[chunk.Index] = true
	r.mu.Unlock()

	return nil
}

// Finalize checks total integrity, resolves collision policy, and moves the file to its destination.
func (r *Receiver) Finalize(fileID string) (string, error) {
	r.mu.Lock()
	sess, ok := r.sessions[fileID]
	if ok {
		delete(r.sessions, fileID)
	}
	r.mu.Unlock()

	if !ok {
		return "", ErrTransferNotFound
	}

	defer func() {
		_ = sess.file.Close()
		_ = os.Remove(sess.partPath) // in case rename was not executed
	}()

	// Verify all chunks received
	for i := 0; i < sess.manifest.TotalChunks; i++ {
		if !sess.received[i] {
			return "", fmt.Errorf("%w: chunk %d is missing", ErrIncompleteTransfer, i)
		}
	}

	// Verify full file SHA-256
	if _, err := sess.file.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("seek part file: %w", err)
	}

	hasher := sha256.New()
	written, err := io.Copy(hasher, sess.file)
	if err != nil {
		return "", fmt.Errorf("read part file for checksum: %w", err)
	}

	if sess.manifest.TotalBytes > 0 && written != sess.manifest.TotalBytes {
		return "", fmt.Errorf("size mismatch: expected %d bytes, got %d", sess.manifest.TotalBytes, written)
	}

	if sess.manifest.SHA256 != "" {
		digest := hex.EncodeToString(hasher.Sum(nil))
		if digest != sess.manifest.SHA256 {
			return "", fmt.Errorf("%w: expected %s, got %s", ErrHashMismatch, sess.manifest.SHA256, digest)
		}
	}

	_ = sess.file.Close()

	destPath := sess.targetPath
	policy := sess.manifest.OverwritePolicy
	if policy == "" {
		policy = OverwriteAlways
	}

	if _, err := os.Stat(destPath); err == nil {
		switch policy {
		case OverwriteNever:
			return "", ErrFileExists
		case OverwriteRename:
			destPath = findUniqueFilename(destPath)
		case OverwriteAlways:
			// replace
		}
	}

	if err := os.Rename(sess.partPath, destPath); err != nil {
		return "", fmt.Errorf("commit file: %w", err)
	}

	return destPath, nil
}

// Abort cancels a transfer session and cleans up temporary files.
func (r *Receiver) Abort(fileID string) {
	r.mu.Lock()
	sess, ok := r.sessions[fileID]
	if ok {
		delete(r.sessions, fileID)
	}
	r.mu.Unlock()

	if ok {
		_ = sess.file.Close()
		_ = os.Remove(sess.partPath)
	}
}

func findUniqueFilename(path string) string {
	ext := filepath.Ext(path)
	baseWithoutExt := strings.TrimSuffix(path, ext)
	counter := 1
	for {
		candidate := fmt.Sprintf("%s (%d)%s", baseWithoutExt, counter, ext)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
		counter++
	}
}
