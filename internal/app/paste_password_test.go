package app_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/harshalranjhani/paste/internal/apptest"
	"github.com/harshalranjhani/paste/internal/config"
)

func TestPasswordProtectedPasteHidesContentUntilUnlocked(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	jar := mustLogin(t, h, "admin", "correct-horse-battery-staple")

	const (
		filename = "secrets.env"
		content  = "API_KEY=super-secret-value"
		password = "paste-password-ok"
	)
	createRes := postJSONJarCSRF(t, h, jar, "/api/v1/pastes", map[string]any{
		"filename": filename,
		"content":  content,
		"password": password,
	})
	createBody := readBody(t, createRes)
	if createRes.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d; body = %q", createRes.StatusCode, createBody)
	}
	if strings.Contains(createBody, password) {
		t.Fatalf("create response leaked plaintext password: %q", createBody)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(createBody), &created); err != nil {
		t.Fatalf("parse create: %v; body = %q", err, createBody)
	}

	viewRes, err := h.GET("/p/" + created.ID)
	if err != nil {
		t.Fatalf("GET locked view: %v", err)
	}
	viewBody := readBody(t, viewRes)
	if viewRes.StatusCode != http.StatusOK {
		t.Fatalf("locked view status = %d; body = %q", viewRes.StatusCode, viewBody)
	}
	if strings.Contains(viewBody, filename) {
		t.Fatalf("locked view leaked filename; body = %q", viewBody)
	}
	if strings.Contains(viewBody, content) || strings.Contains(viewBody, "API_KEY") {
		t.Fatalf("locked view leaked content; body = %q", viewBody)
	}
	if !strings.Contains(strings.ToLower(viewBody), "password") {
		t.Fatalf("locked view missing password prompt; body = %q", viewBody)
	}
}

func TestUnlockWrongPasswordFailsGenericallyCorrectUnlocks(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	owner := mustLogin(t, h, "admin", "correct-horse-battery-staple")

	const (
		filename = "notes.txt"
		content  = "classified-body-text"
		password = "correct-paste-secret"
	)
	createRes := postJSONJarCSRF(t, h, owner, "/api/v1/pastes", map[string]any{
		"filename": filename,
		"content":  content,
		"password": password,
	})
	createBody := readBody(t, createRes)
	if createRes.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d; body = %q", createRes.StatusCode, createBody)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(createBody), &created); err != nil {
		t.Fatalf("parse create: %v", err)
	}

	anon := newJar(t)
	wrong := postFormJar(t, h, anon, "/p/"+created.ID+"/unlock", url.Values{
		"password": {"definitely-wrong"},
	})
	wrongBody := readBody(t, wrong)
	if wrong.StatusCode == http.StatusSeeOther {
		t.Fatalf("wrong password should not redirect to unlocked paste; Location=%q", wrong.Header.Get("Location"))
	}
	if wrong.StatusCode != http.StatusUnauthorized && wrong.StatusCode != http.StatusOK {
		t.Fatalf("wrong password status = %d; body = %q", wrong.StatusCode, wrongBody)
	}
	lower := strings.ToLower(wrongBody)
	if strings.Contains(lower, "not found") || strings.Contains(lower, "does not exist") {
		t.Fatalf("wrong password revealed existence detail; body = %q", wrongBody)
	}
	if !strings.Contains(lower, "invalid") && !strings.Contains(lower, "incorrect") && !strings.Contains(lower, "wrong") {
		t.Fatalf("wrong password missing generic failure message; body = %q", wrongBody)
	}
	if strings.Contains(wrongBody, content) || strings.Contains(wrongBody, filename) {
		t.Fatalf("wrong password response leaked paste data; body = %q", wrongBody)
	}

	okUnlock := postFormJar(t, h, anon, "/p/"+created.ID+"/unlock", url.Values{
		"password": {password},
	})
	okBody := readBody(t, okUnlock)
	if okUnlock.StatusCode != http.StatusSeeOther && okUnlock.StatusCode != http.StatusOK {
		t.Fatalf("correct unlock status = %d; body = %q", okUnlock.StatusCode, okBody)
	}
	if cookieNamed(anon, h.BaseURL, "paste_access") == nil {
		t.Fatal("expected paste_access session cookie after unlock")
	}

	view := getJar(t, h, anon, "/p/"+created.ID)
	viewBody := readBody(t, view)
	if view.StatusCode != http.StatusOK {
		t.Fatalf("unlocked view status = %d; body = %q", view.StatusCode, viewBody)
	}
	if !strings.Contains(viewBody, content) {
		t.Fatalf("unlocked view missing content; body = %q", viewBody)
	}
	if !strings.Contains(viewBody, filename) {
		t.Fatalf("unlocked view missing filename; body = %q", viewBody)
	}
}

