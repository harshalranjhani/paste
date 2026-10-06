package app_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/harshalranjhani/paste/internal/apptest"
)

func TestAuthenticatedCreateReturnsUnlistedURLAnonymousCanView(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	jar := mustLogin(t, h, "admin", "correct-horse-battery-staple")

	const content = "package main\n\nfunc main() {}\n"
	createRes := postJSONJarCSRF(t, h, jar, "/api/v1/pastes", map[string]any{
		"filename": "main.go",
		"content":  content,
	})
	createBody := readBody(t, createRes)
	if createRes.StatusCode != http.StatusCreated && createRes.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/v1/pastes status = %d; body = %q", createRes.StatusCode, createBody)
	}

	var created struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(createBody), &created); err != nil {
		t.Fatalf("parse create response: %v; body = %q", err, createBody)
	}
	if created.ID == "" {
		t.Fatalf("missing id in %q", createBody)
	}
	if !strings.Contains(created.URL, "/p/"+created.ID) {
		t.Fatalf("url %q should contain /p/%s", created.URL, created.ID)
	}

	// Anonymous recipient opens the unlisted URL.
	viewRes, err := h.GET("/p/" + created.ID)
	if err != nil {
		t.Fatalf("GET /p/{id}: %v", err)
	}
	viewBody := readBody(t, viewRes)
	if viewRes.StatusCode != http.StatusOK {
		t.Fatalf("GET /p/{id} status = %d; body = %q", viewRes.StatusCode, viewBody)
	}
	if !strings.Contains(viewBody, "package") || !strings.Contains(viewBody, "main") {
		t.Fatalf("viewer body missing paste content; body = %q", viewBody)
	}
	if strings.Contains(viewBody, `class="tree"`) || strings.Contains(viewBody, `id="file-tree"`) {
		t.Fatalf("single-file viewer must not show a file tree; body = %q", viewBody)
	}
}

