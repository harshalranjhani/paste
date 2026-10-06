package app_test

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/harshalranjhani/paste/internal/app"
	"github.com/harshalranjhani/paste/internal/config"
)

func TestGracefulShutdownStopsAcceptingRequests(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{
		BaseURL:      "http://127.0.0.1:0",
		ListenAddr:   "127.0.0.1:0",
		DatabasePath: filepath.Join(dir, "paste.sqlite"),
		DataDir:      dir,
	}
	srv, err := app.New(cfg)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- srv.Run(ctx)
	}()

	baseURL, err := srv.WaitReady(ctx, 5*time.Second)
	if err != nil {
		cancel()
		t.Fatalf("WaitReady: %v", err)
	}

	res, err := http.Get(baseURL + "/healthz")
	if err != nil {
		cancel()
		t.Fatalf("GET /healthz before shutdown: %v", err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		cancel()
		t.Fatalf("healthz status = %d", res.StatusCode)
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run after cancel: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not shut down within timeout")
	}

	client := &http.Client{Timeout: time.Second}
	_, err = client.Get(baseURL + "/healthz")
	if err == nil {
		t.Fatal("expected connection error after shutdown")
	}
}
