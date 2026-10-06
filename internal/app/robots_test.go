package app_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/harshalranjhani/paste/internal/apptest"
)

func TestRobotsTxtIsRestrictiveAndSitemapHasNoPasteIDs(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	jar := mustLogin(t, h, "admin", "correct-horse-battery-staple")

	res := postJSONJarCSRF(t, h, jar, "/api/v1/pastes", map[string]any{
		"filename": "hidden.txt",
		"content":  "not for crawlers",
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

	robots, err := h.GET("/robots.txt")
	if err != nil {
		t.Fatalf("GET /robots.txt: %v", err)
	}
	robotsBody := readBody(t, robots)
	if robots.StatusCode != http.StatusOK {
		t.Fatalf("robots.txt status = %d; body = %q", robots.StatusCode, robotsBody)
	}
	lower := strings.ToLower(robotsBody)
	if !strings.Contains(lower, "disallow: /") {
		t.Fatalf("robots.txt should disallow all; body = %q", robotsBody)
	}
	if strings.Contains(robotsBody, created.ID) {
		t.Fatal("robots.txt must not contain paste IDs")
	}

	sitemap, err := h.GET("/sitemap.xml")
	if err != nil {
		t.Fatalf("GET /sitemap.xml: %v", err)
	}
	sitemapBody := readBody(t, sitemap)
	if sitemap.StatusCode == http.StatusOK && strings.Contains(sitemapBody, created.ID) {
		t.Fatalf("sitemap must not list paste IDs; body = %q", sitemapBody)
	}
	if sitemap.StatusCode == http.StatusOK && strings.Contains(sitemapBody, "/p/") {
		t.Fatalf("sitemap must not list paste paths; body = %q", sitemapBody)
	}
}