func TestUnauthenticatedCreateIsRejected(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")

	body, err := json.Marshal(map[string]any{
		"filename": "x.txt",
		"content":  "hello",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, h.BaseURL+"/api/v1/pastes", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := h.Client.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resBody := readBody(t, res)
	if res.StatusCode == http.StatusOK || res.StatusCode == http.StatusCreated {
		t.Fatalf("unauthenticated create succeeded: %d %q", res.StatusCode, resBody)
	}
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated create status = %d, want 401; body = %q", res.StatusCode, resBody)
	}
}

func TestPublicIDHasHighEntropy(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	jar := mustLogin(t, h, "admin", "correct-horse-battery-staple")

	ids := map[string]struct{}{}
	for i := 0; i < 5; i++ {
		res := postJSONJarCSRF(t, h, jar, "/api/v1/pastes", map[string]any{
			"filename": "a.txt",
			"content":  "x",
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
		// 16 random bytes → base62 is typically ≥21 chars; require ≥20 and unique.
		if len(created.ID) < 20 {
			t.Fatalf("public_id %q too short for ~96 bits entropy (len=%d)", created.ID, len(created.ID))
		}
		for _, c := range created.ID {
			if !((c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')) {
				t.Fatalf("public_id %q has non-base62 rune %q", created.ID, c)
			}
		}
		if _, dup := ids[created.ID]; dup {
			t.Fatalf("duplicate public_id %q", created.ID)
		}
		ids[created.ID] = struct{}{}
	}
}

func TestViewerShowsHighlightingAndLineNumbers(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	jar := mustLogin(t, h, "admin", "correct-horse-battery-staple")

	res := postJSONJarCSRF(t, h, jar, "/api/v1/pastes", map[string]any{
		"filename": "hello.py",
		"content":  "def greet():\n    return 'hi'\n",
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

	viewRes, err := h.GET("/p/" + created.ID)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	viewBody := readBody(t, viewRes)
	if viewRes.StatusCode != http.StatusOK {
		t.Fatalf("view status = %d; body = %q", viewRes.StatusCode, viewBody)
	}
	// Chroma HTML wraps tokens in spans with classes.
	if !strings.Contains(viewBody, `class="chroma"`) && !strings.Contains(viewBody, `class="line"`) {
		t.Fatalf("expected Chroma-highlighted markup; body = %q", viewBody)
	}
	if !strings.Contains(viewBody, "line-numbers") && !strings.Contains(viewBody, "linenos") && !strings.Contains(viewBody, `class="ln"`) {
		t.Fatalf("expected line numbers in viewer; body = %q", viewBody)
	}
	if strings.Contains(viewBody, `id="file-tree"`) || strings.Contains(viewBody, `class="tree"`) {
		t.Fatalf("single-file viewer must not include tree sidebar; body = %q", viewBody)
	}
}

func TestRawEndpointServesPlainTextWithProtections(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	jar := mustLogin(t, h, "admin", "correct-horse-battery-staple")

	const content = "line1\nline2\n"
	res := postJSONJarCSRF(t, h, jar, "/api/v1/pastes", map[string]any{
		"filename": "notes.txt",
		"content":  content,
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

	rawRes, err := h.GET("/p/" + created.ID + "/raw")
	if err != nil {
		t.Fatalf("GET raw: %v", err)
	}
	rawBody := readBody(t, rawRes)
	if rawRes.StatusCode != http.StatusOK {
		t.Fatalf("raw status = %d; body = %q", rawRes.StatusCode, rawBody)
	}
	ct := rawRes.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type = %q, want text/plain", ct)
	}
	if rawRes.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("missing X-Content-Type-Options: nosniff")
	}
	robots := rawRes.Header.Get("X-Robots-Tag")
	if !strings.Contains(robots, "noindex") {
		t.Fatalf("X-Robots-Tag = %q, want noindex", robots)
	}
	if rawBody != content {
		t.Fatalf("raw body = %q, want %q", rawBody, content)
	}
}

func TestDefaultExpiryIs90DaysAndOverMaxRejected(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	jar := mustLogin(t, h, "admin", "correct-horse-battery-staple")

	def := postJSONJarCSRF(t, h, jar, "/api/v1/pastes", map[string]any{
		"filename": "a.txt",
		"content":  "a",
	})
	defBody := readBody(t, def)
	if def.StatusCode != http.StatusCreated {
		t.Fatalf("default create status = %d; body = %q", def.StatusCode, defBody)
	}
	var created struct {
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.Unmarshal([]byte(defBody), &created); err != nil {
		t.Fatalf("parse: %v", err)
	}
	exp, err := time.Parse(time.RFC3339Nano, created.ExpiresAt)
	if err != nil {
		exp, err = time.Parse(time.RFC3339, created.ExpiresAt)
		if err != nil {
			t.Fatalf("parse expires_at: %v", err)
		}
	}
	until := time.Until(exp)
	if until < 89*24*time.Hour || until > 91*24*time.Hour {
		t.Fatalf("default TTL = %s, want ~90 days", until)
	}

	over := postJSONJarCSRF(t, h, jar, "/api/v1/pastes", map[string]any{
		"filename":   "b.txt",
		"content":    "b",
		"expires_in": "91d",
	})
	overBody := readBody(t, over)
	if over.StatusCode == http.StatusOK || over.StatusCode == http.StatusCreated {
		t.Fatalf("over-max expiry was accepted: %d %q", over.StatusCode, overBody)
	}
	if over.StatusCode != http.StatusBadRequest {
		t.Fatalf("over-max status = %d, want 400; body = %q", over.StatusCode, overBody)
	}
}

func TestUnknownPasteIs404ExpiredIs410(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	jar := mustLogin(t, h, "admin", "correct-horse-battery-staple")

	missing, err := h.GET("/p/doesNotExist00000000001")
	if err != nil {
		t.Fatalf("GET missing: %v", err)
	}
	missingBody := readBody(t, missing)
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown paste status = %d, want 404; body = %q", missing.StatusCode, missingBody)
	}

	res := postJSONJarCSRF(t, h, jar, "/api/v1/pastes", map[string]any{
		"filename":   "soon.txt",
		"content":    "temporary",
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

	time.Sleep(1100 * time.Millisecond)

	gone, err := h.GET("/p/" + created.ID)
	if err != nil {
		t.Fatalf("GET expired: %v", err)
	}
	goneBody := readBody(t, gone)
	if gone.StatusCode != http.StatusGone {
		t.Fatalf("expired paste status = %d, want 410; body = %q", gone.StatusCode, goneBody)
	}

	rawGone, err := h.GET("/p/" + created.ID + "/raw")
	if err != nil {
		t.Fatalf("GET expired raw: %v", err)
	}
	rawBody := readBody(t, rawGone)
	if rawGone.StatusCode != http.StatusGone {
		t.Fatalf("expired raw status = %d, want 410; body = %q", rawGone.StatusCode, rawBody)
	}
}

func TestOwnerCanDeletePaste(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	jar := mustLogin(t, h, "admin", "correct-horse-battery-staple")

	res := postJSONJarCSRF(t, h, jar, "/api/v1/pastes", map[string]any{
		"filename": "del.txt",
		"content":  "bye",
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

	delRes := deleteJSONJarCSRF(t, h, jar, "/api/v1/pastes/"+created.ID)
	delBody := readBody(t, delRes)
	if delRes.StatusCode != http.StatusNoContent && delRes.StatusCode != http.StatusOK {
		t.Fatalf("DELETE status = %d; body = %q", delRes.StatusCode, delBody)
	}

	view, err := h.GET("/p/" + created.ID)
	if err != nil {
		t.Fatalf("GET after delete: %v", err)
	}
	viewBody := readBody(t, view)
	if view.StatusCode != http.StatusNotFound {
		t.Fatalf("after delete status = %d, want 404; body = %q", view.StatusCode, viewBody)
	}
}

func TestPasteResponsesAreNoindexAndNoListing(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	jar := mustLogin(t, h, "admin", "correct-horse-battery-staple")

	res := postJSONJarCSRF(t, h, jar, "/api/v1/pastes", map[string]any{
		"filename": "priv.txt",
		"content":  "secret-ish",
	})
	body := readBody(t, res)
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		t.Fatalf("parse: %v", err)
	}

	view, err := h.GET("/p/" + created.ID)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	viewBody := readBody(t, view)
	if !strings.Contains(view.Header.Get("X-Robots-Tag"), "noindex") {
		t.Fatalf("missing X-Robots-Tag noindex: %q", view.Header.Get("X-Robots-Tag"))
	}
	if !strings.Contains(viewBody, `name="robots"`) || !strings.Contains(viewBody, "noindex") {
		t.Fatalf("missing robots meta noindex; body = %q", viewBody)
	}

	for _, path := range []string{"/pastes", "/recent", "/public", "/api/v1/pastes"} {
		listRes, err := h.GET(path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		listBody := readBody(t, listRes)
		if listRes.StatusCode == http.StatusOK && strings.Contains(listBody, created.ID) {
			t.Fatalf("listing route %s exposed paste id", path)
		}
		if path != "/api/v1/pastes" && listRes.StatusCode == http.StatusOK {
			t.Fatalf("unexpected public listing route %s returned 200", path)
		}
	}
}

func TestNonUTF8ContentIsRejected(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	jar := mustLogin(t, h, "admin", "correct-horse-battery-staple")

	// Raw 0xFF/0xFE inside the JSON string (not JSON escapes) is invalid UTF-8.
	payload := []byte(`{"filename":"bin.dat","content":"hi`)
	payload = append(payload, 0xff, 0xfe)
	payload = append(payload, []byte(`"}`)...)
	req, err := http.NewRequest(http.MethodPost, h.BaseURL+"/api/v1/pastes", strings.NewReader(string(payload)))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if csrf := cookieNamed(jar, h.BaseURL, "csrf"); csrf != nil {
		req.Header.Set("X-CSRF-Token", csrf.Value)
	}
	client := &http.Client{Jar: jar, Timeout: h.Client.Timeout}
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	body := readBody(t, res)
	if res.StatusCode == http.StatusOK || res.StatusCode == http.StatusCreated {
		t.Fatalf("non-UTF-8 create succeeded: %d %q", res.StatusCode, body)
	}
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("non-UTF-8 status = %d, want 400; body = %q", res.StatusCode, body)
	}
}

func TestWebNewCreatesSingleFilePaste(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	jar := mustLogin(t, h, "admin", "correct-horse-battery-staple")

	formPage := getJar(t, h, jar, "/new")
	formBody := readBody(t, formPage)
	if formPage.StatusCode != http.StatusOK {
		t.Fatalf("GET /new status = %d; body = %q", formPage.StatusCode, formBody)
	}
	if !strings.Contains(formBody, `name="content"`) && !strings.Contains(formBody, `name="filename"`) {
		t.Fatalf("/new form missing fields; body = %q", formBody)
	}

	create := postFormJarCSRF(t, h, jar, "/new", url.Values{
		"filename": {"web.go"},
		"content":  {"fmt.Println(\"hi\")"},
	})
	createBody := readBody(t, create)
	if create.StatusCode != http.StatusSeeOther && create.StatusCode != http.StatusOK {
		t.Fatalf("POST /new status = %d; body = %q", create.StatusCode, createBody)
	}

	loc := create.Header.Get("Location")
	if create.StatusCode == http.StatusSeeOther {
		if !strings.Contains(loc, "/p/") {
			t.Fatalf("redirect Location = %q, want /p/...", loc)
		}
	} else if !strings.Contains(createBody, "/p/") {
		t.Fatalf("create response missing paste url; body = %q", createBody)
	}

	pastePath := loc
	if pastePath == "" {
		// fall back: extract from body
		idx := strings.Index(createBody, "/p/")
		if idx < 0 {
			t.Fatal("no paste path found")
		}
		end := idx + 3
		for end < len(createBody) && isPublicIDChar(createBody[end]) {
			end++
		}
		pastePath = createBody[idx:end]
	}
	view, err := h.GET(pastePath)
	if err != nil {
		t.Fatalf("GET created paste: %v", err)
	}
	viewBody := readBody(t, view)
	if view.StatusCode != http.StatusOK {
		t.Fatalf("view status = %d; body = %q", view.StatusCode, viewBody)
	}
	if !strings.Contains(viewBody, "Println") {
		t.Fatalf("viewer missing content; body = %q", viewBody)
	}
}

func TestPasteIsImmutableNoEdit(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	jar := mustLogin(t, h, "admin", "correct-horse-battery-staple")

	res := postJSONJarCSRF(t, h, jar, "/api/v1/pastes", map[string]any{
		"filename": "imm.txt",
		"content":  "original",
	})
	body := readBody(t, res)
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		t.Fatalf("parse: %v", err)
	}

	for _, method := range []string{http.MethodPut, http.MethodPatch} {
		req, err := http.NewRequest(method, h.BaseURL+"/api/v1/pastes/"+created.ID, strings.NewReader(`{"content":"changed"}`))
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		if csrf := cookieNamed(jar, h.BaseURL, "csrf"); csrf != nil {
			req.Header.Set("X-CSRF-Token", csrf.Value)
		}
		client := &http.Client{Jar: jar, Timeout: h.Client.Timeout}
		editRes, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		editBody := readBody(t, editRes)
		if editRes.StatusCode == http.StatusOK || editRes.StatusCode == http.StatusNoContent {
			t.Fatalf("%s edit succeeded: %d %q", method, editRes.StatusCode, editBody)
		}
	}

	view, err := h.GET("/p/" + created.ID)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	viewBody := readBody(t, view)
	if strings.Contains(viewBody, "changed") {
		t.Fatal("paste content was mutated")
	}
	if !strings.Contains(viewBody, "original") {
		t.Fatalf("original content missing; body = %q", viewBody)
	}
}

func deleteJSONJarCSRF(t *testing.T, h *apptest.Harness, jar http.CookieJar, path string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, h.BaseURL+path, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if csrf := cookieNamed(jar, h.BaseURL, "csrf"); csrf != nil {
		req.Header.Set("X-CSRF-Token", csrf.Value)
	}
	client := &http.Client{
		Jar:     jar,
		Timeout: h.Client.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("DELETE %s: %v", path, err)
	}
	return res
}

func isPublicIDChar(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}

func postJSONJarCSRF(t *testing.T, h *apptest.Harness, jar http.CookieJar, path string, payload map[string]any) *http.Response {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, h.BaseURL+path, strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if csrf := cookieNamed(jar, h.BaseURL, "csrf"); csrf != nil {
		req.Header.Set("X-CSRF-Token", csrf.Value)
	}
	client := &http.Client{
		Jar:     jar,
		Timeout: h.Client.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	return res
}
