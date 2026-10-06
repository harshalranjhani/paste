package app_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/harshalranjhani/paste/internal/apptest"
)

func TestSetupCreatesFirstAdminThenRefuses(t *testing.T) {
	h := apptest.Start(t)

	res := postForm(t, h, "/setup", url.Values{
		"username": {"admin"},
		"password": {"correct-horse-battery-staple"},
	})
	body := readBody(t, res)
	if res.StatusCode != http.StatusSeeOther && res.StatusCode != http.StatusOK {
		t.Fatalf("POST /setup status = %d, want 200 or 303; body = %q", res.StatusCode, body)
	}

	res2 := postForm(t, h, "/setup", url.Values{
		"username": {"intruder"},
		"password": {"another-password-here"},
	})
	body2 := readBody(t, res2)
	if res2.StatusCode == http.StatusOK || res2.StatusCode == http.StatusSeeOther {
		t.Fatalf("second POST /setup succeeded with status %d; body = %q", res2.StatusCode, body2)
	}
	if res2.StatusCode != http.StatusForbidden && res2.StatusCode != http.StatusConflict {
		t.Fatalf("second POST /setup status = %d, want 403 or 409; body = %q", res2.StatusCode, body2)
	}
}

func TestConcurrentSetupCreatesOnlyOneAdmin(t *testing.T) {
	h := apptest.Start(t)

	const n = 8
	var successes atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			res := postForm(t, h, "/setup", url.Values{
				"username": {fmt.Sprintf("admin%d", i)},
				"password": {"correct-horse-battery-staple"},
			})
			body := readBody(t, res)
			switch res.StatusCode {
			case http.StatusSeeOther, http.StatusOK:
				successes.Add(1)
			case http.StatusForbidden, http.StatusConflict:
				// expected losers
			default:
				t.Errorf("POST /setup #%d status = %d; body = %q", i, res.StatusCode, body)
			}
		}(i)
	}
	close(start)
	wg.Wait()

	if got := successes.Load(); got != 1 {
		t.Fatalf("successful setups = %d, want 1", got)
	}

	res := postForm(t, h, "/setup", url.Values{
		"username": {"latecomer"},
		"password": {"correct-horse-battery-staple"},
	})
	body := readBody(t, res)
	if res.StatusCode != http.StatusForbidden && res.StatusCode != http.StatusConflict {
		t.Fatalf("post-race setup status = %d, want 403 or 409; body = %q", res.StatusCode, body)
	}
}

func TestLoginIssuesHttpOnlySessionAndLogoutInvalidates(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")

	jar := newJar(t)
	loginRes := postFormJar(t, h, jar, "/login", url.Values{
		"username": {"admin"},
		"password": {"correct-horse-battery-staple"},
	})
	body := readBody(t, loginRes)
	if loginRes.StatusCode != http.StatusSeeOther && loginRes.StatusCode != http.StatusOK {
		t.Fatalf("POST /login status = %d; body = %q", loginRes.StatusCode, body)
	}

	session := setCookieNamed(loginRes, "session")
	if session == nil {
		t.Fatal("expected Set-Cookie session after login")
	}
	if !session.HttpOnly {
		t.Fatal("session cookie must be HttpOnly")
	}
	oldToken := session.Value

	logoutRes := postFormJarCSRF(t, h, jar, "/logout", url.Values{})
	logoutBody := readBody(t, logoutRes)
	if logoutRes.StatusCode != http.StatusSeeOther && logoutRes.StatusCode != http.StatusOK {
		t.Fatalf("POST /logout status = %d; body = %q", logoutRes.StatusCode, logoutBody)
	}

	replayJar := newJar(t)
	u, _ := url.Parse(h.BaseURL)
	replayJar.SetCookies(u, []*http.Cookie{{Name: "session", Value: oldToken}})
	replay := postFormJarCSRF(t, h, replayJar, "/logout", url.Values{})
	replayBody := readBody(t, replay)
	if replay.StatusCode == http.StatusSeeOther || replay.StatusCode == http.StatusOK {
		t.Fatalf("logout with invalidated session succeeded: status %d body %q", replay.StatusCode, replayBody)
	}
}

