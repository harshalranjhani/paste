package app

import (
	"embed"
	"html"
	"net/http"
	"strings"
	"time"
)

//go:embed assets/app.css assets/app.js
var uiAssets embed.FS

func pageStart(title, active string, sess *sessionUser) string {
	var nav strings.Builder
	links := []struct{ path, label string }{{"/", "Overview"}}
	if sess != nil {
		links = append(links, struct{ path, label string }{"/me/pastes", "My Pastes"}, struct{ path, label string }{"/settings/tokens", "API tokens"})
		if sess.Role == "admin" {
			links = append(links, struct{ path, label string }{"/admin/invites", "Invites"}, struct{ path, label string }{"/admin/pastes", "Moderation"})
		}
	}
	for _, link := range links {
		current := ""
		if link.path == active {
			current = ` aria-current="page"`
		}
		nav.WriteString(`<a href="` + link.path + `"` + current + `>` + link.label + `</a>`)
	}
	account := `<a class="button button-primary" href="/login">Sign in ` + icon("arrow") + `</a>`
	if sess != nil {
		account = `<span class="account-name" title="` + html.EscapeString(sess.Username) + `">` + icon("user") + html.EscapeString(sess.Username) + `</span>
<form method="post" action="/logout"><input type="hidden" name="csrf" value="` + html.EscapeString(sess.CSRFToken) + `"><button class="button button-ghost" type="submit">Log out</button></form>`
	}
	return `<!DOCTYPE html><html lang="en"><head>
<meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex,nofollow,noarchive,nosnippet">
<title>` + html.EscapeString(title) + ` · paste</title>
<link rel="stylesheet" href="/assets/app.css"><script src="/assets/app.js" defer></script>
</head><body>
<a class="skip-link" href="#main-content">Skip to content</a>
<header class="app-header"><div class="header-inner">
<a class="brand" href="/" aria-label="paste home"><span class="brand-mark">` + icon("code") + `</span>paste<span class="brand-tag">a little space for code</span></a>
<div class="account-actions">` + account + `</div>
<nav class="app-nav" aria-label="Main navigation">` + nav.String() + `</nav>
</div></header><main class="page" id="main-content" tabindex="-1">`
}

const pageEnd = `</main><footer class="app-footer"><span>Small snippets. Whole ideas.</span><span>Unlisted by default. Shared by you.</span></footer><div id="toast" class="toast" role="status" hidden></div></body></html>`

func writePage(w http.ResponseWriter, title, active string, sess *sessionUser, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(pageStart(title, active, sess) + body + pageEnd))
}

func writeUIError(w http.ResponseWriter, r *http.Request, message string, status int) {
	if !strings.Contains(r.Header.Get("Accept"), "text/html") || strings.HasPrefix(r.URL.Path, "/api/") {
		http.Error(w, message, status)
		return
	}
	back := r.URL.Path
	switch {
	case strings.HasPrefix(back, "/me/pastes"):
		back = "/me/pastes"
	case strings.HasPrefix(back, "/admin/invites"):
		back = "/admin/invites"
	case strings.HasPrefix(back, "/admin/pastes"):
		back = "/admin/pastes"
	case strings.HasPrefix(back, "/settings/tokens"):
		back = "/settings/tokens"
	default:
		back = strings.TrimSuffix(back, "/unlock")
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	writePage(w, http.StatusText(status), "", nil, `<section class="card mx-auto max-w-xl"><div class="card-body"><p class="eyebrow">`+http.StatusText(status)+`</p><h1>Let’s try that again.</h1><p class="alert alert-error mt-5" role="alert">`+html.EscapeString(message)+`</p><div class="form-actions"><a class="button button-primary" href="`+html.EscapeString(back)+`" data-back>Go back</a><a class="button" href="/">Back to overview</a></div></div></section>`)
}

func pageHeading(eyebrow, title, description, action string) string {
	return `<div class="page-heading"><div><p class="eyebrow">` + html.EscapeString(eyebrow) + `</p><h1>` + html.EscapeString(title) + `</h1><p class="description">` + html.EscapeString(description) + `</p></div>` + action + `</div>`
}

func displayTime(value string) string {
	date, err := parseStoredTime(value)
	if err != nil {
		return html.EscapeString(value)
	}
	return `<time datetime="` + html.EscapeString(value) + `" title="` + html.EscapeString(value) + `">` + date.Format("Jan 2, 2006") + `</time>`
}

func pasteBadge(mode, expires string) string {
	if date, err := parseStoredTime(expires); err == nil && time.Now().After(date) {
		return `<span class="badge">Expired</span>`
	}
	if mode == "password" {
		return `<span class="badge">` + icon("lock") + `password protected</span>`
	}
	return `<span class="badge badge-green">Unlisted</span>`
}

func icon(name string) string {
	paths := map[string]string{
		"code":     `<path d="m8 8-4 4 4 4m8-8 4 4-4 4m-3-11-2 14"/>`,
		"file":     `<path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><path d="M14 2v6h6M8 13h8M8 17h5"/>`,
		"folder":   `<path d="M3 7V5a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/>`,
		"plus":     `<path d="M12 5v14M5 12h14"/>`,
		"arrow":    `<path d="M5 12h14m-6-6 6 6-6 6"/>`,
		"copy":     `<rect x="8" y="8" width="12" height="13" rx="2"/><path d="M16 8V5a2 2 0 0 0-2-2H5a2 2 0 0 0-2 2v9a2 2 0 0 0 2 2h3"/>`,
		"download": `<path d="M12 3v12m-5-5 5 5 5-5M4 16v4h16v-4"/>`,
		"lock":     `<rect x="4" y="10" width="16" height="11" rx="2"/><path d="M8 10V7a4 4 0 0 1 8 0v3m-4 5v2"/>`,
		"user":     `<circle cx="12" cy="8" r="4"/><path d="M4 21v-2a8 8 0 0 1 16 0v2"/>`,
		"key":      `<circle cx="8" cy="8" r="5"/><path d="m12 12 9 9m-5-5 3-3m-1 5 3-3"/>`,
		"chevron":  `<path d="m9 5 7 7-7 7"/>`,
		"trash":    `<path d="M3 6h18M9 6V3h6v3M5 6l1 15h12l1-15M10 10v7m4-7v7"/>`,
	}
	return `<svg class="icon" aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">` + paths[name] + `</svg>`
}
