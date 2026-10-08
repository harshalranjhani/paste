package app

import (
	"encoding/json"
	"html"
	"net/http"
	"strings"
	"time"

	"github.com/harshalranjhani/paste/internal/auth"
)

const burnLeaseTTL = 15 * time.Minute

func (s *Server) burnLeaseTTL() time.Duration {
	if s.cfg.BurnLeaseTTL > 0 && s.cfg.BurnLeaseTTL < burnLeaseTTL {
		return s.cfg.BurnLeaseTTL
	}
	return burnLeaseTTL
}

func burnCookieName(publicID string) string { return "paste_burn_" + publicID }

func (s *Server) hasBurnAccess(r *http.Request, paste *pasteRow) bool {
	cookie, err := r.Cookie(burnCookieName(paste.PublicID))
	if err != nil || cookie.Value == "" {
		return false
	}
	var expiresAt string
	err = s.db.QueryRowContext(r.Context(), `SELECT expires_at FROM burn_sessions WHERE paste_id = ? AND token_hash = ?`, paste.ID, auth.HashToken(cookie.Value)).Scan(&expiresAt)
	if err != nil {
		return false
	}
	expired, err := isExpired(expiresAt)
	return err == nil && !expired
}

func (s *Server) burnCSRFToken(w http.ResponseWriter, r *http.Request) (string, error) {
	if cookie, err := r.Cookie("burn_csrf"); err == nil && cookie.Value != "" {
		w.Header().Set("X-CSRF-Token", cookie.Value)
		return cookie.Value, nil
	}
	token, err := auth.NewCSRFToken()
	if err != nil {
		return "", err
	}
	http.SetCookie(w, &http.Cookie{Name: "burn_csrf", Value: token, Path: "/", HttpOnly: true, Secure: strings.HasPrefix(s.cfg.BaseURL, "https://"), SameSite: http.SameSiteStrictMode})
	w.Header().Set("X-CSRF-Token", token)
	return token, nil
}

func (s *Server) handlePasteReveal(w http.ResponseWriter, r *http.Request) {
	paste, ok := s.loadPasteMeta(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	if !paste.BurnAfterRead {
		writePasteError(w, r, http.StatusBadRequest, "not_burn_paste", "paste does not burn after reading")
		return
	}
	if paste.BurnedAt.Valid {
		writePasteError(w, r, http.StatusGone, "gone", "This paste has already been revealed.")
		return
	}
	if paste.ProtectionMode == "password" && !s.hasPasteAccess(r, paste.ID) {
		writePasteError(w, r, http.StatusUnauthorized, "password_required", "password required")
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") && strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		if s.requireAPIAuth(w, r, scopePasteRead) == nil {
			return
		}
	} else {
		cookie, err := r.Cookie("burn_csrf")
		if err != nil || !s.validCSRF(r, &sessionUser{CSRFToken: cookie.Value}) {
			writePasteError(w, r, http.StatusForbidden, "csrf_required", "csrf required")
			return
		}
	}
	token, hash, err := auth.NewSessionToken()
	if err != nil {
		writePasteError(w, r, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}
	now := time.Now().UTC()
	expiresAt := now.Add(s.burnLeaseTTL())
	if expiry, err := parseStoredTime(paste.ExpiresAt); err == nil && expiry.Before(expiresAt) {
		expiresAt = expiry
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		writePasteError(w, r, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(r.Context(), `UPDATE pastes SET burned_at = ? WHERE id = ? AND burn_after_read = 1 AND burned_at IS NULL AND julianday(expires_at) > julianday(?)`, now.Format(time.RFC3339Nano), paste.ID, now.Format(time.RFC3339Nano))
	if err != nil {
		writePasteError(w, r, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}
	if n, _ := res.RowsAffected(); n != 1 {
		writePasteError(w, r, http.StatusGone, "gone", "This paste has already been revealed or expired.")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `INSERT INTO burn_sessions (paste_id, token_hash, expires_at) VALUES (?, ?, ?)`, paste.ID, hash, expiresAt.Format(time.RFC3339Nano)); err != nil {
		writePasteError(w, r, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}
	if err := tx.Commit(); err != nil {
		writePasteError(w, r, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: burnCookieName(paste.PublicID), Value: token, Path: "/", HttpOnly: true, Secure: strings.HasPrefix(s.cfg.BaseURL, "https://"), SameSite: http.SameSiteLaxMode, Expires: expiresAt})
	if strings.HasPrefix(r.URL.Path, "/api/") {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"url": s.cfg.BaseURL + "/p/" + paste.PublicID, "expires_at": expiresAt.Format(time.RFC3339Nano)})
		return
	}
	http.Redirect(w, r, "/p/"+paste.PublicID, http.StatusSeeOther)
}

func (s *Server) requirePasteContent(w http.ResponseWriter, r *http.Request, paste *pasteRow) bool {
	if paste.ProtectionMode == "password" && !paste.BurnedAt.Valid && !s.hasPasteAccess(r, paste.ID) {
		writeJSONError(w, http.StatusUnauthorized, "password_required", "password required")
		return false
	}
	if paste.BurnAfterRead && !paste.BurnedAt.Valid {
		writeJSONError(w, http.StatusForbidden, "reveal_required", "reveal required")
		return false
	}
	return true
}

func (s *Server) writePasteRevealScreen(w http.ResponseWriter, r *http.Request, paste *pasteRow) {
	csrf, err := s.burnCSRFToken(w, r)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive, nosnippet")
	writePage(w, "Burn after read", "", nil, `<div class="auth-layout"><div class="auth-intro"><span class="brand-mark">`+icon("lock")+`</span><p class="eyebrow">One reader. One chance.</p><h1>This paste burns after reading.</h1><p class="description">Revealing consumes this link. Only your browser can read the files for the next 15 minutes, or until the paste expires.</p></div><section class="card auth-card"><h2>Ready to reveal?</h2><p class="description">Opening this page has not consumed the paste. Reveal it when you are ready to read.</p><form class="stack" method="post" action="/p/`+html.EscapeString(paste.PublicID)+`/reveal"><input type="hidden" name="csrf" value="`+html.EscapeString(csrf)+`"><button class="button button-primary" type="submit">Reveal paste `+icon("arrow")+`</button></form><div class="form-actions"><button class="button" type="button" data-copy="`+html.EscapeString(s.cfg.BaseURL+"/p/"+paste.PublicID)+`">`+icon("copy")+`Copy link</button></div></section></div>`)
}
