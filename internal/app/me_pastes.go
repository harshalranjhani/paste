package app

import (
	"database/sql"
	"encoding/json"
	"html"
	"net/http"
	"strconv"
	"strings"
)

type myPasteRow struct {
	PublicID       string
	Title          sql.NullString
	CreatedAt      string
	ExpiresAt      string
	TotalFiles     int
	ProtectionMode string
	FirstPath      string
}

func (s *Server) handleMePastes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
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
	pastes, err := s.listUserPastes(r, sess.UserID)
	if err != nil {
		writeUIError(w, r, "internal error", http.StatusInternalServerError)
		return
	}

	var rows strings.Builder
	for _, p := range pastes {
		title := p.FirstPath
		if p.Title.Valid && p.Title.String != "" {
			title = p.Title.String
		}
		rows.WriteString("<tr>")
		rows.WriteString(`<td><a class="paste-title" href="/p/` + html.EscapeString(p.PublicID) + `">` + html.EscapeString(title) + `</a><p class="paste-id">` + html.EscapeString(p.PublicID) + `</p></td>`)
		rows.WriteString("<td>" + displayTime(p.CreatedAt) + "</td>")
		rows.WriteString("<td>" + displayTime(p.ExpiresAt) + "</td>")
		rows.WriteString("<td>" + strconv.Itoa(p.TotalFiles) + "</td>")
		rows.WriteString("<td>" + pasteBadge(p.ProtectionMode, p.ExpiresAt) + "</td>")
		rows.WriteString(`<td><form method="post" data-confirm="Delete this paste? This cannot be undone." action="/me/pastes/` + html.EscapeString(p.PublicID) + `/delete">` +
			`<input type="hidden" name="csrf" value="` + html.EscapeString(sess.CSRFToken) + `">` +
			`<button class="button button-danger" type="submit">` + icon("trash") + `Delete</button></form></td>`)
		rows.WriteString("</tr>")
	}

	list := `<div class="card table-scroll"><table>
<thead><tr><th>Title</th><th>Created</th><th>Expires</th><th>Files</th><th>Protection</th><th>Delete</th></tr></thead>
<tbody>` + rows.String() + `</tbody>
</table></div>`
	if len(pastes) == 0 {
		list = `<div class="card empty-state"><span class="feature-icon">` + icon("file") + `</span><h2>A clean slate</h2><p>Your pastes will appear here. Create your first snippet or a collection of files.</p><a class="button button-primary" href="/new">` + icon("plus") + `New paste</a></div>`
	}
	writePage(w, "My Pastes", "/me/pastes", sess, pageHeading("Your workspace", "My Pastes", "Everything you’ve shared, in one place. Open a paste or remove a link you no longer need.", `<a class="button button-primary" href="/new">`+icon("plus")+`New paste</a>`)+list)
}

func (s *Server) handleAPIMePastes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
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
	if !sess.hasScope(scopePasteRead) {
		writeJSONError(w, http.StatusForbidden, "insufficient_scope", "missing required scope: "+scopePasteRead)
		return
	}
	if sess.ViaBearer {
		s.touchAPIToken(r, sess.TokenID)
	}
	pastes, err := s.listUserPastes(r, sess.UserID)
	if err != nil {
		writeUIError(w, r, "internal error", http.StatusInternalServerError)
		return
	}
	out := make([]map[string]any, 0, len(pastes))
	for _, p := range pastes {
		title := ""
		if p.Title.Valid {
			title = p.Title.String
		}
		out = append(out, map[string]any{
			"id":         p.PublicID,
			"title":      title,
			"created_at": p.CreatedAt,
			"expires_at": p.ExpiresAt,
			"files":      p.TotalFiles,
			"protection": p.ProtectionMode,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) listUserPastes(r *http.Request, userID int64) ([]myPasteRow, error) {
	rows, err := s.db.QueryContext(r.Context(), `
SELECT public_id, title, created_at, expires_at, total_files, protection_mode,
       COALESCE((SELECT MIN(path) FROM paste_files WHERE paste_id = pastes.id), 'Untitled paste')
FROM pastes
WHERE owner_user_id = ?
ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []myPasteRow
	for rows.Next() {
		var p myPasteRow
		if err := rows.Scan(&p.PublicID, &p.Title, &p.CreatedAt, &p.ExpiresAt, &p.TotalFiles, &p.ProtectionMode, &p.FirstPath); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Server) handleMePasteDelete(w http.ResponseWriter, r *http.Request) {
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
	publicID := r.PathValue("id")
	ok, err := s.deleteOwnedPaste(r, sess.UserID, publicID)
	if err != nil {
		writeUIError(w, r, "internal error", http.StatusInternalServerError)
		return
	}
	if !ok {
		writeUIError(w, r, "forbidden", http.StatusForbidden)
		return
	}
	http.Redirect(w, r, "/me/pastes", http.StatusSeeOther)
}

func (s *Server) deleteOwnedPaste(r *http.Request, userID int64, publicID string) (bool, error) {
	var ownerID sql.NullInt64
	err := s.db.QueryRowContext(r.Context(),
		`SELECT owner_user_id FROM pastes WHERE public_id = ?`, publicID,
	).Scan(&ownerID)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !ownerID.Valid || ownerID.Int64 != userID {
		return false, nil
	}
	if _, err := s.db.ExecContext(r.Context(), `DELETE FROM pastes WHERE public_id = ?`, publicID); err != nil {
		return false, err
	}
	return true, nil
}
