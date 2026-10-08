package app_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/harshalranjhani/paste/internal/apptest"
	"github.com/harshalranjhani/paste/internal/config"
)

func assertBurnAPIError(t *testing.T, res *http.Response, status int, code string) {
	t.Helper()
	body := readBody(t, res)
	var payload struct {
		Error struct{ Code, Message string } `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil || res.StatusCode != status || res.Header.Get("Content-Type") != "application/json" || payload.Error.Code != code || payload.Error.Message == "" {
		t.Fatalf("API error: status=%d type=%q body=%q, want %d %s JSON", res.StatusCode, res.Header.Get("Content-Type"), body, status, code)
	}
}

func TestBurnAPIErrorResponsesAreJSON(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	owner := mustLogin(t, h, "admin", "correct-horse-battery-staple")
	id := createdPasteID(t, postJSONJarCSRF(t, h, owner, "/api/v1/pastes", map[string]any{
		"slug": "api-burn-errors", "content": "private-source", "burn_after_read": true,
	}))
	winner, loser := newJar(t), newJar(t)
	assertBurnAPIError(t, postFormJar(t, h, winner, "/api/v1/pastes/"+id+"/reveal", url.Values{}), http.StatusForbidden, "csrf_required")
	readBody(t, getJar(t, h, winner, "/api/v1/pastes/"+id+"/meta"))
	res := revealBurnPaste(t, h, winner, id, "/api/v1/pastes/")
	readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("reveal: %d", res.StatusCode)
	}
	assertBurnAPIError(t, revealBurnPaste(t, h, winner, id, "/api/v1/pastes/"), http.StatusGone, "gone")
	var metadata struct {
		Files []struct{ ID int64 }
	}
	if err := json.Unmarshal([]byte(readBody(t, getJar(t, h, winner, "/api/v1/pastes/"+id+"/meta"))), &metadata); err != nil || len(metadata.Files) != 1 {
		t.Fatalf("metadata=%+v err=%v", metadata, err)
	}
	file := "/files/" + strconv.FormatInt(metadata.Files[0].ID, 10)
	for _, suffix := range []string{"/meta", "/archive.zip", file, file + "/raw"} {
		assertBurnAPIError(t, getJar(t, h, loser, "/api/v1/pastes/"+id+suffix), http.StatusGone, "gone")
	}
	assertBurnAPIError(t, postFormJar(t, h, loser, "/api/v1/pastes/"+id+"/reveal", url.Values{}), http.StatusGone, "gone")
}

func TestBurnLeaseExpiresEvenAfterRefreshAndCleanup(t *testing.T) {
	h := apptest.Start(t, func(cfg *config.Config) {
		cfg.BurnLeaseTTL = 2 * time.Second
		cfg.CleanupInterval = 30 * time.Millisecond
	})
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	owner := mustLogin(t, h, "admin", "correct-horse-battery-staple")
	id := createdPasteID(t, postJSONJarCSRF(t, h, owner, "/api/v1/pastes", map[string]any{
		"content": "expired-lease-secret", "burn_after_read": true,
	}))
	reader := newJar(t)
	readBody(t, getJar(t, h, reader, "/p/"+id))
	reveal := revealBurnPaste(t, h, reader, id, "/p/")
	readBody(t, reveal)
	if reveal.StatusCode != http.StatusSeeOther {
		t.Fatalf("reveal: %d", reveal.StatusCode)
	}
	refresh := getJar(t, h, reader, "/p/"+id)
	if body := readBody(t, refresh); refresh.StatusCode != http.StatusOK || !strings.Contains(body, "expired-lease-secret") {
		t.Fatalf("live lease refresh: %d", refresh.StatusCode)
	}
	grant := cookieNamed(reader, h.BaseURL, "paste_burn_"+id)
	if grant == nil {
		t.Fatal("reveal must grant a lease cookie")
	}
	time.Sleep(2100 * time.Millisecond)
	// Retain the token beyond browser cookie expiry to verify server enforcement.
	site, err := url.Parse(h.BaseURL)
	if err != nil {
		t.Fatal(err)
	}
	reader.SetCookies(site, []*http.Cookie{grant})
	for _, route := range []string{"/p/" + id, "/p/" + id + "/raw", "/p/" + id + "/archive.zip"} {
		res := getJar(t, h, reader, route)
		body := readBody(t, res)
		if res.StatusCode != http.StatusGone || strings.Contains(body, "expired-lease-secret") {
			t.Fatalf("expired lease at %s: status=%d", route, res.StatusCode)
		}
	}
	for _, suffix := range []string{"/meta", "/archive.zip"} {
		assertBurnAPIError(t, getJar(t, h, reader, "/api/v1/pastes/"+id+suffix), http.StatusGone, "gone")
	}
	res := revealBurnPaste(t, h, reader, id, "/p/")
	readBody(t, res)
	if res.StatusCode != http.StatusGone {
		t.Fatalf("expired lease cannot be renewed: %d", res.StatusCode)
	}
}

func TestPasswordAndHEADRequestsCannotConsumeBurnPaste(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	owner := mustLogin(t, h, "admin", "correct-horse-battery-staple")
	id := createdPasteID(t, postJSONJarCSRF(t, h, owner, "/api/v1/pastes", map[string]any{
		"filename": "hidden.env", "content": "password-burn-secret", "password": "reveal-password", "burn_after_read": true,
	}))
	reader := newJar(t)
	for _, route := range []string{"/p/" + id, "/p/" + id + "/raw", "/p/" + id + "/reveal", "/api/v1/pastes/" + id + "/reveal"} {
		req, err := http.NewRequest(http.MethodHead, h.BaseURL+route, nil)
		if err != nil {
			t.Fatal(err)
		}
		res, err := h.Client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		readBody(t, res)
	}
	locked := getJar(t, h, reader, "/p/"+id)
	lockedBody := readBody(t, locked)
	if strings.Contains(lockedBody, "hidden.env") || strings.Contains(lockedBody, "password-burn-secret") {
		t.Fatal("locked burn leaked data")
	}
	readBody(t, getJar(t, h, reader, "/api/v1/pastes/"+id+"/meta"))
	blocked := revealBurnPaste(t, h, reader, id, "/p/")
	readBody(t, blocked)
	if blocked.StatusCode != http.StatusUnauthorized {
		t.Fatalf("locked reveal: %d", blocked.StatusCode)
	}
	wrong := postFormJar(t, h, reader, "/p/"+id+"/unlock", url.Values{"password": {"wrong"}})
	readBody(t, wrong)
	if wrong.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong password: %d", wrong.StatusCode)
	}
	readBody(t, postFormJar(t, h, reader, "/p/"+id+"/unlock", url.Values{"password": {"reveal-password"}}))
	landing := getJar(t, h, reader, "/p/"+id)
	if body := readBody(t, landing); !strings.Contains(body, "Reveal paste") || strings.Contains(body, "password-burn-secret") {
		t.Fatal("unlock must leave burn unconsumed")
	}
	revealed := revealBurnPaste(t, h, reader, id, "/p/")
	readBody(t, revealed)
	if revealed.StatusCode != http.StatusSeeOther {
		t.Fatalf("reveal after correct password: %d", revealed.StatusCode)
	}
	view := getJar(t, h, reader, "/p/"+id+"/raw")
	if body := readBody(t, view); view.StatusCode != http.StatusOK || body != "password-burn-secret" {
		t.Fatalf("revealed password paste: %d", view.StatusCode)
	}
}

func TestBrowserSharingSettingsCreateBurnPaste(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	owner := mustLogin(t, h, "admin", "correct-horse-battery-staple")
	form := getJar(t, h, owner, "/new")
	if body := readBody(t, form); !strings.Contains(body, `name="burn_after_read"`) || !strings.Contains(body, "Burn after read") {
		t.Fatal("sharing settings must offer burn after read")
	}
	created := postFormJarCSRF(t, h, owner, "/new", url.Values{
		"path": {"secret.txt"}, "content": {"web-burn-secret"}, "burn_after_read": {"on"},
	})
	readBody(t, created)
	if created.StatusCode != http.StatusSeeOther {
		t.Fatalf("web create: %d", created.StatusCode)
	}
	landing := getJar(t, h, owner, created.Header.Get("Location"))
	if body := readBody(t, landing); !strings.Contains(body, "Reveal paste") || strings.Contains(body, "web-burn-secret") {
		t.Fatal("web creation must redirect to safe sharing page")
	}
}

func TestBurnConcurrentAPIRevealsHaveExactlyOneWinner(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	owner := mustLogin(t, h, "admin", "correct-horse-battery-staple")
	id := createdPasteID(t, postJSONJarCSRF(t, h, owner, "/api/v1/pastes", map[string]any{
		"content": "race-secret", "burn_after_read": true,
	}))
	type result struct {
		res *http.Response
		err error
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	for range 2 {
		reader := newJar(t)
		readBody(t, getJar(t, h, reader, "/api/v1/pastes/"+id+"/meta"))
		csrf := cookieNamed(reader, h.BaseURL, "burn_csrf")
		if csrf == nil {
			t.Fatal("metadata must issue reveal CSRF token")
		}
		req, err := http.NewRequest(http.MethodPost, h.BaseURL+"/api/v1/pastes/"+id+"/reveal", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-CSRF-Token", csrf.Value)
		client := &http.Client{Jar: reader, Timeout: h.Client.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		go func() { <-start; res, err := client.Do(req); results <- result{res, err} }()
	}
	close(start)
	statuses := map[int]int{}
	for range 2 {
		got := <-results
		if got.err != nil {
			t.Fatal(got.err)
		}
		if got.res.StatusCode == http.StatusGone {
			assertBurnAPIError(t, got.res, http.StatusGone, "gone")
		} else {
			readBody(t, got.res)
		}
		statuses[got.res.StatusCode]++
	}
	if statuses[http.StatusOK] != 1 || statuses[http.StatusGone] != 1 {
		t.Fatalf("concurrent reveal statuses = %v, want one 200 and one 410", statuses)
	}
}

func TestBurnRevealGrantsOnlyWinningBrowserAllFilesAndZIP(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	owner := mustLogin(t, h, "admin", "correct-horse-battery-staple")
	id := createdPasteID(t, postBundleJarCSRF(t, h, owner, bundleOpts{
		Metadata: map[string]any{"burn_after_read": true},
		Files:    []bundleFile{{Path: "a.txt", Content: "first-secret"}, {Path: "nested/b.txt", Content: "second-secret"}},
	}))
	winner, loser := newJar(t), newJar(t)
	for _, reader := range []http.CookieJar{winner, loser} {
		readBody(t, getJar(t, h, reader, "/p/"+id))
	}
	bad := postFormJar(t, h, winner, "/p/"+id+"/reveal", url.Values{})
	readBody(t, bad)
	if bad.StatusCode != http.StatusForbidden {
		t.Fatalf("reveal without CSRF: %d, want 403", bad.StatusCode)
	}
	reveal := revealBurnPaste(t, h, winner, id, "/p/")
	readBody(t, reveal)
	if reveal.StatusCode != http.StatusSeeOther {
		t.Fatalf("first reveal: %d, want 303", reveal.StatusCode)
	}
	for range 2 {
		view := getJar(t, h, winner, "/p/"+id)
		body := readBody(t, view)
		if view.StatusCode != http.StatusOK || !strings.Contains(body, "first-secret") {
			t.Fatalf("winning browser refresh: %d", view.StatusCode)
		}
	}
	meta := getJar(t, h, winner, "/api/v1/pastes/"+id+"/meta")
	var payload struct {
		Files []struct {
			ID   int64  `json:"id"`
			Path string `json:"path"`
		} `json:"files"`
	}
	if err := json.Unmarshal([]byte(readBody(t, meta)), &payload); err != nil || len(payload.Files) != 2 {
		t.Fatalf("winning metadata: %+v, err=%v", payload, err)
	}
	for _, file := range payload.Files {
		fileRoute := "/api/v1/pastes/" + id + "/files/" + strconv.FormatInt(file.ID, 10)
		for _, route := range []string{fileRoute, fileRoute + "/raw", "/p/" + id + "?file_id=" + strconv.FormatInt(file.ID, 10) + "&partial=1"} {
			res := getJar(t, h, winner, route)
			body := readBody(t, res)
			if res.StatusCode != http.StatusOK || !strings.Contains(body, "secret") || res.Header.Get("Cache-Control") != "no-store" {
				t.Fatalf("winning file %s: status=%d cache=%q", route, res.StatusCode, res.Header.Get("Cache-Control"))
			}
			denied := getJar(t, h, loser, route)
			readBody(t, denied)
			if denied.StatusCode != http.StatusGone {
				t.Fatalf("losing file %s: %d", route, denied.StatusCode)
			}
		}
	}
	archive := getJar(t, h, winner, "/p/"+id+"/archive.zip")
	archiveBody := readBodyBytes(t, archive)
	zr, err := zip.NewReader(bytes.NewReader(archiveBody), int64(len(archiveBody)))
	if err != nil || len(zr.File) != 2 {
		t.Fatalf("winning ZIP: err=%v", err)
	}
	for _, route := range []string{"/p/" + id, "/p/" + id + "/raw", "/p/" + id + "/archive.zip", "/api/v1/pastes/" + id + "/archive.zip", "/api/v1/pastes/" + id + "/meta"} {
		res := getJar(t, h, loser, route)
		readBody(t, res)
		if res.StatusCode != http.StatusGone {
			t.Fatalf("loser %s: %d", route, res.StatusCode)
		}
	}
	second := revealBurnPaste(t, h, loser, id, "/p/")
	readBody(t, second)
	if second.StatusCode != http.StatusGone {
		t.Fatalf("second reveal: %d", second.StatusCode)
	}
}

func revealBurnPaste(t *testing.T, h *apptest.Harness, jar http.CookieJar, id, prefix string) *http.Response {
	t.Helper()
	csrf := cookieNamed(jar, h.BaseURL, "burn_csrf")
	if csrf == nil {
		t.Fatal("landing must issue burn CSRF cookie")
	}
	return postFormJar(t, h, jar, prefix+id+"/reveal", url.Values{"csrf": {csrf.Value}})
}

func TestBurnPasteRequiresExplicitRevealBeforeContentAccess(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	owner := mustLogin(t, h, "admin", "correct-horse-battery-staple")
	res := postJSONJarCSRF(t, h, owner, "/api/v1/pastes", map[string]any{
		"filename": "secret.txt", "content": "one-time-secret", "burn_after_read": true,
	})
	id := createdPasteID(t, res)
	reader := newJar(t)
	for range 2 {
		view := getJar(t, h, reader, "/p/"+id)
		body := readBody(t, view)
		if view.StatusCode != http.StatusOK || !strings.Contains(body, "Reveal paste") || strings.Contains(body, "one-time-secret") {
			t.Fatalf("landing must offer reveal without content: status=%d body=%q", view.StatusCode, body)
		}
	}
	meta := getJar(t, h, reader, "/api/v1/pastes/"+id+"/meta")
	var payload struct {
		BurnAfterRead  bool `json:"burn_after_read"`
		RevealRequired bool `json:"reveal_required"`
	}
	if err := json.Unmarshal([]byte(readBody(t, meta)), &payload); err != nil || !payload.BurnAfterRead || !payload.RevealRequired {
		t.Fatalf("metadata must report required reveal: %+v, err=%v", payload, err)
	}
	for _, route := range []string{"/p/" + id + "/raw", "/p/" + id + "/archive.zip", "/p/" + id + "?partial=1"} {
		res := getJar(t, h, reader, route)
		body := readBody(t, res)
		if res.StatusCode != http.StatusForbidden || strings.Contains(body, "one-time-secret") {
			t.Fatalf("content before reveal at %s: status=%d body=%q", route, res.StatusCode, body)
		}
	}
}

func createdPasteID(t *testing.T, res *http.Response) string {
	t.Helper()
	body := readBody(t, res)
	var created struct {
		ID string `json:"id"`
	}
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create: status=%d body=%q", res.StatusCode, body)
	}
	if err := json.Unmarshal([]byte(body), &created); err != nil || created.ID == "" {
		t.Fatalf("create response: %q, err=%v", body, err)
	}
	return created.ID
}
