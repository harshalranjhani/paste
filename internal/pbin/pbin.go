package pbin

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// Run executes the pbin CLI and returns a process exit code.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: pbin <command>")
		return 2
	}
	switch args[0] {
	case "auth":
		return runAuth(args[1:], stdin, stdout, stderr, getenv)
	case "create":
		return runCreate(args[1:], stdin, stdout, stderr, getenv)
	case "delete":
		return runDelete(args[1:], stdout, stderr, getenv)
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n", args[0])
		return 2
	}
}

func runAuth(args []string, stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: pbin auth <login|status|logout>")
		return 2
	}
	switch args[0] {
	case "login":
		return authLogin(args[1:], stdin, stdout, stderr, getenv)
	case "status":
		return authStatus(stdout, stderr, getenv)
	case "logout":
		return authLogout(stdout, stderr, getenv)
	default:
		fmt.Fprintf(stderr, "unknown auth command: %s\n", args[0])
		return 2
	}
}

func authLogin(args []string, stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string) int {
	server := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--server":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "--server requires a value")
				return 2
			}
			i++
			server = strings.TrimRight(args[i], "/")
		default:
			fmt.Fprintf(stderr, "unknown flag: %s\n", args[i])
			return 2
		}
	}
	if server == "" {
		fmt.Fprintln(stderr, "usage: pbin auth login --server <url>")
		return 2
	}

	fmt.Fprint(stderr, "Token: ")
	token, err := readLine(stdin)
	if err != nil {
		fmt.Fprintf(stderr, "\nread token: %v\n", err)
		return 1
	}
	token = strings.TrimSpace(token)
	if token == "" {
		fmt.Fprintln(stderr, "\ntoken is required")
		return 1
	}
	fmt.Fprintln(stderr)

	if err := validateToken(server, token); err != nil {
		fmt.Fprintf(stderr, "login failed: %v\n", err)
		return 1
	}

	cfg := fileConfig{Server: server, Token: token}
	if err := saveConfig(getenv, cfg); err != nil {
		fmt.Fprintf(stderr, "save credentials: %v\n", err)
		return 1
	}
	fmt.Fprintf(stderr, "logged in to %s\n", server)
	return 0
}

func authStatus(stdout, stderr io.Writer, getenv func(string) string) int {
	cfg, err := loadConfig(getenv)
	if err != nil || cfg.Server == "" || cfg.Token == "" {
		fmt.Fprintln(stderr, "not logged in")
		return 1
	}
	fmt.Fprintf(stdout, "server: %s\n", cfg.Server)
	fmt.Fprintln(stdout, "token: configured")
	return 0
}

func authLogout(stdout, stderr io.Writer, getenv func(string) string) int {
	if err := clearConfig(getenv); err != nil {
		fmt.Fprintf(stderr, "logout failed: %v\n", err)
		return 1
	}
	fmt.Fprintln(stderr, "logged out")
	return 0
}

func validateToken(server, token string) error {
	req, err := http.NewRequest(http.MethodGet, server+"/api/v1/me/pastes", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("invalid token")
	}
	// 403 = valid token, missing paste:read — still accept for login.
	if res.StatusCode == http.StatusOK || res.StatusCode == http.StatusForbidden {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(res.Body, 512))
	return fmt.Errorf("server returned %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
}

type fileConfig struct {
	Server string `json:"server"`
	Token  string `json:"token"`
}

func configDir(getenv func(string) string) (string, error) {
	if d := getenv("PBIN_CONFIG_DIR"); d != "" {
		return d, nil
	}
	if xdg := getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "pbin"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "pbin"), nil
}

