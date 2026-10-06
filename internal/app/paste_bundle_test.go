package app_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/harshalranjhani/paste/internal/apptest"
)

func TestAuthenticatedBundleCreatePreservesNestedPaths(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	jar := mustLogin(t, h, "admin", "correct-horse-battery-staple")

	files := []bundleFile{
		{Path: "README.md", Content: "# hello\n"},
		{Path: "src/main.go", Content: "package main\n"},
		{Path: "src/util/helpers.go", Content: "package util\n"},
	}
	createRes := postBundleJarCSRF(t, h, jar, bundleOpts{
		Title: "demo-tree",
		Files: files,
	})
	createBody := readBody(t, createRes)
	if createRes.StatusCode != http.StatusCreated {
		t.Fatalf("POST /api/v1/pastes/bundle status = %d; body = %q", createRes.StatusCode, createBody)
	}

	var created struct {
		ID    string `json:"id"`
		URL   string `json:"url"`
		Files int    `json:"files"`
		Bytes int    `json:"bytes"`
	}
	if err := json.Unmarshal([]byte(createBody), &created); err != nil {
		t.Fatalf("parse create: %v; body = %q", err, createBody)
	}
	if created.ID == "" {
		t.Fatalf("missing id in %q", createBody)
	}
	if !strings.Contains(created.URL, "/p/"+created.ID) {
		t.Fatalf("url %q should contain /p/%s", created.URL, created.ID)
	}
	if created.Files != 3 {
		t.Fatalf("files = %d, want 3", created.Files)
	}
	wantBytes := len(files[0].Content) + len(files[1].Content) + len(files[2].Content)
	if created.Bytes != wantBytes {
		t.Fatalf("bytes = %d, want %d", created.Bytes, wantBytes)
	}

	metaRes, err := h.GET("/api/v1/pastes/" + created.ID + "/meta")
	if err != nil {
		t.Fatalf("GET meta: %v", err)
	}
	metaBody := readBody(t, metaRes)
	if metaRes.StatusCode != http.StatusOK {
		t.Fatalf("meta status = %d; body = %q", metaRes.StatusCode, metaBody)
	}
	var meta struct {
		Files []struct {
			ID        int64  `json:"id"`
			Path      string `json:"path"`
			SizeBytes int    `json:"size_bytes"`
		} `json:"files"`
	}
	if err := json.Unmarshal([]byte(metaBody), &meta); err != nil {
		t.Fatalf("parse meta: %v; body = %q", err, metaBody)
	}
	if len(meta.Files) != 3 {
		t.Fatalf("meta files = %d, want 3; body = %q", len(meta.Files), metaBody)
	}
	byPath := map[string]int{}
	for _, f := range meta.Files {
		if f.ID == 0 {
			t.Fatalf("file missing id: %+v", f)
		}
		byPath[f.Path] = f.SizeBytes
	}
	for _, f := range files {
		got, ok := byPath[f.Path]
		if !ok {
			t.Fatalf("missing path %q in meta; body = %q", f.Path, metaBody)
		}
		if got != len(f.Content) {
			t.Fatalf("path %q size = %d, want %d", f.Path, got, len(f.Content))
		}
	}
}

type bundleFile struct {
	Path    string
	Content string
}

type bundleOpts struct {
	Title      string
	ExpiresIn  string
	Password   string
	Files      []bundleFile
	BadPath    string // if set, override first file path after writing part (unused by helper; for custom)
	Manifest   any    // optional override
	Metadata   any    // optional override
	SkipAuth   bool
	OmitCSRF   bool
	ExtraParts map[string]string
}

