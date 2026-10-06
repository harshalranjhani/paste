package app_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/harshalranjhani/paste/internal/apptest"
)

func TestUserCreatesTokenSeesPlaintextOnce(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	adminJar := mustLogin(t, h, "admin", "correct-horse-battery-staple")
	aliceJar := mustRedeemInvite(t, h, adminJar, "alice", "alice-password-ok")

	createRes := postJSONJarCSRF(t, h, aliceJar, "/api/v1/tokens", map[string]any{
		"name":   "cli",
		"scopes": []string{"paste:create", "paste:read"},
	})
	createBody := readBody(t, createRes)
	if createRes.StatusCode != http.StatusCreated {
		t.Fatalf("POST /api/v1/tokens status = %d; body = %q", createRes.StatusCode, createBody)
	}
	var created struct {
		ID     int64    `json:"id"`
		Name   string   `json:"name"`
		Token  string   `json:"token"`
		Scopes []string `json:"scopes"`
	}
	if err := json.Unmarshal([]byte(createBody), &created); err != nil {
		t.Fatalf("parse create: %v; body = %q", err, createBody)
	}
	if created.ID == 0 {
		t.Fatalf("missing id in %q", createBody)
	}
	if created.Name != "cli" {
		t.Fatalf("name = %q, want cli", created.Name)
	}
	if !strings.HasPrefix(created.Token, "pb_") {
		t.Fatalf("token %q missing pb_ prefix", created.Token)
	}
	if len(created.Token) < len("pb_")+32 {
		t.Fatalf("token too short: %q", created.Token)
	}
	if !containsAll(created.Scopes, "paste:create", "paste:read") {
		t.Fatalf("scopes = %v", created.Scopes)
	}

	listRes := getJar(t, h, aliceJar, "/api/v1/tokens")
	listBody := readBody(t, listRes)
	if listRes.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/v1/tokens status = %d; body = %q", listRes.StatusCode, listBody)
	}
	if strings.Contains(listBody, created.Token) {
		t.Fatal("token list must not include plaintext token")
	}
	if strings.Contains(listBody, `"token"`) {
		t.Fatalf("list must not expose token field; body = %q", listBody)
	}

	settings := getJar(t, h, aliceJar, "/settings/tokens")
	settingsBody := readBody(t, settings)
	if settings.StatusCode != http.StatusOK {
		t.Fatalf("GET /settings/tokens status = %d; body = %q", settings.StatusCode, settingsBody)
	}
	if strings.Contains(settingsBody, created.Token) {
		t.Fatal("settings page must not include plaintext token")
	}
}

func TestRevokedAndExpiredTokensFail(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	adminJar := mustLogin(t, h, "admin", "correct-horse-battery-staple")
	aliceJar := mustRedeemInvite(t, h, adminJar, "alice", "alice-password-ok")

	created := mustCreatePAT(t, h, aliceJar, map[string]any{
		"name":   "temp",
		"scopes": []string{"paste:create"},
	})

	revoke := postJSONJarCSRF(t, h, aliceJar, "/api/v1/tokens/"+strconv.FormatInt(created.ID, 10)+"/revoke", map[string]any{})
	revokeBody := readBody(t, revoke)
	if revoke.StatusCode != http.StatusNoContent && revoke.StatusCode != http.StatusOK {
		t.Fatalf("revoke status = %d; body = %q", revoke.StatusCode, revokeBody)
	}

	denied := postJSONBearer(t, h, created.Token, "/api/v1/pastes", map[string]any{
		"filename": "x.txt",
		"content":  "nope",
	})
	deniedBody := readBody(t, denied)
	if denied.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked token create status = %d, want 401; body = %q", denied.StatusCode, deniedBody)
	}

	expiring := mustCreatePAT(t, h, aliceJar, map[string]any{
		"name":       "short",
		"scopes":     []string{"paste:create"},
		"expires_in": "1s",
	})
	time.Sleep(1100 * time.Millisecond)
	expired := postJSONBearer(t, h, expiring.Token, "/api/v1/pastes", map[string]any{
		"filename": "x.txt",
		"content":  "nope",
	})
	expiredBody := readBody(t, expired)
	if expired.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expired token create status = %d, want 401; body = %q", expired.StatusCode, expiredBody)
	}
}

