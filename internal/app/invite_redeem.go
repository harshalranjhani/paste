package app

import (
	"context"
	"database/sql"
	"html"
	"net/http"
	"strings"
	"time"

	"github.com/harshalranjhani/paste/internal/auth"
)

func (s *Server) handleInvite(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if token == "" {
		writeUIError(w, r, "not found", http.StatusNotFound)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.handleInviteGet(w, r, token)
	case http.MethodPost:
		s.handleInviteRedeem(w, r, token)
	default:
		writeUIError(w, r, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleInviteGet(w http.ResponseWriter, r *http.Request, token string) {
	inv, status, msg := s.lookupRedeemableInvite(r.Context(), token)
	if inv == nil {
		writeUIError(w, r, msg, status)
		return
	}
	email := ""
	if inv.Email.Valid {
		email = inv.Email.String
	}
	writePage(w, "Accept invite", "", nil, `<div class="auth-layout"><div class="auth-intro"><span class="brand-mark">`+icon("user")+`</span><p class="eyebrow">You’re invited</p><h1>Good ideas are better shared.</h1><p class="description">Join this workspace to create your own pastes. A snippet or an entire project, all in one link.</p></div><section class="card auth-card"><h2>Create your account</h2><p class="description">Choose a username and a secure password.</p>
<form class="stack" method="post" action="/invite/`+html.EscapeString(token)+`">
<label>Username <input name="username" required autocomplete="username" placeholder="Choose a username"></label>
<label>Password <input type="password" name="password" required minlength="8" autocomplete="new-password" placeholder="At least 8 characters"></label>
<label>Email <input type="email" name="email" autocomplete="email" value="`+html.EscapeString(email)+`" placeholder="you@example.com"></label>
<button class="button button-primary" type="submit">Create account `+icon("arrow")+`</button>
</form></section></div>`)
}

func (s *Server) handleInviteRedeem(w http.ResponseWriter, r *http.Request, token string) {
	if err := r.ParseForm(); err != nil {
		writeUIError(w, r, "bad request", http.StatusBadRequest)
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	email := strings.TrimSpace(r.FormValue("email"))
	if username == "" || len(password) < 8 {
		writeUIError(w, r, "username and password (min 8) required", http.StatusBadRequest)
		return
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		writeUIError(w, r, "internal error", http.StatusInternalServerError)
		return
	}

	status, msg, err := s.redeemInvite(r.Context(), token, username, hash, email)
	if err != nil {
		writeUIError(w, r, "internal error", http.StatusInternalServerError)
		return
	}
	if status != http.StatusOK {
		writeUIError(w, r, msg, status)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

type inviteRecord struct {
	ID        int64
	Email     sql.NullString
	ExpiresAt string
	UsedAt    sql.NullString
	RevokedAt sql.NullString
}

func (s *Server) lookupRedeemableInvite(ctx context.Context, token string) (*inviteRecord, int, string) {
	var inv inviteRecord
	err := s.db.QueryRowContext(ctx, `
SELECT id, email, expires_at, used_at, revoked_at
FROM invites WHERE token_hash = ?`, auth.HashToken(token),
	).Scan(&inv.ID, &inv.Email, &inv.ExpiresAt, &inv.UsedAt, &inv.RevokedAt)
	if err == sql.ErrNoRows {
		return nil, http.StatusNotFound, "invite not found"
	}
	if err != nil {
		return nil, http.StatusInternalServerError, "internal error"
	}
	if inv.RevokedAt.Valid {
		return nil, http.StatusGone, "invite revoked"
	}
	if inv.UsedAt.Valid {
		return nil, http.StatusGone, "invite already used"
	}
	exp, err := parseTime(inv.ExpiresAt)
	if err != nil {
		return nil, http.StatusInternalServerError, "internal error"
	}
	if time.Now().UTC().After(exp) {
		return nil, http.StatusGone, "invite expired"
	}
	return &inv, http.StatusOK, ""
}

func (s *Server) redeemInvite(ctx context.Context, token, username, passwordHash, email string) (int, string, error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return 0, "", err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return 0, "", err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
		}
	}()

	var inv inviteRecord
	err = conn.QueryRowContext(ctx, `
SELECT id, email, expires_at, used_at, revoked_at
FROM invites WHERE token_hash = ?`, auth.HashToken(token),
	).Scan(&inv.ID, &inv.Email, &inv.ExpiresAt, &inv.UsedAt, &inv.RevokedAt)
	if err == sql.ErrNoRows {
		return http.StatusNotFound, "invite not found", nil
	}
	if err != nil {
		return 0, "", err
	}
	if inv.RevokedAt.Valid {
		return http.StatusGone, "invite revoked", nil
	}
	if inv.UsedAt.Valid {
		return http.StatusGone, "invite already used", nil
	}
	exp, err := parseTime(inv.ExpiresAt)
	if err != nil {
		return 0, "", err
	}
	if time.Now().UTC().After(exp) {
		return http.StatusGone, "invite expired", nil
	}
	if inv.Email.Valid && inv.Email.String != "" {
		if !strings.EqualFold(strings.TrimSpace(email), inv.Email.String) {
			return http.StatusBadRequest, "email does not match invite", nil
		}
	}

	res, err := conn.ExecContext(ctx,
		`INSERT INTO users (username, password_hash, role) VALUES (?, ?, 'user')`,
		username, passwordHash,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return http.StatusConflict, "username taken", nil
		}
		return 0, "", err
	}
	userID, err := res.LastInsertId()
	if err != nil {
		return 0, "", err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	upd, err := conn.ExecContext(ctx,
		`UPDATE invites SET used_at = ?, used_by_user_id = ? WHERE id = ? AND used_at IS NULL AND revoked_at IS NULL`,
		now, userID, inv.ID,
	)
	if err != nil {
		return 0, "", err
	}
	n, _ := upd.RowsAffected()
	if n == 0 {
		return http.StatusGone, "invite already used", nil
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return 0, "", err
	}
	committed = true
	return http.StatusOK, "", nil
}

func parseTime(v string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, v)
	if err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, v)
}
