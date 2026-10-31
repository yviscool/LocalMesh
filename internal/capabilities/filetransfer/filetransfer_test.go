package filetransfer

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveSafePath(t *testing.T) {
	tempBase := t.TempDir()

	tests := []struct {
		name      string
		relPath   string
		expectErr error
	}{
		{
			name:      "simple relative file",
			relPath:   "homework.docx",
			expectErr: nil,
		},
		{
			name:      "nested relative file",
			relPath:   filepath.Join("math", "week1", "quiz.pdf"),
			expectErr: nil,
		},
		{
			name:      "path traversal via parent directory",
			relPath:   filepath.Join("..", "evil.txt"),
			expectErr: ErrPathTraversal,
		},
		{
			name:      "deep path traversal escape",
			relPath:   filepath.Join("subdir", "..", "..", "evil.txt"),
			expectErr: ErrPathTraversal,
		},
		{
			name:      "path with null byte",
			relPath:   "evil\x00.txt",
			expectErr: ErrPathTraversal,
		},
		{
			name:      "leading slash absolute path",
			relPath:   "/etc/shadow",
			expectErr: ErrPathTraversal,
		},
		{
			name:      "leading backslash",
			relPath:   `\windows\system32`,
			expectErr: ErrPathTraversal,
		},
		{
			name:      "windows drive letter",
			relPath:   `C:\evil.exe`,
			expectErr: ErrPathTraversal,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resolved, err := ResolveSafePath(tempBase, tc.relPath)
			if tc.expectErr != nil {
				if !errors.Is(err, tc.expectErr) {
					t.Fatalf("expected error %v, got %v (resolved=%s)", tc.expectErr, err, resolved)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if !strings.HasPrefix(resolved, tempBase) {
					t.Fatalf("resolved path %s does not start with base %s", resolved, tempBase)
				}
			}
		})
	}
}

func TestSplitIntoChunksAndVerification(t *testing.T) {
	content := []byte("LocalMesh classroom control file distribution payload for testing chunks and hashes.")
	reader := bytes.NewReader(content)

	chunks, overallHash, err := SplitIntoChunks(reader, "file-123", 16)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedOverall := ComputeSHA256(content)
	if overallHash != expectedOverall {
		t.Fatalf("expected overall hash %s, got %s", expectedOverall, overallHash)
	}

	if len(chunks) != (len(content)+15)/16 {
		t.Fatalf("expected %d chunks, got %d", (len(content)+15)/16, len(chunks))
	}

	// Verify each individual chunk hash
	var reassembled []byte
	for i, chunk := range chunks {
		if chunk.Index != i {
			t.Fatalf("expected chunk index %d, got %d", i, chunk.Index)
		}
		expectedChunkHash := ComputeSHA256(chunk.Data)
		if chunk.ChunkSHA256 != expectedChunkHash {
			t.Fatalf("chunk %d hash mismatch", i)
		}
		reassembled = append(reassembled, chunk.Data...)
	}

	if !bytes.Equal(reassembled, content) {
		t.Fatal("reassembled content does not match original")
	}
}

func TestReceiverDistributionWorkflow(t *testing.T) {
	tempBase := t.TempDir()
	content := []byte("Hello students! This is your laboratory assignment.")
	fileID := "lab-assignment-01"

	chunks, overallHash, err := SplitIntoChunks(bytes.NewReader(content), fileID, 10)
	if err != nil {
		t.Fatalf("split chunks: %v", err)
	}

	manifest := FileManifest{
		FileID:          fileID,
		FileName:        "assignment.txt",
		RelativePath:    "coursework/assignment.txt",
		TargetRoot:      TargetRootCustom,
		TotalBytes:      int64(len(content)),
		SHA256:          overallHash,
		ChunkSize:       10,
		TotalChunks:     len(chunks),
		OverwritePolicy: OverwriteAlways,
	}

	receiver := NewReceiver()
	if err := receiver.StartTransfer(manifest, tempBase); err != nil {
		t.Fatalf("start transfer: %v", err)
	}

	// Write all chunks
	for _, chunk := range chunks {
		if err := receiver.WriteChunk(chunk); err != nil {
			t.Fatalf("write chunk %d: %v", chunk.Index, err)
		}
	}

	finalPath, err := receiver.Finalize(fileID)
	if err != nil {
		t.Fatalf("finalize transfer: %v", err)
	}

	// Read and verify saved file
	savedData, err := os.ReadFile(finalPath)
	if err != nil {
		t.Fatalf("read saved file: %v", err)
	}
	if !bytes.Equal(savedData, content) {
		t.Fatal("saved data does not match original content")
	}

	// Verify .part file cleaned up
	partFile := finalPath + ".part." + fileID
	if _, err := os.Stat(partFile); !os.IsNotExist(err) {
		t.Fatal("part file should be cleaned up after finalize")
	}
}