func postBundleJarCSRF(t *testing.T, h *apptest.Harness, jar http.CookieJar, opts bundleOpts) *http.Response {
	t.Helper()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	meta := opts.Metadata
	if meta == nil {
		m := map[string]any{}
		if opts.Title != "" {
			m["title"] = opts.Title
		}
		if opts.ExpiresIn != "" {
			m["expires_in"] = opts.ExpiresIn
		}
		if opts.Password != "" {
			m["password"] = opts.Password
		}
		meta = m
	}
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	if err := w.WriteField("metadata", string(metaJSON)); err != nil {
		t.Fatalf("write metadata: %v", err)
	}

	manifest := opts.Manifest
	if manifest == nil {
		entries := make([]map[string]string, 0, len(opts.Files))
		for i, f := range opts.Files {
			part := "file_" + itoa(i)
			entries = append(entries, map[string]string{"part": part, "path": f.Path})
		}
		manifest = entries
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := w.WriteField("manifest", string(manifestJSON)); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	for i, f := range opts.Files {
		partName := "file_" + itoa(i)
		pw, err := w.CreateFormFile(partName, f.Path)
		if err != nil {
			t.Fatalf("CreateFormFile: %v", err)
		}
		if _, err := io.WriteString(pw, f.Content); err != nil {
			t.Fatalf("write file part: %v", err)
		}
	}
	for name, content := range opts.ExtraParts {
		pw, err := w.CreateFormFile(name, name)
		if err != nil {
			t.Fatalf("CreateFormFile extra: %v", err)
		}
		if _, err := io.WriteString(pw, content); err != nil {
			t.Fatalf("write extra part: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, h.BaseURL+"/api/v1/pastes/bundle", &buf)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	if jar != nil && !opts.OmitCSRF {
		if csrf := cookieNamed(jar, h.BaseURL, "csrf"); csrf != nil {
			req.Header.Set("X-CSRF-Token", csrf.Value)
		}
	}
	client := &http.Client{
		Timeout: h.Client.Timeout,
	}
	if jar != nil && !opts.SkipAuth {
		client.Jar = jar
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST bundle: %v", err)
	}
	return res
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(b[pos:])
}

func TestUnauthenticatedBundleCreateIsRejected(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")

	res := postBundleJarCSRF(t, h, nil, bundleOpts{
		SkipAuth: true,
		Files: []bundleFile{
			{Path: "a.txt", Content: "hi"},
		},
	})
	body := readBody(t, res)
	if res.StatusCode == http.StatusOK || res.StatusCode == http.StatusCreated {
		t.Fatalf("unauthenticated bundle succeeded: %d %q", res.StatusCode, body)
	}
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated bundle status = %d, want 401; body = %q", res.StatusCode, body)
	}
}

func TestBundleRejectsMalformedPaths(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	jar := mustLogin(t, h, "admin", "correct-horse-battery-staple")

	cases := []struct {
		name string
		path string
	}{
		{"absolute", "/etc/passwd"},
		{"traversal", "src/../../etc/passwd"},
		{"dotdot", ".."},
		{"windows_drive", `C:\Windows\system32\drivers\etc\hosts`},
		{"unc", `\\server\share\file.txt`},
		{"nul", "a\x00b.txt"},
		{"over_depth", strings.Repeat("a/", 21) + "f.txt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := postBundleJarCSRF(t, h, jar, bundleOpts{
				Files: []bundleFile{{Path: tc.path, Content: "x"}},
			})
			body := readBody(t, res)
			if res.StatusCode == http.StatusOK || res.StatusCode == http.StatusCreated {
				t.Fatalf("path %q was accepted: %d %q", tc.path, res.StatusCode, body)
			}
			if res.StatusCode != http.StatusBadRequest {
				t.Fatalf("path %q status = %d, want 400; body = %q", tc.path, res.StatusCode, body)
			}
		})
	}

	// Duplicate paths after normalization.
	dup := postBundleJarCSRF(t, h, jar, bundleOpts{
		Files: []bundleFile{
			{Path: "src/a.txt", Content: "1"},
			{Path: `src\a.txt`, Content: "2"},
		},
	})
	dupBody := readBody(t, dup)
	if dup.StatusCode == http.StatusOK || dup.StatusCode == http.StatusCreated {
		t.Fatalf("duplicate normalized paths accepted: %d %q", dup.StatusCode, dupBody)
	}
	if dup.StatusCode != http.StatusBadRequest {
		t.Fatalf("duplicate status = %d, want 400; body = %q", dup.StatusCode, dupBody)
	}
}

