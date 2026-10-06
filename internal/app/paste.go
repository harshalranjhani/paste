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
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	isOwner := ownerID.Valid && ownerID.Int64 == sess.UserID
	isAdmin := sess.Role == "admin"
	if !isOwner && !isAdmin {
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
	_, _ = w.Write([]byte(`<!DOCTYPE html><html><head>
<title>New paste</title>
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>
body{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;margin:1.5rem;max-width:48rem}
label{display:block;margin:0.75rem 0}
.file-row{border:1px solid #ccc;padding:0.75rem;margin:0.75rem 0}
button{font:inherit}
</style>
</head><body>
<h1>New paste</h1>
<form method="post" action="/new" id="paste-form">
<input type="hidden" name="csrf" value="` + stdlibhtml.EscapeString(sess.CSRFToken) + `">
<div id="files">
<div class="file-row">
<label>Path <input name="path" value="paste.txt" required></label>
<label>Content <textarea name="content" rows="12" cols="80" required></textarea></label>
</div>
</div>
<p><button type="button" id="add-file">Add file</button></p>
<label>Expires in
<select name="expires_in">
<option value="90d" selected>90 days</option>
<option value="30d">30 days</option>
<option value="7d">7 days</option>
<option value="1d">1 day</option>
</select>
</label>
<label>Password (optional) <input type="password" name="password" autocomplete="new-password"></label>
<p>Unlisted: anyone with the link can view. Not indexed or listed publicly. Add multiple files with relative paths (no directory picker required).</p>
<button type="submit">Create</button>
</form>
<script>
document.getElementById('add-file').addEventListener('click',function(){
  var row=document.createElement('div');
  row.className='file-row';
  row.innerHTML='<label>Path <input name="path" required></label><label>Content <textarea name="content" rows="8" cols="80" required></textarea></label><button type="button" class="remove-file">Remove</button>';
  row.querySelector('.remove-file').addEventListener('click',function(){ row.remove(); });
  document.getElementById('files').appendChild(row);
});
</script>
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
		http.Error(w, "at least one file is required", http.StatusBadRequest)
		return
	}
	if len(contents) != len(paths) {
		http.Error(w, "path and content counts must match", http.StatusBadRequest)
		return
	}
	if len(paths) > maxFilesPerPaste {
		http.Error(w, "too many files", http.StatusBadRequest)
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
				http.Error(w, "invalid path", http.StatusBadRequest)
				return
			}
			normalized = filename
		}
		if _, dup := seen[normalized]; dup {
			http.Error(w, "duplicate path", http.StatusBadRequest)
			return
		}
		seen[normalized] = struct{}{}
		if !utf8.ValidString(content) {
			http.Error(w, "content must be valid UTF-8 text", http.StatusBadRequest)
			return
		}
		if len(content) > maxFileBytes {
			http.Error(w, "content too large", http.StatusBadRequest)
			return
		}
		totalBytes += len(content)
		if totalBytes > maxPasteBytes {
			http.Error(w, "paste too large", http.StatusBadRequest)
			return
		}
		files = append(files, pasteFileInput{Path: normalized, Content: content})
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
	publicID, err := s.insertPasteFiles(r, sess.UserID, "", password, ttl, files)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
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
	files, ok := s.loadPasteFileList(w, r, paste.ID)
	if !ok {
		return
	}

	// Fragment for lazy tree pane highlighting.
	if r.URL.Query().Get("partial") == "1" {
		fileID, err := strconv.ParseInt(r.URL.Query().Get("file_id"), 10, 64)
		if err != nil || fileID <= 0 {
			http.NotFound(w, r)
			return
		}
		file, ok := s.loadPasteFileByID(w, r, paste.ID, fileID)
		if !ok {
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive, nosnippet")
		_, _ = w.Write([]byte(highlightCode(file.Path, file.Content)))
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive, nosnippet")

	if len(files) == 1 {
		file, ok := s.loadPasteFileByID(w, r, paste.ID, files[0].ID)
		if !ok {
			return
		}
		s.writeSingleFileViewer(w, publicID, file)
		return
	}
	s.writeTreeViewer(w, publicID, files)
}

func (s *Server) writeSingleFileViewer(w http.ResponseWriter, publicID string, file *pasteFileRow) {
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
<a href="` + stdlibhtml.EscapeString(pasteURL+"/archive.zip") + `">ZIP</a>
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

func (s *Server) writeTreeViewer(w http.ResponseWriter, publicID string, files []pasteFileMeta) {
	pasteURL := s.cfg.BaseURL + "/p/" + publicID
	apiBase := "/api/v1/pastes/" + publicID
	viewBase := "/p/" + publicID

	var treeItems strings.Builder
	for i, f := range files {
		attrs := ` aria-selected="false" class="tree-item" tabindex="-1"`
		if i == 0 {
			attrs = ` aria-selected="true" class="tree-item selected" tabindex="0"`
		}
		treeItems.WriteString(`<li role="treeitem"` + attrs +
			` data-file-id="` + strconv.FormatInt(f.ID, 10) +
			`" data-path="` + stdlibhtml.EscapeString(f.Path) + `">` +
			stdlibhtml.EscapeString(f.Path) + `</li>`)
	}

	firstID := strconv.FormatInt(files[0].ID, 10)
	_, _ = w.Write([]byte(`<!DOCTYPE html><html><head>
<meta name="robots" content="noindex,nofollow,noarchive,nosnippet">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Paste ` + stdlibhtml.EscapeString(publicID) + `</title>
<style>
:root{color-scheme:light dark}
body{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;margin:0;min-height:100vh}
.layout{display:grid;grid-template-columns:minmax(12rem,18rem) 1fr;min-height:100vh}
.sidebar{border-right:1px solid #ccc;padding:0.75rem;background:#f6f8fa}
@media (prefers-color-scheme:dark){.sidebar{background:#1a1a1a;border-color:#333}}
.sidebar h1{font-size:0.875rem;margin:0 0 0.75rem}
#file-tree{list-style:none;margin:0;padding:0}
.tree-item{padding:0.35rem 0.5rem;cursor:pointer;border-radius:0.25rem}
.tree-item:hover,.tree-item:focus{outline:2px solid #0969da;outline-offset:-2px}
.tree-item.selected{background:#ddf4ff}
@media (prefers-color-scheme:dark){.tree-item.selected{background:#1f3a5f}}
.main{padding:1rem;min-width:0}
.actions{display:flex;flex-wrap:wrap;gap:0.75rem;margin:0 0 0.75rem;font-size:0.875rem}
.chroma{overflow:auto;padding:1rem;background:#f6f8fa}
.chroma .line{display:flex}
.chroma .ln{user-select:none;min-width:3ch;padding-right:1rem;text-align:right;opacity:.5}
.drawer-toggle{display:none}
.file-picker{display:none;width:100%;margin-bottom:0.75rem;font:inherit}
@media (max-width:720px){
  .layout{grid-template-columns:1fr}
  .sidebar{display:none;position:fixed;inset:0 auto 0 0;width:min(80vw,18rem);z-index:10;box-shadow:0 0 0 100vmax rgba(0,0,0,.35)}
  .sidebar.open{display:block}
  .drawer-toggle{display:inline-block}
  .file-picker{display:block}
}
</style>
</head><body>
<div class="layout">
<aside class="sidebar" id="sidebar">
<h1>Files</h1>
<ul id="file-tree" role="tree" aria-label="Paste files">` + treeItems.String() + `</ul>
</aside>
<main class="main">
<p class="actions">
<button type="button" class="drawer-toggle" id="drawer-toggle" aria-controls="sidebar" aria-expanded="false">Files</button>
<select class="file-picker" id="file-picker" aria-label="Select file"></select>
<a id="raw-link" href="#">Raw</a>
<a href="` + stdlibhtml.EscapeString(pasteURL+"/archive.zip") + `">ZIP</a>
<button type="button" id="copy-url" data-url="` + stdlibhtml.EscapeString(pasteURL) + `">Copy URL</button>
<button type="button" id="copy-contents">Copy contents</button>
</p>
<div id="code-pane"><p>Loading…</p></div>
</main>
</div>
<script>
(function(){
  var apiBase=` + strconv.Quote(apiBase) + `;
  var viewBase=` + strconv.Quote(viewBase) + `;
  var items=[].slice.call(document.querySelectorAll('#file-tree [role="treeitem"]'));
  var pane=document.getElementById('code-pane');
  var rawLink=document.getElementById('raw-link');
  var picker=document.getElementById('file-picker');
  var drawer=document.getElementById('drawer-toggle');
  var sidebar=document.getElementById('sidebar');

  items.forEach(function(el){
    var opt=document.createElement('option');
    opt.value=el.getAttribute('data-file-id');
    opt.textContent=el.getAttribute('data-path');
    picker.appendChild(opt);
  });

  function selectItem(el){
    if(!el) return;
    items.forEach(function(i){
      i.classList.remove('selected');
      i.setAttribute('aria-selected','false');
      i.tabIndex=-1;
    });
    el.classList.add('selected');
    el.setAttribute('aria-selected','true');
    el.tabIndex=0;
    el.focus();
    picker.value=el.getAttribute('data-file-id');
    loadFile(el.getAttribute('data-file-id'));
    sidebar.classList.remove('open');
    drawer.setAttribute('aria-expanded','false');
  }

  function loadFile(id){
    pane.innerHTML='<p>Loading…</p>';
    rawLink.href=apiBase+'/files/'+id+'/raw';
    fetch(viewBase+'?partial=1&file_id='+id,{credentials:'same-origin'}).then(function(r){
      if(!r.ok) throw new Error('load failed');
      return r.text();
    }).then(function(html){
      pane.innerHTML=html;
    }).catch(function(){
      return fetch(apiBase+'/files/'+id,{credentials:'same-origin'}).then(function(r){
        if(!r.ok) throw new Error('load failed');
        return r.json();
      }).then(function(data){
        pane.innerHTML='<pre id="paste-content" class="chroma"></pre>';
        document.getElementById('paste-content').textContent=data.content;
      }).catch(function(){
        pane.innerHTML='<p>Failed to load file.</p>';
      });
    });
  }

  items.forEach(function(el){
    el.addEventListener('click',function(){ selectItem(el); });
    el.addEventListener('keydown',function(e){
      var idx=items.indexOf(el);
      if(e.key==='ArrowDown'||e.key==='j'){ e.preventDefault(); selectItem(items[Math.min(items.length-1,idx+1)]); }
      else if(e.key==='ArrowUp'||e.key==='k'){ e.preventDefault(); selectItem(items[Math.max(0,idx-1)]); }
      else if(e.key==='Enter'||e.key===' '){ e.preventDefault(); selectItem(el); }
      else if(e.key==='Home'){ e.preventDefault(); selectItem(items[0]); }
      else if(e.key==='End'){ e.preventDefault(); selectItem(items[items.length-1]); }
    });
  });
  picker.addEventListener('change',function(){
    var el=items.find(function(i){ return i.getAttribute('data-file-id')===picker.value; });
    selectItem(el);
  });
  drawer.addEventListener('click',function(){
    var open=sidebar.classList.toggle('open');
    drawer.setAttribute('aria-expanded', open?'true':'false');
  });
  document.getElementById('copy-url').addEventListener('click',function(){
    navigator.clipboard.writeText(this.getAttribute('data-url'));
  });
  document.getElementById('copy-contents').addEventListener('click',function(){
    var el=document.getElementById('paste-content');
    navigator.clipboard.writeText(el?el.innerText:'');
  });
  loadFile(` + strconv.Quote(firstID) + `);
})();
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
		`SELECT id, path, content FROM paste_files WHERE paste_id = ? ORDER BY id LIMIT 1`, pasteID,
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

func (s *Server) loadPasteFileList(w http.ResponseWriter, r *http.Request, pasteID int64) ([]pasteFileMeta, bool) {
	rows, err := s.db.QueryContext(r.Context(),
		`SELECT id, path, size_bytes FROM paste_files WHERE paste_id = ? ORDER BY path`, pasteID,
	)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return nil, false
	}
	defer rows.Close()
	var files []pasteFileMeta
	for rows.Next() {
		var f pasteFileMeta
		if err := rows.Scan(&f.ID, &f.Path, &f.SizeBytes); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return nil, false
		}
		files = append(files, f)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return nil, false
	}
	if len(files) == 0 {
		http.NotFound(w, r)
		return nil, false
	}
	return files, true
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
