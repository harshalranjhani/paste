package app

import (
	"net/http"
)

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sess, err := s.sessionFromRequest(r)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if sess == nil {
		_, _ = w.Write([]byte(`<!DOCTYPE html><html><head><title>paste</title></head><body>
<h1>paste</h1>
<p>Sign in to create pastes.</p>
<p><a href="/login">Sign in</a></p>
</body></html>`))
		return
	}
	_, _ = w.Write([]byte(`<!DOCTYPE html><html><head><title>paste</title></head><body>
<h1>paste</h1>
<p>Signed in as ` + sess.Username + `.</p>
<p><a href="/new">New paste</a> · <a href="/me/pastes">My Pastes</a> · <a href="/settings/tokens">API tokens</a>` + adminLinks(sess) + `</p>
<form method="post" action="/logout"><input type="hidden" name="csrf" value="` + sess.CSRFToken + `"><button type="submit">Log out</button></form>
</body></html>`))
}

func adminLinks(sess *sessionUser) string {
	if sess == nil || sess.Role != "admin" {
		return ""
	}
	return ` · <a href="/admin/pastes">Moderate paste</a> · <a href="/admin/invites">Invites</a>`
}
