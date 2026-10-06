package app_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/harshalranjhani/paste/internal/apptest"
)

func TestAdminCanLocatePasteByExactIDSeeSafeMetadataAndDelete(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	adminJar := mustLogin(t, h, "admin", "correct-horse-battery-staple")
	aliceJar := mustRedeemInvite(t, h, adminJar, "alice", "alice-password-ok")

	const secretContent = "top-secret-body-should-not-leak"
	paste := mustCreatePasteJSON(t, h, aliceJar, map[string]any{
		"title":    "User Paste",
		"filename": "secret.go",
		"content":  secretContent,
		"password": "gate",
	})

	form := getJar(t, h, adminJar, "/admin/pastes")
	formBody := readBody(t, form)
	if form.StatusCode != http.StatusOK {
		t.Fatalf("GET /admin/pastes status = %d; body = %q", form.StatusCode, formBody)
	}
	if !strings.Contains(formBody, `name="id"`) && !strings.Contains(formBody, `name="public_id"`) {
		t.Fatalf("admin paste lookup form missing id field; body = %q", formBody)
	}

	lookup := getJar(t, h, adminJar, "/admin/pastes?id="+url.QueryEscape(paste.ID))
	lookupBody := readBody(t, lookup)
	if lookup.StatusCode != http.StatusOK {
		t.Fatalf("admin lookup status = %d; body = %q", lookup.StatusCode, lookupBody)
	}
	if !strings.Contains(lookupBody, paste.ID) {
		t.Fatalf("admin lookup missing paste id; body = %q", lookupBody)
	}
	if !strings.Contains(lookupBody, "User Paste") {
		t.Fatalf("admin lookup missing title; body = %q", lookupBody)
	}
	if !strings.Contains(lookupBody, "password") {
		t.Fatalf("admin lookup missing protection; body = %q", lookupBody)
	}
	if !strings.Contains(lookupBody, "alice") {
		t.Fatalf("admin lookup missing owner username; body = %q", lookupBody)
	}
	if strings.Contains(lookupBody, secretContent) || strings.Contains(lookupBody, "secret.go") {
		t.Fatalf("admin lookup leaked file content or filenames; body = %q", lookupBody)
	}

	del := postFormJarCSRF(t, h, adminJar, "/admin/pastes/"+paste.ID+"/delete", url.Values{})
	delBody := readBody(t, del)
	if del.StatusCode != http.StatusSeeOther && del.StatusCode != http.StatusOK && del.StatusCode != http.StatusNoContent {
		t.Fatalf("admin delete status = %d; body = %q", del.StatusCode, delBody)
	}

	view, err := h.GET("/p/" + paste.ID)
	if err != nil {
		t.Fatalf("GET after admin delete: %v", err)
	}
	viewBody := readBody(t, view)
	if view.StatusCode != http.StatusNotFound {
		t.Fatalf("after admin delete status = %d, want 404; body = %q", view.StatusCode, viewBody)
	}
}

func TestAdminModerationHasNoSitewidePasteBrowser(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	adminJar := mustLogin(t, h, "admin", "correct-horse-battery-staple")
	aliceJar := mustRedeemInvite(t, h, adminJar, "alice", "alice-password-ok")

	paste := mustCreatePasteJSON(t, h, aliceJar, map[string]any{
		"title":    "Hidden From Browser",
		"filename": "x.txt",
		"content":  "not listed",
	})

	page := getJar(t, h, adminJar, "/admin/pastes")
	pageBody := readBody(t, page)
	if page.StatusCode != http.StatusOK {
		t.Fatalf("GET /admin/pastes status = %d; body = %q", page.StatusCode, pageBody)
	}
	if strings.Contains(pageBody, paste.ID) || strings.Contains(pageBody, "Hidden From Browser") {
		t.Fatalf("admin pastes page listed paste without exact-ID lookup; body = %q", pageBody)
	}

	for _, path := range []string{"/admin/pastes/all", "/admin/pastes/recent", "/admin/browse", "/pastes"} {
		res := getJar(t, h, adminJar, path)
		body := readBody(t, res)
		if res.StatusCode == http.StatusOK && (strings.Contains(body, paste.ID) || strings.Contains(body, "Hidden From Browser")) {
			t.Fatalf("browse route %s exposed paste; body = %q", path, body)
		}
		if res.StatusCode == http.StatusOK && path != "/admin/pastes" {
			t.Fatalf("unexpected browse route %s returned 200", path)
		}
	}

	userJar := mustRedeemInvite(t, h, adminJar, "bob", "bob-password-ok")
	deny := getJar(t, h, userJar, "/admin/pastes")
	denyBody := readBody(t, deny)
	if deny.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin /admin/pastes status = %d, want 403; body = %q", deny.StatusCode, denyBody)
	}
}

func TestAdminCanDeleteAnyPasteViaAPI(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	adminJar := mustLogin(t, h, "admin", "correct-horse-battery-staple")
	aliceJar := mustRedeemInvite(t, h, adminJar, "alice", "alice-password-ok")

	paste := mustCreatePasteJSON(t, h, aliceJar, map[string]any{
		"filename": "owned.txt",
		"content":  "alice owns this",
	})

	del := deleteJSONJarCSRF(t, h, adminJar, "/api/v1/pastes/"+paste.ID)
	delBody := readBody(t, del)
	if del.StatusCode != http.StatusNoContent && del.StatusCode != http.StatusOK {
		t.Fatalf("admin API delete status = %d; body = %q", del.StatusCode, delBody)
	}

	view, err := h.GET("/p/" + paste.ID)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	viewBody := readBody(t, view)
	if view.StatusCode != http.StatusNotFound {
		t.Fatalf("after admin API delete status = %d, want 404; body = %q", view.StatusCode, viewBody)
	}
}
