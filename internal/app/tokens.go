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
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAPICreateToken(w http.ResponseWriter, r *http.Request) {
	sess, err := s.sessionFromRequest(r)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	if !s.validCSRF(r, sess) {
		http.Error(w, "csrf required", http.StatusForbidden)
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
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	res, err := s.db.ExecContext(r.Context(),
		`INSERT INTO api_tokens (user_id, name, token_hash, scopes, expires_at) VALUES (?, ?, ?, ?, ?)`,
		sess.UserID, name, auth.HashToken(plaintext), strings.Join(scopes, " "), expiresAt,
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
		http.Error(w, "internal error", http.StatusInternalServerError)
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
		http.Error(w, "internal error", http.StatusInternalServerError)
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
			http.Error(w, "internal error", http.StatusInternalServerError)
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
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSettingsTokensList(w http.ResponseWriter, r *http.Request) {
	sess, err := s.sessionFromRequest(r)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if sess == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `
SELECT id, name, scopes, created_at, expires_at, last_used_at, revoked_at
FROM api_tokens
WHERE user_id = ?
ORDER BY id DESC`, sess.UserID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var b strings.Builder
	b.WriteString(`<!DOCTYPE html><html><head><title>API tokens</title></head><body>
<h1>Personal access tokens</h1>
<p>Tokens are shown only once at creation. Store them securely.</p>
<form method="post" action="/settings/tokens">
<input type="hidden" name="csrf" value="` + html.EscapeString(sess.CSRFToken) + `">
<label>Name <input name="name" required></label>
<label><input type="checkbox" name="scope" value="paste:create" checked> paste:create</label>
<label><input type="checkbox" name="scope" value="paste:read" checked> paste:read</label>
<label><input type="checkbox" name="scope" value="paste:delete"> paste:delete</label>
<label>Expires in <input name="expires_in" placeholder="optional e.g. 30d"></label>
<button type="submit">Create token</button>
</form>
<table><thead><tr><th>Name</th><th>Scopes</th><th>Created</th><th>Expires</th><th>Last used</th><th>Status</th><th></th></tr></thead><tbody>`)
	for rows.Next() {
		var id int64
		var name, scopes, createdAt string
		var expiresAt, lastUsedAt, revokedAt sql.NullString
		if err := rows.Scan(&id, &name, &scopes, &createdAt, &expiresAt, &lastUsedAt, &revokedAt); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
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
			expDisplay = html.EscapeString(expiresAt.String)
		}
		lastDisplay := "—"
		if lastUsedAt.Valid {
			lastDisplay = html.EscapeString(lastUsedAt.String)
		}
		b.WriteString(`<tr><td>` + html.EscapeString(name) + `</td><td>` + html.EscapeString(scopes) +
			`</td><td>` + html.EscapeString(createdAt) + `</td><td>` + expDisplay +
			`</td><td>` + lastDisplay + `</td><td>` + status + `</td><td>`)
		if status == "active" {
			b.WriteString(`<form method="post" action="/settings/tokens/` + strconv.FormatInt(id, 10) + `/revoke">` +
				`<input type="hidden" name="csrf" value="` + html.EscapeString(sess.CSRFToken) + `">` +
				`<button type="submit">Revoke</button></form>`)
		}
		b.WriteString(`</td></tr>`)
	}
	b.WriteString(`</tbody></table>
<p><a href="/">Home</a></p>
</body></html>`)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
}

func (s *Server) handleSettingsTokensCreate(w http.ResponseWriter, r *http.Request) {
	sess, err := s.sessionFromRequest(r)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if sess == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
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
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	scopes, errMsg := normalizeScopes(r.Form["scope"])
	if errMsg != "" {
		http.Error(w, errMsg, http.StatusBadRequest)
		return
	}
	var expiresAt any
	if raw := strings.TrimSpace(r.FormValue("expires_in")); raw != "" {
		ttl, err := parseDurationDays(raw)
		if err != nil || ttl < time.Second {
			http.Error(w, "invalid expires_in", http.StatusBadRequest)
			return
		}
		expiresAt = time.Now().UTC().Add(ttl).Format(time.RFC3339Nano)
	}
	plaintext, err := newPATToken()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	_, err = s.db.ExecContext(r.Context(),
		`INSERT INTO api_tokens (user_id, name, token_hash, scopes, expires_at) VALUES (?, ?, ?, ?, ?)`,
		sess.UserID, name, auth.HashToken(plaintext), strings.Join(scopes, " "), expiresAt,
	)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!DOCTYPE html><html><head><title>Token created</title></head><body>
<h1>Token created</h1>
<p>Copy this token now. It will not be shown again.</p>
<pre>` + html.EscapeString(plaintext) + `</pre>
<p><a href="/settings/tokens">Back to tokens</a></p>
</body></html>`))
}

func (s *Server) handleAPIRevokeToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sess, err := s.sessionFromRequest(r)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	if !s.validCSRF(r, sess) {
		http.Error(w, "csrf required", http.StatusForbidden)
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
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSettingsTokenRevoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sess, err := s.sessionFromRequest(r)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if sess == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !s.validCSRF(r, sess) {
		http.Error(w, "csrf required", http.StatusForbidden)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := s.revokeOwnToken(r, sess.UserID, id); err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
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