func configPath(getenv func(string) string) (string, error) {
	dir, err := configDir(getenv)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

func saveConfig(getenv func(string) string, cfg fileConfig) error {
	dir, err := configDir(getenv)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(dir, "config.json")
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

func loadConfig(getenv func(string) string) (fileConfig, error) {
	path, err := configPath(getenv)
	if err != nil {
		return fileConfig{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fileConfig{}, err
	}
	var cfg fileConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fileConfig{}, err
	}
	cfg.Server = strings.TrimRight(cfg.Server, "/")
	return cfg, nil
}

func clearConfig(getenv func(string) string) error {
	path, err := configPath(getenv)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func readLine(r io.Reader) (string, error) {
	br, ok := r.(*bufio.Reader)
	if !ok {
		br = bufio.NewReader(r)
	}
	line, err := br.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func runCreate(args []string, stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string) int {
	name := ""
	expires := ""
	jsonOut := false
	passwordPrompt := false
	passwordStdin := false
	var pathArg string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--name":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "--name requires a value")
				return 2
			}
			i++
			name = args[i]
		case "--expires":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "--expires requires a value")
				return 2
			}
			i++
			expires = args[i]
		case "--json":
			jsonOut = true
		case "--password":
			passwordPrompt = true
		case "--password-stdin":
			passwordStdin = true
		default:
			if strings.HasPrefix(args[i], "-") {
				fmt.Fprintf(stderr, "unknown flag: %s\n", args[i])
				return 2
			}
			if pathArg != "" {
				fmt.Fprintln(stderr, "create accepts at most one path")
				return 2
			}
			pathArg = args[i]
		}
	}
	if passwordPrompt && passwordStdin {
		fmt.Fprintln(stderr, "use only one of --password or --password-stdin")
		return 2
	}

	cfg, err := loadConfig(getenv)
	if err != nil || cfg.Server == "" || cfg.Token == "" {
		fmt.Fprintln(stderr, "not logged in; run: pbin auth login --server <url>")
		return 1
	}

	if pathArg != "" {
		info, err := os.Stat(pathArg)
		if err != nil {
			fmt.Fprintf(stderr, "read path: %v\n", err)
			return 1
		}
		if info.IsDir() {
			password, code := readCreatePassword(passwordPrompt, passwordStdin, stdin, stderr)
			if code != 0 {
				return code
			}
			return createFromDirectory(cfg, pathArg, name, expires, password, jsonOut, stdin, stdout, stderr)
		}
		content, err := os.ReadFile(pathArg)
		if err != nil {
			fmt.Fprintf(stderr, "read file: %v\n", err)
			return 1
		}
		filename := name
		if filename == "" {
			filename = filepath.Base(pathArg)
		}
		password, code := readCreatePassword(passwordPrompt, passwordStdin, stdin, stderr)
		if code != 0 {
			return code
		}
		return createSingleFile(cfg, filename, content, expires, password, jsonOut, stdout, stderr)
	}

	if passwordStdin {
		fmt.Fprintln(stderr, "--password-stdin requires a file path (stdin is the password)")
		return 2
	}
	content, err := io.ReadAll(stdin)
	if err != nil {
		fmt.Fprintf(stderr, "read stdin: %v\n", err)
		return 1
	}
	filename := name
	if filename == "" {
		filename = "paste.txt"
	}
	password, code := readCreatePassword(passwordPrompt, false, stdin, stderr)
	if code != 0 {
		return code
	}
	return createSingleFile(cfg, filename, content, expires, password, jsonOut, stdout, stderr)
}

func readCreatePassword(prompt, fromStdin bool, stdin io.Reader, stderr io.Writer) (string, int) {
	if fromStdin {
		line, err := readLine(stdin)
		if err != nil {
			fmt.Fprintf(stderr, "read password: %v\n", err)
			return "", 1
		}
		password := strings.TrimSpace(line)
		if password == "" {
			fmt.Fprintln(stderr, "password is required")
			return "", 1
		}
		return password, 0
	}
	if prompt {
		fmt.Fprint(stderr, "Password: ")
		line, err := readLine(stdin)
		if err != nil {
			fmt.Fprintf(stderr, "\nread password: %v\n", err)
			return "", 1
		}
		password := strings.TrimSpace(line)
		fmt.Fprintln(stderr)
		if password == "" {
			fmt.Fprintln(stderr, "password is required")
			return "", 1
		}
		return password, 0
	}
	return "", 0
}