func TestReceiverErrorHandlingAndPolicies(t *testing.T) {
	tempBase := t.TempDir()
	content := []byte("Secure content")
	fileID := "test-error-handling"

	chunks, overallHash, _ := SplitIntoChunks(bytes.NewReader(content), fileID, 10)

	t.Run("chunk hash tampering rejection", func(t *testing.T) {
		receiver := NewReceiver()
		manifest := FileManifest{
			FileID:       fileID + "-tamper",
			FileName:     "test.txt",
			RelativePath: "test.txt",
			TotalBytes:   int64(len(content)),
			SHA256:       overallHash,
			TotalChunks:  len(chunks),
		}
		_ = receiver.StartTransfer(manifest, tempBase)
		defer receiver.Abort(manifest.FileID)

		corruptedChunk := chunks[0]
		corruptedChunk.FileID = manifest.FileID
		corruptedChunk.Data = []byte("corrupted!")

		err := receiver.WriteChunk(corruptedChunk)
		if !errors.Is(err, ErrHashMismatch) {
			t.Fatalf("expected ErrHashMismatch, got %v", err)
		}
	})

	t.Run("missing chunks on finalize", func(t *testing.T) {
		receiver := NewReceiver()
		manifest := FileManifest{
			FileID:       fileID + "-missing",
			FileName:     "test.txt",
			RelativePath: "test.txt",
			TotalBytes:   int64(len(content)),
			SHA256:       overallHash,
			TotalChunks:  len(chunks),
		}
		_ = receiver.StartTransfer(manifest, tempBase)
		defer receiver.Abort(manifest.FileID)

		// Write only first chunk
		chunk := chunks[0]
		chunk.FileID = manifest.FileID
		_ = receiver.WriteChunk(chunk)

		_, err := receiver.Finalize(manifest.FileID)
		if !errors.Is(err, ErrIncompleteTransfer) {
			t.Fatalf("expected ErrIncompleteTransfer, got %v", err)
		}
	})

	t.Run("overwrite rename policy", func(t *testing.T) {
		receiver := NewReceiver()
		relPath := "report.txt"
		existingTarget := filepath.Join(tempBase, relPath)
		_ = os.WriteFile(existingTarget, []byte("existing"), 0644)

		manifest := FileManifest{
			FileID:          fileID + "-rename",
			FileName:        "report.txt",
			RelativePath:    relPath,
			TotalBytes:      int64(len(content)),
			SHA256:          overallHash,
			TotalChunks:     len(chunks),
			OverwritePolicy: OverwriteRename,
		}

		_ = receiver.StartTransfer(manifest, tempBase)
		for _, c := range chunks {
			c.FileID = manifest.FileID
			_ = receiver.WriteChunk(c)
		}

		savedPath, err := receiver.Finalize(manifest.FileID)
		if err != nil {
			t.Fatalf("finalize failed: %v", err)
		}

		// Should be saved as report (1).txt
		if !strings.Contains(savedPath, "report (1).txt") {
			t.Fatalf("expected renamed file name, got %s", savedPath)
		}
		// Existing file still intact
		oldData, _ := os.ReadFile(existingTarget)
		if string(oldData) != "existing" {
			t.Fatal("existing file should remain untouched")
		}
	})

	t.Run("overwrite never policy", func(t *testing.T) {
		receiver := NewReceiver()
		relPath := "skip.txt"
		existingTarget := filepath.Join(tempBase, relPath)
		_ = os.WriteFile(existingTarget, []byte("existing"), 0644)

		manifest := FileManifest{
			FileID:          fileID + "-skip",
			FileName:        "skip.txt",
			RelativePath:    relPath,
			TotalBytes:      int64(len(content)),
			SHA256:          overallHash,
			TotalChunks:     len(chunks),
			OverwritePolicy: OverwriteNever,
		}

		_ = receiver.StartTransfer(manifest, tempBase)
		for _, c := range chunks {
			c.FileID = manifest.FileID
			_ = receiver.WriteChunk(c)
		}

		_, err := receiver.Finalize(manifest.FileID)
		if !errors.Is(err, ErrFileExists) {
			t.Fatalf("expected ErrFileExists, got %v", err)
		}
	})
}