func TestBundleFailedCreateLeavesNoPartialPaste(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	jar := mustLogin(t, h, "admin", "correct-horse-battery-staple")

	// Valid file + invalid path in same request must fail entirely.
	fail := postBundleJarCSRF(t, h, jar, bundleOpts{
		Title: "should-not-exist",
		Files: []bundleFile{
			{Path: "ok.txt", Content: "good"},
			{Path: "../evil.txt", Content: "bad"},
		},
	})
	failBody := readBody(t, fail)
	if fail.StatusCode == http.StatusOK || fail.StatusCode == http.StatusCreated {
		t.Fatalf("partial-invalid bundle succeeded: %d %q", fail.StatusCode, failBody)
	}
	var failed struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal([]byte(failBody), &failed)
	if failed.ID != "" {
		t.Fatalf("failed create returned paste id %q", failed.ID)
	}

	// Successful create afterward still works (transaction did not leave DB wedged).
	ok := postBundleJarCSRF(t, h, jar, bundleOpts{
		Title: "exists",
		Files: []bundleFile{
			{Path: "ok.txt", Content: "good"},
			{Path: "nested/b.txt", Content: "also"},
		},
	})
	okBody := readBody(t, ok)
	if ok.StatusCode != http.StatusCreated {
		t.Fatalf("follow-up create status = %d; body = %q", ok.StatusCode, okBody)
	}
	var created struct {
		ID    string `json:"id"`
		Files int    `json:"files"`
	}
	if err := json.Unmarshal([]byte(okBody), &created); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if created.Files != 2 {
		t.Fatalf("files = %d, want 2", created.Files)
	}
	metaRes, err := h.GET("/api/v1/pastes/" + created.ID + "/meta")
	if err != nil {
		t.Fatalf("meta: %v", err)
	}
	metaBody := readBody(t, metaRes)
	if strings.Contains(metaBody, "../evil") || strings.Contains(metaBody, "evil.txt") {
		t.Fatalf("successful paste somehow includes failed upload path; body = %q", metaBody)
	}
}

func TestBundleNormalizesWindowsSeparators(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	jar := mustLogin(t, h, "admin", "correct-horse-battery-staple")

	res := postBundleJarCSRF(t, h, jar, bundleOpts{
		Files: []bundleFile{
			{Path: `refs\note.md`, Content: "n"},
		},
	})
	body := readBody(t, res)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d; body = %q", res.StatusCode, body)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		t.Fatalf("parse: %v", err)
	}
	metaRes, err := h.GET("/api/v1/pastes/" + created.ID + "/meta")
	if err != nil {
		t.Fatalf("meta: %v", err)
	}
	metaBody := readBody(t, metaRes)
	if !strings.Contains(metaBody, `"path":"refs/note.md"`) {
		t.Fatalf("expected POSIX-normalized path; body = %q", metaBody)
	}
}

func TestBundleRejectsNonUTF8File(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	jar := mustLogin(t, h, "admin", "correct-horse-battery-staple")

	res := postBundleJarCSRF(t, h, jar, bundleOpts{
		Files: []bundleFile{
			{Path: "bin.dat", Content: "ok\xff\xfe"},
		},
	})
	body := readBody(t, res)
	if res.StatusCode == http.StatusOK || res.StatusCode == http.StatusCreated {
		t.Fatalf("non-UTF-8 bundle succeeded: %d %q", res.StatusCode, body)
	}
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("non-UTF-8 status = %d, want 400; body = %q", res.StatusCode, body)
	}
}