func TestLoginRotatesSessionToken(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")

	jar := newJar(t)
	firstRes := postFormJar(t, h, jar, "/login", url.Values{
		"username": {"admin"},
		"password": {"correct-horse-battery-staple"},
	})
	first := setCookieNamed(firstRes, "session")
	readBody(t, firstRes)
	if first == nil {
		t.Fatal("expected session cookie after first login")
	}

	secondRes := postFormJar(t, h, jar, "/login", url.Values{
		"username": {"admin"},
		"password": {"correct-horse-battery-staple"},
	})
	second := setCookieNamed(secondRes, "session")
	readBody(t, secondRes)
	if second == nil {
		t.Fatal("expected session cookie after second login")
	}
	if first.Value == second.Value {
		t.Fatal("login must rotate session token")
	}

	oldJar := newJar(t)
	u, _ := url.Parse(h.BaseURL)
	oldJar.SetCookies(u, []*http.Cookie{{Name: "session", Value: first.Value}})
	replay := postFormJarCSRF(t, h, oldJar, "/logout", url.Values{})
	replayBody := readBody(t, replay)
	if replay.StatusCode == http.StatusSeeOther || replay.StatusCode == http.StatusOK {
		t.Fatalf("old session still valid after rotation: status %d body %q", replay.StatusCode, replayBody)
	}
}

func TestCSRFRequiredOnCookieAuthenticatedMutation(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")

	jar := newJar(t)
	loginRes := postFormJar(t, h, jar, "/login", url.Values{
		"username": {"admin"},
		"password": {"correct-horse-battery-staple"},
	})
	readBody(t, loginRes)

	// Mutation without CSRF must fail.
	noCSRF := postFormJar(t, h, jar, "/logout", url.Values{})
	noBody := readBody(t, noCSRF)
	if noCSRF.StatusCode != http.StatusForbidden {
		t.Fatalf("logout without CSRF status = %d, want 403; body = %q", noCSRF.StatusCode, noBody)
	}

	// Same mutation with CSRF succeeds.
	withCSRF := postFormJarCSRF(t, h, jar, "/logout", url.Values{})
	withBody := readBody(t, withCSRF)
	if withCSRF.StatusCode != http.StatusSeeOther && withCSRF.StatusCode != http.StatusOK {
		t.Fatalf("logout with CSRF status = %d; body = %q", withCSRF.StatusCode, withBody)
	}
}

func mustSetup(t *testing.T, h *apptest.Harness, username, password string) {
	t.Helper()
	res := postForm(t, h, "/setup", url.Values{
		"username": {username},
		"password": {password},
	})
	body := readBody(t, res)
	if res.StatusCode != http.StatusSeeOther && res.StatusCode != http.StatusOK {
		t.Fatalf("setup: status %d body %q", res.StatusCode, body)
	}
}

func newJar(t *testing.T) http.CookieJar {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	return jar
}

func setCookieNamed(res *http.Response, name string) *http.Cookie {
	for _, c := range res.Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func postFormJar(t *testing.T, h *apptest.Harness, jar http.CookieJar, path string, form url.Values) *http.Response {
	t.Helper()
	return doForm(t, h, jar, path, form, false)
}

func postFormJarCSRF(t *testing.T, h *apptest.Harness, jar http.CookieJar, path string, form url.Values) *http.Response {
	t.Helper()
	return doForm(t, h, jar, path, form, true)
}

func doForm(t *testing.T, h *apptest.Harness, jar http.CookieJar, path string, form url.Values, withCSRF bool) *http.Response {
	t.Helper()
	if withCSRF {
		if csrf := cookieNamed(jar, h.BaseURL, "csrf"); csrf != nil {
			form = cloneValues(form)
			form.Set("csrf", csrf.Value)
		}
	}
	req, err := http.NewRequest(http.MethodPost, h.BaseURL+path, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if withCSRF {
		if csrf := cookieNamed(jar, h.BaseURL, "csrf"); csrf != nil {
			req.Header.Set("X-CSRF-Token", csrf.Value)
		}
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

func cookieNamed(jar http.CookieJar, baseURL, name string) *http.Cookie {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil
	}
	for _, c := range jar.Cookies(u) {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func cloneValues(v url.Values) url.Values {
	out := make(url.Values, len(v))
	for k, vals := range v {
		out[k] = append([]string(nil), vals...)
	}
	return out
}

func postForm(t *testing.T, h *apptest.Harness, path string, form url.Values) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, h.BaseURL+path, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
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

func readBody(t *testing.T, res *http.Response) string {
	t.Helper()
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}
