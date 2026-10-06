package app_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/harshalranjhani/paste/internal/apptest"
)

func TestMyPastesListsOnlyCurrentUserPastesWithColumns(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	adminJar := mustLogin(t, h, "admin", "correct-horse-battery-staple")

	aliceJar := mustRedeemInvite(t, h, adminJar, "alice", "alice-password-ok")
	bobJar := mustRedeemInvite(t, h, adminJar, "bob", "bob-password-ok")

	alicePaste := mustCreatePasteJSON(t, h, aliceJar, map[string]any{
		"title":    "Alice Notes",
		"filename": "notes.txt",
		"content":  "alice content",
		"password": "secret",
	})
	bobPaste := mustCreatePasteJSON(t, h, bobJar, map[string]any{
		"title":    "Bob Secrets",
		"filename": "secret.txt",
		"content":  "bob content",
	})

	page := getJar(t, h, aliceJar, "/me/pastes")
	pageBody := readBody(t, page)
	if page.StatusCode != http.StatusOK {
		t.Fatalf("GET /me/pastes status = %d; body = %q", page.StatusCode, pageBody)
	}
	for _, col := range []string{"Title", "Created", "Expires", "Files", "Protection", "Delete"} {
		if !strings.Contains(pageBody, col) {
			t.Fatalf("/me/pastes missing column %q; body = %q", col, pageBody)
		}
	}
	if !strings.Contains(pageBody, "Alice Notes") {
		t.Fatalf("/me/pastes missing alice title; body = %q", pageBody)
	}
	if !strings.Contains(pageBody, alicePaste.ID) {
		t.Fatalf("/me/pastes missing alice paste id; body = %q", pageBody)
	}
	if !strings.Contains(pageBody, "password") {
		t.Fatalf("/me/pastes missing protection indicator; body = %q", pageBody)
	}
	if strings.Contains(pageBody, bobPaste.ID) || strings.Contains(pageBody, "Bob Secrets") {
		t.Fatalf("/me/pastes leaked bob's paste; body = %q", pageBody)
	}

	api := getJar(t, h, aliceJar, "/api/v1/me/pastes")
	apiBody := readBody(t, api)
	if api.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/v1/me/pastes status = %d; body = %q", api.StatusCode, apiBody)
	}
	var listed []struct {
		ID         string `json:"id"`
		Title      string `json:"title"`
		CreatedAt  string `json:"created_at"`
		ExpiresAt  string `json:"expires_at"`
		Files      int    `json:"files"`
		Protection string `json:"protection"`
	}
	if err := json.Unmarshal([]byte(apiBody), &listed); err != nil {
		t.Fatalf("parse /api/v1/me/pastes: %v; body = %q", err, apiBody)
	}
	if len(listed) != 1 {
		t.Fatalf("api listed %d pastes, want 1; body = %q", len(listed), apiBody)
	}
	if listed[0].ID != alicePaste.ID {
		t.Fatalf("listed id = %q, want %q", listed[0].ID, alicePaste.ID)
	}
	if listed[0].Title != "Alice Notes" {
		t.Fatalf("listed title = %q, want Alice Notes", listed[0].Title)
	}
	if listed[0].CreatedAt == "" || listed[0].ExpiresAt == "" {
		t.Fatalf("missing created/expires; %#v", listed[0])
	}
	if listed[0].Files != 1 {
		t.Fatalf("files = %d, want 1", listed[0].Files)
	}
	if listed[0].Protection != "password" {
		t.Fatalf("protection = %q, want password", listed[0].Protection)
	}
}

func TestOwnerCanDeleteFromMyPastes(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	adminJar := mustLogin(t, h, "admin", "correct-horse-battery-staple")
	aliceJar := mustRedeemInvite(t, h, adminJar, "alice", "alice-password-ok")

	paste := mustCreatePasteJSON(t, h, aliceJar, map[string]any{
		"title":    "To Delete",
		"filename": "gone.txt",
		"content":  "temporary",
	})

	del := postFormJarCSRF(t, h, aliceJar, "/me/pastes/"+paste.ID+"/delete", url.Values{})
	delBody := readBody(t, del)
	if del.StatusCode != http.StatusSeeOther && del.StatusCode != http.StatusOK && del.StatusCode != http.StatusNoContent {
		t.Fatalf("POST delete status = %d; body = %q", del.StatusCode, delBody)
	}

	view, err := h.GET("/p/" + paste.ID)
	if err != nil {
		t.Fatalf("GET after delete: %v", err)
	}
	viewBody := readBody(t, view)
	if view.StatusCode != http.StatusNotFound {
		t.Fatalf("after delete status = %d, want 404; body = %q", view.StatusCode, viewBody)
	}

	list := getJar(t, h, aliceJar, "/me/pastes")
	listBody := readBody(t, list)
	if strings.Contains(listBody, paste.ID) || strings.Contains(listBody, "To Delete") {
		t.Fatalf("deleted paste still on My Pastes; body = %q", listBody)
	}
}

