package app_test

import (
	"io"
	"net/http"
	"testing"

	"github.com/harshalranjhani/paste/internal/apptest"
)

func TestHealthzReturnsOKWhenProcessAndDBHealthy(t *testing.T) {
	h := apptest.Start(t)

	res, err := h.GET("/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("status = %d, want %d; body = %q", res.StatusCode, http.StatusOK, body)
	}
}
