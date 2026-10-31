package filetransfer

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var (
	ErrCollectionSizeExceeded = errors.New("collection size limit exceeded")
	ErrEmptyCollection        = errors.New("no files matched collection pattern")
)

type CollectionFormat string

const (
	FormatZip CollectionFormat = "zip"
	FormatRaw CollectionFormat = "raw"
)

// CollectRequest defines the teacher request to harvest files from student workstations.
type CollectRequest struct {
	CollectionID  string           `json:"collection_id"`
	Pattern       string           `json:"pattern"`         // glob pattern e.g. "*.docx", "hw*.py"
	SourceDir     string           `json:"source_dir"`      // directory to scan on student device
	Format        CollectionFormat `json:"format"`          // zip or raw
	NamingPattern string           `json:"naming_pattern"`  // e.g. "{{student}}_{{device}}_{{filename}}"
	MaxFileSize   int64            `json:"max_file_size"`   // limit per file in bytes (0 for default 50MB)
	MaxTotalBytes int64            `json:"max_total_bytes"` // limit total collection size (0 for default 500MB)
}

// StudentContext supplies identification metadata for filename formatting.
type StudentContext struct {
	StudentID   string `json:"student_id"`
	StudentName string `json:"student_name"`
	DeviceID    string `json:"device_id"`
}

// SanitizePathComponent cleans an identifier so it does not contain
// directory separators or characters invalid in file systems.
func SanitizePathComponent(s string) string {
	s = strings.TrimSpace(s)
	// Replace illegal Windows and Unix path characters
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
		"\x00", "",
	)
	clean := replacer.Replace(s)
	// Remove consecutive dots to prevent traversal
	for strings.Contains(clean, "..") {
		clean = strings.ReplaceAll(clean, "..", ".")
	}
	if clean == "" {
		return "unknown"
	}
	return clean
}

// FormatCollectedFilename formats the destination filename according to the naming pattern.
// Tokens supported: {{student}}, {{device}}, {{filename}}, {{id}}
func FormatCollectedFilename(pattern string, sCtx StudentContext, origFilename string) string {
	if strings.TrimSpace(pattern) == "" {
		pattern = "{{student}}_{{device}}_{{filename}}"
	}

	studentName := sCtx.StudentName
	if studentName == "" {
		studentName = sCtx.StudentID
	}
	student := SanitizePathComponent(studentName)
	device := SanitizePathComponent(sCtx.DeviceID)
	id := SanitizePathComponent(sCtx.StudentID)
	filename := filepath.Base(origFilename)

	res := pattern
	res = strings.ReplaceAll(res, "{{student}}", student)
	res = strings.ReplaceAll(res, "{{device}}", device)
	res = strings.ReplaceAll(res, "{{id}}", id)
	res = strings.ReplaceAll(res, "{{filename}}", filename)

	return SanitizePathComponent(res)
}

// FindMatchingFiles scans sourceDir for files matching the glob pattern.
func FindMatchingFiles(sourceDir, pattern string, maxFileSize int64) ([]string, error) {
	if strings.TrimSpace(sourceDir) == "" {
		return nil, errors.New("empty source directory")
	}
	if strings.TrimSpace(pattern) == "" {
		pattern = "*"
	}
	if maxFileSize <= 0 {
		maxFileSize = 50 * 1024 * 1024 // 50 MB default
	}

	var matched []string
	cleanDir := filepath.Clean(sourceDir)

	err := filepath.WalkDir(cleanDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}

		match, err := filepath.Match(strings.ToLower(pattern), strings.ToLower(d.Name()))
		if err != nil {
			return fmt.Errorf("invalid pattern %q: %w", pattern, err)
		}

		if match {
			info, err := d.Info()
			if err != nil {
				return err
			}
			if info.Size() <= maxFileSize {
				matched = append(matched, path)
			}
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("walk directory: %w", err)
	}

	return matched, nil
}

// CollectionSummary reports the metrics of an executed collection.
type CollectionSummary struct {
	TotalFiles int    `json:"total_files"`
	TotalBytes int64  `json:"total_bytes"`
	SHA256     string `json:"sha256"`
}

// PackToZip packages the matched files into a ZIP archive, applying the naming pattern.
func PackToZip(files []string, sCtx StudentContext, namingPattern string, maxTotalBytes int64, w io.Writer) (*CollectionSummary, error) {
	if len(files) == 0 {
		return nil, ErrEmptyCollection
	}
	if maxTotalBytes <= 0 {
		maxTotalBytes = 500 * 1024 * 1024 // 500 MB default
	}

	hasher := sha256.New()
	multiWriter := io.MultiWriter(w, hasher)
	zipWriter := zip.NewWriter(multiWriter)

	var totalBytes int64
	for _, filePath := range files {
		f, err := os.Open(filePath)
		if err != nil {
			_ = zipWriter.Close()
			return nil, fmt.Errorf("open file %q: %w", filePath, err)
		}

		info, err := f.Stat()
		if err != nil {
			_ = f.Close()
			_ = zipWriter.Close()
			return nil, err
		}

		if totalBytes+info.Size() > maxTotalBytes {
			_ = f.Close()
			_ = zipWriter.Close()
			return nil, fmt.Errorf("%w: max %d bytes", ErrCollectionSizeExceeded, maxTotalBytes)
		}

		destName := FormatCollectedFilename(namingPattern, sCtx, filepath.Base(filePath))

		header, err := zip.FileInfoHeader(info)
		if err != nil {
			_ = f.Close()
			_ = zipWriter.Close()
			return nil, err
		}
		header.Name = destName
		header.Method = zip.Deflate

		entryWriter, err := zipWriter.CreateHeader(header)
		if err != nil {
			_ = f.Close()
			_ = zipWriter.Close()
			return nil, err
		}

		written, err := io.Copy(entryWriter, f)
		_ = f.Close()
		if err != nil {
			_ = zipWriter.Close()
			return nil, fmt.Errorf("write zip entry %q: %w", destName, err)
		}
		totalBytes += written
	}

	if err := zipWriter.Close(); err != nil {
		return nil, fmt.Errorf("close zip writer: %w", err)
	}

	return &CollectionSummary{
		TotalFiles: len(files),
		TotalBytes: totalBytes,
		SHA256:     hex.EncodeToString(hasher.Sum(nil)),
	}, nil
}

// CollectedItem describes an individual file prepared for raw transfer.
type CollectedItem struct {
	OriginalPath  string `json:"original_path"`
	CollectedName string `json:"collected_name"`
	Size          int64  `json:"size"`
	SHA256        string `json:"sha256"`
}

// PrepareRawItems prepares metadata for individual raw file collection items.
func PrepareRawItems(files []string, sCtx StudentContext, namingPattern string) ([]CollectedItem, error) {
	if len(files) == 0 {
		return nil, ErrEmptyCollection
	}

	items := make([]CollectedItem, 0, len(files))
	for _, filePath := range files {
		f, err := os.Open(filePath)
		if err != nil {
			return nil, fmt.Errorf("open %q: %w", filePath, err)
		}

		hasher := sha256.New()
		size, err := io.Copy(hasher, f)
		_ = f.Close()
		if err != nil {
			return nil, fmt.Errorf("hash %q: %w", filePath, err)
		}

		collectedName := FormatCollectedFilename(namingPattern, sCtx, filepath.Base(filePath))
		items = append(items, CollectedItem{
			OriginalPath:  filePath,
			CollectedName: collectedName,
			Size:          size,
			SHA256:        hex.EncodeToString(hasher.Sum(nil)),
		})
	}

	return items, nil
}
