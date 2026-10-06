package app

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	stdlibhtml "html"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/alecthomas/chroma/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"

	"github.com/harshalranjhani/paste/internal/auth"
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
		http.Error(w, "internal error", http.StatusInternalServerError)
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
	publicID := r.PathValue("id")
	var ownerID sql.NullInt64
	err = s.db.QueryRowContext(r.Context(),
		`SELECT owner_user_id FROM pastes WHERE public_id = ?`, publicID,
	).Scan(&ownerID)
	if err == sql.ErrNoRows {
		writeJSONError(w, http.StatusNotFound, "not_found", "paste not found")
		return
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !ownerID.Valid || ownerID.Int64 != sess.UserID {
		writeJSONError(w, http.StatusForbidden, "forbidden", "not the paste owner")
		return
	}
	if _, err := s.db.ExecContext(r.Context(), `DELETE FROM pastes WHERE public_id = ?`, publicID); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
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
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleNewPasteForm(w http.ResponseWriter, r *http.Request) {
	sess, err := s.sessionFromRequest(r)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if sess == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!DOCTYPE html><html><head><title>New paste</title></head><body>
<h1>New paste</h1>
<form method="post" action="/new">
<input type="hidden" name="csrf" value="` + stdlibhtml.EscapeString(sess.CSRFToken) + `">
<label>Filename <input name="filename" value="paste.txt" required></label>
<label>Content <textarea name="content" rows="20" cols="80" required></textarea></label>
<label>Expires in
<select name="expires_in">
<option value="90d" selected>90 days</option>
<option value="30d">30 days</option>
<option value="7d">7 days</option>
<option value="1d">1 day</option>
</select>
</label>
<label>Password (optional) <input type="password" name="password" autocomplete="new-password"></label>
<p>Unlisted: anyone with the link can view. Not indexed or listed publicly.</p>
<button type="submit">Create</button>
</form>
</body></html>`))
}

func (s *Server) handleNewPasteCreate(w http.ResponseWriter, r *http.Request) {
	sess, err := s.sessionFromRequest(r)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if sess == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
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
	filename := strings.TrimSpace(r.FormValue("filename"))
	content := r.FormValue("content")
	expiresIn := strings.TrimSpace(r.FormValue("expires_in"))
	password := r.FormValue("password")
	if filename == "" {
		filename = "paste.txt"
	}
	if strings.Contains(filename, "/") || strings.Contains(filename, "\\") || filename == ".." || filename == "." {
		http.Error(w, "invalid filename", http.StatusBadRequest)
		return
	}
	if !utf8.ValidString(content) {
		http.Error(w, "content must be valid UTF-8 text", http.StatusBadRequest)
		return
	}
	if len(content) > maxFileBytes {
		http.Error(w, "content too large", http.StatusBadRequest)
		return
	}
	ttl := defaultPasteTTL
	if expiresIn != "" {
		parsed, err := parseDurationDays(expiresIn)
		if err != nil {
			http.Error(w, "invalid expires_in", http.StatusBadRequest)
			return
		}
		if parsed > maxPasteTTL {
			http.Error(w, "expires_in exceeds maximum of 90 days", http.StatusBadRequest)
			return
		}
		if parsed < time.Second {
			http.Error(w, "expires_in too short", http.StatusBadRequest)
			return
		}
		ttl = parsed
	}
	publicID, err := s.insertPaste(r, sess.UserID, filename, content, "", password, ttl)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/p/"+publicID, http.StatusSeeOther)
}

func (s *Server) insertPaste(r *http.Request, ownerID int64, filename, content, title, password string, ttl time.Duration) (string, error) {
	expiresAt := time.Now().UTC().Add(ttl)
	publicID, err := newPublicID()
	if err != nil {
		return "", err
	}
	size := len(content)
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

	res, err := tx.ExecContext(r.Context(), `
INSERT INTO pastes (public_id, owner_user_id, title, protection_mode, password_hash, expires_at, total_files, total_bytes)
VALUES (?, ?, ?, ?, ?, ?, 1, ?)`,
		publicID, ownerID, nullIfEmpty(title), protectionMode, passwordHash, expiresAt.Format(time.RFC3339Nano), size,
	)
	if err != nil {
		return "", err
	}
	pasteID, err := res.LastInsertId()
	if err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(r.Context(), `
INSERT INTO paste_files (paste_id, path, display_name, mime_type, size_bytes, content)
VALUES (?, ?, ?, 'text/plain; charset=utf-8', ?, ?)`,
		pasteID, filename, filename, size, content,
	); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return publicID, nil
}

func (s *Server) handlePasteView(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	publicID := r.PathValue("id")
	paste, ok := s.loadPasteMeta(w, r, publicID)
	if !ok {
		return
	}
	if paste.ProtectionMode == "password" && !s.hasPasteAccess(r, paste.ID) {
		s.writePasteLockScreen(w, publicID, "", http.StatusOK)
		return
	}
	file, ok := s.loadPasteFileBody(w, r, paste.ID)
	if !ok {
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive, nosnippet")

	highlighted := highlightCode(file.Path, file.Content)
	pasteURL := s.cfg.BaseURL + "/p/" + publicID
	rawURL := pasteURL + "/raw"
	_, _ = w.Write([]byte(`<!DOCTYPE html><html><head>
<meta name="robots" content="noindex,nofollow,noarchive,nosnippet">
<title>` + stdlibhtml.EscapeString(file.Path) + `</title>
<style>
body{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;margin:1.5rem}
h1{font-size:1rem;font-weight:600}
.actions{display:flex;gap:0.75rem;margin:0.75rem 0;font-size:0.875rem}
.chroma{overflow:auto;padding:1rem;background:#f6f8fa}
.chroma .line{display:flex}
.chroma .ln{user-select:none;min-width:3ch;padding-right:1rem;text-align:right;opacity:.5}
</style>
</head><body>
<h1>` + stdlibhtml.EscapeString(file.Path) + `</h1>
<p class="actions">
<a href="` + stdlibhtml.EscapeString(rawURL) + `">Raw</a>
<button type="button" id="copy-url" data-url="` + stdlibhtml.EscapeString(pasteURL) + `">Copy URL</button>
<button type="button" id="copy-contents">Copy contents</button>
</p>
` + highlighted + `
<script>
document.getElementById('copy-url').addEventListener('click',function(){
  navigator.clipboard.writeText(this.getAttribute('data-url'));
});
document.getElementById('copy-contents').addEventListener('click',function(){
  var el=document.getElementById('paste-content');
  navigator.clipboard.writeText(el?el.innerText:'');
});
</script>
</body></html>`))
}

func (s *Server) writePasteLockScreen(w http.ResponseWriter, publicID, errMsg string, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive, nosnippet")
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	msg := ""
	if errMsg != "" {
		msg = `<p class="error">` + stdlibhtml.EscapeString(errMsg) + `</p>`
	}
	_, _ = w.Write([]byte(`<!DOCTYPE html><html><head>
<meta name="robots" content="noindex,nofollow,noarchive,nosnippet">
<title>Password required</title>
<style>
body{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;margin:1.5rem}
.error{color:#a40}
</style>
</head><body>
<h1>Password required</h1>
<p>This paste is locked. Enter the password to continue.</p>
` + msg + `
<form method="post" action="/p/` + stdlibhtml.EscapeString(publicID) + `/unlock">
<label>Password <input type="password" name="password" required autocomplete="current-password"></label>
<button type="submit">Unlock</button>
</form>
</body></html>`))
}

func (s *Server) handleAPIPasteMeta(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
		file, ok := s.loadPasteFileBody(w, r, paste.ID)
		if !ok {
			return
		}
		resp["files"] = []map[string]any{
			{"path": file.Path, "size_bytes": len(file.Content)},
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive, nosnippet")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handlePasteRaw(w http.ResponseWriter, r *http.Request) {
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
	ExpiresAt      string
	ProtectionMode string
	PasswordHash   sql.NullString
}

type pasteFileRow struct {
	Path    string
	Content string
}

func (s *Server) loadPasteMeta(w http.ResponseWriter, r *http.Request, publicID string) (*pasteRow, bool) {
	if publicID == "" {
		http.NotFound(w, r)
		return nil, false
	}
	var paste pasteRow
	err := s.db.QueryRowContext(r.Context(),
		`SELECT id, public_id, expires_at, protection_mode, password_hash FROM pastes WHERE public_id = ?`,
		publicID,
	).Scan(&paste.ID, &paste.PublicID, &paste.ExpiresAt, &paste.ProtectionMode, &paste.PasswordHash)
	if err == sql.ErrNoRows {
		http.NotFound(w, r)
		return nil, false
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return nil, false
	}
	if expired, err := isExpired(paste.ExpiresAt); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return nil, false
	} else if expired {
		http.Error(w, "gone", http.StatusGone)
		return nil, false
	}
	return &paste, true
}

func (s *Server) loadPasteFileBody(w http.ResponseWriter, r *http.Request, pasteID int64) (*pasteFileRow, bool) {
	var file pasteFileRow
	err := s.db.QueryRowContext(r.Context(),
		`SELECT path, content FROM paste_files WHERE paste_id = ? ORDER BY id LIMIT 1`, pasteID,
	).Scan(&file.Path, &file.Content)
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

func highlightCode(filename, content string) string {
	lexer := lexers.Match(filename)
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
