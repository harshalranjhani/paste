package app

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"html"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/harshalranjhani/paste/internal/auth"
)

const (
	scopePasteCreate = "paste:create"
	scopePasteRead   = "paste:read"
	scopePasteDelete = "paste:delete"
)

var allowedPATScopes = map[string]bool{
	scopePasteCreate: true,
	scopePasteRead:   true,
	scopePasteDelete: true,
}

type createTokenRequest struct {
	Name      string   `json:"name"`
	Scopes    []string `json:"scopes"`
	ExpiresIn string   `json:"expires_in"`
}

func (s *Server) handleAPITokens(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleAPIListTokens(w, r)
	case http.MethodPost:
		s.handleAPICreateToken(w, r)
	default:
		writeUIError(w, r, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAPICreateToken(w http.ResponseWriter, r *http.Request) {
	sess, err := s.sessionFromRequest(r)
	if err != nil {
		writeUIError(w, r, "internal error", http.StatusInternalServerError)
		return
	}
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	if !s.validCSRF(r, sess) {
		writeUIError(w, r, "csrf required", http.StatusForbidden)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "could not read body")
		return
	}
	var req createTokenRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "invalid_name", "name is required")
		return
	}
	scopes, errMsg := normalizeScopes(req.Scopes)
	if errMsg != "" {
		writeJSONError(w, http.StatusBadRequest, "invalid_scopes", errMsg)
		return
	}

	var expiresAt any
	var expiresAtStr *string
	if strings.TrimSpace(req.ExpiresIn) != "" {
		ttl, err := parseDurationDays(req.ExpiresIn)
		if err != nil || ttl < time.Second {
			writeJSONError(w, http.StatusBadRequest, "invalid_expires_in", "invalid expires_in")
			return
		}
		formatted := time.Now().UTC().Add(ttl).Format(time.RFC3339Nano)
		expiresAt = formatted
		expiresAtStr = &formatted
	}

	plaintext, err := newPATToken()
	if err != nil {
		writeUIError(w, r, "internal error", http.StatusInternalServerError)
		return
	}
	res, err := s.db.ExecContext(r.Context(),
		`INSERT INTO api_tokens (user_id, name, token_hash, scopes, expires_at) VALUES (?, ?, ?, ?, ?)`,
		sess.UserID, name, auth.HashToken(plaintext), strings.Join(scopes, " "), expiresAt,
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

	out := map[string]any{
		"id":     id,
		"name":   name,
		"token":  plaintext,
		"scopes": scopes,
	}
	if expiresAtStr != nil {
		out["expires_at"] = *expiresAtStr
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) handleAPIListTokens(w http.ResponseWriter, r *http.Request) {
	sess, err := s.sessionFromRequest(r)
	if err != nil {
		writeUIError(w, r, "internal error", http.StatusInternalServerError)
		return
	}
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `
SELECT id, name, scopes, created_at, expires_at, last_used_at, revoked_at
FROM api_tokens
WHERE user_id = ?
ORDER BY id DESC`, sess.UserID)
	if err != nil {
		writeUIError(w, r, "internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type tokenRow struct {
		ID         int64    `json:"id"`
		Name       string   `json:"name"`
		Scopes     []string `json:"scopes"`
		CreatedAt  string   `json:"created_at"`
		ExpiresAt  *string  `json:"expires_at,omitempty"`
		LastUsedAt *string  `json:"last_used_at,omitempty"`
		RevokedAt  *string  `json:"revoked_at,omitempty"`
	}
	out := []tokenRow{}
	for rows.Next() {
		var row tokenRow
		var scopes string
		var expiresAt, lastUsedAt, revokedAt sql.NullString
		if err := rows.Scan(&row.ID, &row.Name, &scopes, &row.CreatedAt, &expiresAt, &lastUsedAt, &revokedAt); err != nil {
			writeUIError(w, r, "internal error", http.StatusInternalServerError)
			return
		}
		row.Scopes = splitScopes(scopes)
		if expiresAt.Valid {
			row.ExpiresAt = &expiresAt.String
		}
		if lastUsedAt.Valid {
			row.LastUsedAt = &lastUsedAt.String
		}
		if revokedAt.Valid {
			row.RevokedAt = &revokedAt.String
		}
		out = append(out, row)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) handleSettingsTokens(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleSettingsTokensList(w, r)
	case http.MethodPost:
		s.handleSettingsTokensCreate(w, r)
	default:
		writeUIError(w, r, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSettingsTokensList(w http.ResponseWriter, r *http.Request) {
	sess, err := s.sessionFromRequest(r)
	if err != nil {
		writeUIError(w, r, "internal error", http.StatusInternalServerError)
		return
	}
	if sess == nil {
		writeUIError(w, r, "unauthorized", http.StatusUnauthorized)
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `
SELECT id, name, scopes, created_at, expires_at, last_used_at, revoked_at
FROM api_tokens
WHERE user_id = ?
ORDER BY id DESC`, sess.UserID)
	if err != nil {
		writeUIError(w, r, "internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var b strings.Builder
	b.WriteString(pageStart("API tokens", "/settings/tokens", sess))
	b.WriteString(pageHeading("Developer settings", "Personal access tokens", "Connect your terminal and tools. Tokens are shown only once at creation; store them securely.", ""))
	b.WriteString(`<section class="card mb-7"><div class="card-header"><h2>Create a token</h2><span class="badge">CLI & API</span></div>
<form class="card-body" method="post" action="/settings/tokens">
<input type="hidden" name="csrf" value="` + html.EscapeString(sess.CSRFToken) + `">
<div class="form-grid"><label>Name <input name="name" required placeholder="e.g. My laptop"></label><label>Expires in <input name="expires_in" placeholder="Optional, e.g. 30d"></label></div>
<fieldset class="mt-5"><legend class="mb-3 text-sm font-medium">Permissions</legend><div class="flex flex-wrap gap-5">
<label class="checkbox-label"><input type="checkbox" name="scope" value="paste:create" checked> Create pastes <code class="muted">paste:create</code></label>
<label class="checkbox-label"><input type="checkbox" name="scope" value="paste:read" checked> Read pastes <code class="muted">paste:read</code></label>
<label class="checkbox-label"><input type="checkbox" name="scope" value="paste:delete"> Delete pastes <code class="muted">paste:delete</code></label></div></fieldset>
<div class="form-actions"><button class="button button-primary" type="submit">` + icon("key") + `Create token</button><p class="muted">Grant only the permissions your tool needs.</p></div>
</form></section><div class="section-title"><h2>Your tokens</h2></div>
<div class="card table-scroll"><table><thead><tr><th>Name</th><th>Scopes</th><th>Created</th><th>Expires</th><th>Last used</th><th>Status</th><th>Actions</th></tr></thead><tbody>`)
	count := 0
	for rows.Next() {
		count++
		var id int64
		var name, scopes, createdAt string
		var expiresAt, lastUsedAt, revokedAt sql.NullString
		if err := rows.Scan(&id, &name, &scopes, &createdAt, &expiresAt, &lastUsedAt, &revokedAt); err != nil {
			writeUIError(w, r, "internal error", http.StatusInternalServerError)
			return
		}
		status := "active"
		if revokedAt.Valid {
			status = "revoked"
		} else if expiresAt.Valid {
			if exp, err := parseStoredTime(expiresAt.String); err == nil && time.Now().UTC().After(exp) {
				status = "expired"
			}
		}
		expDisplay := "—"
		if expiresAt.Valid {
			expDisplay = displayTime(expiresAt.String)
		}
		lastDisplay := "—"
		if lastUsedAt.Valid {
			lastDisplay = displayTime(lastUsedAt.String)
		}
		b.WriteString(`<tr><td>` + html.EscapeString(name) + `</td><td>` + html.EscapeString(scopes) +
			`</td><td>` + displayTime(createdAt) + `</td><td>` + expDisplay +
			`</td><td>` + lastDisplay + `</td><td><span class="badge">` + status + `</span></td><td>`)
		if status == "active" {
			b.WriteString(`<form method="post" data-confirm="Revoke this token? Tools using it will lose access." action="/settings/tokens/` + strconv.FormatInt(id, 10) + `/revoke">` +
				`<input type="hidden" name="csrf" value="` + html.EscapeString(sess.CSRFToken) + `">` +
				`<button class="button button-danger" type="submit">Revoke</button></form>`)
		}
		b.WriteString(`</td></tr>`)
	}
	if err := rows.Err(); err != nil {
		writeUIError(w, r, "internal error", http.StatusInternalServerError)
		return
	}
	if count == 0 {
		b.WriteString(`<tr><td colspan="7"><div class="empty-state"><span class="feature-icon">` + icon("key") + `</span><h2>No tokens yet</h2><p>Create a token above, then run <code>pbin auth login --server ` + html.EscapeString(s.cfg.BaseURL) + `</code> to connect your terminal.</p></div></td></tr>`)
	}
	b.WriteString(`</tbody></table></div>` + pageEnd)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(b.String()))
}

func (s *Server) handleSettingsTokensCreate(w http.ResponseWriter, r *http.Request) {
	sess, err := s.sessionFromRequest(r)
	if err != nil {
		writeUIError(w, r, "internal error", http.StatusInternalServerError)
		return
	}
	if sess == nil {
		writeUIError(w, r, "unauthorized", http.StatusUnauthorized)
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
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		writeUIError(w, r, "name required", http.StatusBadRequest)
		return
	}
	scopes, errMsg := normalizeScopes(r.Form["scope"])
	if errMsg != "" {
		writeUIError(w, r, errMsg, http.StatusBadRequest)
		return
	}
	var expiresAt any
	if raw := strings.TrimSpace(r.FormValue("expires_in")); raw != "" {
		ttl, err := parseDurationDays(raw)
		if err != nil || ttl < time.Second {
			writeUIError(w, r, "invalid expires_in", http.StatusBadRequest)
			return
		}
		expiresAt = time.Now().UTC().Add(ttl).Format(time.RFC3339Nano)
	}
	plaintext, err := newPATToken()
	if err != nil {
		writeUIError(w, r, "internal error", http.StatusInternalServerError)
		return
	}
	_, err = s.db.ExecContext(r.Context(),
		`INSERT INTO api_tokens (user_id, name, token_hash, scopes, expires_at) VALUES (?, ?, ?, ?, ?)`,
		sess.UserID, name, auth.HashToken(plaintext), strings.Join(scopes, " "), expiresAt,
	)
	if err != nil {
		writeUIError(w, r, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	writePage(w, "Token created", "/settings/tokens", sess, pageHeading("Developer settings", "Token created", "Copy this token now. It will not be shown again.", "")+`<section class="card max-w-2xl"><div class="card-body"><div class="alert">Treat this token like a password. Anyone with it can use the permissions you granted.</div><pre class="secret">`+html.EscapeString(plaintext)+`</pre><div class="form-actions"><button class="button button-primary" type="button" data-copy="`+html.EscapeString(plaintext)+`">`+icon("copy")+`Copy token</button><a class="button" href="/settings/tokens">Back to tokens</a></div></div></section>`)
}

func (s *Server) handleAPIRevokeToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeUIError(w, r, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sess, err := s.sessionFromRequest(r)
	if err != nil {
		writeUIError(w, r, "internal error", http.StatusInternalServerError)
		return
	}
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	if !s.validCSRF(r, sess) {
		writeUIError(w, r, "csrf required", http.StatusForbidden)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "invalid token id")
		return
	}
	if err := s.revokeOwnToken(r, sess.UserID, id); err != nil {
		if err == sql.ErrNoRows {
			writeJSONError(w, http.StatusNotFound, "not_found", "token not found")
			return
		}
		writeUIError(w, r, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSettingsTokenRevoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeUIError(w, r, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sess, err := s.sessionFromRequest(r)
	if err != nil {
		writeUIError(w, r, "internal error", http.StatusInternalServerError)
		return
	}
	if sess == nil {
		writeUIError(w, r, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !s.validCSRF(r, sess) {
		writeUIError(w, r, "csrf required", http.StatusForbidden)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeUIError(w, r, "bad request", http.StatusBadRequest)
		return
	}
	if err := s.revokeOwnToken(r, sess.UserID, id); err != nil {
		if err == sql.ErrNoRows {
			writeUIError(w, r, "not found", http.StatusNotFound)
			return
		}
		writeUIError(w, r, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/settings/tokens", http.StatusSeeOther)
}

func (s *Server) revokeOwnToken(r *http.Request, userID, tokenID int64) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(r.Context(),
		`UPDATE api_tokens SET revoked_at = ? WHERE id = ? AND user_id = ? AND revoked_at IS NULL`,
		now, tokenID, userID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func normalizeScopes(raw []string) ([]string, string) {
	seen := make(map[string]bool)
	out := make([]string, 0, len(raw))
	for _, s := range raw {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if !allowedPATScopes[s] {
			return nil, "unknown scope: " + s
		}
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil, "at least one scope is required"
	}
	return out, ""
}

func splitScopes(s string) []string {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return []string{}
	}
	return fields
}

func newPATToken() (string, error) {
	raw := make([]byte, 32) // 256 bits
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "pb_" + base64.RawURLEncoding.EncodeToString(raw), nil
}

func parseStoredTime(raw string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, raw)
}
