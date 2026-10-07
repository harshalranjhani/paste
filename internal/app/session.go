package app

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"html"
	"net/http"
	"strings"
	"time"

	"github.com/harshalranjhani/paste/internal/auth"
)

const (
	sessionCookieName = "session"
	csrfCookieName    = "csrf"
	sessionTTL        = 30 * 24 * time.Hour
)

type sessionUser struct {
	SessionID int64
	UserID    int64
	Username  string
	Role      string
	CSRFToken string
	TokenID   int64
	Scopes    map[string]bool
	ViaBearer bool
}

func (u *sessionUser) hasScope(scope string) bool {
	if u == nil {
		return false
	}
	if !u.ViaBearer {
		return true
	}
	return u.Scopes[scope]
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeLoginPage(w, "", "", http.StatusOK)
	case http.MethodPost:
		s.handleLoginPost(w, r)
	default:
		writeUIError(w, r, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func writeLoginPage(w http.ResponseWriter, username, errorMessage string, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	message := ""
	if errorMessage != "" {
		message = `<p class="alert alert-error mt-5" role="alert">` + html.EscapeString(errorMessage) + `</p>`
	}
	writePage(w, "Sign in", "/login", nil, `<div class="auth-layout">
<div class="auth-intro"><span class="brand-mark">`+icon("code")+`</span><p class="eyebrow">Your code, a link away</p><h1>A small space for your next big idea.</h1><p class="description">Share a snippet or a whole file tree. Simple, unlisted, and easy to read.</p></div>
<section class="card auth-card"><h2>Welcome back</h2><p class="description">Sign in to create and manage your pastes.</p>
`+message+`<form class="stack" method="post" action="/login">
<label>Username <input name="username" required autocomplete="username" placeholder="Your username" value="`+html.EscapeString(username)+`"></label>
<label>Password <input type="password" name="password" required autocomplete="current-password" placeholder="Your password"></label>
<button class="button button-primary" type="submit">Sign in `+icon("arrow")+`</button>
<p class="muted">Accounts are invite-only. Ask your administrator for an invite.</p>
</form></section></div>`)
}

func (s *Server) handleLoginPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeUIError(w, r, "bad request", http.StatusBadRequest)
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	if username == "" || password == "" {
		writeLoginPage(w, username, "Invalid credentials. Check your username and password, then try again.", http.StatusUnauthorized)
		return
	}

	var userID int64
	var passwordHash string
	err := s.db.QueryRowContext(r.Context(),
		`SELECT id, password_hash FROM users WHERE username = ? COLLATE NOCASE`, username,
	).Scan(&userID, &passwordHash)
	if err == sql.ErrNoRows {
		writeLoginPage(w, username, "Invalid credentials. Check your username and password, then try again.", http.StatusUnauthorized)
		return
	}
	if err != nil {
		writeUIError(w, r, "internal error", http.StatusInternalServerError)
		return
	}
	ok, err := auth.CheckPassword(passwordHash, password)
	if err != nil || !ok {
		writeLoginPage(w, username, "Invalid credentials. Check your username and password, then try again.", http.StatusUnauthorized)
		return
	}

	token, csrf, err := s.createSession(r.Context(), userID)
	if err != nil {
		writeUIError(w, r, "internal error", http.StatusInternalServerError)
		return
	}
	s.setSessionCookies(w, token, csrf)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeUIError(w, r, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sess, err := s.sessionFromRequest(r)
	if err != nil || sess == nil {
		writeUIError(w, r, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !s.validCSRF(r, sess) {
		writeUIError(w, r, "csrf required", http.StatusForbidden)
		return
	}
	if _, err := s.db.ExecContext(r.Context(), `DELETE FROM sessions WHERE id = ?`, sess.SessionID); err != nil {
		writeUIError(w, r, "internal error", http.StatusInternalServerError)
		return
	}
	s.clearSessionCookies(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) validCSRF(r *http.Request, sess *sessionUser) bool {
	token := r.Header.Get("X-CSRF-Token")
	if token == "" {
		_ = r.ParseForm()
		token = r.FormValue("csrf")
	}
	if token == "" || sess.CSRFToken == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(sess.CSRFToken)) == 1
}

func (s *Server) createSession(ctx context.Context, userID int64) (token, csrf string, err error) {
	token, tokenHash, err := auth.NewSessionToken()
	if err != nil {
		return "", "", err
	}
	csrf, err = auth.NewCSRFToken()
	if err != nil {
		return "", "", err
	}
	expires := time.Now().UTC().Add(sessionTTL).Format(time.RFC3339Nano)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = tx.Rollback() }()

	// Rotate: invalidate all existing sessions for this user on login.
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID); err != nil {
		return "", "", err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO sessions (user_id, token_hash, csrf_token, expires_at) VALUES (?, ?, ?, ?)`,
		userID, tokenHash, csrf, expires,
	); err != nil {
		return "", "", err
	}
	if err := tx.Commit(); err != nil {
		return "", "", err
	}
	return token, csrf, nil
}

func (s *Server) sessionFromRequest(r *http.Request) (*sessionUser, error) {
	if token, ok := bearerToken(r); ok {
		return s.bearerFromRequest(r, token)
	}

	c, err := r.Cookie(sessionCookieName)
	if err != nil || c.Value == "" {
		return nil, nil
	}
	hash := auth.HashToken(c.Value)
	var sess sessionUser
	var expiresAt string
	err = s.db.QueryRowContext(r.Context(), `
SELECT s.id, s.user_id, u.username, u.role, s.csrf_token, s.expires_at
FROM sessions s
JOIN users u ON u.id = s.user_id
WHERE s.token_hash = ?`, hash,
	).Scan(&sess.SessionID, &sess.UserID, &sess.Username, &sess.Role, &sess.CSRFToken, &expiresAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	exp, err := time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil {
		exp, err = time.Parse(time.RFC3339, expiresAt)
		if err != nil {
			return nil, err
		}
	}
	if time.Now().UTC().After(exp) {
		_, _ = s.db.ExecContext(r.Context(), `DELETE FROM sessions WHERE id = ?`, sess.SessionID)
		return nil, nil
	}
	return &sess, nil
}

func bearerToken(r *http.Request) (string, bool) {
	authz := r.Header.Get("Authorization")
	parts := strings.SplitN(authz, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}
	token := strings.TrimSpace(parts[1])
	if token == "" {
		return "", false
	}
	return token, true
}

func (s *Server) bearerFromRequest(r *http.Request, token string) (*sessionUser, error) {
	token = strings.TrimSpace(token)
	if token == "" || !strings.HasPrefix(token, "pb_") {
		return nil, nil
	}
	hash := auth.HashToken(token)
	var sess sessionUser
	var scopes string
	var expiresAt, revokedAt sql.NullString
	err := s.db.QueryRowContext(r.Context(), `
SELECT t.id, t.user_id, u.username, u.role, t.scopes, t.expires_at, t.revoked_at
FROM api_tokens t
JOIN users u ON u.id = t.user_id
WHERE t.token_hash = ?`, hash,
	).Scan(&sess.TokenID, &sess.UserID, &sess.Username, &sess.Role, &scopes, &expiresAt, &revokedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if revokedAt.Valid {
		return nil, nil
	}
	if expiresAt.Valid {
		exp, err := parseStoredTime(expiresAt.String)
		if err != nil {
			return nil, err
		}
		if time.Now().UTC().After(exp) {
			return nil, nil
		}
	}
	sess.ViaBearer = true
	sess.Scopes = make(map[string]bool)
	for _, sc := range splitScopes(scopes) {
		sess.Scopes[sc] = true
	}
	return &sess, nil
}

func (s *Server) touchAPIToken(r *http.Request, tokenID int64) {
	if tokenID == 0 {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, _ = s.db.ExecContext(r.Context(),
		`UPDATE api_tokens SET last_used_at = ? WHERE id = ?`,
		now, tokenID,
	)
}

func (s *Server) requireAPIAuth(w http.ResponseWriter, r *http.Request, scope string) *sessionUser {
	sess, err := s.sessionFromRequest(r)
	if err != nil {
		writeUIError(w, r, "internal error", http.StatusInternalServerError)
		return nil
	}
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return nil
	}
	if !sess.ViaBearer {
		if !s.validCSRF(r, sess) {
			writeUIError(w, r, "csrf required", http.StatusForbidden)
			return nil
		}
	}
	if !sess.hasScope(scope) {
		writeJSONError(w, http.StatusForbidden, "insufficient_scope", "missing required scope: "+scope)
		return nil
	}
	if sess.ViaBearer {
		s.touchAPIToken(r, sess.TokenID)
	}
	return sess
}

func (s *Server) setSessionCookies(w http.ResponseWriter, token, csrf string) {
	secure := strings.HasPrefix(s.cfg.BaseURL, "https://")
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    csrf,
		Path:     "/",
		HttpOnly: false,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
}

func (s *Server) clearSessionCookies(w http.ResponseWriter) {
	secure := strings.HasPrefix(s.cfg.BaseURL, "https://")
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: false,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}
