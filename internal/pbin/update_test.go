package pbin_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/harshalranjhani/paste/internal/pbin"
)

// Redirect external release HTTP traffic only in the test subprocess. The CLI
// still runs against and replaces its own executable, using its public entrypoint.
type releaseTestTransport struct {
	endpoint  *url.URL
	transport http.RoundTripper
}

func (transport releaseTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	request.URL.Scheme = transport.endpoint.Scheme
	request.URL.Host = transport.endpoint.Host
	return transport.transport.RoundTrip(request)
}

func runUpdateTestProcess() {
	endpoint, err := url.Parse(os.Getenv("PBIN_TEST_RELEASE_URL"))
	if err != nil {
		panic(err)
	}
	http.DefaultTransport = releaseTestTransport{endpoint, http.DefaultTransport}
	os.Exit(pbin.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, os.Getenv, os.Getenv("PBIN_TEST_VERSION")))
}

func serveCLIRelease(t *testing.T, version string, assets map[string][]byte) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/harshalranjhani/paste/releases/latest" {
			_ = json.NewEncoder(w).Encode(map[string]string{"tag_name": version})
			return
		}
		if strings.HasPrefix(r.URL.Path, "/harshalranjhani/paste/releases/download/"+version+"/") {
			if body, ok := assets[filepath.Base(r.URL.Path)]; ok {
				_, _ = w.Write(body)
				return
			}
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	return server
}

func cliReleaseArchive(t *testing.T, name string, binary []byte) (string, []byte) {
	t.Helper()
	var archive bytes.Buffer
	extension := ".tar.gz"
	if runtime.GOOS == "windows" {
		extension = ".zip"
		writer := zip.NewWriter(&archive)
		file, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(binary); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
	} else {
		compressed := gzip.NewWriter(&archive)
		writer := tar.NewWriter(compressed)
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(binary))}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(binary); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		if err := compressed.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return "pbin_v0.1.2_" + runtime.GOOS + "_" + runtime.GOARCH + extension, archive.Bytes()
}

func installedUpdateCLI(t *testing.T, serverURL, currentVersion string) string {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	installed := filepath.Join(directory, "pbin")
	if runtime.GOOS == "windows" {
		installed += ".exe"
	}
	if err := os.WriteFile(installed, binary, 0o755); err != nil {
		t.Fatal(err)
	}
	original := pbinBin
	pbinBin = installed
	t.Cleanup(func() { pbinBin = original })
	t.Setenv("PBIN_TEST_RELEASE_URL", serverURL)
	t.Setenv("PBIN_TEST_VERSION", currentVersion)
	return directory
}