func TestPasswordGateAppliesToRawAndMeta(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	owner := mustLogin(t, h, "admin", "correct-horse-battery-staple")

	const (
		filename = "hidden.go"
		content  = "package hidden"
		password = "meta-gate-secret"
	)
	createRes := postJSONJarCSRF(t, h, owner, "/api/v1/pastes", map[string]any{
		"filename": filename,
		"content":  content,
		"password": password,
	})
	createBody := readBody(t, createRes)
	if createRes.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d; body = %q", createRes.StatusCode, createBody)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(createBody), &created); err != nil {
		t.Fatalf("parse create: %v", err)
	}

	rawLocked, err := h.GET("/p/" + created.ID + "/raw")
	if err != nil {
		t.Fatalf("GET raw locked: %v", err)
	}
	rawLockedBody := readBody(t, rawLocked)
	if rawLocked.StatusCode == http.StatusOK {
		t.Fatalf("raw bypassed password gate: %q", rawLockedBody)
	}
	if strings.Contains(rawLockedBody, content) {
		t.Fatalf("locked raw leaked content; body = %q", rawLockedBody)
	}

	metaLocked, err := h.GET("/api/v1/pastes/" + created.ID + "/meta")
	if err != nil {
		t.Fatalf("GET meta locked: %v", err)
	}
	metaLockedBody := readBody(t, metaLocked)
	if metaLocked.StatusCode != http.StatusOK {
		t.Fatalf("locked meta status = %d; body = %q", metaLocked.StatusCode, metaLockedBody)
	}
	if strings.Contains(metaLockedBody, filename) || strings.Contains(metaLockedBody, content) {
		t.Fatalf("locked meta leaked filenames/content; body = %q", metaLockedBody)
	}
	var lockedMeta struct {
		PasswordRequired bool `json:"password_required"`
	}
	if err := json.Unmarshal([]byte(metaLockedBody), &lockedMeta); err != nil {
		t.Fatalf("parse locked meta: %v; body = %q", err, metaLockedBody)
	}
	if !lockedMeta.PasswordRequired {
		t.Fatalf("locked meta missing password_required; body = %q", metaLockedBody)
	}

	anon := newJar(t)
	unlock := postFormJar(t, h, anon, "/p/"+created.ID+"/unlock", url.Values{
		"password": {password},
	})
	_ = readBody(t, unlock)
	if unlock.StatusCode != http.StatusSeeOther && unlock.StatusCode != http.StatusOK {
		t.Fatalf("unlock status = %d", unlock.StatusCode)
	}

	rawOK := getJar(t, h, anon, "/p/"+created.ID+"/raw")
	rawOKBody := readBody(t, rawOK)
	if rawOK.StatusCode != http.StatusOK {
		t.Fatalf("unlocked raw status = %d; body = %q", rawOK.StatusCode, rawOKBody)
	}
	if rawOKBody != content {
		t.Fatalf("unlocked raw body = %q, want %q", rawOKBody, content)
	}

	metaOK := getJar(t, h, anon, "/api/v1/pastes/"+created.ID+"/meta")
	metaOKBody := readBody(t, metaOK)
	if metaOK.StatusCode != http.StatusOK {
		t.Fatalf("unlocked meta status = %d; body = %q", metaOK.StatusCode, metaOKBody)
	}
	if !strings.Contains(metaOKBody, filename) {
		t.Fatalf("unlocked meta missing filename; body = %q", metaOKBody)
	}
}

