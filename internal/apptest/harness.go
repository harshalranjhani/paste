package apptest

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/harshalranjhani/paste/internal/app"
	"github.com/harshalranjhani/paste/internal/config"
)

// Harness is a running app instance backed by a temporary SQLite database.
type Harness struct {
	BaseURL string
	Client  *http.Client
	Config  config.Config
	cancel  context.CancelFunc
	done    <-chan error
}

// Start boots the application against a temp SQLite DB and returns an HTTP client seam.
func Start(t *testing.T, opts ...func(*config.Config)) *Harness {
	t.Helper()

	dir := t.TempDir()
	cfg := config.Config{
		BaseURL:      "http://127.0.0.1:0",
		ListenAddr:   "127.0.0.1:0",
		DatabasePath: filepath.Join(dir, "paste.sqlite"),
		DataDir:      dir,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return StartWithConfig(t, cfg)
}

// StartWithConfig boots the application with an explicit config.
func StartWithConfig(t *testing.T, cfg config.Config) *Harness {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	srv, err := app.New(cfg)
	if err != nil {
		cancel()
		t.Fatalf("app.New: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- srv.Run(ctx)
	}()

	baseURL, err := srv.WaitReady(ctx, 5*time.Second)
	if err != nil {
		cancel()
		t.Fatalf("WaitReady: %v", err)
	}

	h := &Harness{
		BaseURL: baseURL,
		Client:  &http.Client{Timeout: 5 * time.Second},
		Config:  cfg,
		cancel:  cancel,
		done:    done,
	}
	t.Cleanup(func() {
		h.Close()
	})
	return h
}

// Close shuts down the harness server.
func (h *Harness) Close() {
	if h.cancel != nil {
		h.cancel()
		h.cancel = nil
		select {
		case <-h.done:
		case <-time.After(5 * time.Second):
		}
	}
}

// GET issues a GET request against the harness base URL.
func (h *Harness) GET(path string) (*http.Response, error) {
	return h.Client.Get(h.BaseURL + path)
}