func TestUpdateInstallsVerifiedReleaseAndPreservesLogin(t *testing.T) {
	binary, err := os.ReadFile(pbinBin)
	if err != nil {
		t.Fatal(err)
	}
	name, archive := cliReleaseArchive(t, filepath.Base(pbinBin), binary)
	server := serveCLIRelease(t, "v0.1.2", map[string][]byte{
		name: archive, "checksums.txt": fmt.Appendf(nil, "%x  %s\n", sha256.Sum256(archive), name),
	})
	installedUpdateCLI(t, server.URL, "v0.1.1")
	configDir := t.TempDir()
	const config = `{"server":"https://paste.example.com","token":"saved-token"}`
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := runPbin(t, configDir, "", "update")
	if code != 0 || !strings.Contains(stdout, "Updated pbin to v0.1.2") || stderr != "" {
		t.Fatalf("update: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	stdout, stderr, code = runPbin(t, configDir, "", "version")
	if code != 0 || stdout != "pbin v0.1.2\n" || stderr != "" {
		t.Fatalf("updated executable: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	stdout, stderr, code = runPbin(t, configDir, "", "auth", "status")
	if code != 0 || !strings.Contains(stdout, "https://paste.example.com") || stderr != "" {
		t.Fatalf("preserved auth: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	stored, err := os.ReadFile(filepath.Join(configDir, "config.json"))
	if err != nil || string(stored) != config {
		t.Fatalf("login config changed: %q err=%v", stored, err)
	}
}

func TestUpdateSkipsCurrentAndNewerInstalledVersions(t *testing.T) {
	for _, current := range []string{"v0.1.2", "v0.2.0", "v0.1.10"} {
		t.Run(current, func(t *testing.T) {
			// No assets are available: an up-to-date CLI must not need a download.
			server := serveCLIRelease(t, "v0.1.2", nil)
			installedUpdateCLI(t, server.URL, current)
			stdout, stderr, code := runPbin(t, t.TempDir(), "", "update")
			if code != 0 || !strings.Contains(stdout, "already up to date") || stderr != "" {
				t.Fatalf("up-to-date CLI %s: code=%d stdout=%q stderr=%q", current, code, stdout, stderr)
			}
			stdout, stderr, code = runPbin(t, t.TempDir(), "", "version")
			if code != 0 || stdout != "pbin "+current+"\n" || stderr != "" {
				t.Fatalf("current executable changed: code=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
		})
	}
}

func TestUpdateFailuresLeaveInstalledCLIUsable(t *testing.T) {
	binary, err := os.ReadFile(pbinBin)
	if err != nil {
		t.Fatal(err)
	}
	name, goodArchive := cliReleaseArchive(t, filepath.Base(pbinBin), binary)
	_, badBinary := cliReleaseArchive(t, filepath.Base(pbinBin), []byte("not an executable"))
	_, wrongPath := cliReleaseArchive(t, "../"+filepath.Base(pbinBin), binary)
	_, emptyBinary := cliReleaseArchive(t, filepath.Base(pbinBin), nil)
	for _, failure := range []struct {
		name, version, checksum string
		archive                 []byte
	}{
		{"checksum mismatch", "v0.1.2", strings.Repeat("0", 64), goodArchive},
		{"non-executable payload", "v0.1.2", "", badBinary},
		{"wrong embedded version", "v0.1.3", "", goodArchive},
		{"unsafe archive path", "v0.1.2", "", wrongPath},
		{"empty executable", "v0.1.2", "", emptyBinary},
		{"corrupt archive", "v0.1.2", "", []byte("invalid archive")},
		{"malformed checksum", "v0.1.2", "invalid-hash", goodArchive},
		{"unsafe release tag", "v0.1.2/../other", "", goodArchive},
	} {
		t.Run(failure.name, func(t *testing.T) {
			checksum := failure.checksum
			if checksum == "" {
				checksum = fmt.Sprintf("%x", sha256.Sum256(failure.archive))
			}
			assetName := strings.Replace(name, "v0.1.2", failure.version, 1)
			server := serveCLIRelease(t, failure.version, map[string][]byte{
				assetName: failure.archive, "checksums.txt": []byte(checksum + "  " + assetName + "\n"),
			})
			installedUpdateCLI(t, server.URL, "v0.1.1")
			stdout, stderr, code := runPbin(t, t.TempDir(), "", "update")
			if code != 1 || stdout != "" || !strings.Contains(stderr, "update failed:") {
				t.Fatalf("failed update: code=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
			stdout, stderr, code = runPbin(t, t.TempDir(), "", "version")
			if code != 0 || stdout != "pbin v0.1.1\n" || stderr != "" {
				t.Fatalf("original executable unusable: code=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
		})
	}
}

func TestUpdateRejectsInsecureReleaseRedirects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/harshalranjhani/paste/releases/latest" {
			http.Redirect(w, r, "http://example.com/release", http.StatusFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"tag_name": "v0.1.2"})
	}))
	defer server.Close()
	installedUpdateCLI(t, server.URL, "v0.1.2")
	stdout, stderr, code := runPbin(t, t.TempDir(), "", "update")
	if code != 1 || stdout != "" || !strings.Contains(stderr, "insecure release redirect") {
		t.Fatalf("insecure redirect: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestUpdateReportsUnavailableReleaseAndAssets(t *testing.T) {
	name, _ := cliReleaseArchive(t, "pbin", nil)
	for _, failure := range []string{"API unavailable", "invalid metadata", "missing checksums", "missing archive"} {
		t.Run(failure, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/repos/harshalranjhani/paste/releases/latest" {
					if failure == "API unavailable" {
						http.Error(w, "unavailable", http.StatusServiceUnavailable)
					} else if failure == "invalid metadata" {
						_, _ = w.Write([]byte("invalid JSON"))
					} else {
						_ = json.NewEncoder(w).Encode(map[string]string{"tag_name": "v0.1.2"})
					}
					return
				}
				if failure == "missing archive" && strings.HasSuffix(r.URL.Path, "/checksums.txt") {
					fmt.Fprintf(w, "%s  %s\n", strings.Repeat("0", 64), name)
					return
				}
				http.NotFound(w, r)
			}))
			defer server.Close()
			installedUpdateCLI(t, server.URL, "v0.1.1")
			stdout, stderr, code := runPbin(t, t.TempDir(), "", "update")
			if code != 1 || stdout != "" || !strings.Contains(stderr, "Manual download:") {
				t.Fatalf("unavailable release: code=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
			stdout, stderr, code = runPbin(t, t.TempDir(), "", "version")
			if code != 0 || stdout != "pbin v0.1.1\n" || stderr != "" {
				t.Fatalf("original executable unusable: code=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
		})
	}
}

func TestUpdateKeepsSymlinkedInstallationWorking(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows symlinks require privileges; running-image replacement is tested separately")
	}
	binary, err := os.ReadFile(pbinBin)
	if err != nil {
		t.Fatal(err)
	}
	name, archive := cliReleaseArchive(t, "pbin", binary)
	server := serveCLIRelease(t, "v0.1.2", map[string][]byte{
		name: archive, "checksums.txt": fmt.Appendf(nil, "%x  %s\n", sha256.Sum256(archive), name),
	})
	directory := installedUpdateCLI(t, server.URL, "v0.1.1")
	link := filepath.Join(directory, "pbin-link")
	if err := os.Symlink(pbinBin, link); err != nil {
		t.Fatal(err)
	}
	pbinBin = link
	stdout, stderr, code := runPbin(t, t.TempDir(), "", "update")
	if code != 0 || !strings.Contains(stdout, "Updated pbin to v0.1.2") || stderr != "" {
		t.Fatalf("symlink update: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	stdout, stderr, code = runPbin(t, t.TempDir(), "", "version")
	if code != 0 || stdout != "pbin v0.1.2\n" || stderr != "" {
		t.Fatalf("updated symlink: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("installation symlink replaced: %v", err)
	}
}

func TestUpdateRejectsUnexpectedArguments(t *testing.T) {
	stdout, stderr, code := runPbin(t, t.TempDir(), "", "update", "unexpected")
	if code != 2 || stdout != "" || stderr != "usage: pbin update\n" {
		t.Fatalf("update args: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestUpdateReportsUnwritableInstallationWithoutReplacingCLI(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("Unix directory permissions require a non-root process")
	}
	binary, err := os.ReadFile(pbinBin)
	if err != nil {
		t.Fatal(err)
	}
	name, archive := cliReleaseArchive(t, "pbin", binary)
	server := serveCLIRelease(t, "v0.1.2", map[string][]byte{
		name: archive, "checksums.txt": fmt.Appendf(nil, "%x  %s\n", sha256.Sum256(archive), name),
	})
	directory := installedUpdateCLI(t, server.URL, "v0.1.1")
	if err := os.Chmod(directory, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(directory, 0o755) })
	stdout, stderr, code := runPbin(t, t.TempDir(), "", "update")
	if code != 1 || stdout != "" || !strings.Contains(stderr, "replace installed executable") {
		t.Fatalf("unwritable installation: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	stdout, stderr, code = runPbin(t, t.TempDir(), "", "version")
	if code != 0 || stdout != "pbin v0.1.1\n" || stderr != "" {
		t.Fatalf("original executable unusable: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}