func TestPasteAccessSessionIsBrowserSessionAndNotAccount(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	owner := mustLogin(t, h, "admin", "correct-horse-battery-staple")

	createRes := postJSONJarCSRF(t, h, owner, "/api/v1/pastes", map[string]any{
		"filename": "x.txt",
		"content":  "body",
		"password": "session-secret",
	})
	createBody := readBody(t, createRes)
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(createBody), &created); err != nil {
		t.Fatalf("parse create: %v", err)
	}

	anon := newJar(t)
	unlock := postFormJar(t, h, anon, "/p/"+created.ID+"/unlock", url.Values{
		"password": {"session-secret"},
	})
	unlockBody := readBody(t, unlock)
	if unlock.StatusCode != http.StatusSeeOther && unlock.StatusCode != http.StatusOK {
		t.Fatalf("unlock status = %d; body = %q", unlock.StatusCode, unlockBody)
	}

	var accessCookie *http.Cookie
	for _, c := range unlock.Cookies() {
		if c.Name == "paste_access" {
			accessCookie = c
			break
		}
	}
	if accessCookie == nil {
		t.Fatal("unlock response missing paste_access Set-Cookie")
	}
	if !accessCookie.HttpOnly {
		t.Fatal("paste_access cookie must be HttpOnly")
	}
	if accessCookie.MaxAge != 0 || !accessCookie.Expires.IsZero() {
		t.Fatalf("paste_access must be a browser session cookie; MaxAge=%d Expires=%v", accessCookie.MaxAge, accessCookie.Expires)
	}
	if cookieNamed(anon, h.BaseURL, "session") != nil {
		t.Fatal("paste unlock must not create an account session cookie")
	}

	// Paste-access alone cannot create pastes or open authenticated surfaces.
	createAttempt := postJSONJarCSRF(t, h, anon, "/api/v1/pastes", map[string]any{
		"filename": "nope.txt",
		"content":  "no",
	})
	createAttemptBody := readBody(t, createAttempt)
	if createAttempt.StatusCode != http.StatusUnauthorized {
		t.Fatalf("paste-access create status = %d, want 401; body = %q", createAttempt.StatusCode, createAttemptBody)
	}
	newPage := getJar(t, h, anon, "/new")
	_ = readBody(t, newPage)
	if newPage.StatusCode != http.StatusSeeOther && newPage.StatusCode != http.StatusUnauthorized {
		t.Fatalf("/new with paste-access only status = %d, want redirect to login or 401", newPage.StatusCode)
	}
}

func TestPasteAccessSessionExpiresServerSide(t *testing.T) {
	h := apptest.Start(t, func(cfg *config.Config) {
		cfg.PasteAccessTTL = time.Second
	})
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	owner := mustLogin(t, h, "admin", "correct-horse-battery-staple")

	createRes := postJSONJarCSRF(t, h, owner, "/api/v1/pastes", map[string]any{
		"filename": "ttl.txt",
		"content":  "short-lived-access",
		"password": "ttl-secret",
	})
	createBody := readBody(t, createRes)
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(createBody), &created); err != nil {
		t.Fatalf("parse create: %v", err)
	}

	anon := newJar(t)
	unlock := postFormJar(t, h, anon, "/p/"+created.ID+"/unlock", url.Values{
		"password": {"ttl-secret"},
	})
	_ = readBody(t, unlock)

	view := getJar(t, h, anon, "/p/"+created.ID)
	viewBody := readBody(t, view)
	if view.StatusCode != http.StatusOK || !strings.Contains(viewBody, "short-lived-access") {
		t.Fatalf("expected immediate access after unlock; status=%d body=%q", view.StatusCode, viewBody)
	}

	time.Sleep(1100 * time.Millisecond)

	expired := getJar(t, h, anon, "/p/"+created.ID)
	expiredBody := readBody(t, expired)
	if strings.Contains(expiredBody, "short-lived-access") {
		t.Fatalf("paste-access session still valid after server TTL; body = %q", expiredBody)
	}
	if !strings.Contains(strings.ToLower(expiredBody), "password") {
		t.Fatalf("expected lock screen after expiry; body = %q", expiredBody)
	}
}

func TestUnlockAttemptsAreRateLimited(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	owner := mustLogin(t, h, "admin", "correct-horse-battery-staple")

	createRes := postJSONJarCSRF(t, h, owner, "/api/v1/pastes", map[string]any{
		"filename": "rl.txt",
		"content":  "rate-limited-body",
		"password": "real-password",
	})
	createBody := readBody(t, createRes)
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(createBody), &created); err != nil {
		t.Fatalf("parse create: %v", err)
	}

	anon := newJar(t)
	var sawLimited bool
	for i := 0; i < 20; i++ {
		res := postFormJar(t, h, anon, "/p/"+created.ID+"/unlock", url.Values{
			"password": {"wrong-password"},
		})
		body := readBody(t, res)
		if res.StatusCode == http.StatusTooManyRequests {
			sawLimited = true
			if strings.Contains(body, "rate-limited-body") {
				t.Fatalf("rate-limit response leaked content; body = %q", body)
			}
			break
		}
		if res.StatusCode != http.StatusUnauthorized && res.StatusCode != http.StatusOK {
			t.Fatalf("attempt %d status = %d; body = %q", i+1, res.StatusCode, body)
		}
	}
	if !sawLimited {
		t.Fatal("expected unlock attempts to be rate-limited")
	}

	// Even the correct password is rejected while limited.
	blocked := postFormJar(t, h, anon, "/p/"+created.ID+"/unlock", url.Values{
		"password": {"real-password"},
	})
	blockedBody := readBody(t, blocked)
	if blocked.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("correct password during limit status = %d, want 429; body = %q", blocked.StatusCode, blockedBody)
	}
}
