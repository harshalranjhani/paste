package app_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/harshalranjhani/paste/internal/apptest"
)

func TestRegisterPathUnavailable(t *testing.T) {
	h := apptest.Start(t)

	res, err := h.GET("/register")
	if err != nil {
		t.Fatalf("GET /register: %v", err)
	}
	body := readBody(t, res)
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /register status = %d, want 404; body = %q", res.StatusCode, body)
	}
}

func TestHomePromptsSignInWhenUnauthenticated(t *testing.T) {
	h := apptest.Start(t)

	res, err := h.GET("/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	body := readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET / status = %d; body = %q", res.StatusCode, body)
	}
	lower := strings.ToLower(body)
	if !strings.Contains(lower, "sign in") && !strings.Contains(lower, "log in") {
		t.Fatalf("home should prompt sign-in; body = %q", body)
	}
	if strings.Contains(lower, "<textarea") || strings.Contains(lower, `name="content"`) || strings.Contains(lower, `action="/new"`) {
		t.Fatalf("unauthenticated home must not show create editor; body = %q", body)
	}
}
