package app

import (
	"encoding/json"
	"io"
	"net/http"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/harshalranjhani/paste/internal/auth"
)

type bundleMetadata struct {
	pasteSlugOptions
	Title         string `json:"title"`
	ExpiresIn     string `json:"expires_in"`
	Password      string `json:"password"`
	BurnAfterRead bool   `json:"burn_after_read"`
}

type bundleManifestEntry struct {
	Part string `json:"part"`
	Path string `json:"path"`
}

type pasteFileInput struct {
	Path    string
	Content string
}

func (s *Server) handleAPICreatePasteBundle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sess := s.requireAPIAuth(w, r, scopePasteCreate)
	if sess == nil {
		return
	}

	// Bound total request size: total paste bytes + overhead for multipart framing.
	r.Body = http.MaxBytesReader(w, r.Body, int64(maxPasteBytes)+int64(maxFilesPerPaste)*1024+1<<20)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "could not parse multipart body")
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	metaRaw := r.FormValue("metadata")
	var meta bundleMetadata
	if metaRaw != "" {
		if !utf8.ValidString(metaRaw) {
			writeJSONError(w, http.StatusBadRequest, "bad_request", "metadata must be valid UTF-8")
			return
		}
		if err := json.Unmarshal([]byte(metaRaw), &meta); err != nil {
			writeJSONError(w, http.StatusBadRequest, "bad_request", "invalid metadata JSON")
			return
		}
	}

	manifestRaw := r.FormValue("manifest")
	if manifestRaw == "" {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "manifest is required")
		return
	}
	if !utf8.ValidString(manifestRaw) {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "manifest must be valid UTF-8")
		return
	}
	var manifest []bundleManifestEntry
	if err := json.Unmarshal([]byte(manifestRaw), &manifest); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "invalid manifest JSON")
		return
	}
	if len(manifest) == 0 {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "manifest must include at least one file")
		return
	}
	if len(manifest) > maxFilesPerPaste {
		writeJSONError(w, http.StatusBadRequest, "too_many_files", "file count exceeds limit")
		return
	}

	files := make([]pasteFileInput, 0, len(manifest))
	seenPaths := make(map[string]struct{}, len(manifest))
	seenParts := make(map[string]struct{}, len(manifest))
	totalBytes := 0

	for _, entry := range manifest {
		part := strings.TrimSpace(entry.Part)
		if part == "" {
			writeJSONError(w, http.StatusBadRequest, "invalid_manifest", "manifest entry missing part")
			return
		}
		if _, dup := seenParts[part]; dup {
			writeJSONError(w, http.StatusBadRequest, "invalid_manifest", "duplicate manifest part")
			return
		}
		seenParts[part] = struct{}{}

		normalized, err := normalizePastePath(entry.Path)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_path", err.Error())
			return
		}
		if _, dup := seenPaths[normalized]; dup {
			writeJSONError(w, http.StatusBadRequest, "duplicate_path", "duplicate path")
			return
		}
		seenPaths[normalized] = struct{}{}

		fh, _, err := r.FormFile(part)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "missing_file", "missing file part: "+part)
			return
		}
		contentBytes, err := io.ReadAll(io.LimitReader(fh, int64(maxFileBytes)+1))
		_ = fh.Close()
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "bad_request", "could not read file part")
			return
		}
		if len(contentBytes) > maxFileBytes {
			writeJSONError(w, http.StatusBadRequest, "too_large", "file exceeds size limit")
			return
		}
		if !utf8.Valid(contentBytes) {
			writeJSONError(w, http.StatusBadRequest, "invalid_content", "content must be valid UTF-8 text")
			return
		}
		totalBytes += len(contentBytes)
		if totalBytes > maxPasteBytes {
			writeJSONError(w, http.StatusBadRequest, "too_large", "paste total size exceeds limit")
			return
		}
		files = append(files, pasteFileInput{
			Path:    normalized,
			Content: string(contentBytes),
		})
	}

	ttl := defaultPasteTTL
	if meta.ExpiresIn != "" {
		parsed, err := parseDurationDays(meta.ExpiresIn)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_expires_in", "invalid expires_in")
			return
		}
		if parsed > maxPasteTTL {
			writeJSONError(w, http.StatusBadRequest, "expires_too_long", "expires_in exceeds maximum of 90 days")
			return
		}
		if parsed < time.Second {
			writeJSONError(w, http.StatusBadRequest, "invalid_expires_in", "expires_in too short")
			return
		}
		ttl = parsed
	}

	publicID, err := s.insertPasteFiles(r, sess.UserID, strings.TrimSpace(meta.Title), meta.Password, ttl, files, meta.BurnAfterRead, meta.pasteSlugOptions)
	if err != nil {
		writePasteCreateError(w, r, err)
		return
	}

	expiresAt := time.Now().UTC().Add(ttl)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":         publicID,
		"url":        s.cfg.BaseURL + "/p/" + publicID,
		"expires_at": expiresAt.Format(time.RFC3339Nano),
		"files":      len(files),
		"bytes":      totalBytes,
	})
}

func (s *Server) insertPasteFiles(r *http.Request, ownerID int64, title, password string, ttl time.Duration, files []pasteFileInput, burnAfterRead bool, slug pasteSlugOptions) (string, error) {
	if len(files) == 0 {
		return "", errString("no files")
	}
	expiresAt := time.Now().UTC().Add(ttl)
	publicID, err := slug.publicID()
	if err != nil {
		return "", err
	}
	totalBytes := 0
	for _, f := range files {
		totalBytes += len(f.Content)
	}
	protectionMode := "open"
	var passwordHash any
	if password != "" {
		hash, err := auth.HashPassword(password)
		if err != nil {
			return "", err
		}
		protectionMode = "password"
		passwordHash = hash
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()

	var pasteID int64
	for attempt := 0; attempt < 5; attempt++ {
		res, err := tx.ExecContext(r.Context(), `
INSERT INTO pastes (public_id, owner_user_id, title, protection_mode, password_hash, expires_at, total_files, total_bytes, burn_after_read)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(public_id) DO NOTHING`,
			publicID, ownerID, nullIfEmpty(title), protectionMode, passwordHash, expiresAt.Format(time.RFC3339Nano), len(files), totalBytes, burnAfterRead,
		)
		if err != nil {
			return "", err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return "", err
		}
		if n == 1 {
			pasteID, err = res.LastInsertId()
			if err != nil {
				return "", err
			}
			break
		}
		if slug.Slug != "" || attempt == 4 {
			return "", errSlugTaken
		}
		publicID, err = slug.publicID()
		if err != nil {
			return "", err
		}
	}
	for _, f := range files {
		displayName := path.Base(f.Path)
		size := len(f.Content)
		if _, err := tx.ExecContext(r.Context(), `
INSERT INTO paste_files (paste_id, path, display_name, mime_type, size_bytes, content)
VALUES (?, ?, ?, 'text/plain; charset=utf-8', ?, ?)`,
			pasteID, f.Path, displayName, size, f.Content,
		); err != nil {
			return "", err
		}
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return publicID, nil
}
