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
}

func (s *Server) handleMePastes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
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
	pastes, err := s.listUserPastes(r, sess.UserID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	var rows strings.Builder
	for _, p := range pastes {
		title := p.PublicID
		if p.Title.Valid && p.Title.String != "" {
			title = p.Title.String
		}
		rows.WriteString("<tr>")
		rows.WriteString("<td>" + html.EscapeString(title) + "</td>")
		rows.WriteString("<td>" + html.EscapeString(p.CreatedAt) + "</td>")
		rows.WriteString("<td>" + html.EscapeString(p.ExpiresAt) + "</td>")
		rows.WriteString("<td>" + strconv.Itoa(p.TotalFiles) + "</td>")
		rows.WriteString("<td>" + html.EscapeString(p.ProtectionMode) + "</td>")
		rows.WriteString(`<td><form method="post" action="/me/pastes/` + html.EscapeString(p.PublicID) + `/delete">` +
			`<input type="hidden" name="csrf" value="` + html.EscapeString(sess.CSRFToken) + `">` +
			`<button type="submit">Delete</button></form></td>`)
		rows.WriteString("</tr>")
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!DOCTYPE html><html><head><title>My Pastes</title></head><body>
<h1>My Pastes</h1>
<table>
<thead><tr><th>Title</th><th>Created</th><th>Expires</th><th>Files</th><th>Protection</th><th>Delete</th></tr></thead>
<tbody>` + rows.String() + `</tbody>
</table>
<p><a href="/">Home</a> · <a href="/new">New paste</a></p>
</body></html>`))
}

func (s *Server) handleAPIMePastes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
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
	if !sess.hasScope(scopePasteRead) {
		writeJSONError(w, http.StatusForbidden, "insufficient_scope", "missing required scope: "+scopePasteRead)
		return
	}
	if sess.ViaBearer {
		s.touchAPIToken(r, sess.TokenID)
	}
	pastes, err := s.listUserPastes(r, sess.UserID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
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
SELECT public_id, title, created_at, expires_at, total_files, protection_mode
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
		if err := rows.Scan(&p.PublicID, &p.Title, &p.CreatedAt, &p.ExpiresAt, &p.TotalFiles, &p.ProtectionMode); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Server) handleMePasteDelete(w http.ResponseWriter, r *http.Request) {
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
	publicID := r.PathValue("id")
	ok, err := s.deleteOwnedPaste(r, sess.UserID, publicID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
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
