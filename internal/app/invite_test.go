package app_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/harshalranjhani/paste/internal/apptest"
)

func TestInviteRedeemCreatesUserAndBlocksReuse(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	adminJar := mustLogin(t, h, "admin", "correct-horse-battery-staple")

	created := mustCreateInvite(t, h, adminJar, url.Values{})

	redeemRes := postForm(t, h, "/invite/"+created.Token, url.Values{
		"username": {"alice"},
		"password": {"alice-password-ok"},
		"email":    {"alice@example.com"},
	})
	redeemBody := readBody(t, redeemRes)
	if redeemRes.StatusCode != http.StatusSeeOther && redeemRes.StatusCode != http.StatusOK {
		t.Fatalf("redeem status = %d; body = %q", redeemRes.StatusCode, redeemBody)
	}

	// New user can log in.
	userJar := mustLogin(t, h, "alice", "alice-password-ok")

	// Normal user cannot create invites.
	deny := postFormJarCSRF(t, h, userJar, "/admin/invites", url.Values{})
	denyBody := readBody(t, deny)
	if deny.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin create invite status = %d, want 403; body = %q", deny.StatusCode, denyBody)
	}

	// Reuse fails clearly.
	reuse := postForm(t, h, "/invite/"+created.Token, url.Values{
		"username": {"bob"},
		"password": {"bob-password-ok"},
	})
	reuseBody := readBody(t, reuse)
	if reuse.StatusCode == http.StatusSeeOther || reuse.StatusCode == http.StatusOK {
		t.Fatalf("reused invite succeeded: %d %q", reuse.StatusCode, reuseBody)
	}
	if reuse.StatusCode != http.StatusGone && reuse.StatusCode != http.StatusConflict && reuse.StatusCode != http.StatusBadRequest {
		t.Fatalf("reused invite status = %d, want 410/409/400; body = %q", reuse.StatusCode, reuseBody)
	}
}

func TestExpiredInviteFailsClearly(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	adminJar := mustLogin(t, h, "admin", "correct-horse-battery-staple")

	created := mustCreateInvite(t, h, adminJar, url.Values{"ttl_seconds": {"1"}})
	time.Sleep(1100 * time.Millisecond)

	res := postForm(t, h, "/invite/"+created.Token, url.Values{
		"username": {"late"},
		"password": {"late-password-ok"},
	})
	body := readBody(t, res)
	if res.StatusCode == http.StatusSeeOther || res.StatusCode == http.StatusOK {
		t.Fatalf("expired invite succeeded: %d %q", res.StatusCode, body)
	}
	if res.StatusCode != http.StatusGone && res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expired invite status = %d, want 410 or 400; body = %q", res.StatusCode, body)
	}
}

func TestInviteEmailConstraintEnforced(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	adminJar := mustLogin(t, h, "admin", "correct-horse-battery-staple")

	created := mustCreateInvite(t, h, adminJar, url.Values{"email": {"allowed@example.com"}})

	wrong := postForm(t, h, "/invite/"+created.Token, url.Values{
		"username": {"carol"},
		"password": {"carol-password-ok"},
		"email":    {"other@example.com"},
	})
	wrongBody := readBody(t, wrong)
	if wrong.StatusCode == http.StatusSeeOther || wrong.StatusCode == http.StatusOK {
		t.Fatalf("wrong email redeem succeeded: %d %q", wrong.StatusCode, wrongBody)
	}

	ok := postForm(t, h, "/invite/"+created.Token, url.Values{
		"username": {"carol"},
		"password": {"carol-password-ok"},
		"email":    {"allowed@example.com"},
	})
	okBody := readBody(t, ok)
	if ok.StatusCode != http.StatusSeeOther && ok.StatusCode != http.StatusOK {
		t.Fatalf("matching email redeem status = %d; body = %q", ok.StatusCode, okBody)
	}
	mustLogin(t, h, "carol", "carol-password-ok")
}