func TestMetaListsFilesWithoutBodiesAndFileEndpointLoadsLazily(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	jar := mustLogin(t, h, "admin", "correct-horse-battery-staple")

	const bodyA = "content-of-a-unique"
	const bodyB = "content-of-b-unique"
	createRes := postBundleJarCSRF(t, h, jar, bundleOpts{
		Files: []bundleFile{
			{Path: "a.txt", Content: bodyA},
			{Path: "dir/b.txt", Content: bodyB},
		},
	})
	createBody := readBody(t, createRes)
	if createRes.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d; body = %q", createRes.StatusCode, createBody)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(createBody), &created); err != nil {
		t.Fatalf("parse: %v", err)
	}

	metaRes, err := h.GET("/api/v1/pastes/" + created.ID + "/meta")
	if err != nil {
		t.Fatalf("meta: %v", err)
	}
	metaBody := readBody(t, metaRes)
	if metaRes.StatusCode != http.StatusOK {
		t.Fatalf("meta status = %d; body = %q", metaRes.StatusCode, metaBody)
	}
	if strings.Contains(metaBody, bodyA) || strings.Contains(metaBody, bodyB) {
		t.Fatalf("meta leaked file bodies; body = %q", metaBody)
	}
	var meta struct {
		Files []struct {
			ID   int64  `json:"id"`
			Path string `json:"path"`
		} `json:"files"`
	}
	if err := json.Unmarshal([]byte(metaBody), &meta); err != nil {
		t.Fatalf("parse meta: %v", err)
	}
	var fileBID int64
	for _, f := range meta.Files {
		if f.Path == "dir/b.txt" {
			fileBID = f.ID
		}
	}
	if fileBID == 0 {
		t.Fatalf("missing dir/b.txt in meta; body = %q", metaBody)
	}

	fileRes, err := h.GET("/api/v1/pastes/" + created.ID + "/files/" + itoa(int(fileBID)))
	if err != nil {
		t.Fatalf("GET file: %v", err)
	}
	fileBody := readBody(t, fileRes)
	if fileRes.StatusCode != http.StatusOK {
		t.Fatalf("file status = %d; body = %q", fileRes.StatusCode, fileBody)
	}
	var filePayload struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(fileBody), &filePayload); err != nil {
		t.Fatalf("parse file: %v; body = %q", err, fileBody)
	}
	if filePayload.Path != "dir/b.txt" || filePayload.Content != bodyB {
		t.Fatalf("file payload = %+v, want path=dir/b.txt content=%q", filePayload, bodyB)
	}

	rawRes, err := h.GET("/api/v1/pastes/" + created.ID + "/files/" + itoa(int(fileBID)) + "/raw")
	if err != nil {
		t.Fatalf("GET raw file: %v", err)
	}
	rawBody := readBody(t, rawRes)
	if rawRes.StatusCode != http.StatusOK {
		t.Fatalf("raw file status = %d; body = %q", rawRes.StatusCode, rawBody)
	}
	if rawBody != bodyB {
		t.Fatalf("raw body = %q, want %q", rawBody, bodyB)
	}
	if !strings.HasPrefix(rawRes.Header.Get("Content-Type"), "text/plain") {
		t.Fatalf("raw Content-Type = %q", rawRes.Header.Get("Content-Type"))
	}
}

