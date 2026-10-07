package app_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/harshalranjhani/paste/internal/apptest"
)

func TestLoginProvidesAccessiblePageNavigation(t *testing.T) {
	h := apptest.Start(t)
	res, err := h.GET("/login")
	if err != nil {
		t.Fatal(err)
	}
	body := readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d", res.StatusCode)
	}
	for _, landmark := range []string{`lang="en"`, `name="viewport"`, `href="#main-content"`, `aria-label="Main navigation"`, `id="main-content"`} {
		if !strings.Contains(body, landmark) {
			t.Fatalf("login is missing accessible navigation %s", landmark)
		}
	}
}

func TestPublicPagesOfferAThemeToggle(t *testing.T) {
	h := apptest.Start(t)
	for _, path := range []string{"/", "/login", "/setup"} {
		res, err := h.GET(path)
		if err != nil {
			t.Fatal(err)
		}
		body := readBody(t, res)
		if !strings.Contains(body, `data-theme="dark"`) || !strings.Contains(body, `aria-label="Switch to light mode"`) {
			t.Fatalf("%s must start in dark mode and offer an accessible theme toggle", path)
		}
	}
}

func TestHighlightingStylesCoverBothThemes(t *testing.T) {
	h := apptest.Start(t)
	res, err := h.GET("/assets/highlight.css")
	if err != nil {
		t.Fatal(err)
	}
	css := readBody(t, res)
	if res.StatusCode != http.StatusOK || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/css") {
		t.Fatal("syntax highlighting must have a locally served stylesheet")
	}
	for _, theme := range []string{"light", "dark"} {
		for _, token := range []string{"k", "nt", "na", "s", "o", "c"} {
			if !strings.Contains(css, `[data-theme="`+theme+`"] .chroma .`+token+` {`) {
				t.Fatalf("%s highlighting is missing colors for %s tokens", theme, token)
			}
		}
	}
}

func TestRecipientCanExpandTheFileViewer(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	jar := mustLogin(t, h, "admin", "correct-horse-battery-staple")
	paste := mustCreatePasteJSON(t, h, jar, map[string]any{"filename": "hello.go", "content": "package main\n"})
	res, err := h.GET("/p/" + paste.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readBody(t, res), `aria-label="Enter fullscreen"`) {
		t.Fatal("the recipient must be able to expand the viewer into fullscreen")
	}
}

func TestRecipientCanChooseSyntaxForAnAmbiguousFilename(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	jar := mustLogin(t, h, "admin", "correct-horse-battery-staple")
	paste := mustCreatePasteJSON(t, h, jar, map[string]any{
		"filename": "notes.txt", "content": "def greet(name):\n    return 'Hello ' + name\n",
	})
	for _, suffix := range []string{"?language=python", "?language=python&partial=1"} {
		res, err := h.GET("/p/" + paste.ID + suffix)
		if err != nil {
			t.Fatal(err)
		}
		body := readBody(t, res)
		if res.StatusCode != http.StatusOK || !strings.Contains(body, `<span class="k">def</span>`) {
			t.Fatal("choosing Python must highlight an ambiguous .txt file in full and partial views")
		}
		if res.Header.Get("X-Syntax-Language") != "Python" {
			t.Fatal("language aliases must resolve to the selected syntax name during browser navigation")
		}
		if !strings.Contains(suffix, "partial") && (!strings.Contains(body, `aria-label="Syntax language"`) || !strings.Contains(body, `value="Python" selected`)) {
			t.Fatal("the viewer must show and retain the selected syntax language")
		}
	}
}

func TestMyPastesLetsOwnerOpenAPaste(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	jar := mustLogin(t, h, "admin", "correct-horse-battery-staple")
	paste := mustCreatePasteJSON(t, h, jar, map[string]any{
		"title": "Deployment notes", "filename": "notes.txt", "content": "Ship on Friday.",
	})
	res := getJar(t, h, jar, "/me/pastes")
	body := readBody(t, res)
	if !strings.Contains(body, `href="/p/`+paste.ID+`"`) {
		t.Fatal("My Pastes does not let the owner open their paste")
	}
}

func TestMyPastesIdentifiesUntitledPastesByFilename(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	jar := mustLogin(t, h, "admin", "correct-horse-battery-staple")
	mustCreatePasteJSON(t, h, jar, map[string]any{"filename": "deployment.txt", "content": "Ship on Friday."})
	res := getJar(t, h, jar, "/me/pastes")
	if !strings.Contains(readBody(t, res), "deployment.txt") {
		t.Fatal("an untitled paste must be recognizable by its filename")
	}
}