func mustCreateInvite(t *testing.T, h *apptest.Harness, jar http.CookieJar, form url.Values) struct {
	ID    int64  `json:"id"`
	URL   string `json:"url"`
	Token string `json:"token"`
} {
	t.Helper()
	res := postFormJarCSRF(t, h, jar, "/admin/invites", form)
	body := readBody(t, res)
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusCreated {
		t.Fatalf("create invite status = %d; body = %q", res.StatusCode, body)
	}
	var created struct {
		ID    int64  `json:"id"`
		URL   string `json:"url"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		t.Fatalf("parse invite: %v; body = %q", err, body)
	}
	if created.Token == "" {
		t.Fatalf("missing token in %q", body)
	}
	return created
}

func TestAdminCanCreateAndRevokeInviteShownOnce(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	jar := mustLogin(t, h, "admin", "correct-horse-battery-staple")

	createRes := postFormJarCSRF(t, h, jar, "/admin/invites", url.Values{})
	createBody := readBody(t, createRes)
	if createRes.StatusCode != http.StatusOK && createRes.StatusCode != http.StatusCreated {
		t.Fatalf("create invite status = %d; body = %q", createRes.StatusCode, createBody)
	}

	var created struct {
		ID        int64  `json:"id"`
		URL       string `json:"url"`
		Token     string `json:"token"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.Unmarshal([]byte(createBody), &created); err != nil {
		t.Fatalf("parse create response: %v; body = %q", err, createBody)
	}
	if created.Token == "" || created.URL == "" {
		t.Fatalf("expected token and url in create response; body = %q", createBody)
	}
	if !strings.Contains(created.URL, created.Token) {
		t.Fatalf("url %q should contain token %q", created.URL, created.Token)
	}
	exp, err := time.Parse(time.RFC3339Nano, created.ExpiresAt)
	if err != nil {
		exp, err = time.Parse(time.RFC3339, created.ExpiresAt)
		if err != nil {
			t.Fatalf("parse expires_at: %v", err)
		}
	}
	until := time.Until(exp)
	if until < 6*24*time.Hour || until > 8*24*time.Hour {
		t.Fatalf("default TTL = %s, want ~7 days", until)
	}

	listRes := getJar(t, h, jar, "/admin/invites")
	listBody := readBody(t, listRes)
	if listRes.StatusCode != http.StatusOK {
		t.Fatalf("list invites status = %d; body = %q", listRes.StatusCode, listBody)
	}
	if strings.Contains(listBody, created.Token) {
		t.Fatal("invite list must not include plaintext token")
	}

	revokeRes := postFormJarCSRF(t, h, jar, "/admin/invites/"+strconv.FormatInt(created.ID, 10)+"/revoke", url.Values{})
	revokeBody := readBody(t, revokeRes)
	if revokeRes.StatusCode != http.StatusOK && revokeRes.StatusCode != http.StatusSeeOther && revokeRes.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke status = %d; body = %q", revokeRes.StatusCode, revokeBody)
	}
}

func TestUnauthenticatedCannotCreateInvite(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")

	res := postForm(t, h, "/admin/invites", url.Values{})
	body := readBody(t, res)
	if res.StatusCode == http.StatusOK || res.StatusCode == http.StatusCreated {
		t.Fatalf("unauthenticated create invite succeeded: %d %q", res.StatusCode, body)
	}
}

func mustLogin(t *testing.T, h *apptest.Harness, username, password string) http.CookieJar {
	t.Helper()
	jar := newJar(t)
	res := postFormJar(t, h, jar, "/login", url.Values{
		"username": {username},
		"password": {password},
	})
	body := readBody(t, res)
	if res.StatusCode != http.StatusSeeOther && res.StatusCode != http.StatusOK {
		t.Fatalf("login: status %d body %q", res.StatusCode, body)
	}
	return jar
}

func getJar(t *testing.T, h *apptest.Harness, jar http.CookieJar, path string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, h.BaseURL+path, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Accept", "application/json")
	client := &http.Client{
		Jar:     jar,
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
