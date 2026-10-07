package app

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	stdlibhtml "html"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/alecthomas/chroma/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

const (
	defaultPasteTTL = 90 * 24 * time.Hour
	maxPasteTTL     = 90 * 24 * time.Hour
	maxFileBytes    = 2 << 20 // 2 MiB
	base62Alphabet  = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
)

type createPasteRequest struct {
	Filename  string `json:"filename"`
	Content   string `json:"content"`
	Title     string `json:"title"`
	ExpiresIn string `json:"expires_in"`
	Password  string `json:"password"`
}

func (s *Server) handleAPICreatePaste(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeUIError(w, r, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sess := s.requireAPIAuth(w, r, scopePasteCreate)
	if sess == nil {
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxFileBytes+4096))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "could not read body")
		return
	}
	if !utf8.Valid(body) {
		writeJSONError(w, http.StatusBadRequest, "invalid_content", "content must be valid UTF-8 text")
		return
	}
	var req createPasteRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return
	}
	filename := strings.TrimSpace(req.Filename)
	if filename == "" {
		filename = "paste.txt"
	}
	if strings.Contains(filename, "/") || strings.Contains(filename, "\\") || filename == ".." || filename == "." {
		writeJSONError(w, http.StatusBadRequest, "invalid_filename", "filename must be a single path segment")
		return
	}
	if !utf8.ValidString(req.Content) {
		writeJSONError(w, http.StatusBadRequest, "invalid_content", "content must be valid UTF-8 text")
		return
	}
	if len(req.Content) > maxFileBytes {
		writeJSONError(w, http.StatusBadRequest, "too_large", "content exceeds size limit")
		return
	}

	ttl := defaultPasteTTL
	if req.ExpiresIn != "" {
		parsed, err := parseDurationDays(req.ExpiresIn)
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
	expiresAt := time.Now().UTC().Add(ttl)
	title := strings.TrimSpace(req.Title)
	size := len(req.Content)

	publicID, err := s.insertPaste(r, sess.UserID, filename, req.Content, title, req.Password, ttl)
	if err != nil {
		writeUIError(w, r, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":         publicID,
		"url":        s.cfg.BaseURL + "/p/" + publicID,
		"expires_at": expiresAt.Format(time.RFC3339Nano),
		"files":      1,
		"bytes":      size,
	})
}

