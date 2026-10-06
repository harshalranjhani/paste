package pbin_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/harshalranjhani/paste/internal/apptest"
)

var pbinBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "pbin-bin-*")
	if err != nil {
		panic(err)
	}
	pbinBin = filepath.Join(dir, "pbin")
	cmd := exec.Command("go", "build", "-o", pbinBin, "github.com/harshalranjhani/paste/cmd/pbin")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		_ = os.RemoveAll(dir)
		panic("build pbin: " + err.Error())
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func TestAuthLoginStatusLogoutStoresCredentialsPrivately(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	jar := mustLogin(t, h, "admin", "correct-horse-battery-staple")
	pat := mustCreatePAT(t, h, jar, map[string]any{
		"name":   "cli",
		"scopes": []string{"paste:create", "paste:read", "paste:delete"},
	})

	configDir := t.TempDir()

	stdout, stderr, code := runPbin(t, configDir, pat.Token+"\n", "auth", "login", "--server", h.BaseURL)
	if code != 0 {
		t.Fatalf("auth login exit = %d; stdout=%q stderr=%q", code, stdout, stderr)
	}
	if strings.Contains(stdout, pat.Token) || strings.Contains(stderr, pat.Token) {
		t.Fatal("login output leaked token")
	}

	cfgPath := filepath.Join(configDir, "config.json")
	info, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatalf("config file missing: %v", err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Fatalf("config permissions = %o, want no group/other bits", perm)
	}

	stdout, stderr, code = runPbin(t, configDir, "", "auth", "status")
	if code != 0 {
		t.Fatalf("auth status exit = %d; stdout=%q stderr=%q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, h.BaseURL) && !strings.Contains(stderr, h.BaseURL) {
		t.Fatalf("status missing server URL; stdout=%q stderr=%q", stdout, stderr)
	}

	stdout, stderr, code = runPbin(t, configDir, "", "auth", "logout")
	if code != 0 {
		t.Fatalf("auth logout exit = %d; stdout=%q stderr=%q", code, stdout, stderr)
	}

	stdout, stderr, code = runPbin(t, configDir, "", "auth", "status")
	if code == 0 {
		t.Fatalf("auth status after logout should fail; stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestCreateFromStdinAndFilePrintsOnlyURL(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	jar := mustLogin(t, h, "admin", "correct-horse-battery-staple")
	pat := mustCreatePAT(t, h, jar, map[string]any{
		"name":   "cli",
		"scopes": []string{"paste:create", "paste:read", "paste:delete"},
	})
	configDir := t.TempDir()
	mustLoginCLI(t, configDir, h.BaseURL, pat.Token)

	stdout, stderr, code := runPbin(t, configDir, "hello from stdin\n", "create", "--name", "hello.txt")
	if code != 0 {
		t.Fatalf("create stdin exit = %d; stdout=%q stderr=%q", code, stdout, stderr)
	}
	stdinURL := strings.TrimSpace(stdout)
	if stdinURL == "" || strings.Contains(stdinURL, "\n") {
		t.Fatalf("stdout must be a single URL line; got %q", stdout)
	}
	if !strings.Contains(stdinURL, "/p/") {
		t.Fatalf("stdout URL missing /p/; got %q", stdinURL)
	}
	assertPasteContains(t, h, stdinURL, "hello from stdin")

	filePath := filepath.Join(t.TempDir(), "file.py")
	if err := os.WriteFile(filePath, []byte("print('hi')\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	stdout, stderr, code = runPbin(t, configDir, "", "create", filePath)
	if code != 0 {
		t.Fatalf("create file exit = %d; stdout=%q stderr=%q", code, stdout, stderr)
	}
	fileURL := strings.TrimSpace(stdout)
	if fileURL == "" || strings.Contains(fileURL, "\n") {
		t.Fatalf("stdout must be a single URL line; got %q", stdout)
	}
	assertPasteContains(t, h, fileURL, "print('hi')")
}

func TestCreateJSONExpiresAndPasswordStdin(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	jar := mustLogin(t, h, "admin", "correct-horse-battery-staple")
	pat := mustCreatePAT(t, h, jar, map[string]any{
		"name":   "cli",
		"scopes": []string{"paste:create", "paste:read", "paste:delete"},
	})
	configDir := t.TempDir()
	mustLoginCLI(t, configDir, h.BaseURL, pat.Token)

	stdout, stderr, code := runPbin(t, configDir, "json body\n", "create", "--name", "a.txt", "--json", "--expires", "7d")
	if code != 0 {
		t.Fatalf("create --json exit = %d; stdout=%q stderr=%q", code, stdout, stderr)
	}
	var payload struct {
		URL       string `json:"url"`
		ID        string `json:"id"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &payload); err != nil {
		t.Fatalf("parse --json stdout: %v; stdout=%q", err, stdout)
	}
	if payload.URL == "" || payload.ID == "" {
		t.Fatalf("json missing url/id: %+v", payload)
	}
	if payload.ExpiresAt == "" {
		t.Fatalf("json missing expires_at: %+v", payload)
	}
	assertPasteContains(t, h, payload.URL, "json body")

	filePath := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(filePath, []byte("top secret\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	stdout, stderr, code = runPbin(t, configDir, "gate-secret\n", "create", filePath, "--password-stdin")
	if code != 0 {
		t.Fatalf("create --password-stdin exit = %d; stdout=%q stderr=%q", code, stdout, stderr)
	}
	secretURL := strings.TrimSpace(stdout)
	id := pasteIDFromURL(t, secretURL)

	// Locked without password: meta should require password.
	meta, err := h.GET("/api/v1/pastes/" + id + "/meta")
	if err != nil {
		t.Fatalf("meta: %v", err)
	}
	metaBody := readBody(t, meta)
	if meta.StatusCode != http.StatusOK && meta.StatusCode != http.StatusUnauthorized {
		t.Fatalf("meta status = %d; body = %q", meta.StatusCode, metaBody)
	}
	if !strings.Contains(metaBody, "password") {
		t.Fatalf("expected password gate in meta; body = %q", metaBody)
	}
	raw, err := h.GET("/p/" + id + "/raw")
	if err != nil {
		t.Fatalf("raw: %v", err)
	}
	rawBody := readBody(t, raw)
	if raw.StatusCode == http.StatusOK && strings.Contains(rawBody, "top secret") {
		t.Fatalf("raw exposed password-protected content without unlock")
	}
}

func TestDeleteOwnedPaste(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	jar := mustLogin(t, h, "admin", "correct-horse-battery-staple")
	pat := mustCreatePAT(t, h, jar, map[string]any{
		"name":   "cli",
		"scopes": []string{"paste:create", "paste:read", "paste:delete"},
	})
	configDir := t.TempDir()
	mustLoginCLI(t, configDir, h.BaseURL, pat.Token)

	stdout, stderr, code := runPbin(t, configDir, "bye soon\n", "create", "--name", "gone.txt")
	if code != 0 {
		t.Fatalf("create exit = %d; stdout=%q stderr=%q", code, stdout, stderr)
	}
	pasteURL := strings.TrimSpace(stdout)
	id := pasteIDFromURL(t, pasteURL)
	assertPasteContains(t, h, pasteURL, "bye soon")

	stdout, stderr, code = runPbin(t, configDir, "", "delete", id)
	if code != 0 {
		t.Fatalf("delete exit = %d; stdout=%q stderr=%q", code, stdout, stderr)
	}

	res, err := h.GET("/p/" + id + "/raw")
	if err != nil {
		t.Fatalf("GET raw after delete: %v", err)
	}
	body := readBody(t, res)
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("after delete status = %d, want 404; body = %q", res.StatusCode, body)
	}
}

func pasteIDFromURL(t *testing.T, pasteURL string) string {
	t.Helper()
	idx := strings.LastIndex(pasteURL, "/p/")
	if idx < 0 {
		t.Fatalf("bad paste url %q", pasteURL)
	}
	id := strings.TrimSuffix(pasteURL[idx+len("/p/"):], "/")
	return strings.Split(id, "?")[0]
}

func mustLoginCLI(t *testing.T, configDir, server, token string) {
	t.Helper()
	stdout, stderr, code := runPbin(t, configDir, token+"\n", "auth", "login", "--server", server)
	if code != 0 {
		t.Fatalf("cli login exit = %d; stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func assertPasteContains(t *testing.T, h *apptest.Harness, pasteURL, want string) {
	t.Helper()
	idx := strings.LastIndex(pasteURL, "/p/")
	if idx < 0 {
		t.Fatalf("bad paste url %q", pasteURL)
	}
	id := strings.TrimSuffix(pasteURL[idx+len("/p/"):], "/")
	id = strings.Split(id, "?")[0]
	res, err := h.GET("/p/" + id + "/raw")
	if err != nil {
		t.Fatalf("GET paste raw: %v", err)
	}
	body := readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /p/%s/raw status = %d; body = %q", id, res.StatusCode, body)
	}
	if !strings.Contains(body, want) {
		t.Fatalf("paste body missing %q; body = %q", want, body)
	}
}

func runPbin(t *testing.T, configDir, stdin string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(pbinBin, args...)
	cmd.Env = append(os.Environ(), "PBIN_CONFIG_DIR="+configDir)
	cmd.Stdin = strings.NewReader(stdin)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	stdout = outBuf.String()
	stderr = errBuf.String()
	if err == nil {
		return stdout, stderr, 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return stdout, stderr, ee.ExitCode()
	}
	t.Fatalf("run pbin %v: %v", args, err)
	return "", "", -1
}

func mustSetup(t *testing.T, h *apptest.Harness, username, password string) {
	t.Helper()
	res := postForm(t, h, nil, "/setup", url.Values{
		"username": {username},
		"password": {password},
	})
	body := readBody(t, res)
	if res.StatusCode != http.StatusSeeOther && res.StatusCode != http.StatusOK {
		t.Fatalf("setup: status %d body %q", res.StatusCode, body)
	}
}

func mustLogin(t *testing.T, h *apptest.Harness, username, password string) http.CookieJar {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	res := postForm(t, h, jar, "/login", url.Values{
		"username": {username},
		"password": {password},
	})
	body := readBody(t, res)
	if res.StatusCode != http.StatusSeeOther && res.StatusCode != http.StatusOK {
		t.Fatalf("login: status %d body %q", res.StatusCode, body)
	}
	return jar
}

func mustCreatePAT(t *testing.T, h *apptest.Harness, jar http.CookieJar, payload map[string]any) struct {
	ID    int64  `json:"id"`
	Token string `json:"token"`
} {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, h.BaseURL+"/api/v1/tokens", bytes.NewReader(body))
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
		t.Fatalf("POST tokens: %v", err)
	}
	raw := readBody(t, res)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create PAT status = %d; body = %q", res.StatusCode, raw)
	}
	var created struct {
		ID    int64  `json:"id"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(raw), &created); err != nil {
		t.Fatalf("parse PAT: %v; body = %q", err, raw)
	}
	if created.Token == "" {
		t.Fatalf("missing token in %q", raw)
	}
	return created
}

func postForm(t *testing.T, h *apptest.Harness, jar http.CookieJar, path string, form url.Values) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, h.BaseURL+path, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
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
	if err != nil || jar == nil {
		return nil
	}
	for _, c := range jar.Cookies(u) {
		if c.Name == name {
			return c
		}
	}
	return nil
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
