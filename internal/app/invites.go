package app

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"html"
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
		writeUIError(w, r, "internal error", http.StatusInternalServerError)
		return nil
	}
	if sess == nil {
		writeUIError(w, r, "unauthorized", http.StatusUnauthorized)
		return nil
	}
	if sess.Role != "admin" {
		writeUIError(w, r, "forbidden", http.StatusForbidden)
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
		writeUIError(w, r, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAdminInvitesCreate(w http.ResponseWriter, r *http.Request) {
	sess := s.requireAdmin(w, r)
	if sess == nil {
		return
	}
	if !s.validCSRF(r, sess) {
		writeUIError(w, r, "csrf required", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		writeUIError(w, r, "bad request", http.StatusBadRequest)
		return
	}
	email := strings.TrimSpace(r.FormValue("email"))
	ttl := defaultInviteTTL
	if raw := strings.TrimSpace(r.FormValue("ttl_seconds")); raw != "" {
		secs, err := strconv.Atoi(raw)
		if err != nil || secs < 1 {
			writeUIError(w, r, "invalid ttl_seconds", http.StatusBadRequest)
			return
		}
		ttl = time.Duration(secs) * time.Second
	}
	token, err := newInviteToken()
	if err != nil {
		writeUIError(w, r, "internal error", http.StatusInternalServerError)
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
		writeUIError(w, r, "internal error", http.StatusInternalServerError)
		return
	}
	id, err := res.LastInsertId()
	if err != nil {
		writeUIError(w, r, "internal error", http.StatusInternalServerError)
		return
	}
	url := strings.TrimRight(s.cfg.BaseURL, "/") + "/invite/" + token
	w.Header().Set("Vary", "Accept")
	if strings.Contains(r.Header.Get("Accept"), "text/html") {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusCreated)
		writePage(w, "Invite created", "/admin/invites", sess, pageHeading("Grow your workspace", "Invite created", "Copy this link now. It is shown only once, and can be used by one person.", "")+`<section class="card max-w-2xl"><div class="card-body"><div class="alert">Share this link directly with your teammate. It expires `+displayTime(expiresAt.Format(time.RFC3339Nano))+`.</div><pre class="secret">`+html.EscapeString(url)+`</pre><div class="form-actions"><button class="button button-primary" type="button" data-copy="`+html.EscapeString(url)+`">`+icon("copy")+`Copy invite link</button><a class="button" href="/admin/invites">Back to invites</a></div></div></section>`)
		return
	}
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
		writeUIError(w, r, "internal error", http.StatusInternalServerError)
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
			writeUIError(w, r, "internal error", http.StatusInternalServerError)
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
	if err := rows.Err(); err != nil {
		writeUIError(w, r, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Vary", "Accept")
	if strings.Contains(r.Header.Get("Accept"), "text/html") {
		var table strings.Builder
		for _, inv := range out {
			email, status := "Anyone with the link", "active"
			if inv.Email != nil {
				email = *inv.Email
			}
			if inv.RevokedAt != nil {
				status = "revoked"
			} else if inv.UsedAt != nil {
				status = "used"
			} else if expiry, err := parseStoredTime(inv.ExpiresAt); err == nil && time.Now().After(expiry) {
				status = "expired"
			}
			table.WriteString(`<tr><td>` + html.EscapeString(email) + `</td><td>` + displayTime(inv.CreatedAt) + `</td><td>` + displayTime(inv.ExpiresAt) + `</td><td><span class="badge">` + status + `</span></td><td>`)
			if status == "active" {
				table.WriteString(`<form method="post" data-confirm="Revoke this invite?" action="/admin/invites/` + strconv.FormatInt(inv.ID, 10) + `/revoke"><input type="hidden" name="csrf" value="` + html.EscapeString(sess.CSRFToken) + `"><button class="button button-danger" type="submit">Revoke</button></form>`)
			}
			table.WriteString(`</td></tr>`)
		}
		list := `<div class="card table-scroll"><table><thead><tr><th>Recipient</th><th>Created</th><th>Expires</th><th>Status</th><th>Actions</th></tr></thead><tbody>` + table.String() + `</tbody></table></div>`
		if len(out) == 0 {
			list = `<div class="card empty-state"><span class="feature-icon">` + icon("user") + `</span><h2>Better with company</h2><p>Create an invite above to bring someone into your workspace.</p></div>`
		}
		form := `<section class="card mb-7"><div class="card-header"><h2>Create an invite</h2><span class="badge">Single use</span></div><form class="card-body" method="post" action="/admin/invites"><input type="hidden" name="csrf" value="` + html.EscapeString(sess.CSRFToken) + `"><div class="form-grid"><label>Email <span class="muted">Optional. Restrict this invite to an email address.</span><input type="email" name="email" placeholder="teammate@example.com"></label><label>Expires in <span class="muted">Give your teammate time to join.</span><select name="ttl_seconds"><option value="604800">7 days</option><option value="259200">3 days</option><option value="86400">1 day</option><option value="3600">1 hour</option></select></label></div><div class="form-actions"><button class="button button-primary" type="submit">` + icon("plus") + `Create invite</button><p class="muted">You’ll get a link to share manually. No email is sent.</p></div></form></section>`
		writePage(w, "Invites", "/admin/invites", sess, pageHeading("Workspace access", "Invites", "Invite people you trust. Each link creates one account and can be revoked before it is used.", "")+form+`<div class="section-title"><h2>Invite history</h2></div>`+list)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) handleAdminInviteRevoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeUIError(w, r, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sess := s.requireAdmin(w, r)
	if sess == nil {
		return
	}
	if !s.validCSRF(r, sess) {
		writeUIError(w, r, "csrf required", http.StatusForbidden)
		return
	}
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeUIError(w, r, "bad request", http.StatusBadRequest)
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(r.Context(),
		`UPDATE invites SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL AND used_at IS NULL`,
		now, id,
	)
	if err != nil {
		writeUIError(w, r, "internal error", http.StatusInternalServerError)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeUIError(w, r, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Vary", "Accept")
	if strings.Contains(r.Header.Get("Accept"), "text/html") {
		http.Redirect(w, r, "/admin/invites", http.StatusSeeOther)
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
