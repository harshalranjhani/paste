package pbin_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/harshalranjhani/paste/internal/apptest"
)

var pbinBin string

func TestVersionReportsEmbeddedReleaseVersion(t *testing.T) {
	for _, command := range []string{"version", "--version"} {
		stdout, stderr, code := runPbin(t, t.TempDir(), "", command)
		if code != 0 || stdout != "pbin v0.1.2\n" || stderr != "" {
			t.Fatalf("%s: code=%d stdout=%q stderr=%q", command, code, stdout, stderr)
		}
	}
}

func TestCreateSlugChoicesWorkForStdinFileAndDirectory(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	owner := mustLogin(t, h, "admin", "correct-horse-battery-staple")
	pat := mustCreatePAT(t, h, owner, map[string]any{"name": "slug-cli", "scopes": []string{"paste:create"}})
	configDir := t.TempDir()
	mustLoginCLI(t, configDir, h.BaseURL, pat.Token)
	root := t.TempDir()
	file := filepath.Join(root, "notes.txt")
	mustWriteFile(t, file, "cli-slug-source")
	for _, source := range []struct {
		name, stdin string
		args        []string
	}{
		{"stdin", "cli-slug-source", []string{"create"}},
		{"file", "", []string{"create", file}},
		{"directory", "y\n", []string{"create", root}},
	} {
		for _, mode := range []string{"custom", "short", "long"} {
			args := append([]string(nil), source.args...)
			if mode == "custom" {
				args = append(args, "--slug", "cli-"+source.name)
			} else {
				args = append(args, "--slug-length", mode)
			}
			stdout, stderr, code := runPbin(t, configDir, source.stdin, args...)
			if code != 0 {
				t.Fatalf("%s %s: code=%d stderr=%q", source.name, mode, code, stderr)
			}
			id := pasteIDFromURL(t, strings.TrimSpace(stdout))
			pattern := `^[A-Za-z0-9]{9,22}$`
			if mode == "short" {
				pattern = `^[A-Za-z0-9]{8}$`
			}
			if mode == "custom" {
				pattern = "^cli-" + source.name + "$"
			}
			if !regexp.MustCompile(pattern).MatchString(id) {
				t.Fatalf("%s %s: id=%q", source.name, mode, id)
			}
			res, err := h.GET("/p/" + id + "/raw")
			if err != nil {
				t.Fatal(err)
			}
			if body := readBody(t, res); body != "cli-slug-source" {
				t.Fatalf("CLI source: %q", body)
			}
		}
	}
	for _, bad := range []struct {
		args    []string
		code    int
		message string
	}{
		{[]string{"create", "--slug", "cli-stdin"}, 1, "slug_taken"},
		{[]string{"create", "--slug", "../unsafe"}, 1, "invalid_slug"},
		{[]string{"create", "--slug-length", "tiny"}, 1, "invalid_slug_length"},
		{[]string{"create", "--slug"}, 2, "--slug requires a value"},
		{[]string{"create", "--slug-length"}, 2, "--slug-length requires a value"},
	} {
		stdout, stderr, code := runPbin(t, configDir, "replacement-source", bad.args...)
		if code != bad.code || stdout != "" || !strings.Contains(stderr, bad.message) {
			t.Fatalf("args=%v: code=%d stdout=%q stderr=%q", bad.args, code, stdout, stderr)
		}
	}
	res, err := h.GET("/p/cli-stdin/raw")
	if err != nil {
		t.Fatal(err)
	}
	if body := readBody(t, res); body != "cli-slug-source" {
		t.Fatalf("CLI collision overwrote paste: %q", body)
	}
}

func TestCreateBurnFlagWorksForStdinFileAndDirectory(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	owner := mustLogin(t, h, "admin", "correct-horse-battery-staple")
	pat := mustCreatePAT(t, h, owner, map[string]any{"name": "burn-cli", "scopes": []string{"paste:create", "paste:read"}})
	configDir := t.TempDir()
	mustLoginCLI(t, configDir, h.BaseURL, pat.Token)
	root := t.TempDir()
	file := filepath.Join(root, "secret.txt")
	mustWriteFile(t, file, "cli-burn-secret")
	for _, tc := range []struct {
		name, stdin string
		args        []string
	}{
		{"stdin", "cli-burn-secret", []string{"create", "--burn"}},
		{"file", "", []string{"create", file, "--burn"}},
		{"directory", "y\n", []string{"create", root, "--burn"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, code := runPbin(t, configDir, tc.stdin, tc.args...)
			if code != 0 {
				t.Fatalf("create --burn: code=%d stderr=%q", code, stderr)
			}
			id := pasteIDFromURL(t, strings.TrimSpace(stdout))
			res, err := h.GET("/p/" + id)
			if err != nil {
				t.Fatal(err)
			}
			body := readBody(t, res)
			if !strings.Contains(body, "Reveal paste") || strings.Contains(body, "cli-burn-secret") {
				t.Fatal("CLI burn must require reveal")
			}
		})
	}
}

