package app

import (
	"context"
	"database/sql"
	"net/http"
	"strings"

	"github.com/harshalranjhani/paste/internal/auth"
)

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleSetupGet(w, r)
	case http.MethodPost:
		s.handleSetupPost(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSetupGet(w http.ResponseWriter, r *http.Request) {
	n, err := s.countUsers(r.Context())
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if n > 0 {
		http.Error(w, "setup disabled", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!DOCTYPE html><html><head><title>Setup</title></head><body>
<h1>Create admin account</h1>
<form method="post" action="/setup">
<label>Username <input name="username" required></label>
<label>Password <input type="password" name="password" required minlength="8"></label>
<button type="submit">Create admin</button>
</form>
</body></html>`))
}

func (s *Server) handleSetupPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	if username == "" || len(password) < 8 {
		http.Error(w, "username and password (min 8) required", http.StatusBadRequest)
		return
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	created, err := s.createBootstrapAdmin(r.Context(), username, hash)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !created {
		http.Error(w, "setup disabled", http.StatusForbidden)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) countUsers(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// createBootstrapAdmin inserts the first admin under BEGIN IMMEDIATE so concurrent
// setup posts cannot both succeed.
func (s *Server) createBootstrapAdmin(ctx context.Context, username, passwordHash string) (bool, error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return false, err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
		}
	}()

	var n int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return false, err
	}
	if n > 0 {
		return false, nil
	}

	if _, err := conn.ExecContext(ctx,
		`INSERT INTO users (username, password_hash, role) VALUES (?, ?, 'admin')`,
		username, passwordHash,
	); err != nil {
		return false, err
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return false, err
	}
	committed = true
	return true, nil
}

// Ensure sql import used if we later need TxOptions; keep Conn-based path above.
var _ = sql.ErrNoRows
