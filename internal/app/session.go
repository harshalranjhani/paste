package app

import (
	"context"
	"crypto/subtle"
	"database/sql"
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
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!DOCTYPE html><html><head><title>Login</title></head><body>
<h1>Sign in</h1>
<form method="post" action="/login">
<label>Username <input name="username" required></label>
<label>Password <input type="password" name="password" required></label>
<button type="submit">Sign in</button>
</form>
</body></html>`))
	case http.MethodPost:
		s.handleLoginPost(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleLoginPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	if username == "" || password == "" {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	var userID int64
	var passwordHash string
	err := s.db.QueryRowContext(r.Context(),
		`SELECT id, password_hash FROM users WHERE username = ? COLLATE NOCASE`, username,
	).Scan(&userID, &passwordHash)
	if err == sql.ErrNoRows {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	ok, err := auth.CheckPassword(passwordHash, password)
	if err != nil || !ok {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	token, csrf, err := s.createSession(r.Context(), userID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.setSessionCookies(w, token, csrf)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sess, err := s.sessionFromRequest(r)
	if err != nil || sess == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !s.validCSRF(r, sess) {
		http.Error(w, "csrf required", http.StatusForbidden)
		return
	}
	if _, err := s.db.ExecContext(r.Context(), `DELETE FROM sessions WHERE id = ?`, sess.SessionID); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
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
