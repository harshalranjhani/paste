package app

import (
	"archive/zip"
	"io"
	"net/http"
	"path"
	"strings"
)

func (s *Server) handleAPIPasteArchive(w http.ResponseWriter, r *http.Request) {
	s.servePasteArchive(w, r, r.PathValue("id"))
}

func (s *Server) handlePasteArchive(w http.ResponseWriter, r *http.Request) {
	s.servePasteArchive(w, r, r.PathValue("id"))
}

func (s *Server) servePasteArchive(w http.ResponseWriter, r *http.Request, publicID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	paste, ok := s.loadPasteMeta(w, r, publicID)
	if !ok {
		return
	}
	if paste.ProtectionMode == "password" && !s.hasPasteAccess(r, paste.ID) {
		writeJSONError(w, http.StatusUnauthorized, "password_required", "password required")
		return
	}

	rows, err := s.db.QueryContext(r.Context(),
		`SELECT path, content FROM paste_files WHERE paste_id = ? ORDER BY path`, paste.ID,
	)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type entry struct {
		Path    string
		Content string
	}
	var entries []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.Path, &e.Content); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		safe, err := zipSafePath(e.Path)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		e.Path = safe
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if len(entries) == 0 {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+publicID+`.zip"`)
	w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive, nosnippet")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	zw := zip.NewWriter(w)
	defer func() { _ = zw.Close() }()
	for _, e := range entries {
		fw, err := zw.Create(e.Path)
		if err != nil {
			return
		}
		if _, err := io.WriteString(fw, e.Content); err != nil {
			return
		}
	}
}

func zipSafePath(p string) (string, error) {
	normalized, err := normalizePastePath(p)
	if err != nil {
		return "", err
	}
	// Defense in depth: never emit absolute or traversal names into ZIP.
	if path.IsAbs(normalized) || strings.HasPrefix(normalized, "../") || normalized == ".." {
		return "", errString("unsafe zip path")
	}
	if strings.Contains(normalized, "\\") {
		return "", errString("unsafe zip path")
	}
	return normalized, nil
}