type uploadFile struct {
	Path    string
	Content []byte
}

func createSingleFile(cfg fileConfig, filename string, content []byte, expires, password string, jsonOut bool, stdout, stderr io.Writer) int {
	payload := map[string]any{
		"filename": filename,
		"content":  string(content),
	}
	if expires != "" {
		payload["expires_in"] = expires
	}
	if password != "" {
		payload["password"] = password
	}
	body, err := json.Marshal(payload)
	if err != nil {
		fmt.Fprintf(stderr, "marshal: %v\n", err)
		return 1
	}
	req, err := http.NewRequest(http.MethodPost, cfg.Server+"/api/v1/pastes", bytes.NewReader(body))
	if err != nil {
		fmt.Fprintf(stderr, "request: %v\n", err)
		return 1
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	client := &http.Client{Timeout: 30 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(stderr, "create: %v\n", err)
		return 1
	}
	defer res.Body.Close()
	return writeCreateResponse(res, cfg, filename, len(content), jsonOut, stdout, stderr)
}

func createFromDirectory(cfg fileConfig, root, title, expires, password string, jsonOut bool, stdin io.Reader, stdout, stderr io.Writer) int {
	files, err := collectDirectoryFiles(root)
	if err != nil {
		fmt.Fprintf(stderr, "scan directory: %v\n", err)
		return 1
	}
	if len(files) == 0 {
		fmt.Fprintln(stderr, "no files to upload")
		return 1
	}
	totalBytes := 0
	for _, f := range files {
		totalBytes += len(f.Content)
	}
	expiryLabel := expires
	if expiryLabel == "" {
		expiryLabel = "90d"
	}
	if warnings := sensitivePathWarnings(files); len(warnings) > 0 {
		fmt.Fprintln(stderr, "warning: possible sensitive filenames:")
		for _, w := range warnings {
			fmt.Fprintf(stderr, "  %s\n", w)
		}
	}
	fmt.Fprintf(stderr, "Upload summary:\n")
	fmt.Fprintf(stderr, "  %d files\n", len(files))
	fmt.Fprintf(stderr, "  %d bytes\n", totalBytes)
	fmt.Fprintf(stderr, "  expires: %s\n", expiryLabel)
	fmt.Fprintf(stderr, "  visibility: unlisted\n")
	fmt.Fprint(stderr, "Upload? [y/N] ")
	answer, err := readLine(stdin)
	if err != nil && err != io.EOF {
		fmt.Fprintf(stderr, "\nread confirmation: %v\n", err)
		return 1
	}
	fmt.Fprintln(stderr)
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer != "y" && answer != "yes" {
		fmt.Fprintln(stderr, "aborted")
		return 1
	}
	return uploadBundle(cfg, title, expires, password, files, jsonOut, stdout, stderr)
}

func sensitivePathWarnings(files []uploadFile) []string {
	var out []string
	for _, f := range files {
		base := strings.ToLower(path.Base(f.Path))
		if isSensitiveFilename(base) {
			out = append(out, f.Path)
		}
	}
	return out
}

func isSensitiveFilename(base string) bool {
	switch base {
	case ".env", ".env.local", ".env.production", ".env.development":
		return true
	case "id_rsa", "id_dsa", "id_ecdsa", "id_ed25519":
		return true
	case "credentials", "credentials.json", "service-account.json":
		return true
	}
	if strings.HasPrefix(base, ".env.") {
		return true
	}
	if strings.HasSuffix(base, ".pem") || strings.HasSuffix(base, ".key") || strings.HasSuffix(base, ".p12") || strings.HasSuffix(base, ".pfx") {
		return true
	}
	if strings.Contains(base, "id_rsa") || strings.Contains(base, "id_ed25519") {
		return true
	}
	return false
}

func collectDirectoryFiles(root string) ([]uploadFile, error) {
	ignorer, err := loadDirectoryIgnores(root)
	if err != nil {
		return nil, err
	}
	var files []uploadFile
	err = filepath.WalkDir(root, func(fullPath string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, fullPath)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() {
			base := filepath.Base(fullPath)
			if base == ".git" || ignorer.Match(rel, true) {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if ignorer.Match(rel, false) {
			return nil
		}
		content, err := os.ReadFile(fullPath)
		if err != nil {
			return err
		}
		if !utf8.Valid(content) {
			return nil
		}
		files = append(files, uploadFile{Path: path.Clean(rel), Content: content})
		return nil
	})
	return files, err
}

type pathIgnorer struct {
	patterns []ignorePattern
}

type ignorePattern struct {
	raw       string
	dirOnly   bool
	negated   bool
	matchPath string // pattern used with path.Match against full relative path or base
}

func loadDirectoryIgnores(root string) (*pathIgnorer, error) {
	var patterns []ignorePattern
	for _, name := range []string{".gitignore", ".pasteignore"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		patterns = append(patterns, parseIgnorePatterns(string(data))...)
	}
	return &pathIgnorer{patterns: patterns}, nil
}

func parseIgnorePatterns(content string) []ignorePattern {
	var out []ignorePattern
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		negated := false
		if strings.HasPrefix(line, "!") {
			negated = true
			line = line[1:]
		}
		dirOnly := strings.HasSuffix(line, "/")
		if dirOnly {
			line = strings.TrimSuffix(line, "/")
		}
		line = strings.TrimPrefix(line, "/")
		if line == "" {
			continue
		}
		out = append(out, ignorePattern{
			raw:       line,
			dirOnly:   dirOnly,
			negated:   negated,
			matchPath: line,
		})
	}
	return out
}

func (i *pathIgnorer) Match(rel string, isDir bool) bool {
	if rel == ".git" || strings.HasPrefix(rel, ".git/") {
		return true
	}
	ignored := false
	for _, p := range i.patterns {
		if matchesIgnore(p, rel, isDir) {
			ignored = !p.negated
		}
	}
	return ignored
}

func matchesIgnore(p ignorePattern, rel string, isDir bool) bool {
	pat := p.matchPath
	if p.dirOnly {
		return rel == pat || hasPathPrefix(rel, pat)
	}
	if matched, _ := path.Match(pat, rel); matched {
		return true
	}
	if matched, _ := path.Match(pat, path.Base(rel)); matched {
		return true
	}
	if strings.Contains(pat, "/") {
		return false
	}
	for _, seg := range strings.Split(rel, "/") {
		if matched, _ := path.Match(pat, seg); matched {
			return true
		}
	}
	_ = isDir
	return false
}

func hasPathPrefix(rel, prefix string) bool {
	return rel == prefix || strings.HasPrefix(rel, prefix+"/")
}

func uploadBundle(cfg fileConfig, title, expires, password string, files []uploadFile, jsonOut bool, stdout, stderr io.Writer) int {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	meta := map[string]any{}
	if title != "" {
		meta["title"] = title
	}
	if expires != "" {
		meta["expires_in"] = expires
	}
	if password != "" {
		meta["password"] = password
	}
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		fmt.Fprintf(stderr, "marshal metadata: %v\n", err)
		return 1
	}
	if err := w.WriteField("metadata", string(metaJSON)); err != nil {
		fmt.Fprintf(stderr, "write metadata: %v\n", err)
		return 1
	}

	manifest := make([]map[string]string, 0, len(files))
	for i, f := range files {
		part := fmt.Sprintf("file_%d", i)
		manifest = append(manifest, map[string]string{"part": part, "path": f.Path})
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		fmt.Fprintf(stderr, "marshal manifest: %v\n", err)
		return 1
	}
	if err := w.WriteField("manifest", string(manifestJSON)); err != nil {
		fmt.Fprintf(stderr, "write manifest: %v\n", err)
		return 1
	}

	totalBytes := 0
	for i, f := range files {
		part := fmt.Sprintf("file_%d", i)
		pw, err := w.CreateFormFile(part, f.Path)
		if err != nil {
			fmt.Fprintf(stderr, "form file: %v\n", err)
			return 1
		}
		if _, err := pw.Write(f.Content); err != nil {
			fmt.Fprintf(stderr, "write file part: %v\n", err)
			return 1
		}
		totalBytes += len(f.Content)
	}
	if err := w.Close(); err != nil {
		fmt.Fprintf(stderr, "close multipart: %v\n", err)
		return 1
	}

	req, err := http.NewRequest(http.MethodPost, cfg.Server+"/api/v1/pastes/bundle", &buf)
	if err != nil {
		fmt.Fprintf(stderr, "request: %v\n", err)
		return 1
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	client := &http.Client{Timeout: 120 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(stderr, "create: %v\n", err)
		return 1
	}
	defer res.Body.Close()
	return writeCreateResponse(res, cfg, fmt.Sprintf("%d files", len(files)), totalBytes, jsonOut, stdout, stderr)
}

func writeCreateResponse(res *http.Response, cfg fileConfig, label string, bytes int, jsonOut bool, stdout, stderr io.Writer) int {
	respBody, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusCreated && res.StatusCode != http.StatusOK {
		fmt.Fprintf(stderr, "create failed (%d): %s\n", res.StatusCode, strings.TrimSpace(string(respBody)))
		return 1
	}
	var created struct {
		ID        string `json:"id"`
		URL       string `json:"url"`
		ExpiresAt string `json:"expires_at"`
		Files     int    `json:"files"`
		Bytes     int    `json:"bytes"`
	}
	if err := json.Unmarshal(respBody, &created); err != nil {
		fmt.Fprintf(stderr, "parse response: %v\n", err)
		return 1
	}
	if created.ID == "" {
		fmt.Fprintln(stderr, "create response missing id")
		return 1
	}
	// Prefer CLI-configured server host so local test/dev BaseURL mismatches still yield a usable URL.
	pasteURL := cfg.Server + "/p/" + created.ID
	if jsonOut {
		out, err := json.Marshal(map[string]any{
			"id":         created.ID,
			"url":        pasteURL,
			"expires_at": created.ExpiresAt,
			"files":      created.Files,
			"bytes":      created.Bytes,
		})
		if err != nil {
			fmt.Fprintf(stderr, "marshal json: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, string(out))
		return 0
	}
	fmt.Fprintln(stdout, pasteURL)
	fmt.Fprintf(stderr, "created %s (%d bytes)\n", label, bytes)
	return 0
}

func runDelete(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		fmt.Fprintln(stderr, "usage: pbin delete <id>")
		return 2
	}
	id := strings.TrimSpace(args[0])
	if id == "" {
		fmt.Fprintln(stderr, "usage: pbin delete <id>")
		return 2
	}
	// Allow full URL as convenience.
	if idx := strings.LastIndex(id, "/p/"); idx >= 0 {
		id = strings.TrimSuffix(id[idx+len("/p/"):], "/")
		id = strings.Split(id, "?")[0]
	}

	cfg, err := loadConfig(getenv)
	if err != nil || cfg.Server == "" || cfg.Token == "" {
		fmt.Fprintln(stderr, "not logged in; run: pbin auth login --server <url>")
		return 1
	}

	req, err := http.NewRequest(http.MethodDelete, cfg.Server+"/api/v1/pastes/"+id, nil)
	if err != nil {
		fmt.Fprintf(stderr, "request: %v\n", err)
		return 1
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	client := &http.Client{Timeout: 15 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(stderr, "delete: %v\n", err)
		return 1
	}
	defer res.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(res.Body, 512))
	if res.StatusCode != http.StatusNoContent && res.StatusCode != http.StatusOK {
		fmt.Fprintf(stderr, "delete failed (%d): %s\n", res.StatusCode, strings.TrimSpace(string(respBody)))
		return 1
	}
	fmt.Fprintf(stderr, "deleted %s\n", id)
	return 0
}
