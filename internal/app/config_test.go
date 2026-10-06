package app_test

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/harshalranjhani/paste/internal/apptest"
	"github.com/harshalranjhani/paste/internal/config"
)

func TestAppRespectsEnvConfig(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "custom.sqlite")

	t.Setenv("BASE_URL", "https://paste.example.com")
	t.Setenv("LISTEN_ADDR", "127.0.0.1:0")
	t.Setenv("DATABASE_PATH", dbPath)
	t.Setenv("DATA_DIR", dir)
	t.Setenv("SESSION_SECRET", "test-session-secret-placeholder")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	h := apptest.StartWithConfig(t, cfg)

	if h.Config.BaseURL != "https://paste.example.com" {
		t.Fatalf("BaseURL = %q, want %q", h.Config.BaseURL, "https://paste.example.com")
	}
	if h.Config.DatabasePath != dbPath {
		t.Fatalf("DatabasePath = %q, want %q", h.Config.DatabasePath, dbPath)
	}

	res, err := h.GET("/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.StatusCode, http.StatusOK)
	}

	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("database file not created at configured path: %v", err)
	}
}