func TestUserCannotListOrDeleteOthersPastes(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	adminJar := mustLogin(t, h, "admin", "correct-horse-battery-staple")
	aliceJar := mustRedeemInvite(t, h, adminJar, "alice", "alice-password-ok")
	bobJar := mustRedeemInvite(t, h, adminJar, "bob", "bob-password-ok")

	alicePaste := mustCreatePasteJSON(t, h, aliceJar, map[string]any{
		"title":    "Alice Only",
		"filename": "a.txt",
		"content":  "private",
	})

	bobList := getJar(t, h, bobJar, "/api/v1/me/pastes")
	bobListBody := readBody(t, bobList)
	if bobList.StatusCode != http.StatusOK {
		t.Fatalf("bob list status = %d; body = %q", bobList.StatusCode, bobListBody)
	}
	if strings.Contains(bobListBody, alicePaste.ID) {
		t.Fatalf("bob API list leaked alice paste; body = %q", bobListBody)
	}

	bobPage := getJar(t, h, bobJar, "/me/pastes")
	bobPageBody := readBody(t, bobPage)
	if strings.Contains(bobPageBody, alicePaste.ID) || strings.Contains(bobPageBody, "Alice Only") {
		t.Fatalf("bob My Pastes leaked alice paste; body = %q", bobPageBody)
	}

	denyWeb := postFormJarCSRF(t, h, bobJar, "/me/pastes/"+alicePaste.ID+"/delete", url.Values{})
	denyWebBody := readBody(t, denyWeb)
	if denyWeb.StatusCode != http.StatusForbidden && denyWeb.StatusCode != http.StatusNotFound {
		t.Fatalf("bob web delete status = %d, want 403/404; body = %q", denyWeb.StatusCode, denyWebBody)
	}

	denyAPI := deleteJSONJarCSRF(t, h, bobJar, "/api/v1/pastes/"+alicePaste.ID)
	denyAPIBody := readBody(t, denyAPI)
	if denyAPI.StatusCode != http.StatusForbidden {
		t.Fatalf("bob API delete status = %d, want 403; body = %q", denyAPI.StatusCode, denyAPIBody)
	}

	view, err := h.GET("/p/" + alicePaste.ID)
	if err != nil {
		t.Fatalf("GET alice paste: %v", err)
	}
	viewBody := readBody(t, view)
	if view.StatusCode != http.StatusOK {
		t.Fatalf("alice paste should still exist; status = %d; body = %q", view.StatusCode, viewBody)
	}
}

func mustRedeemInvite(t *testing.T, h *apptest.Harness, adminJar http.CookieJar, username, password string) http.CookieJar {
	t.Helper()
	created := mustCreateInvite(t, h, adminJar, url.Values{})
	res := postForm(t, h, "/invite/"+created.Token, url.Values{
		"username": {username},
		"password": {password},
	})
	body := readBody(t, res)
	if res.StatusCode != http.StatusSeeOther && res.StatusCode != http.StatusOK {
		t.Fatalf("redeem invite for %s: status %d body %q", username, res.StatusCode, body)
	}
	return mustLogin(t, h, username, password)
}

func mustCreatePasteJSON(t *testing.T, h *apptest.Harness, jar http.CookieJar, payload map[string]any) struct {
	ID string `json:"id"`
} {
	t.Helper()
	res := postJSONJarCSRF(t, h, jar, "/api/v1/pastes", payload)
	body := readBody(t, res)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create paste status = %d; body = %q", res.StatusCode, body)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		t.Fatalf("parse create: %v; body = %q", err, body)
	}
	if created.ID == "" {
		t.Fatalf("missing id in %q", body)
	}
	return created
}