func TestMain(m *testing.M) {
	if os.Getenv("PBIN_TEST_RELEASE_URL") != "" {
		runUpdateTestProcess()
	}
	dir, err := os.MkdirTemp("", "pbin-bin-*")
	if err != nil {
		panic(err)
	}
	pbinBin = filepath.Join(dir, "pbin")
	if runtime.GOOS == "windows" {
		pbinBin += ".exe"
	}
	cmd := exec.Command("go", "build", "-ldflags", "-X main.version=v0.1.2", "-o", pbinBin, "github.com/harshalranjhani/paste/cmd/pbin")
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

func TestCreateDirectoryZIPRoundTripsHierarchy(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	jar := mustLogin(t, h, "admin", "correct-horse-battery-staple")
	pat := mustCreatePAT(t, h, jar, map[string]any{
		"name":   "cli",
		"scopes": []string{"paste:create", "paste:read", "paste:delete"},
	})
	configDir := t.TempDir()
	mustLoginCLI(t, configDir, h.BaseURL, pat.Token)

	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "README.md"), "# top\n")
	mustWriteFile(t, filepath.Join(root, "src", "main.go"), "package main\n")
	mustWriteFile(t, filepath.Join(root, "docs", "guide.txt"), "guide\n")

	stdout, stderr, code := runPbin(t, configDir, "y\n", "create", root)
	if code != 0 {
		t.Fatalf("create dir exit = %d; stdout=%q stderr=%q", code, stdout, stderr)
	}
	id := pasteIDFromURL(t, strings.TrimSpace(stdout))

	zipRes, err := h.GET("/api/v1/pastes/" + id + "/archive.zip")
	if err != nil {
		t.Fatalf("GET zip: %v", err)
	}
	defer zipRes.Body.Close()
	if zipRes.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(zipRes.Body)
		t.Fatalf("zip status = %d; body = %q", zipRes.StatusCode, body)
	}
	zipBytes, err := io.ReadAll(zipRes.Body)
	if err != nil {
		t.Fatalf("read zip: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	got := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open entry: %v", err)
		}
		body, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatalf("read entry: %v", err)
		}
		got[f.Name] = string(body)
	}
	if got["README.md"] != "# top\n" {
		t.Fatalf("README.md = %q", got["README.md"])
	}
	if got["src/main.go"] != "package main\n" {
		t.Fatalf("src/main.go = %q", got["src/main.go"])
	}
	if got["docs/guide.txt"] != "guide\n" {
		t.Fatalf("docs/guide.txt = %q", got["docs/guide.txt"])
	}
}

func TestCreateDirectoryWarnsOnSensitiveNamesWithoutBlocking(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	jar := mustLogin(t, h, "admin", "correct-horse-battery-staple")
	pat := mustCreatePAT(t, h, jar, map[string]any{
		"name":   "cli",
		"scopes": []string{"paste:create", "paste:read", "paste:delete"},
	})
	configDir := t.TempDir()
	mustLoginCLI(t, configDir, h.BaseURL, pat.Token)

	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, ".env"), "SECRET=1\n")
	mustWriteFile(t, filepath.Join(root, "id_rsa"), "-----BEGIN\n")
	mustWriteFile(t, filepath.Join(root, "certs", "server.pem"), "pem\n")
	mustWriteFile(t, filepath.Join(root, "ok.txt"), "fine\n")

	stdout, stderr, code := runPbin(t, configDir, "y\n", "create", root)
	if code != 0 {
		t.Fatalf("create dir exit = %d; stdout=%q stderr=%q", code, stdout, stderr)
	}
	for _, needle := range []string{".env", "id_rsa", "server.pem"} {
		if !strings.Contains(stderr, needle) {
			t.Fatalf("expected sensitive warning mentioning %q; stderr=%q", needle, stderr)
		}
	}
	id := pasteIDFromURL(t, strings.TrimSpace(stdout))
	metaRes, err := h.GET("/api/v1/pastes/" + id + "/meta")
	if err != nil {
		t.Fatalf("GET meta: %v", err)
	}
	metaBody := readBody(t, metaRes)
	for _, want := range []string{".env", "id_rsa", "certs/server.pem", "ok.txt"} {
		if !strings.Contains(metaBody, `"`+want+`"`) && !strings.Contains(metaBody, want) {
			t.Fatalf("sensitive file %q was blocked; body = %q", want, metaBody)
		}
	}
}