func TestCollectionWorkflow(t *testing.T) {
	tempSrc := t.TempDir()

	// Create test files in student workspace
	doc1 := filepath.Join(tempSrc, "essay.docx")
	doc2 := filepath.Join(tempSrc, "homework_final.docx")
	code := filepath.Join(tempSrc, "main.go")
	subDoc := filepath.Join(tempSrc, "drafts", "notes.docx")

	_ = os.MkdirAll(filepath.Dir(subDoc), 0755)
	_ = os.WriteFile(doc1, []byte("Essay body"), 0644)
	_ = os.WriteFile(doc2, []byte("Final homework answer"), 0644)
	_ = os.WriteFile(code, []byte("package main"), 0644)
	_ = os.WriteFile(subDoc, []byte("Draft notes"), 0644)

	sCtx := StudentContext{
		StudentID:   "STU-042",
		StudentName: "Alice Smith",
		DeviceID:    "DESK-WIN-12",
	}

	t.Run("FormatCollectedFilename sanitization and replacement", func(t *testing.T) {
		formatted := FormatCollectedFilename("{{student}}_{{device}}_{{filename}}", sCtx, "lab:final?.docx")
		expected := "Alice Smith_DESK-WIN-12_lab_final_.docx"
		if formatted != expected {
			t.Fatalf("expected %q, got %q", expected, formatted)
		}
	})

	t.Run("FindMatchingFiles with glob", func(t *testing.T) {
		matches, err := FindMatchingFiles(tempSrc, "*.docx", 1024*1024)
		if err != nil {
			t.Fatalf("find matching files: %v", err)
		}

		// Should find doc1, doc2, subDoc (3 files)
		if len(matches) != 3 {
			t.Fatalf("expected 3 docx matches, got %d: %v", len(matches), matches)
		}
	})

	t.Run("PackToZip archive creation and verification", func(t *testing.T) {
		matches, _ := FindMatchingFiles(tempSrc, "*.docx", 1024*1024)

		buf := new(bytes.Buffer)
		summary, err := PackToZip(matches, sCtx, "{{student}}_{{filename}}", 10*1024*1024, buf)
		if err != nil {
			t.Fatalf("pack to zip: %v", err)
		}

		if summary.TotalFiles != 3 {
			t.Fatalf("expected 3 files packed, got %d", summary.TotalFiles)
		}
		if summary.SHA256 == "" {
			t.Fatal("expected summary sha256 to be populated")
		}

		// Read back zip and check entries
		zipReader, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
		if err != nil {
			t.Fatalf("open zip: %v", err)
		}

		if len(zipReader.File) != 3 {
			t.Fatalf("expected 3 zip entries, got %d", len(zipReader.File))
		}

		foundFiles := make(map[string]bool)
		for _, f := range zipReader.File {
			foundFiles[f.Name] = true
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("open entry %s: %v", f.Name, err)
			}
			content, _ := io.ReadAll(rc)
			_ = rc.Close()
			if len(content) == 0 {
				t.Fatalf("entry %s has empty content", f.Name)
			}
		}

		if !foundFiles["Alice Smith_essay.docx"] || !foundFiles["Alice Smith_homework_final.docx"] || !foundFiles["Alice Smith_notes.docx"] {
			t.Fatalf("unexpected files in zip: %+v", foundFiles)
		}
	})

	t.Run("PrepareRawItems metadata generation", func(t *testing.T) {
		matches, _ := FindMatchingFiles(tempSrc, "essay.docx", 1024)
		items, err := PrepareRawItems(matches, sCtx, "{{id}}_{{filename}}")
		if err != nil {
			t.Fatalf("prepare raw items: %v", err)
		}

		if len(items) != 1 {
			t.Fatalf("expected 1 item, got %d", len(items))
		}
		item := items[0]
		if item.CollectedName != "STU-042_essay.docx" {
			t.Fatalf("expected STU-042_essay.docx, got %s", item.CollectedName)
		}
		if item.Size != int64(len("Essay body")) {
			t.Fatalf("expected size %d, got %d", len("Essay body"), item.Size)
		}
		if item.SHA256 != ComputeSHA256([]byte("Essay body")) {
			t.Fatal("raw item hash mismatch")
		}
	})
}
