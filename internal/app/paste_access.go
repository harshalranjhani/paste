package app

import (
	"database/sql"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/harshalranjhani/paste/internal/auth"
)

const (
	pasteAccessCookieName = "paste_access"
	pasteAccessTTL        = time.Hour
	genericUnlockFailure  = "Invalid password."
	unlockRateLimitMax    = 5
	unlockRateLimitWindow = time.Minute
)

type unlockAttemptWindow struct {
	count   int
	resetAt time.Time
}

func (s *Server) handlePasteUnlock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	publicID := r.PathValue("id")
	paste, ok := s.loadPasteMeta(w, r, publicID)
	if !ok {
		return
	}
	if paste.ProtectionMode != "password" || !paste.PasswordHash.Valid {
		http.Redirect(w, r, "/p/"+publicID, http.StatusSeeOther)
		return
	}
	clientKey := unlockClientKey(r)
	if s.unlockRateLimited(clientKey, publicID) {
		http.Error(w, "too many unlock attempts", http.StatusTooManyRequests)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	password := r.FormValue("password")
	match, err := auth.CheckPassword(paste.PasswordHash.String, password)
	if err != nil || !match {
		s.recordUnlockFailure(clientKey, publicID)
		s.writePasteLockScreen(w, publicID, genericUnlockFailure, http.StatusUnauthorized)
		return
	}
	s.clearUnlockFailures(clientKey, publicID)
	token, err := s.createPasteAccessSession(r, paste.ID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.setPasteAccessCookie(w, token)
	http.Redirect(w, r, "/p/"+publicID, http.StatusSeeOther)
}

func unlockClientKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) unlockRateKey(clientKey, publicID string) string {
	return clientKey + "\x00" + publicID
}

func (s *Server) unlockRateLimited(clientKey, publicID string) bool {
	key := s.unlockRateKey(clientKey, publicID)
	now := time.Now()
	s.unlockMu.Lock()
	defer s.unlockMu.Unlock()
	win, ok := s.unlockAttempts[key]
	if !ok || now.After(win.resetAt) {
		return false
	}
	return win.count >= unlockRateLimitMax
}

func (s *Server) recordUnlockFailure(clientKey, publicID string) {
	key := s.unlockRateKey(clientKey, publicID)
	now := time.Now()
	s.unlockMu.Lock()
	defer s.unlockMu.Unlock()
	win, ok := s.unlockAttempts[key]
	if !ok || now.After(win.resetAt) {
		s.unlockAttempts[key] = unlockAttemptWindow{count: 1, resetAt: now.Add(unlockRateLimitWindow)}
		return
	}
	win.count++
	s.unlockAttempts[key] = win
}

func (s *Server) clearUnlockFailures(clientKey, publicID string) {
	key := s.unlockRateKey(clientKey, publicID)
	s.unlockMu.Lock()
	defer s.unlockMu.Unlock()
	delete(s.unlockAttempts, key)
}

func (s *Server) pasteAccessTTL() time.Duration {
	ttl := s.cfg.PasteAccessTTL
	if ttl <= 0 || ttl > pasteAccessTTL {
		return pasteAccessTTL
	}
	return ttl
}

func (s *Server) createPasteAccessSession(r *http.Request, pasteID int64) (string, error) {
	token, tokenHash, err := auth.NewSessionToken()
	if err != nil {
		return "", err
	}
	expires := time.Now().UTC().Add(s.pasteAccessTTL()).Format(time.RFC3339Nano)
	if _, err := s.db.ExecContext(r.Context(),
		`INSERT INTO paste_access_sessions (paste_id, token_hash, expires_at) VALUES (?, ?, ?)`,
		pasteID, tokenHash, expires,
	); err != nil {
		return "", err
	}
	return token, nil
}

func (s *Server) hasPasteAccess(r *http.Request, pasteID int64) bool {
	c, err := r.Cookie(pasteAccessCookieName)
	if err != nil || c.Value == "" {
		return false
	}
	hash := auth.HashToken(c.Value)
	var sessionPasteID int64
	var expiresAt string
	err = s.db.QueryRowContext(r.Context(), `
SELECT paste_id, expires_at FROM paste_access_sessions WHERE token_hash = ?`, hash,
	).Scan(&sessionPasteID, &expiresAt)
	if err == sql.ErrNoRows {
		return false
	}
	if err != nil {
		return false
	}
	if sessionPasteID != pasteID {
		return false
	}
	expired, err := isExpired(expiresAt)
	if err != nil || expired {
		_, _ = s.db.ExecContext(r.Context(), `DELETE FROM paste_access_sessions WHERE token_hash = ?`, hash)
		return false
	}
	return true
}

func (s *Server) setPasteAccessCookie(w http.ResponseWriter, token string) {
	secure := strings.HasPrefix(s.cfg.BaseURL, "https://")
	http.SetCookie(w, &http.Cookie{
		Name:     pasteAccessCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		// Session cookie: omit MaxAge/Expires so the browser drops it on close.
	})
}
