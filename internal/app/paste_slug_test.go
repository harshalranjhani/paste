package app_test

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/harshalranjhani/paste/internal/apptest"
)

func TestShortRandomSlugRetriesCollisionWithoutReplacingExistingPaste(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	owner := mustLogin(t, h, "admin", "correct-horse-battery-staple")
	createdPasteID(t, postJSONJarCSRF(t, h, owner, "/api/v1/pastes", map[string]any{"content": "original-source", "slug": "00000000"}))
	// Control randomness at the system boundary to exercise a real URL collision.
	originalRandom := rand.Reader
	rand.Reader = bytes.NewReader(append(make([]byte, 8), bytes.Repeat([]byte{1}, 8)...))
	t.Cleanup(func() { rand.Reader = originalRandom })
	id := createdPasteID(t, postJSONJarCSRF(t, h, owner, "/api/v1/pastes", map[string]any{"content": "new-source", "slug_length": "short"}))
	if len(id) != 8 || id == "00000000" {
		t.Fatalf("retry id: %q", id)
	}
	for slug, expected := range map[string]string{"00000000": "original-source", id: "new-source"} {
		raw, err := h.GET("/p/" + slug + "/raw")
		if err != nil {
			t.Fatal(err)
		}
		if body := readBody(t, raw); body != expected {
			t.Fatalf("source at %s: %q, want %q", slug, body, expected)
		}
	}
}

func TestRandomSlugLengthChoiceAcrossCreateAPIs(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	owner := mustLogin(t, h, "admin", "correct-horse-battery-staple")
	seen := map[string]bool{}
	for _, bundle := range []bool{false, true} {
		for _, length := range []string{"", "long", "short"} {
			for range 3 {
				var res *http.Response
				if bundle {
					res = postBundleJarCSRF(t, h, owner, bundleOpts{Metadata: map[string]any{"slug_length": length}, Files: []bundleFile{{Path: "notes.txt", Content: "random-slug-source"}}})
				} else {
					res = postJSONJarCSRF(t, h, owner, "/api/v1/pastes", map[string]any{"content": "random-slug-source", "slug_length": length})
				}
				id := createdPasteID(t, res)
				// The existing 128-bit base62 encoding omits leading zeroes.
				pattern := `^[A-Za-z0-9]{9,22}$`
				if length == "short" {
					pattern = `^[A-Za-z0-9]{8}$`
				}
				if !regexp.MustCompile(pattern).MatchString(id) || seen[id] {
					t.Fatalf("bundle=%v length=%q: unexpected or duplicate id=%q", bundle, length, id)
				}
				seen[id] = true
				raw, err := h.GET("/p/" + id + "/raw")
				if err != nil {
					t.Fatal(err)
				}
				if body := readBody(t, raw); body != "random-slug-source" {
					t.Fatalf("random URL source: %q", body)
				}
			}
		}
	}
	bad := postJSONJarCSRF(t, h, owner, "/api/v1/pastes", map[string]any{"content": "x", "slug_length": "tiny"})
	body := readBody(t, bad)
	if bad.StatusCode != http.StatusBadRequest || !strings.Contains(body, `"code":"invalid_slug_length"`) {
		t.Fatalf("invalid slug length: %d %q", bad.StatusCode, body)
	}
	id := createdPasteID(t, postJSONJarCSRF(t, h, owner, "/api/v1/pastes", map[string]any{"content": "x", "slug": "custom-overrides-random", "slug_length": "short"}))
	if id != "custom-overrides-random" {
		t.Fatalf("custom slug precedence: %q", id)
	}
}

