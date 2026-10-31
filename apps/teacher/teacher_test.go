package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"localmesh/internal/platform/common"
)

func TestTeacherAppEndpoints(t *testing.T) {
	rt, err := common.RuntimeForCurrentOS()
	if err != nil {
		t.Fatalf("RuntimeForCurrentOS failed: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "teacher.db")
	ctx := context.Background()

	app, err := NewTeacherApp(ctx, rt, dbPath, "127.0.0.1:0", "", nil)
	if err != nil {
		t.Fatalf("NewTeacherApp failed: %v", err)
	}
	defer app.Stop()

	handler := app.apiServer.Handler()

	// 1. Test /api/v1/health
	{
		req := httptest.NewRequest("GET", "/api/v1/health", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 for /health, got %d", rec.Code)
		}
	}

	// 2. Test /api/v1/candidates
	{
		req := httptest.NewRequest("GET", "/api/v1/candidates", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 for /candidates, got %d", rec.Code)
		}
	}

	// 3. Test Chat send and retrieve
	{
		body, _ := json.Marshal(map[string]string{
			"message": "Hello class!",
		})
		req := httptest.NewRequest("POST", "/api/v1/chat/messages", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 for chat send, got %d", rec.Code)
		}

		getReq := httptest.NewRequest("GET", "/api/v1/chat/messages", nil)
		getRec := httptest.NewRecorder()
		handler.ServeHTTP(getRec, getReq)
		if getRec.Code != http.StatusOK {
			t.Errorf("expected 200 for chat get, got %d", getRec.Code)
		}
	}

	// 4. Test Lock command
	{
		body, _ := json.Marshal(map[string]any{
			"action":          "lock",
			"title":           "Focus",
			"message":         "Please watch the teacher screen",
			"lock_input_only": false,
		})
		req := httptest.NewRequest("POST", "/api/v1/commands/lock", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 for lock command, got %d", rec.Code)
		}
	}

	// 5. Test Power command
	{
		body, _ := json.Marshal(map[string]any{
			"action": "lock",
			"force":  true,
		})
		req := httptest.NewRequest("POST", "/api/v1/commands/power", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 for power command, got %d", rec.Code)
		}
	}

	// 6. Test Web UI serving
	{
		req := httptest.NewRequest("GET", "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 for Web UI root, got %d", rec.Code)
		}
	}
}
