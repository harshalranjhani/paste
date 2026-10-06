package app

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/harshalranjhani/paste/internal/config"
	"github.com/harshalranjhani/paste/internal/db"
)

// Server is the HTTP application process.
type Server struct {
	cfg            config.Config
	db             *sql.DB
	http           *http.Server
	ln             net.Listener
	mu             sync.Mutex
	ready          chan struct{}
	baseURL        string
	unlockMu       sync.Mutex
	unlockAttempts map[string]unlockAttemptWindow
}

// New constructs a server from config. It opens the database but does not start listening.
func New(cfg config.Config) (*Server, error) {
	if err := os.MkdirAll(filepath.Dir(cfg.DatabasePath), 0o755); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}

	database, err := db.Open(cfg.DatabasePath)
	if err != nil {
		return nil, err
	}
	if err := db.Migrate(database); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	s := &Server{
		cfg:            cfg,
		db:             database,
		ready:          make(chan struct{}),
		unlockAttempts: make(map[string]unlockAttemptWindow),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /robots.txt", s.handleRobotsTxt)
	mux.HandleFunc("GET /{$}", s.handleHome)
	mux.HandleFunc("/setup", s.handleSetup)
	mux.HandleFunc("/login", s.handleLogin)
	mux.HandleFunc("POST /logout", s.handleLogout)
	mux.HandleFunc("/admin/invites", s.handleAdminInvites)
	mux.HandleFunc("POST /admin/invites/{id}/revoke", s.handleAdminInviteRevoke)
	mux.HandleFunc("GET /admin/pastes", s.handleAdminPastes)
	mux.HandleFunc("POST /admin/pastes/{id}/delete", s.handleAdminPasteDelete)
	mux.HandleFunc("/invite/{token}", s.handleInvite)
	mux.HandleFunc("POST /api/v1/pastes", s.handleAPICreatePaste)
	mux.HandleFunc("POST /api/v1/pastes/bundle", s.handleAPICreatePasteBundle)
	mux.HandleFunc("DELETE /api/v1/pastes/{id}", s.handleAPIDeletePaste)
	mux.HandleFunc("GET /api/v1/pastes/{id}/meta", s.handleAPIPasteMeta)
	mux.HandleFunc("GET /api/v1/pastes/{id}/files/{file_id}/raw", s.handleAPIPasteFileRaw)
	mux.HandleFunc("GET /api/v1/pastes/{id}/files/{file_id}", s.handleAPIPasteFile)
	mux.HandleFunc("GET /api/v1/pastes/{id}/archive.zip", s.handleAPIPasteArchive)
	mux.HandleFunc("GET /api/v1/me/pastes", s.handleAPIMePastes)
	mux.HandleFunc("/api/v1/tokens", s.handleAPITokens)
	mux.HandleFunc("POST /api/v1/tokens/{id}/revoke", s.handleAPIRevokeToken)
	mux.HandleFunc("GET /me/pastes", s.handleMePastes)
	mux.HandleFunc("POST /me/pastes/{id}/delete", s.handleMePasteDelete)
	mux.HandleFunc("/settings/tokens", s.handleSettingsTokens)
	mux.HandleFunc("POST /settings/tokens/{id}/revoke", s.handleSettingsTokenRevoke)
	mux.HandleFunc("/new", s.handleNewPaste)
	mux.HandleFunc("GET /p/{id}/raw", s.handlePasteRaw)
	mux.HandleFunc("GET /p/{id}/archive.zip", s.handlePasteArchive)
	mux.HandleFunc("POST /p/{id}/unlock", s.handlePasteUnlock)
	mux.HandleFunc("GET /p/{id}", s.handlePasteView)

	s.http = &http.Server{
		Handler:           withSecurityHeaders(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}
	return s, nil
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if err := s.db.PingContext(r.Context()); err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

// Run listens, serves, and blocks until ctx is cancelled or the server fails.
func (s *Server) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	s.mu.Lock()
	s.ln = ln
	s.baseURL = "http://" + ln.Addr().String()
	close(s.ready)
	s.mu.Unlock()

	errCh := make(chan error, 1)
	go func() {
		err := s.http.Serve(ln)
		if err != nil && err != http.ErrServerClosed {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	go s.runCleanupLoop(ctx)

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.http.Shutdown(shutdownCtx)
		_ = s.db.Close()
		return <-errCh
	case err := <-errCh:
		_ = s.db.Close()
		return err
	}
}

// WaitReady blocks until the server is listening or the timeout elapses.
func (s *Server) WaitReady(ctx context.Context, timeout time.Duration) (string, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-s.ready:
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.baseURL, nil
	case <-ctx.Done():
		return "", ctx.Err()
	case <-timer.C:
		return "", fmt.Errorf("server not ready after %s", timeout)
	}
}