func TestBearerAuthScopesAndLastUsed(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	adminJar := mustLogin(t, h, "admin", "correct-horse-battery-staple")
	aliceJar := mustRedeemInvite(t, h, adminJar, "alice", "alice-password-ok")

	createOnly := mustCreatePAT(t, h, aliceJar, map[string]any{
		"name":   "uploader",
		"scopes": []string{"paste:create"},
	})
	readOnly := mustCreatePAT(t, h, aliceJar, map[string]any{
		"name":   "reader",
		"scopes": []string{"paste:read"},
	})
	deleteOnly := mustCreatePAT(t, h, aliceJar, map[string]any{
		"name":   "deleter",
		"scopes": []string{"paste:delete"},
	})
	full := mustCreatePAT(t, h, aliceJar, map[string]any{
		"name":   "full",
		"scopes": []string{"paste:create", "paste:read", "paste:delete"},
	})

	// Wrong scope: create with read-only token.
	wrongCreate := postJSONBearer(t, h, readOnly.Token, "/api/v1/pastes", map[string]any{
		"filename": "a.txt",
		"content":  "x",
	})
	wrongCreateBody := readBody(t, wrongCreate)
	if wrongCreate.StatusCode != http.StatusForbidden {
		t.Fatalf("read-only create status = %d, want 403; body = %q", wrongCreate.StatusCode, wrongCreateBody)
	}

	// Create without CSRF via Bearer paste:create.
	okCreate := postJSONBearer(t, h, createOnly.Token, "/api/v1/pastes", map[string]any{
		"filename": "a.txt",
		"content":  "hello bearer",
	})
	okCreateBody := readBody(t, okCreate)
	if okCreate.StatusCode != http.StatusCreated {
		t.Fatalf("bearer create status = %d; body = %q", okCreate.StatusCode, okCreateBody)
	}
	var paste struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(okCreateBody), &paste); err != nil || paste.ID == "" {
		t.Fatalf("parse create: %v body = %q", err, okCreateBody)
	}

	// Read scope for /api/v1/me/pastes.
	denyRead := getBearer(t, h, createOnly.Token, "/api/v1/me/pastes")
	denyReadBody := readBody(t, denyRead)
	if denyRead.StatusCode != http.StatusForbidden {
		t.Fatalf("create-only me/pastes status = %d, want 403; body = %q", denyRead.StatusCode, denyReadBody)
	}
	okRead := getBearer(t, h, readOnly.Token, "/api/v1/me/pastes")
	okReadBody := readBody(t, okRead)
	if okRead.StatusCode != http.StatusOK {
		t.Fatalf("read token me/pastes status = %d; body = %q", okRead.StatusCode, okReadBody)
	}

	// Delete scope.
	denyDelete := deleteJSONBearer(t, h, createOnly.Token, "/api/v1/pastes/"+paste.ID)
	denyDeleteBody := readBody(t, denyDelete)
	if denyDelete.StatusCode != http.StatusForbidden {
		t.Fatalf("create-only delete status = %d, want 403; body = %q", denyDelete.StatusCode, denyDeleteBody)
	}
	okDelete := deleteJSONBearer(t, h, deleteOnly.Token, "/api/v1/pastes/"+paste.ID)
	okDeleteBody := readBody(t, okDelete)
	if okDelete.StatusCode != http.StatusNoContent {
		t.Fatalf("delete token status = %d; body = %q", okDelete.StatusCode, okDeleteBody)
	}

	// Full token can create without CSRF.
	again := postJSONBearer(t, h, full.Token, "/api/v1/pastes", map[string]any{
		"filename": "b.txt",
		"content":  "again",
	})
	againBody := readBody(t, again)
	if again.StatusCode != http.StatusCreated {
		t.Fatalf("full token create status = %d; body = %q", again.StatusCode, againBody)
	}

	listRes := getJar(t, h, aliceJar, "/api/v1/tokens")
	listBody := readBody(t, listRes)
	if listRes.StatusCode != http.StatusOK {
		t.Fatalf("list tokens status = %d; body = %q", listRes.StatusCode, listBody)
	}
	var listed []struct {
		ID         int64   `json:"id"`
		LastUsedAt *string `json:"last_used_at"`
	}
	if err := json.Unmarshal([]byte(listBody), &listed); err != nil {
		t.Fatalf("parse list: %v; body = %q", err, listBody)
	}
	used := map[int64]bool{}
	for _, row := range listed {
		if row.LastUsedAt != nil && *row.LastUsedAt != "" {
			used[row.ID] = true
		}
	}
	for _, id := range []int64{createOnly.ID, readOnly.ID, deleteOnly.ID, full.ID} {
		if !used[id] {
			t.Fatalf("token %d missing last_used_at after successful API use; body = %q", id, listBody)
		}
	}
}