func TestMultiFileViewerShowsTreeSingleFileOmitsTree(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	jar := mustLogin(t, h, "admin", "correct-horse-battery-staple")

	multi := postBundleJarCSRF(t, h, jar, bundleOpts{
		Files: []bundleFile{
			{Path: "README.md", Content: "# root\n"},
			{Path: "src/app.go", Content: "package src\n"},
		},
	})
	multiBody := readBody(t, multi)
	if multi.StatusCode != http.StatusCreated {
		t.Fatalf("multi create status = %d; body = %q", multi.StatusCode, multiBody)
	}
	var multiCreated struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(multiBody), &multiCreated); err != nil {
		t.Fatalf("parse: %v", err)
	}

	viewRes, err := h.GET("/p/" + multiCreated.ID)
	if err != nil {
		t.Fatalf("GET multi view: %v", err)
	}
	viewBody := readBody(t, viewRes)
	if viewRes.StatusCode != http.StatusOK {
		t.Fatalf("multi view status = %d; body = %q", viewRes.StatusCode, viewBody)
	}
	if !strings.Contains(viewBody, `id="file-tree"`) {
		t.Fatalf("multi-file viewer missing file tree; body = %q", viewBody)
	}
	if !strings.Contains(viewBody, `role="tree"`) {
		t.Fatalf("multi-file tree missing ARIA role=tree; body = %q", viewBody)
	}
	if !strings.Contains(viewBody, "README.md") || !strings.Contains(viewBody, "src/app.go") {
		t.Fatalf("tree missing filenames; body = %q", viewBody)
	}
	if !strings.Contains(viewBody, "package src") && !strings.Contains(viewBody, "# root") {
		// Initial selected file body may be lazy-loaded; shell should still render pane.
		if !strings.Contains(viewBody, `id="code-pane"`) && !strings.Contains(viewBody, `id="paste-content"`) {
			t.Fatalf("viewer missing code pane; body = %q", viewBody)
		}
	}
	if !strings.Contains(viewBody, "drawer") && !strings.Contains(viewBody, "file-picker") {
		t.Fatalf("mobile drawer/picker missing; body = %q", viewBody)
	}

	single := postJSONJarCSRF(t, h, jar, "/api/v1/pastes", map[string]any{
		"filename": "alone.txt",
		"content":  "just-one",
	})
	singleBody := readBody(t, single)
	var singleCreated struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(singleBody), &singleCreated); err != nil {
		t.Fatalf("parse single: %v", err)
	}
	singleView, err := h.GET("/p/" + singleCreated.ID)
	if err != nil {
		t.Fatalf("GET single: %v", err)
	}
	singleViewBody := readBody(t, singleView)
	if strings.Contains(singleViewBody, `id="file-tree"`) {
		t.Fatalf("single-file viewer must omit tree; body = %q", singleViewBody)
	}
}

