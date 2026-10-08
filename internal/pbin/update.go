package pbin

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const maxUpdateBytes = 64 << 20
const releaseRepository = "harshalranjhani/paste"

var stableReleaseTag = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

func runUpdate(args []string, stdout, stderr io.Writer, currentVersion string) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "usage: pbin update")
		return 2
	}
	message, err := updateCLI(currentVersion)
	if err != nil {
		fmt.Fprintf(stderr, "update failed: %v\nManual download: https://github.com/%s/releases/latest\n", err, releaseRepository)
		return 1
	}
	fmt.Fprintln(stdout, message)
	return 0
}

func updateCLI(currentVersion string) (string, error) {
	client := &http.Client{
		Timeout: 2 * time.Minute,
		CheckRedirect: func(request *http.Request, previous []*http.Request) error {
			if request.URL.Scheme != "https" {
				return errors.New("insecure release redirect")
			}
			if len(previous) >= 10 {
				return errors.New("too many release redirects")
			}
			return nil
		},
	}
	metadata, err := downloadUpdate(client, "https://api.github.com/repos/"+releaseRepository+"/releases/latest", 1<<20)
	if err != nil {
		return "", err
	}
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(metadata, &release); err != nil {
		return "", fmt.Errorf("invalid release response: %w", err)
	}
	if !stableReleaseTag.MatchString(release.TagName) {
		return "", fmt.Errorf("invalid stable release version %q", release.TagName)
	}
	if !newerRelease(release.TagName, currentVersion) {
		return "pbin " + currentVersion + " is already up to date (latest stable " + release.TagName + ").", nil
	}
	extension := ".tar.gz"
	if runtime.GOOS == "windows" {
		extension = ".zip"
	}
	name := fmt.Sprintf("pbin_%s_%s_%s%s", release.TagName, runtime.GOOS, runtime.GOARCH, extension)
	baseURL := "https://github.com/" + releaseRepository + "/releases/download/" + release.TagName + "/"
	checksums, err := downloadUpdate(client, baseURL+"checksums.txt", 1<<20)
	if err != nil {
		return "", err
	}
	var checksum []byte
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == name {
			checksum, err = hex.DecodeString(fields[0])
			if err != nil || len(checksum) != sha256.Size {
				return "", errors.New("invalid release checksum")
			}
			break
		}
	}
	if checksum == nil {
		return "", fmt.Errorf("no checksum for %s", name)
	}
	archive, err := downloadUpdate(client, baseURL+name, maxUpdateBytes)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(archive)
	if !bytes.Equal(checksum, digest[:]) {
		return "", errors.New("release checksum mismatch")
	}
	binary, err := readUpdateBinary(archive)
	if err != nil {
		return "", err
	}
	if err := installUpdate(binary, release.TagName); err != nil {
		return "", fmt.Errorf("replace installed executable: %w", err)
	}
	return "Updated pbin to " + release.TagName, nil
}

func downloadUpdate(client *http.Client, address string, limit int64) ([]byte, error) {
	request, err := http.NewRequest(http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "pbin")
	if request.URL.Host == "api.github.com" {
		request.Header.Set("Accept", "application/vnd.github+json")
		request.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", address, response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, errors.New("release download exceeds size limit")
	}
	return body, nil
}

func readUpdateBinary(archive []byte) ([]byte, error) {
	var reader io.ReadCloser
	if runtime.GOOS == "windows" {
		files, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, err
		}
		for _, file := range files.File {
			if file.Name == "pbin.exe" && file.Mode().IsRegular() {
				reader, err = file.Open()
				if err != nil {
					return nil, err
				}
				break
			}
		}
	} else {
		compressed, err := gzip.NewReader(bytes.NewReader(archive))
		if err != nil {
			return nil, err
		}
		defer compressed.Close()
		files := tar.NewReader(io.LimitReader(compressed, maxUpdateBytes+1024))
		for {
			file, err := files.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
			if file.Name == "pbin" && file.Typeflag == tar.TypeReg {
				reader = io.NopCloser(files)
				break
			}
		}
	}
	if reader == nil {
		return nil, errors.New("release archive is missing the pbin executable")
	}
	defer reader.Close()
	binary, err := io.ReadAll(io.LimitReader(reader, maxUpdateBytes+1))
	if err != nil {
		return nil, err
	}
	if len(binary) == 0 || len(binary) > maxUpdateBytes {
		return nil, errors.New("invalid release executable size")
	}
	return binary, nil
}

func installUpdate(binary []byte, version string) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return err
	}
	info, err := os.Stat(executable)
	if err != nil {
		return err
	}
	pattern := ".pbin-update-*"
	if runtime.GOOS == "windows" {
		pattern += ".exe"
	}
	file, err := os.CreateTemp(filepath.Dir(executable), pattern)
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if _, err := file.Write(binary); err != nil {
		return err
	}
	if err := file.Chmod(info.Mode().Perm()); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, file.Name(), "version").Output()
	if err != nil {
		return fmt.Errorf("downloaded executable failed version check: %w", err)
	}
	if strings.TrimSpace(string(output)) != "pbin "+version {
		return fmt.Errorf("downloaded executable does not report %s", version)
	}
	if runtime.GOOS != "windows" {
		return os.Rename(file.Name(), executable)
	}
	// Windows cannot overwrite the running image. Rename it first and restore it
	// if installation fails; an in-use backup is removed on the next update.
	backup := executable + ".old"
	if err := os.Remove(backup); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(executable, backup); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), executable); err != nil {
		return errors.Join(err, os.Rename(backup, executable))
	}
	_ = os.Remove(backup)
	return nil
}
