package app

import (
	"database/sql"
	"html"
	"net/http"
	"strconv"
	"strings"
)

type adminPasteMeta struct {
	PublicID       string
	Title          sql.NullString
	CreatedAt      string
	ExpiresAt      string
	TotalFiles     int
	TotalBytes     int
	ProtectionMode string
	OwnerUsername  sql.NullString
}

func (s *Server) handleAdminPastes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sess := s.requireAdmin(w, r)
	if sess == nil {
		return
	}

	publicID := strings.TrimSpace(r.URL.Query().Get("id"))
	if publicID == "" {
		publicID = strings.TrimSpace(r.URL.Query().Get("public_id"))
	}

	var resultHTML string
	if publicID != "" {
		meta, err := s.loadAdminPasteMeta(r, publicID)
		if err == sql.ErrNoRows {
			resultHTML = `<p>No paste found for that ID.</p>`
		} else if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		} else {
			title := "(untitled)"
			if meta.Title.Valid && meta.Title.String != "" {
				title = meta.Title.String
			}
			owner := "(none)"
			if meta.OwnerUsername.Valid {
				owner = meta.OwnerUsername.String
			}
			resultHTML = `<h2>Paste metadata</h2>
<dl>
<dt>ID</dt><dd>` + html.EscapeString(meta.PublicID) + `</dd>
<dt>Title</dt><dd>` + html.EscapeString(title) + `</dd>
<dt>Owner</dt><dd>` + html.EscapeString(owner) + `</dd>
<dt>Created</dt><dd>` + html.EscapeString(meta.CreatedAt) + `</dd>
<dt>Expires</dt><dd>` + html.EscapeString(meta.ExpiresAt) + `</dd>
<dt>Files</dt><dd>` + strconv.Itoa(meta.TotalFiles) + `</dd>
<dt>Bytes</dt><dd>` + strconv.Itoa(meta.TotalBytes) + `</dd>
<dt>Protection</dt><dd>` + html.EscapeString(meta.ProtectionMode) + `</dd>
</dl>
<form method="post" action="/admin/pastes/` + html.EscapeString(meta.PublicID) + `/delete">
<input type="hidden" name="csrf" value="` + html.EscapeString(sess.CSRFToken) + `">
<button type="submit">Delete paste</button>
</form>`
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!DOCTYPE html><html><head><title>Paste moderation</title></head><body>
<h1>Paste moderation</h1>
<p>Look up a paste by exact public ID. There is no sitewide paste browser.</p>
<form method="get" action="/admin/pastes">
<label>Paste ID <input type="text" name="id" value="` + html.EscapeString(publicID) + `" required></label>
<button type="submit">Look up</button>
</form>
` + resultHTML + `
<p><a href="/">Home</a> · <a href="/admin/invites">Invites</a></p>
</body></html>`))
}

func (s *Server) handleAdminPasteDelete(w http.ResponseWriter, r *http.Request) {
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
	publicID := r.PathValue("id")
	ok, err := s.deletePasteByPublicID(r, publicID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/admin/pastes", http.StatusSeeOther)
}

func (s *Server) loadAdminPasteMeta(r *http.Request, publicID string) (*adminPasteMeta, error) {
	var meta adminPasteMeta
	err := s.db.QueryRowContext(r.Context(), `
SELECT p.public_id, p.title, p.created_at, p.expires_at, p.total_files, p.total_bytes, p.protection_mode, u.username
FROM pastes p
LEFT JOIN users u ON u.id = p.owner_user_id
WHERE p.public_id = ?`, publicID,
	).Scan(
		&meta.PublicID, &meta.Title, &meta.CreatedAt, &meta.ExpiresAt,
		&meta.TotalFiles, &meta.TotalBytes, &meta.ProtectionMode, &meta.OwnerUsername,
	)
	if err != nil {
		return nil, err
	}
	return &meta, nil
}

func (s *Server) deletePasteByPublicID(r *http.Request, publicID string) (bool, error) {
	res, err := s.db.ExecContext(r.Context(), `DELETE FROM pastes WHERE public_id = ?`, publicID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
