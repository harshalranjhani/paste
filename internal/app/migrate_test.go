package app_test

import (
	"net/http"
	"path/filepath"
	"testing"

	"github.com/harshalranjhani/paste/internal/apptest"
	"github.com/harshalranjhani/paste/internal/config"
)

func TestMigrationsApplyDeterministicallyAcrossRestarts(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "paste.sqlite")

	withDB := func(cfg *config.Config) {
		cfg.DatabasePath = dbPath
		cfg.DataDir = dir
	}

	h1 := apptest.Start(t, withDB)
	res1, err := h1.GET("/healthz")
	if err != nil {
		t.Fatalf("first boot GET /healthz: %v", err)
	}
	res1.Body.Close()
	if res1.StatusCode != http.StatusOK {
		t.Fatalf("first boot status = %d, want %d", res1.StatusCode, http.StatusOK)
	}
	h1.Close()

	h2 := apptest.Start(t, withDB)
	res2, err := h2.GET("/healthz")
	if err != nil {
		t.Fatalf("second boot GET /healthz: %v", err)
	}
	res2.Body.Close()
	if res2.StatusCode != http.StatusOK {
		t.Fatalf("second boot status = %d, want %d", res2.StatusCode, http.StatusOK)
	}
}
