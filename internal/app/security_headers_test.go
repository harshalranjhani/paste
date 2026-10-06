package app_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/harshalranjhani/paste/internal/apptest"
)

func TestAppResponsesIncludeSecurityHeadersAndCSP(t *testing.T) {
	h := apptest.Start(t)

	res, err := h.GET("/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}

	csp := res.Header.Get("Content-Security-Policy")
	if csp == "" {
		t.Fatal("missing Content-Security-Policy")
	}
	if !strings.Contains(csp, "default-src 'none'") {
		t.Fatalf("CSP missing default-src 'none': %q", csp)
	}
	if !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Fatalf("CSP missing frame-ancestors 'none': %q", csp)
	}

	if res.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", res.Header.Get("X-Content-Type-Options"))
	}
	if res.Header.Get("X-Frame-Options") != "DENY" {
		t.Fatalf("X-Frame-Options = %q, want DENY", res.Header.Get("X-Frame-Options"))
	}
	if res.Header.Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("Referrer-Policy = %q, want no-referrer", res.Header.Get("Referrer-Policy"))
	}
}