func TestAdminCannotReadAnotherUsersTokenPlaintext(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	adminJar := mustLogin(t, h, "admin", "correct-horse-battery-staple")
	aliceJar := mustRedeemInvite(t, h, adminJar, "alice", "alice-password-ok")

	created := mustCreatePAT(t, h, aliceJar, map[string]any{
		"name":   "secret",
		"scopes": []string{"paste:create"},
	})

	adminList := getJar(t, h, adminJar, "/api/v1/tokens")
	adminListBody := readBody(t, adminList)
	if adminList.StatusCode != http.StatusOK {
		t.Fatalf("admin list own tokens status = %d; body = %q", adminList.StatusCode, adminListBody)
	}
	if strings.Contains(adminListBody, created.Token) {
		t.Fatal("admin token list leaked alice plaintext")
	}

	// Admin has no endpoint to fetch another user's token plaintext.
	probe := getJar(t, h, adminJar, "/api/v1/tokens/"+strconv.FormatInt(created.ID, 10))
	probeBody := readBody(t, probe)
	if probe.StatusCode == http.StatusOK && strings.Contains(probeBody, created.Token) {
		t.Fatalf("admin retrieved alice plaintext via GET token; body = %q", probeBody)
	}
	if strings.Contains(probeBody, created.Token) {
		t.Fatalf("admin response contained alice plaintext; body = %q", probeBody)
	}

	aliceSettings := getJar(t, h, aliceJar, "/settings/tokens")
	aliceSettingsBody := readBody(t, aliceSettings)
	adminSettings := getJar(t, h, adminJar, "/settings/tokens")
	adminSettingsBody := readBody(t, adminSettings)
	if strings.Contains(aliceSettingsBody, created.Token) || strings.Contains(adminSettingsBody, created.Token) {
		t.Fatal("settings UI exposed plaintext after creation")
	}
}

func mustCreatePAT(t *testing.T, h *apptest.Harness, jar http.CookieJar, payload map[string]any) struct {
	ID    int64  `json:"id"`
	Token string `json:"token"`
} {
	t.Helper()
	res := postJSONJarCSRF(t, h, jar, "/api/v1/tokens", payload)
	body := readBody(t, res)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create PAT status = %d; body = %q", res.StatusCode, body)
	}
	var created struct {
		ID    int64  `json:"id"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		t.Fatalf("parse PAT: %v; body = %q", err, body)
	}
	if created.ID == 0 || created.Token == "" {
		t.Fatalf("missing id/token in %q", body)
	}
	return created
}

func postJSONBearer(t *testing.T, h *apptest.Harness, token, path string, payload map[string]any) *http.Response {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, h.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{
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

func deleteJSONBearer(t *testing.T, h *apptest.Harness, token, path string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, h.BaseURL+path, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{
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

func getBearer(t *testing.T, h *apptest.Harness, token, path string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, h.BaseURL+path, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	client := &http.Client{
		Timeout: h.Client.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return res
}

func containsAll(have []string, want ...string) bool {
	set := make(map[string]bool, len(have))
	for _, s := range have {
		set[s] = true
	}
	for _, w := range want {
		if !set[w] {
			return false
		}
	}
	return true
}
