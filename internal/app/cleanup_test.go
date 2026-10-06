package app_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/harshalranjhani/paste/internal/apptest"
	"github.com/harshalranjhani/paste/internal/config"
)

func TestCleanupDeletesExpiredPasteSoLookupBecomes404(t *testing.T) {
	h := apptest.Start(t, func(cfg *config.Config) {
		cfg.CleanupInterval = 100 * time.Millisecond
	})
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	jar := mustLogin(t, h, "admin", "correct-horse-battery-staple")

	res := postJSONJarCSRF(t, h, jar, "/api/v1/pastes", map[string]any{
		"filename":   "ephemeral.txt",
		"content":    "gone soon",
		"expires_in": "1s",
	})
	body := readBody(t, res)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d; body = %q", res.StatusCode, body)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		t.Fatalf("parse: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	gotGone := false
	for time.Now().Before(deadline) {
		gone, err := h.GET("/p/" + created.ID)
		if err != nil {
			t.Fatalf("GET while waiting for expiry: %v", err)
		}
		status := gone.StatusCode
		_ = gone.Body.Close()
		if status == http.StatusGone {
			gotGone = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !gotGone {
		t.Fatal("expected 410 before cleanup")
	}

	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		cleaned, err := h.GET("/p/" + created.ID)
		if err != nil {
			t.Fatalf("GET while waiting for cleanup: %v", err)
		}
		status := cleaned.StatusCode
		_ = cleaned.Body.Close()
		if status == http.StatusNotFound {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("cleanup did not remove expired paste (still not 404)")
}

func TestCleanupRemovesExpiredInviteFromAdminList(t *testing.T) {
	h := apptest.Start(t, func(cfg *config.Config) {
		cfg.CleanupInterval = 100 * time.Millisecond
	})
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	jar := mustLogin(t, h, "admin", "correct-horse-battery-staple")

	created := mustCreateInvite(t, h, jar, url.Values{"ttl_seconds": {"1"}})

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		listRes := getJar(t, h, jar, "/admin/invites")
		listBody := readBody(t, listRes)
		if listRes.StatusCode != http.StatusOK {
			t.Fatalf("list status = %d; body = %q", listRes.StatusCode, listBody)
		}
		var invites []struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal([]byte(listBody), &invites); err != nil {
			t.Fatalf("parse list: %v; body = %q", err, listBody)
		}
		found := false
		for _, inv := range invites {
			if inv.ID == created.ID {
				found = true
				break
			}
		}
		if !found {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("cleanup did not remove expired invite from admin list")
}
