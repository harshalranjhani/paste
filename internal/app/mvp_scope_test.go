package app_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/harshalranjhani/paste/internal/apptest"
)

func TestOutOfScopeBurnAndAnonymousAndWebDirectoryRemainAbsent(t *testing.T) {
	h := apptest.Start(t)
	mustSetup(t, h, "admin", "correct-horse-battery-staple")
	jar := mustLogin(t, h, "admin", "correct-horse-battery-staple")

	for _, path := range []string{
		"/api/v1/pastes/someid/burn",
		"/api/v1/pastes/someid/reveal",
		"/p/someid/burn",
		"/p/someid/reveal",
	} {
		res, err := h.GET(path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		body := readBody(t, res)
		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("out-of-scope burn route %s status = %d; body = %q", path, res.StatusCode, body)
		}
	}

	newPage := getJar(t, h, jar, "/new")
	newBody := readBody(t, newPage)
	if newPage.StatusCode != http.StatusOK {
		t.Fatalf("/new status = %d; body = %q", newPage.StatusCode, newBody)
	}
	lower := strings.ToLower(newBody)
	if strings.Contains(lower, "webkitdirectory") || strings.Contains(lower, "directory=\"\"") {
		t.Fatal("/new must not expose web directory upload controls in MVP")
	}
}