func (s *Server) handleAPIDeletePaste(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeUIError(w, r, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sess := s.requireAPIAuth(w, r, scopePasteDelete)
	if sess == nil {
		return
	}
	publicID := r.PathValue("id")
	var ownerID sql.NullInt64
	err := s.db.QueryRowContext(r.Context(),
		`SELECT owner_user_id FROM pastes WHERE public_id = ?`, publicID,
	).Scan(&ownerID)
	if err == sql.ErrNoRows {
		writeJSONError(w, http.StatusNotFound, "not_found", "paste not found")
		return
	}
	if err != nil {
		writeUIError(w, r, "internal error", http.StatusInternalServerError)
		return
	}
	isOwner := ownerID.Valid && ownerID.Int64 == sess.UserID
	isAdmin := sess.Role == "admin"
	if !isOwner && !isAdmin {
		writeJSONError(w, http.StatusForbidden, "forbidden", "not the paste owner")
		return
	}
	if _, err := s.db.ExecContext(r.Context(), `DELETE FROM pastes WHERE public_id = ?`, publicID); err != nil {
		writeUIError(w, r, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleNewPaste(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleNewPasteForm(w, r)
	case http.MethodPost:
		s.handleNewPasteCreate(w, r)
	default:
		writeUIError(w, r, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleNewPasteForm(w http.ResponseWriter, r *http.Request) {
	sess, err := s.sessionFromRequest(r)
	if err != nil {
		writeUIError(w, r, "internal error", http.StatusInternalServerError)
		return
	}
	if sess == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	writePage(w, "New paste", "", sess, pageHeading("Create something", "New paste", "A quick snippet or a collection of files. Give it a name, then share it with one link.", "")+`
<form method="post" action="/new" id="paste-form">
<input type="hidden" name="csrf" value="`+stdlibhtml.EscapeString(sess.CSRFToken)+`">
<div class="editor-layout"><div>
<label class="mb-5">Title <span class="muted">Optional, but useful for finding this paste later.</span><input name="title" placeholder="e.g. A tiny HTTP server" maxlength="200"></label>
<div class="section-title"><h2>Files <span class="badge" id="file-count">1 file</span></h2><button class="button" type="button" id="add-file">`+icon("plus")+`Add file</button></div>
<div id="files"><section class="file-row card"><div class="card-header">
<label class="file-path">`+icon("file")+`<span class="sr-only">Path</span><input name="path" value="paste.txt" placeholder="src/main.go" required aria-label="File path"></label>
<button class="button button-ghost remove-file" type="button" disabled aria-label="Remove file">Remove</button></div>
<label><span class="sr-only">Content</span><textarea name="content" rows="14" aria-label="File contents" placeholder="Paste your code or text here…" spellcheck="false"></textarea></label>
</section></div><p class="muted">Use relative paths like src/main.go to create folders. Text files only, up to 2 MB each.</p>
</div><aside class="card editor-settings"><div class="card-header"><h2>Sharing settings</h2></div><div class="card-body stack">
<div><span class="badge badge-green">`+icon("lock")+`Unlisted</span><p class="muted mt-3">Anyone with the link can view. Your paste won’t appear in a public list or search engine.</p></div>
<label>Expires in<select name="expires_in"><option value="90d" selected>90 days</option><option value="30d">30 days</option><option value="7d">7 days</option><option value="1d">1 day</option></select></label>
<label>Password <span class="muted">Optional. Require a password to view.</span><input type="password" name="password" autocomplete="new-password" placeholder="Add a password"></label>
<button class="button button-primary" type="submit">Create paste `+icon("arrow")+`</button><p class="muted">Pastes are read only after creation. Double-check your files before sharing.</p>
</div></aside></div></form>`)

}

func (s *Server) handleNewPasteCreate(w http.ResponseWriter, r *http.Request) {
	sess, err := s.sessionFromRequest(r)
	if err != nil {
		writeUIError(w, r, "internal error", http.StatusInternalServerError)
		return
	}
	if sess == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
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
	expiresIn := strings.TrimSpace(r.FormValue("expires_in"))
	password := r.FormValue("password")

	paths := r.Form["path"]
	contents := r.Form["content"]
	// Backward compatible single-file field names.
	if len(paths) == 0 {
		if filename := strings.TrimSpace(r.FormValue("filename")); filename != "" {
			paths = []string{filename}
			contents = []string{r.FormValue("content")}
		}
	}
	if len(paths) == 0 {
		writeUIError(w, r, "at least one file is required", http.StatusBadRequest)
		return
	}
	if len(contents) != len(paths) {
		writeUIError(w, r, "path and content counts must match", http.StatusBadRequest)
		return
	}
	if len(paths) > maxFilesPerPaste {
		writeUIError(w, r, "too many files", http.StatusBadRequest)
		return
	}

	files := make([]pasteFileInput, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	totalBytes := 0
	for i, rawPath := range paths {
		content := contents[i]
		normalized, err := normalizePastePath(strings.TrimSpace(rawPath))
		if err != nil {
			// Single-segment filenames without slash are allowed (legacy).
			filename := strings.TrimSpace(rawPath)
			if filename == "" {
				filename = "paste.txt"
			}
			if strings.Contains(filename, "/") || strings.Contains(filename, "\\") || filename == ".." || filename == "." {
				writeUIError(w, r, "invalid path", http.StatusBadRequest)
				return
			}
			normalized = filename
		}
		if _, dup := seen[normalized]; dup {
			writeUIError(w, r, "duplicate path", http.StatusBadRequest)
			return
		}
		seen[normalized] = struct{}{}
		if !utf8.ValidString(content) {
			writeUIError(w, r, "content must be valid UTF-8 text", http.StatusBadRequest)
			return
		}
		if len(content) > maxFileBytes {
			writeUIError(w, r, "content too large", http.StatusBadRequest)
			return
		}
		totalBytes += len(content)
		if totalBytes > maxPasteBytes {
			writeUIError(w, r, "paste too large", http.StatusBadRequest)
			return
		}
		files = append(files, pasteFileInput{Path: normalized, Content: content})
	}

	ttl := defaultPasteTTL
	if expiresIn != "" {
		parsed, err := parseDurationDays(expiresIn)
		if err != nil {
			writeUIError(w, r, "invalid expires_in", http.StatusBadRequest)
			return
		}
		if parsed > maxPasteTTL {
			writeUIError(w, r, "expires_in exceeds maximum of 90 days", http.StatusBadRequest)
			return
		}
		if parsed < time.Second {
			writeUIError(w, r, "expires_in too short", http.StatusBadRequest)
			return
		}
		ttl = parsed
	}
	publicID, err := s.insertPasteFiles(r, sess.UserID, strings.TrimSpace(r.FormValue("title")), password, ttl, files)
	if err != nil {
		writeUIError(w, r, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/p/"+publicID, http.StatusSeeOther)
}

func (s *Server) insertPaste(r *http.Request, ownerID int64, filename, content, title, password string, ttl time.Duration) (string, error) {
	return s.insertPasteFiles(r, ownerID, title, password, ttl, []pasteFileInput{
		{Path: filename, Content: content},
	})
}

func (s *Server) handlePasteView(w http.ResponseWriter, r *http.Request) {
	paste, ok := s.loadPasteMeta(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	if paste.ProtectionMode == "password" && !s.hasPasteAccess(r, paste.ID) {
		if r.URL.Query().Get("partial") == "1" {
			writeJSONError(w, http.StatusUnauthorized, "password_required", "password required")
		} else {
			s.writePasteLockScreen(w, paste.PublicID, "", http.StatusOK)
		}
		return
	}
	files, ok := s.loadPasteFileList(w, r, paste.ID)
	if !ok {
		return
	}
	if len(files) == 0 {
		writeUIError(w, r, "Paste not found.", http.StatusNotFound)
		return
	}
	fileID := files[0].ID
	if rawID := r.URL.Query().Get("file_id"); rawID != "" {
		var err error
		fileID, err = strconv.ParseInt(rawID, 10, 64)
		if err != nil || fileID <= 0 {
			writeUIError(w, r, "Paste not found.", http.StatusNotFound)
			return
		}
	}
	file, ok := s.loadPasteFileByID(w, r, paste.ID, fileID)
	if !ok {
		return
	}
	w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive, nosnippet")
	w.Header().Set("Cache-Control", "no-store")
	language := ""
	if lexer := lexers.Get(r.URL.Query().Get("language")); lexer != nil {
		language = lexer.Config().Name
	}
	w.Header().Set("X-Syntax-Language", language)
	if r.URL.Query().Get("partial") == "1" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(highlightCode(file.Path, file.Content, language)))
		return
	}
	sess, err := s.sessionFromRequest(r)
	if err != nil {
		writeUIError(w, r, "internal error", http.StatusInternalServerError)
		return
	}
	s.writePasteViewer(w, sess, paste, files, file, language)
}

func (s *Server) writePasteLockScreen(w http.ResponseWriter, publicID, errMsg string, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive, nosnippet")
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	msg := ""
	if errMsg != "" {
		msg = `<p class="alert alert-error" role="alert">` + stdlibhtml.EscapeString(errMsg) + `</p>`
	}
	writePage(w, "Password required", "", nil, `<div class="auth-layout"><div class="auth-intro"><span class="brand-mark">`+icon("lock")+`</span><p class="eyebrow">A little extra privacy</p><h1>This paste is password protected.</h1><p class="description">Ask the person who shared this link for the password. The files stay private until you unlock them.</p></div><section class="card auth-card"><h2>Password required</h2><p class="description">Enter the password to continue.</p>`+msg+`<form class="stack" method="post" action="/p/`+stdlibhtml.EscapeString(publicID)+`/unlock"><label>Password <input type="password" name="password" required autocomplete="current-password" autofocus></label><button class="button button-primary" type="submit">`+icon("lock")+`Unlock paste</button></form></section></div>`)
}

func (s *Server) handleAPIPasteMeta(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeUIError(w, r, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	publicID := r.PathValue("id")
	paste, ok := s.loadPasteMeta(w, r, publicID)
	if !ok {
		return
	}
	passwordRequired := paste.ProtectionMode == "password"
	unlocked := !passwordRequired || s.hasPasteAccess(r, paste.ID)

	resp := map[string]any{
		"id":                publicID,
		"password_required": passwordRequired,
		"expires_at":        paste.ExpiresAt,
	}
	if unlocked {
		files, ok := s.loadPasteFileList(w, r, paste.ID)
		if !ok {
			return
		}
		out := make([]map[string]any, 0, len(files))
		for _, f := range files {
			out = append(out, map[string]any{
				"id":         f.ID,
				"path":       f.Path,
				"size_bytes": f.SizeBytes,
			})
		}
		resp["files"] = out
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive, nosnippet")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handlePasteRaw(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeUIError(w, r, "method not allowed", http.StatusMethodNotAllowed)
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
	file, ok := s.loadPasteFileBody(w, r, paste.ID)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive, nosnippet")
	_, _ = w.Write([]byte(file.Content))
}

type pasteRow struct {
	ID             int64
	PublicID       string
	Title          sql.NullString
	ExpiresAt      string
	ProtectionMode string
	PasswordHash   sql.NullString
}

type pasteFileRow struct {
	ID      int64
	Path    string
	Content string
}

type pasteFileMeta struct {
	ID        int64
	Path      string
	SizeBytes int
}

func (s *Server) loadPasteMeta(w http.ResponseWriter, r *http.Request, publicID string) (*pasteRow, bool) {
	if publicID == "" {
		writeUIError(w, r, "Paste not found.", http.StatusNotFound)
		return nil, false
	}
	var paste pasteRow
	err := s.db.QueryRowContext(r.Context(),
		`SELECT id, public_id, expires_at, protection_mode, password_hash, title FROM pastes WHERE public_id = ?`,
		publicID,
	).Scan(&paste.ID, &paste.PublicID, &paste.ExpiresAt, &paste.ProtectionMode, &paste.PasswordHash, &paste.Title)
	if err == sql.ErrNoRows {
		writeUIError(w, r, "Paste not found.", http.StatusNotFound)
		return nil, false
	}
	if err != nil {
		writeUIError(w, r, "internal error", http.StatusInternalServerError)
		return nil, false
	}
	if expired, err := isExpired(paste.ExpiresAt); err != nil {
		writeUIError(w, r, "internal error", http.StatusInternalServerError)
		return nil, false
	} else if expired {
		writeUIError(w, r, "gone", http.StatusGone)
		return nil, false
	}
	return &paste, true
}

func (s *Server) loadPasteFileBody(w http.ResponseWriter, r *http.Request, pasteID int64) (*pasteFileRow, bool) {
	var file pasteFileRow
	err := s.db.QueryRowContext(r.Context(),
		`SELECT id, path, content FROM paste_files WHERE paste_id = ? ORDER BY id LIMIT 1`, pasteID,
	).Scan(&file.ID, &file.Path, &file.Content)
	if err == sql.ErrNoRows {
		writeUIError(w, r, "Paste not found.", http.StatusNotFound)
		return nil, false
	}
	if err != nil {
		writeUIError(w, r, "internal error", http.StatusInternalServerError)
		return nil, false
	}
	return &file, true
}

func (s *Server) loadPasteFileList(w http.ResponseWriter, r *http.Request, pasteID int64) ([]pasteFileMeta, bool) {
	rows, err := s.db.QueryContext(r.Context(),
		`SELECT id, path, size_bytes FROM paste_files WHERE paste_id = ? ORDER BY path`, pasteID,
	)
	if err != nil {
		writeUIError(w, r, "internal error", http.StatusInternalServerError)
		return nil, false
	}
	defer rows.Close()
	var files []pasteFileMeta
	for rows.Next() {
		var f pasteFileMeta
		if err := rows.Scan(&f.ID, &f.Path, &f.SizeBytes); err != nil {
			writeUIError(w, r, "internal error", http.StatusInternalServerError)
			return nil, false
		}
		files = append(files, f)
	}
	if err := rows.Err(); err != nil {
		writeUIError(w, r, "internal error", http.StatusInternalServerError)
		return nil, false
	}
	if len(files) == 0 {
		writeUIError(w, r, "Paste not found.", http.StatusNotFound)
		return nil, false
	}
	return files, true
}

func highlightCode(filename, content, language string) string {
	lexer := lexers.Get(language)
	if lexer == nil {
		lexer = lexers.Match(filename)
	}
	if lexer == nil {
		lexer = lexers.Analyse(content)
	}
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)
	style := styles.Get("github")
	if style == nil {
		style = styles.Fallback
	}
	formatter := chromahtml.New(
		chromahtml.WithClasses(true),
		chromahtml.WithAllClasses(true),
		chromahtml.WithLineNumbers(true),
		chromahtml.LineNumbersInTable(false),
		chromahtml.ClassPrefix(""),
	)
	iterator, err := lexer.Tokenise(nil, content)
	if err != nil {
		return `<pre id="paste-content" class="chroma">` + stdlibhtml.EscapeString(content) + `</pre>`
	}
	var buf strings.Builder
	if err := formatter.Format(&buf, style, iterator); err != nil {
		return `<pre id="paste-content" class="chroma">` + stdlibhtml.EscapeString(content) + `</pre>`
	}
	out := buf.String()
	// Ensure the paste-content id is present for copy-contents JS.
	if strings.Contains(out, `class="chroma"`) {
		out = strings.Replace(out, `class="chroma"`, `class="chroma" id="paste-content"`, 1)
	} else {
		out = `<div id="paste-content">` + out + `</div>`
	}
	return out
}

func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func newPublicID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return encodeBase62(raw), nil
}

func encodeBase62(b []byte) string {
	// Interpret bytes as a big-endian integer and encode in base62.
	const base = 62
	digits := make([]byte, 0, 22)
	// Work on a copy as a big integer via repeated division.
	n := make([]byte, len(b))
	copy(n, b)
	for !allZero(n) {
		var rem int
		for i := 0; i < len(n); i++ {
			acc := rem*256 + int(n[i])
			n[i] = byte(acc / base)
			rem = acc % base
		}
		digits = append(digits, base62Alphabet[rem])
	}
	if len(digits) == 0 {
		return "0"
	}
	// Reverse (least-significant digit was appended first).
	for i, j := 0, len(digits)-1; i < j; i, j = i+1, j-1 {
		digits[i], digits[j] = digits[j], digits[i]
	}
	return string(digits)
}

func allZero(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}

func parseDurationDays(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "d") {
		n := strings.TrimSuffix(s, "d")
		var days int
		for _, c := range n {
			if c < '0' || c > '9' {
				return 0, errInvalidDuration
			}
			days = days*10 + int(c-'0')
		}
		if days == 0 && n != "0" {
			return 0, errInvalidDuration
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

var errInvalidDuration = errString("invalid duration")

type errString string

func (e errString) Error() string { return string(e) }

func isExpired(expiresAt string) (bool, error) {
	exp, err := time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil {
		exp, err = time.Parse(time.RFC3339, expiresAt)
		if err != nil {
			return false, err
		}
	}
	return time.Now().UTC().After(exp), nil
}