func TestZIPDownloadPreservesHierarchyAndIsSafe(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	jar := mustLogin(t, h, "admin", "correct-horse-battery-staple")

	createRes := postBundleJarCSRF(t, h, jar, bundleOpts{
		Files: []bundleFile{
			{Path: "README.md", Content: "# top\n"},
			{Path: "src/main.go", Content: "package main\n"},
			{Path: "docs/guide.txt", Content: "guide\n"},
		},
	})
	createBody := readBody(t, createRes)
	if createRes.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d; body = %q", createRes.StatusCode, createBody)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(createBody), &created); err != nil {
		t.Fatalf("parse: %v", err)
	}

	zipRes, err := h.GET("/api/v1/pastes/" + created.ID + "/archive.zip")
	if err != nil {
		t.Fatalf("GET zip: %v", err)
	}
	zipBytes := readBodyBytes(t, zipRes)
	if zipRes.StatusCode != http.StatusOK {
		t.Fatalf("zip status = %d; body = %q", zipRes.StatusCode, string(zipBytes))
	}
	if !strings.Contains(zipRes.Header.Get("Content-Type"), "zip") {
		t.Fatalf("Content-Type = %q, want zip", zipRes.Header.Get("Content-Type"))
	}
	cd := zipRes.Header.Get("Content-Disposition")
	if !strings.Contains(cd, "attachment") {
		t.Fatalf("Content-Disposition = %q, want attachment", cd)
	}

	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	got := map[string]string{}
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "/") || strings.Contains(f.Name, "..") || strings.Contains(f.Name, `\`) {
			t.Fatalf("unsafe zip entry name %q", f.Name)
		}
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

func TestPasswordGateAppliesToTreeFileRawAndZIP(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	owner := mustLogin(t, h, "admin", "correct-horse-battery-staple")

	const password = "tree-secret"
	createRes := postBundleJarCSRF(t, h, owner, bundleOpts{
		Password: password,
		Files: []bundleFile{
			{Path: "secret/a.txt", Content: "classified-a"},
			{Path: "secret/b.txt", Content: "classified-b"},
		},
	})
	createBody := readBody(t, createRes)
	if createRes.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d; body = %q", createRes.StatusCode, createBody)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(createBody), &created); err != nil {
		t.Fatalf("parse: %v", err)
	}

	anon := newJar(t)

	viewRes := getJar(t, h, anon, "/p/"+created.ID)
	viewBody := readBody(t, viewRes)
	if strings.Contains(viewBody, "secret/a.txt") || strings.Contains(viewBody, "classified") {
		t.Fatalf("locked view leaked tree/content; body = %q", viewBody)
	}
	if !strings.Contains(strings.ToLower(viewBody), "password") {
		t.Fatalf("locked view missing password prompt; body = %q", viewBody)
	}

	metaRes := getJar(t, h, anon, "/api/v1/pastes/"+created.ID+"/meta")
	metaBody := readBody(t, metaRes)
	if strings.Contains(metaBody, "secret/") || strings.Contains(metaBody, "classified") {
		t.Fatalf("locked meta leaked files; body = %q", metaBody)
	}

	fileRes := getJar(t, h, anon, "/api/v1/pastes/"+created.ID+"/files/1")
	fileBody := readBody(t, fileRes)
	if fileRes.StatusCode == http.StatusOK {
		t.Fatalf("locked file endpoint returned 200: %q", fileBody)
	}

	rawRes := getJar(t, h, anon, "/api/v1/pastes/"+created.ID+"/files/1/raw")
	rawBody := readBody(t, rawRes)
	if rawRes.StatusCode == http.StatusOK {
		t.Fatalf("locked raw returned 200: %q", rawBody)
	}

	zipRes := getJar(t, h, anon, "/api/v1/pastes/"+created.ID+"/archive.zip")
	zipBody := readBody(t, zipRes)
	if zipRes.StatusCode == http.StatusOK {
		t.Fatalf("locked zip returned 200")
	}
	if strings.Contains(zipBody, "classified") {
		t.Fatalf("locked zip leaked content")
	}

	unlock := postFormJar(t, h, anon, "/p/"+created.ID+"/unlock", url.Values{
		"password": {password},
	})
	_ = readBody(t, unlock)
	if unlock.StatusCode != http.StatusSeeOther && unlock.StatusCode != http.StatusOK {
		t.Fatalf("unlock status = %d", unlock.StatusCode)
	}

	metaOK := getJar(t, h, anon, "/api/v1/pastes/"+created.ID+"/meta")
	metaOKBody := readBody(t, metaOK)
	if metaOK.StatusCode != http.StatusOK {
		t.Fatalf("unlocked meta status = %d; body = %q", metaOK.StatusCode, metaOKBody)
	}
	if !strings.Contains(metaOKBody, "secret/a.txt") {
		t.Fatalf("unlocked meta missing paths; body = %q", metaOKBody)
	}

	var meta struct {
		Files []struct {
			ID   int64  `json:"id"`
			Path string `json:"path"`
		} `json:"files"`
	}
	if err := json.Unmarshal([]byte(metaOKBody), &meta); err != nil {
		t.Fatalf("parse meta: %v", err)
	}
	var fileID int64
	for _, f := range meta.Files {
		if f.Path == "secret/a.txt" {
			fileID = f.ID
		}
	}
	if fileID == 0 {
		t.Fatal("missing secret/a.txt id")
	}

	fileOK := getJar(t, h, anon, "/api/v1/pastes/"+created.ID+"/files/"+itoa(int(fileID)))
	fileOKBody := readBody(t, fileOK)
	if fileOK.StatusCode != http.StatusOK || !strings.Contains(fileOKBody, "classified-a") {
		t.Fatalf("unlocked file = %d %q", fileOK.StatusCode, fileOKBody)
	}

	zipOK := getJar(t, h, anon, "/api/v1/pastes/"+created.ID+"/archive.zip")
	zipOKBytes := readBodyBytes(t, zipOK)
	if zipOK.StatusCode != http.StatusOK {
		t.Fatalf("unlocked zip status = %d", zipOK.StatusCode)
	}
	zr, err := zip.NewReader(bytes.NewReader(zipOKBytes), int64(len(zipOKBytes)))
	if err != nil {
		t.Fatalf("open unlocked zip: %v", err)
	}
	found := false
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
		if strings.Contains(string(body), "classified-a") {
			found = true
		}
	}
	if !found {
		t.Fatalf("unlocked zip missing content")
	}
}

func TestBundleEnforcesFileCountAndSizeLimits(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	jar := mustLogin(t, h, "admin", "correct-horse-battery-staple")

	tooBig := strings.Repeat("x", (2<<20)+1)
	big := postBundleJarCSRF(t, h, jar, bundleOpts{
		Files: []bundleFile{{Path: "big.txt", Content: tooBig}},
	})
	bigBody := readBody(t, big)
	if big.StatusCode == http.StatusOK || big.StatusCode == http.StatusCreated {
		t.Fatalf("oversized file accepted: %d %q", big.StatusCode, bigBody)
	}
	if big.StatusCode != http.StatusBadRequest {
		t.Fatalf("oversized status = %d, want 400; body = %q", big.StatusCode, bigBody)
	}

	// Over path length
	longPath := strings.Repeat("a", 513)
	long := postBundleJarCSRF(t, h, jar, bundleOpts{
		Files: []bundleFile{{Path: longPath, Content: "x"}},
	})
	longBody := readBody(t, long)
	if long.StatusCode == http.StatusOK || long.StatusCode == http.StatusCreated {
		t.Fatalf("over-long path accepted: %d %q", long.StatusCode, longBody)
	}
}

func TestWebNewSupportsMultiFileAdd(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	jar := mustLogin(t, h, "admin", "correct-horse-battery-staple")

	formPage := getJar(t, h, jar, "/new")
	formBody := readBody(t, formPage)
	if formPage.StatusCode != http.StatusOK {
		t.Fatalf("GET /new status = %d; body = %q", formPage.StatusCode, formBody)
	}
	if !strings.Contains(formBody, `name="paths"`) && !strings.Contains(formBody, `name="path"`) && !strings.Contains(formBody, "add-file") {
		t.Fatalf("/new missing multi-file controls; body = %q", formBody)
	}

	create := postFormJarCSRF(t, h, jar, "/new", url.Values{
		"path":    {"README.md", "src/main.go"},
		"content": {"# hi\n", "package main\n"},
	})
	createBody := readBody(t, create)
	if create.StatusCode != http.StatusSeeOther && create.StatusCode != http.StatusOK {
		t.Fatalf("POST /new multi status = %d; body = %q", create.StatusCode, createBody)
	}
	loc := create.Header.Get("Location")
	if loc == "" || !strings.Contains(loc, "/p/") {
		t.Fatalf("redirect Location = %q", loc)
	}

	view, err := h.GET(loc)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	viewBody := readBody(t, view)
	if view.StatusCode != http.StatusOK {
		t.Fatalf("view status = %d; body = %q", view.StatusCode, viewBody)
	}
	if !strings.Contains(viewBody, `id="file-tree"`) {
		t.Fatalf("web multi-file paste missing tree; body = %q", viewBody)
	}
	if !strings.Contains(viewBody, "README.md") || !strings.Contains(viewBody, "src/main.go") {
		t.Fatalf("tree missing uploaded paths; body = %q", viewBody)
	}
}

func readBodyBytes(t *testing.T, res *http.Response) []byte {
	t.Helper()
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return b
}

func TestBundleRejectsTooManyFiles(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	jar := mustLogin(t, h, "admin", "correct-horse-battery-staple")

	files := make([]bundleFile, 501)
	for i := range files {
		files[i] = bundleFile{Path: "f/" + itoa(i) + ".txt", Content: "x"}
	}
	res := postBundleJarCSRF(t, h, jar, bundleOpts{Files: files})
	body := readBody(t, res)
	if res.StatusCode == http.StatusOK || res.StatusCode == http.StatusCreated {
		t.Fatalf("501 files accepted: %d %q", res.StatusCode, body)
	}
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("too many files status = %d, want 400; body = %q", res.StatusCode, body)
	}
}
