package app

import (
	"html"
	"net/http"
)

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		writeUIError(w, r, "Paste not found.", http.StatusNotFound)
		return
	}
	if r.Method != http.MethodGet {
		writeUIError(w, r, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sess, err := s.sessionFromRequest(r)
	if err != nil {
		writeUIError(w, r, "internal error", http.StatusInternalServerError)
		return
	}
	if sess == nil {
		writePage(w, "A little space for code", "/", nil, `<section class="hero"><div><p class="eyebrow">Good code deserves a good home</p><h1>From a snippet<br>to a whole idea.</h1><p class="description">A simple place to share code and file trees with one unlisted link. Sign in to create pastes.</p><div class="form-actions"><a class="button button-primary" href="/login">Sign in `+icon("arrow")+`</a><span class="muted">Have a paste link? Open it directly.</span></div></div><span class="feature-icon">`+icon("code")+`</span></section>`+homeFeatures(false))
		return
	}
	writePage(w, "Overview", "/", sess, `<section class="hero"><div><p class="eyebrow">Your workspace</p><h1>Make something worth sharing.</h1><p class="description">Signed in as `+html.EscapeString(sess.Username)+`. Drop in a snippet, add a few files, and give your ideas a link.</p><div class="form-actions"><a class="button button-primary" href="/new">`+icon("plus")+`New paste</a><a class="button" href="/me/pastes">My Pastes `+icon("arrow")+`</a></div></div></section>`+homeFeatures(true))
}

func homeFeatures(signedIn bool) string {
	actions := []string{"", "", ""}
	if signedIn {
		actions = []string{`<a class="button button-ghost" href="/new">Start a paste ` + icon("arrow") + `</a>`, `<a class="button button-ghost" href="/me/pastes">Open your pastes ` + icon("arrow") + `</a>`, `<a class="button button-ghost" href="/settings/tokens">Manage API tokens ` + icon("arrow") + `</a>`}
	}
	return `<div class="feature-grid"><section class="card feature-card"><span class="feature-icon">` + icon("folder") + `</span><h2>One link. Every file.</h2><p>Keep your folder structure intact. Browse files in a familiar tree, with syntax highlighting and line numbers.</p>` + actions[0] + `</section><section class="card feature-card"><span class="feature-icon">` + icon("lock") + `</span><h2>Shared on your terms.</h2><p>Pastes are unlisted and expire within 90 days. Add a password for another layer of privacy.</p>` + actions[1] + `</section><section class="card feature-card"><span class="feature-icon">` + icon("code") + `</span><h2>At home in your terminal.</h2><p>Use the pbin CLI to share a file or an entire directory. Personal access tokens keep access in your control.</p>` + actions[2] + `</section></div>`
}
