package app_test

import (
	"html"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/harshalranjhani/paste/internal/apptest"
)

func TestHTMLPreviewIsSandboxedAndUnknownFormatsKeepCodeView(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	owner := mustLogin(t, h, "admin", "correct-horse-battery-staple")
	const source = `<h1>HTML document</h1><style>h1 { color: red; }</style><script>parent.hacked = true</script><img src="https://example.com/tracker"><form action="/logout" method="post"></form>`
	id := createdPasteID(t, postJSONJarCSRF(t, h, owner, "/api/v1/pastes", map[string]any{"filename": "index.HTML", "content": source}))
	res := getJar(t, h, owner, "/p/"+id+"?view=preview")
	doc := previewDocument(t, readBody(t, res))
	if !strings.Contains(doc, source) || !strings.Contains(doc, "default-src 'none'") || !strings.Contains(doc, "form-action 'none'") || strings.Contains(doc, "allow-scripts") || strings.Contains(doc, "allow-same-origin") {
		t.Fatal("HTML must render under restrictive CSP and sandbox")
	}
	if !strings.Contains(res.Header.Get("Content-Security-Policy"), "frame-src 'self'") {
		t.Fatal("viewer CSP must allow its sandboxed frame")
	}
	plainID := createdPasteID(t, postJSONJarCSRF(t, h, owner, "/api/v1/pastes", map[string]any{"filename": "notes.txt", "content": "plain text"}))
	plain := getJar(t, h, owner, "/p/"+plainID+"?view=preview&partial=1")
	body := readBody(t, plain)
	if plain.StatusCode != http.StatusOK || plain.Header.Get("X-Viewer-Mode") != "code" || strings.Contains(body, "<iframe") || !strings.Contains(body, "plain text") {
		t.Fatal("unsupported previews must fall back to readable code")
	}
}

func TestMarkdownPreviewRendersDocumentAndKeepsSourceAvailable(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	owner := mustLogin(t, h, "admin", "correct-horse-battery-staple")
	const source = "# Project notes\n\n**Ready** for sharing.\n\n| File | State |\n| --- | --- |\n| app.go | done |\n\n- [x] Ship it\n\n<script>parent.hacked = true</script>\n\n[bad](javascript:alert(1))\n"
	id := createdPasteID(t, postJSONJarCSRF(t, h, owner, "/api/v1/pastes", map[string]any{"filename": "README.md", "content": source}))
	for _, suffix := range []string{"?view=preview", "?view=preview&partial=1"} {
		res := getJar(t, h, owner, "/p/"+id+suffix)
		body := readBody(t, res)
		doc := previewDocument(t, body)
		for _, rendered := range []string{"<h1>Project notes</h1>", "<strong>Ready</strong>", "<table>", `type="checkbox"`} {
			if !strings.Contains(doc, rendered) {
				t.Fatalf("preview missing %q", rendered)
			}
		}
		if strings.Contains(doc, "parent.hacked") || strings.Contains(doc, "javascript:alert") {
			t.Fatal("Markdown preview must omit unsafe markup and links")
		}
		if res.Header.Get("X-Viewer-Mode") != "preview" || res.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("preview must identify mode and avoid caching")
		}
	}
	code := getJar(t, h, owner, "/p/"+id)
	if body := readBody(t, code); !strings.Contains(body, `id="view-preview"`) || strings.Contains(body, `<iframe`) || !strings.Contains(body, "chroma") {
		t.Fatal("source mode must remain default with preview control")
	}
	raw := getJar(t, h, owner, "/p/"+id+"/raw")
	if readBody(t, raw) != source {
		t.Fatal("raw must preserve exact Markdown source")
	}
}

func previewDocument(t *testing.T, body string) string {
	t.Helper()
	if !strings.Contains(body, `sandbox=""`) || !strings.Contains(body, `referrerpolicy="no-referrer"`) {
		t.Fatal("preview must use an isolated sandbox")
	}
	match := regexp.MustCompile(`<iframe[^>]+srcdoc="([^"]*)"`).FindStringSubmatch(body)
	if len(match) != 2 {
		t.Fatal("missing preview document")
	}
	return html.UnescapeString(match[1])
}