func TestCustomSlugConcurrentCreationPreservesWinningPaste(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	owner := mustLogin(t, h, "admin", "correct-horse-battery-staple")
	csrf := cookieNamed(owner, h.BaseURL, "csrf")
	type result struct {
		content string
		res     *http.Response
		err     error
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	for _, content := range []string{"first-source", "second-source"} {
		body, err := json.Marshal(map[string]any{"slug": "Shared-slug", "content": content})
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequest(http.MethodPost, h.BaseURL+"/api/v1/pastes", strings.NewReader(string(body)))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", csrf.Value)
		go func() {
			<-start
			res, err := (&http.Client{Jar: owner, Timeout: h.Client.Timeout}).Do(req)
			results <- result{content, res, err}
		}()
	}
	close(start)
	statuses := map[int]int{}
	winner := ""
	for range 2 {
		got := <-results
		if got.err != nil {
			t.Fatal(got.err)
		}
		body := readBody(t, got.res)
		statuses[got.res.StatusCode]++
		if got.res.StatusCode == http.StatusCreated {
			winner = got.content
		}
		if got.res.StatusCode == http.StatusConflict && !strings.Contains(body, `"code":"slug_taken"`) {
			t.Fatalf("collision error: %q", body)
		}
	}
	if statuses[http.StatusCreated] != 1 || statuses[http.StatusConflict] != 1 {
		t.Fatalf("creation statuses=%v, want one 201 and one 409", statuses)
	}
	duplicate := postBundleJarCSRF(t, h, owner, bundleOpts{Metadata: map[string]any{"slug": "Shared-slug"}, Files: []bundleFile{{Path: "replacement.txt", Content: "replacement"}}})
	readBody(t, duplicate)
	if duplicate.StatusCode != http.StatusConflict {
		t.Fatalf("bundle collision: %d", duplicate.StatusCode)
	}
	raw, err := h.GET("/p/Shared-slug/raw")
	if err != nil {
		t.Fatal(err)
	}
	if body := readBody(t, raw); body != winner {
		t.Fatalf("original paste overwritten: %q, want %q", body, winner)
	}
	otherID := createdPasteID(t, postJSONJarCSRF(t, h, owner, "/api/v1/pastes", map[string]any{"slug": "shared-slug", "content": "different-case"}))
	if otherID != "shared-slug" {
		t.Fatal("custom slugs must preserve case")
	}
}

func TestCustomSlugRejectsUnsafeOrOverlongNames(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	owner := mustLogin(t, h, "admin", "correct-horse-battery-staple")
	for _, slug := range []string{"../notes", "a/b", "a\\b", "two words", "<script>", "quoted\"name", "a?b", "a#b", "a%2fb", "-notes", "\nnotes", "notes\n", "café", strings.Repeat("a", 65)} {
		res := postJSONJarCSRF(t, h, owner, "/api/v1/pastes", map[string]any{"content": "private-source", "slug": slug})
		body := readBody(t, res)
		if res.StatusCode != http.StatusBadRequest || !strings.Contains(body, `"code":"invalid_slug"`) {
			t.Fatalf("slug %q: status=%d body=%q", slug, res.StatusCode, body)
		}
	}
	for _, slug := range []string{"a", strings.Repeat("A", 64)} {
		if id := createdPasteID(t, postJSONJarCSRF(t, h, owner, "/api/v1/pastes", map[string]any{"content": "valid", "slug": slug})); id != slug {
			t.Fatalf("valid boundary slug changed: %q", id)
		}
	}
}

func TestCustomSlugCreatesPasteAtChosenURL(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	owner := mustLogin(t, h, "admin", "correct-horse-battery-staple")
	res := postJSONJarCSRF(t, h, owner, "/api/v1/pastes", map[string]any{
		"filename": "notes.txt", "content": "custom-slug-source", "slug": "My-notes_2026",
	})
	id := createdPasteID(t, res)
	if id != "My-notes_2026" {
		t.Fatalf("id=%q, want chosen slug", id)
	}
	raw, err := h.GET("/p/My-notes_2026/raw")
	if err != nil {
		t.Fatal(err)
	}
	if body := readBody(t, raw); raw.StatusCode != http.StatusOK || body != "custom-slug-source" {
		t.Fatalf("custom URL: %d %q", raw.StatusCode, body)
	}
}