func TestCreateDirectoryShowsSummaryAndCanAbort(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	jar := mustLogin(t, h, "admin", "correct-horse-battery-staple")
	pat := mustCreatePAT(t, h, jar, map[string]any{
		"name":   "cli",
		"scopes": []string{"paste:create", "paste:read", "paste:delete"},
	})
	configDir := t.TempDir()
	mustLoginCLI(t, configDir, h.BaseURL, pat.Token)

	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "a.txt"), "aaa\n")
	mustWriteFile(t, filepath.Join(root, "b.txt"), "bbbb\n")

	stdout, stderr, code := runPbin(t, configDir, "n\n", "create", root, "--expires", "7d")
	if code == 0 {
		t.Fatalf("abort should be non-zero; stdout=%q stderr=%q", stdout, stderr)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Fatalf("abort must not print URL; stdout=%q", stdout)
	}
	for _, needle := range []string{"2 files", "9 bytes", "7d", "unlisted"} {
		if !strings.Contains(stderr, needle) {
			t.Fatalf("summary missing %q; stderr=%q", needle, stderr)
		}
	}

	stdout, stderr, code = runPbin(t, configDir, "y\n", "create", root, "--expires", "7d")
	if code != 0 {
		t.Fatalf("confirm create exit = %d; stdout=%q stderr=%q", code, stdout, stderr)
	}
	pasteURL := strings.TrimSpace(stdout)
	if !strings.Contains(pasteURL, "/p/") {
		t.Fatalf("expected paste URL; stdout=%q stderr=%q", stdout, stderr)
	}
	assertPasteContains(t, h, pasteURL, "aaa")
}

func TestCreateDirectorySkipsSymlinks(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	jar := mustLogin(t, h, "admin", "correct-horse-battery-staple")
	pat := mustCreatePAT(t, h, jar, map[string]any{
		"name":   "cli",
		"scopes": []string{"paste:create", "paste:read", "paste:delete"},
	})
	configDir := t.TempDir()
	mustLoginCLI(t, configDir, h.BaseURL, pat.Token)

	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "real.txt"), "real\n")
	outside := filepath.Join(t.TempDir(), "outside.txt")
	mustWriteFile(t, outside, "escaped\n")
	if err := os.Symlink(outside, filepath.Join(root, "link.txt")); err != nil {
		t.Fatalf("symlink file: %v", err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "linkdir")); err != nil {
		t.Fatalf("symlink dir: %v", err)
	}

	stdout, stderr, code := runPbin(t, configDir, "y\n", "create", root)
	if code != 0 {
		t.Fatalf("create dir exit = %d; stdout=%q stderr=%q", code, stdout, stderr)
	}
	id := pasteIDFromURL(t, strings.TrimSpace(stdout))

	metaRes, err := h.GET("/api/v1/pastes/" + id + "/meta")
	if err != nil {
		t.Fatalf("GET meta: %v", err)
	}
	metaBody := readBody(t, metaRes)
	var meta struct {
		Files []struct {
			Path string `json:"path"`
		} `json:"files"`
	}
	if err := json.Unmarshal([]byte(metaBody), &meta); err != nil {
		t.Fatalf("parse meta: %v; body = %q", err, metaBody)
	}
	byPath := map[string]bool{}
	for _, f := range meta.Files {
		byPath[f.Path] = true
	}
	if !byPath["real.txt"] {
		t.Fatalf("expected real.txt; body = %q", metaBody)
	}
	if byPath["link.txt"] {
		t.Fatalf("symlink file was uploaded; body = %q", metaBody)
	}
	if strings.Contains(metaBody, "escaped") {
		t.Fatalf("symlink target content leaked into meta; body = %q", metaBody)
	}
}