func TestRecipientCanBrowseNestedFilesWithoutJavaScript(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	jar := mustLogin(t, h, "admin", "correct-horse-battery-staple")
	res := postBundleJarCSRF(t, h, jar, bundleOpts{Files: []bundleFile{
		{Path: "README.txt", Content: "Start here."},
		{Path: "src/lib/notes.txt", Content: "Nested file contents."},
	}})
	var created struct{ ID string }
	if err := json.Unmarshal([]byte(readBody(t, res)), &created); err != nil {
		t.Fatal(err)
	}
	view, err := h.GET("/p/" + created.ID)
	if err != nil {
		t.Fatal(err)
	}
	body := readBody(t, view)
	if !strings.Contains(body, `role="group"`) || !strings.Contains(body, "<summary") {
		t.Fatal("nested paths must render as expandable folders")
	}
	if !strings.Contains(body, "Start here.") {
		t.Fatal("the initial file must be readable without JavaScript")
	}
	link := regexp.MustCompile(`href="(/p/[^"?]+\?file_id=[0-9]+)"[^>]*data-path="src/lib/notes.txt"`).FindStringSubmatch(body)
	if len(link) != 2 {
		t.Fatal("nested file must have a working navigation link")
	}
	selected, err := h.GET(link[1])
	if err != nil {
		t.Fatal(err)
	}
	selectedBody := readBody(t, selected)
	if selected.StatusCode != http.StatusOK || !strings.Contains(selectedBody, "Nested file contents.") || strings.Contains(selectedBody, "Start here.") {
		t.Fatal("following a nested file link must show only that file's contents")
	}
}

func TestWebPasteKeepsItsTitle(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	jar := mustLogin(t, h, "admin", "correct-horse-battery-staple")
	res := postFormJarCSRF(t, h, jar, "/new", url.Values{
		"title": {"Release checklist"}, "path": {"checklist.txt"}, "content": {"Run tests."},
	})
	_ = readBody(t, res)
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("create status = %d", res.StatusCode)
	}
	view, err := h.GET(res.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readBody(t, view), "Release checklist") {
		t.Fatal("the paste viewer must display the title entered in the web form")
	}
}

func TestAdminCanCreateAnInviteFromBrowserForm(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	jar := mustLogin(t, h, "admin", "correct-horse-battery-staple")
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	pageReq, _ := http.NewRequest(http.MethodGet, h.BaseURL+"/admin/invites", nil)
	pageReq.Header.Set("Accept", "text/html")
	page, err := client.Do(pageReq)
	if err != nil {
		t.Fatal(err)
	}
	body := readBody(t, page)
	if !strings.Contains(body, `action="/admin/invites"`) {
		t.Fatal("the browser invites page must include an invite creation form")
	}
	form := url.Values{"email": {"teammate@example.com"}}
	u, _ := url.Parse(h.BaseURL)
	for _, cookie := range jar.Cookies(u) {
		if cookie.Name == "csrf" {
			form.Set("csrf", cookie.Value)
		}
	}
	req, _ := http.NewRequest(http.MethodPost, h.BaseURL+"/admin/invites", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "text/html")
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	createdBody := readBody(t, res)
	if res.StatusCode != http.StatusCreated || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/html") || !strings.Contains(createdBody, "/invite/") {
		t.Fatal("creating an invite in the browser must show a shareable link in a usable page")
	}
	list, err := client.Do(pageReq)
	if err != nil {
		t.Fatal(err)
	}
	if listBody := readBody(t, list); !strings.Contains(listBody, "teammate@example.com") || !strings.Contains(listBody, "Revoke") {
		t.Fatal("created invites must be visible and revocable from the browser")
	}
}

func TestInvalidBrowserLoginKeepsTheSignInForm(t *testing.T) {
	h := apptest.Start(t)
	req, _ := http.NewRequest(http.MethodPost, h.BaseURL+"/login", strings.NewReader("username=missing&password=wrong"))
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := h.Client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body := readBody(t, res)
	if res.StatusCode != http.StatusUnauthorized || !strings.Contains(body, `role="alert"`) || !strings.Contains(body, `action="/login"`) {
		t.Fatal("invalid browser credentials must show an error alongside a usable sign-in form")
	}
}

func TestInvalidBrowserPasteOffersAPathBackToTheEditor(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	jar := mustLogin(t, h, "admin", "correct-horse-battery-staple")
	form := url.Values{"path": {"../escape.txt"}, "content": {"Keep my work."}, "csrf": {cookieNamed(jar, h.BaseURL, "csrf").Value}}
	req, _ := http.NewRequest(http.MethodPost, h.BaseURL+"/new", strings.NewReader(form.Encode()))
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Jar: jar}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body := readBody(t, res)
	if res.StatusCode != http.StatusBadRequest || !strings.Contains(body, `role="alert"`) || !strings.Contains(body, `href="/new"`) {
		t.Fatal("an invalid browser paste must explain the error and let the user return to the editor")
	}
}
