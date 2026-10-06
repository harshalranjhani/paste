package app

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
)

func (s *Server) handleAPIPasteFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	paste, ok := s.loadPasteMeta(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	if paste.ProtectionMode == "password" && !s.hasPasteAccess(r, paste.ID) {
		writeJSONError(w, http.StatusUnauthorized, "password_required", "password required")
		return
	}
	fileID, err := strconv.ParseInt(r.PathValue("file_id"), 10, 64)
	if err != nil || fileID <= 0 {
		writeJSONError(w, http.StatusNotFound, "not_found", "file not found")
		return
	}
	file, ok := s.loadPasteFileByID(w, r, paste.ID, fileID)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive, nosnippet")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":         file.ID,
		"path":       file.Path,
		"size_bytes": len(file.Content),
		"content":    file.Content,
	})
}

func (s *Server) handleAPIPasteFileRaw(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	paste, ok := s.loadPasteMeta(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	if paste.ProtectionMode == "password" && !s.hasPasteAccess(r, paste.ID) {
		writeJSONError(w, http.StatusUnauthorized, "password_required", "password required")
		return
	}
	fileID, err := strconv.ParseInt(r.PathValue("file_id"), 10, 64)
	if err != nil || fileID <= 0 {
		http.NotFound(w, r)
		return
	}
	file, ok := s.loadPasteFileByID(w, r, paste.ID, fileID)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive, nosnippet")
	_, _ = w.Write([]byte(file.Content))
}

func (s *Server) loadPasteFileByID(w http.ResponseWriter, r *http.Request, pasteID, fileID int64) (*pasteFileRow, bool) {
	var file pasteFileRow
	err := s.db.QueryRowContext(r.Context(),
		`SELECT id, path, content FROM paste_files WHERE paste_id = ? AND id = ?`, pasteID, fileID,
	).Scan(&file.ID, &file.Path, &file.Content)
	if err == sql.ErrNoRows {
		http.NotFound(w, r)
		return nil, false
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return nil, false
	}
	return &file, true
}
