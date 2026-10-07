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
		writeUIError(w, r, "method not allowed", http.StatusMethodNotAllowed)
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
			resultHTML = `<div class="card empty-state"><span class="feature-icon">` + icon("file") + `</span><h2>No paste found for that ID.</h2><p>Check the exact ID from the paste link and try again.</p></div>`
		} else if err != nil {
			writeUIError(w, r, "internal error", http.StatusInternalServerError)
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
			resultHTML = `<section class="card"><div class="card-header"><h2>Paste metadata</h2><span class="badge">Metadata only</span></div><div class="card-body">
<dl class="metadata">
<dt>ID</dt><dd>` + html.EscapeString(meta.PublicID) + `</dd>
<dt>Title</dt><dd>` + html.EscapeString(title) + `</dd>
<dt>Owner</dt><dd>` + html.EscapeString(owner) + `</dd>
<dt>Created</dt><dd>` + displayTime(meta.CreatedAt) + `</dd>
<dt>Expires</dt><dd>` + displayTime(meta.ExpiresAt) + `</dd>
<dt>Files</dt><dd>` + strconv.Itoa(meta.TotalFiles) + `</dd>
<dt>Bytes</dt><dd>` + strconv.Itoa(meta.TotalBytes) + `</dd>
<dt>Protection</dt><dd>` + html.EscapeString(meta.ProtectionMode) + `</dd>
</dl>
<form class="form-actions" method="post" data-confirm="Delete this paste permanently? Its shared link will stop working." action="/admin/pastes/` + html.EscapeString(meta.PublicID) + `/delete">
<input type="hidden" name="csrf" value="` + html.EscapeString(sess.CSRFToken) + `">
<button class="button button-danger" type="submit">` + icon("trash") + `Delete paste</button>
</form></div></section>`
		}
	}

	writePage(w, "Paste moderation", "/admin/pastes", sess, pageHeading("Workspace administration", "Paste moderation", "Look up a paste by exact public ID. Inspect safe metadata and remove a share when needed.", "")+`<div class="max-w-3xl"><section class="card mb-6"><div class="card-body"><form class="stack" method="get" action="/admin/pastes"><label>Paste ID <input type="text" name="id" value="`+html.EscapeString(publicID)+`" placeholder="The ID after /p/ in a paste link" required></label><div class="flex flex-wrap items-center gap-3"><button class="button button-primary" type="submit">Look up `+icon("arrow")+`</button><p class="muted">There is no sitewide paste browser. File contents stay private.</p></div></form></div></section>`+resultHTML+`</div>`)
}

func (s *Server) handleAdminPasteDelete(w http.ResponseWriter, r *http.Request) {
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
	publicID := r.PathValue("id")
	ok, err := s.deletePasteByPublicID(r, publicID)
	if err != nil {
		writeUIError(w, r, "internal error", http.StatusInternalServerError)
		return
	}
	if !ok {
		writeUIError(w, r, "Paste not found.", http.StatusNotFound)
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
