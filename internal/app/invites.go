package app

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/harshalranjhani/paste/internal/auth"
)

const defaultInviteTTL = 7 * 24 * time.Hour

func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) *sessionUser {
	sess, err := s.sessionFromRequest(r)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return nil
	}
	if sess == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return nil
	}
	if sess.Role != "admin" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return nil
	}
	return sess
}

func (s *Server) handleAdminInvites(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleAdminInvitesList(w, r)
	case http.MethodPost:
		s.handleAdminInvitesCreate(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAdminInvitesCreate(w http.ResponseWriter, r *http.Request) {
	sess := s.requireAdmin(w, r)
	if sess == nil {
		return
	}
	if !s.validCSRF(r, sess) {
		http.Error(w, "csrf required", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	email := strings.TrimSpace(r.FormValue("email"))
	ttl := defaultInviteTTL
	if raw := strings.TrimSpace(r.FormValue("ttl_seconds")); raw != "" {
		secs, err := strconv.Atoi(raw)
		if err != nil || secs < 1 {
			http.Error(w, "invalid ttl_seconds", http.StatusBadRequest)
			return
		}
		ttl = time.Duration(secs) * time.Second
	}
	token, err := newInviteToken()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	expiresAt := time.Now().UTC().Add(ttl)
	var emailArg any
	if email != "" {
		emailArg = email
	}
	res, err := s.db.ExecContext(r.Context(),
		`INSERT INTO invites (created_by_admin_id, token_hash, email, expires_at) VALUES (?, ?, ?, ?)`,
		sess.UserID, auth.HashToken(token), emailArg, expiresAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	id, err := res.LastInsertId()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	url := strings.TrimRight(s.cfg.BaseURL, "/") + "/invite/" + token
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":         id,
		"url":        url,
		"token":      token,
		"expires_at": expiresAt.Format(time.RFC3339Nano),
		"email":      email,
	})
}

func (s *Server) handleAdminInvitesList(w http.ResponseWriter, r *http.Request) {
	sess := s.requireAdmin(w, r)
	if sess == nil {
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `
SELECT id, email, expires_at, used_at, revoked_at, created_at
FROM invites
ORDER BY id DESC`)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type inviteRow struct {
		ID        int64   `json:"id"`
		Email     *string `json:"email"`
		ExpiresAt string  `json:"expires_at"`
		UsedAt    *string `json:"used_at"`
		RevokedAt *string `json:"revoked_at"`
		CreatedAt string  `json:"created_at"`
	}
	out := []inviteRow{}
	for rows.Next() {
		var row inviteRow
		var email, usedAt, revokedAt sql.NullString
		if err := rows.Scan(&row.ID, &email, &row.ExpiresAt, &usedAt, &revokedAt, &row.CreatedAt); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if email.Valid {
			row.Email = &email.String
		}
		if usedAt.Valid {
			row.UsedAt = &usedAt.String
		}
		if revokedAt.Valid {
			row.RevokedAt = &revokedAt.String
		}
		out = append(out, row)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) handleAdminInviteRevoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sess := s.requireAdmin(w, r)
	if sess == nil {
		return
	}
	if !s.validCSRF(r, sess) {
		http.Error(w, "csrf required", http.StatusForbidden)
		return
	}
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(r.Context(),
		`UPDATE invites SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL AND used_at IS NULL`,
		now, id,
	)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func newInviteToken() (string, error) {
	raw := make([]byte, 32) // 256 bits
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
