package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo"
)

const (
	filesRoot      = "data/files"
	collectedRoot  = "data/collected"
	maxCollectBody = 512 << 20 // 512 MB per upload
)

// FilesService implements file distribution and collection.
type FilesService struct{ c *Console }

// CollectedFile is one homework archive uploaded by a student.
type CollectedFile struct {
	DeviceID  string `json:"device_id"`
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_bytes"`
	UpdatedAt string `json:"updated_at"`
}

// Distribute opens a file picker, stages the file for download and queues a
// distribute command to the selected devices ("*" = all).
func (s *FilesService) Distribute(ctx context.Context, deviceIDs []string) (string, error) {
	src, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent: mygo.CallerWindow(ctx),
		Title:  "选择要分发的文件",
	})
	if err != nil {
		return "", err
	}
	if len(src) == 0 || src[0] == "" {
		return "", nil // cancelled
	}

	fileID := newFileToken()
	stagedDir := filepath.Join(filesRoot, fileID)
	if err := os.MkdirAll(stagedDir, 0o755); err != nil {
		return "", err
	}

	name := filepath.Base(src[0])
	stagedPath := filepath.Join(stagedDir, name)
	if err := copyFile(src[0], stagedPath); err != nil {
		return "", fmt.Errorf("stage file: %w", err)
	}

	payload, _ := json.Marshal(map[string]any{
		"url":  fmt.Sprintf("http://%s/files/%s/%s", localLanIP(), fileID, name),
		"name": name,
	})
	target := "*"
	if len(deviceIDs) == 1 {
		target = deviceIDs[0]
	}
	s.c.hub.issue(target, "file.distribute", string(payload))
	return name, nil
}

// ListCollected returns the homework archives received from students.
func (s *FilesService) ListCollected(_ context.Context) ([]CollectedFile, error) {
	out := []CollectedFile{}
	entries, err := os.ReadDir(collectedRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	for _, deviceDir := range entries {
		if !deviceDir.IsDir() {
			continue
		}
		files, _ := os.ReadDir(filepath.Join(collectedRoot, deviceDir.Name()))
		for _, f := range files {
			info, err := f.Info()
			if err != nil {
				continue
			}
			out = append(out, CollectedFile{
				DeviceID:  deviceDir.Name(),
				Name:      f.Name(),
				SizeBytes: info.Size(),
				UpdatedAt: info.ModTime().Format(time.RFC3339),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out, nil
}

// HTTP handlers -------------------------------------------------------------

// handleServeFile serves a staged distribution file: /files/{id}/{name}.
func (c *Console) handleServeFile(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/files/")
	if rest == "" || strings.Contains(rest, "..") {
		http.NotFound(w, r)
		return
	}
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(filesRoot, filepath.FromSlash(parts[0]), filepath.Base(parts[1]))
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeFile(w, r, path)
}

// handleCollectUpload receives a homework archive from a student agent.
func (c *Console) handleCollectUpload(w http.ResponseWriter, r *http.Request) {
	deviceID := r.Header.Get("X-Device-ID")
	if deviceID == "" {
		http.Error(w, "missing X-Device-ID header", http.StatusBadRequest)
		return
	}
	name := r.Header.Get("X-Filename")
	if name == "" {
		name = "homework.zip"
	}
	name = filepath.Base(name)

	deviceDir := filepath.Join(collectedRoot, deviceID)
	if err := os.MkdirAll(deviceDir, 0o755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	dst, err := os.Create(filepath.Join(deviceDir, fmt.Sprintf("%d_%s", time.Now().Unix(), name)))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer dst.Close()
	if _, err := io.Copy(dst, io.LimitReader(r.Body, maxCollectBody)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// helpers -------------------------------------------------------------------

func newFileToken() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// localLanIP returns the first non-loopback IPv4 address of this machine,
// used in download URLs handed to student agents.
func localLanIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "127.0.0.1"
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipNet.IP.To4()
			if ip != nil {
				return ip.String()
			}
		}
	}
	return "127.0.0.1"
}