func TestCreateDirectoryHonorsIgnoreRules(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	jar := mustLogin(t, h, "admin", "correct-horse-battery-staple")
	pat := mustCreatePAT(t, h, jar, map[string]any{
		"name":   "cli",
		"scopes": []string{"paste:create", "paste:read", "paste:delete"},
	})
	configDir := t.TempDir()
	mustLoginCLI(t, configDir, h.BaseURL, pat.Token)

	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "keep.txt"), "keep\n")
	mustWriteFile(t, filepath.Join(root, "build", "out.txt"), "built\n")
	mustWriteFile(t, filepath.Join(root, "secret.local"), "secret\n")
	mustWriteFile(t, filepath.Join(root, ".git", "config"), "gitmeta\n")
	mustWriteFile(t, filepath.Join(root, ".gitignore"), "build/\n")
	mustWriteFile(t, filepath.Join(root, ".pasteignore"), "*.local\n")

	stdout, stderr, code := runPbin(t, configDir, "y\n", "create", root)
	if code != 0 {
		t.Fatalf("create dir exit = %d; stdout=%q stderr=%q", code, stdout, stderr)
	}
	id := pasteIDFromURL(t, strings.TrimSpace(stdout))

	metaRes, err := h.GET("/api/v1/pastes/" + id + "/meta")
	if err != nil {
		t.Fatalf("GET meta: %v", err)
	}
	metaBody := readBody(t, metaRes)
	if metaRes.StatusCode != http.StatusOK {
		t.Fatalf("meta status = %d; body = %q", metaRes.StatusCode, metaBody)
	}
	var meta struct {
		Files []struct {
			Path string `json:"path"`
		} `json:"files"`
	}
	if err := json.Unmarshal([]byte(metaBody), &meta); err != nil {
		t.Fatalf("parse meta: %v; body = %q", err, metaBody)
	}
	byPath := map[string]bool{}
	for _, f := range meta.Files {
		byPath[f.Path] = true
	}
	if !byPath["keep.txt"] {
		t.Fatalf("expected keep.txt; body = %q", metaBody)
	}
	for _, banned := range []string{"build/out.txt", "secret.local", ".git/config"} {
		if byPath[banned] {
			t.Fatalf("unexpected path %q uploaded; body = %q", banned, metaBody)
		}
	}
}

func TestCreateDirectoryUploadsNestedPathsAsOnePaste(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	jar := mustLogin(t, h, "admin", "correct-horse-battery-staple")
	pat := mustCreatePAT(t, h, jar, map[string]any{
		"name":   "cli",
		"scopes": []string{"paste:create", "paste:read", "paste:delete"},
	})
	configDir := t.TempDir()
	mustLoginCLI(t, configDir, h.BaseURL, pat.Token)

	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "README.md"), "# hello\n")
	mustWriteFile(t, filepath.Join(root, "src", "main.go"), "package main\n")
	mustWriteFile(t, filepath.Join(root, "src", "util", "helper.go"), "package util\n")

	stdout, stderr, code := runPbin(t, configDir, "y\n", "create", root)
	if code != 0 {
		t.Fatalf("create dir exit = %d; stdout=%q stderr=%q", code, stdout, stderr)
	}
	pasteURL := strings.TrimSpace(stdout)
	if pasteURL == "" || strings.Contains(pasteURL, "\n") {
		t.Fatalf("stdout must be a single URL line; got %q", stdout)
	}
	id := pasteIDFromURL(t, pasteURL)

	metaRes, err := h.GET("/api/v1/pastes/" + id + "/meta")
	if err != nil {
		t.Fatalf("GET meta: %v", err)
	}
	metaBody := readBody(t, metaRes)
	if metaRes.StatusCode != http.StatusOK {
		t.Fatalf("meta status = %d; body = %q", metaRes.StatusCode, metaBody)
	}
	var meta struct {
		Files []struct {
			Path string `json:"path"`
		} `json:"files"`
	}
	if err := json.Unmarshal([]byte(metaBody), &meta); err != nil {
		t.Fatalf("parse meta: %v; body = %q", err, metaBody)
	}
	byPath := map[string]bool{}
	for _, f := range meta.Files {
		byPath[f.Path] = true
	}
	for _, want := range []string{"README.md", "src/main.go", "src/util/helper.go"} {
		if !byPath[want] {
			t.Fatalf("missing path %q in meta; body = %q", want, metaBody)
		}
	}
	if len(meta.Files) != 3 {
		t.Fatalf("meta files = %d, want 3; body = %q", len(meta.Files), metaBody)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
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
