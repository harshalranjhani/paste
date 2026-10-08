package app

import (
	"context"
	"log/slog"
	"time"
)

const defaultCleanupInterval = 5 * time.Minute

func (s *Server) cleanupInterval() time.Duration {
	if s.cfg.CleanupInterval < 0 {
		return 0 // disabled
	}
	if s.cfg.CleanupInterval == 0 {
		return defaultCleanupInterval
	}
	return s.cfg.CleanupInterval
}

func (s *Server) runCleanupLoop(ctx context.Context) {
	interval := s.cleanupInterval()
	if interval == 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	s.runCleanup(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runCleanup(ctx)
		}
	}
}

func (s *Server) runCleanup(ctx context.Context) {
	now := time.Now().UTC().Format(time.RFC3339Nano)

	// Keep the consumed paste as a tombstone (410), but erase its file bodies.
	if _, err := s.db.ExecContext(ctx, `DELETE FROM paste_files WHERE paste_id IN (
SELECT p.id FROM pastes p LEFT JOIN burn_sessions b ON b.paste_id = p.id
WHERE p.burned_at IS NOT NULL AND (b.expires_at IS NULL OR julianday(b.expires_at) <= julianday(?))
)`, now); err != nil {
		slog.Error("cleanup burned paste files", "err", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM burn_sessions WHERE julianday(expires_at) <= julianday(?)`, now); err != nil {
		slog.Error("cleanup burn sessions", "err", err)
	}

	pastes, err := s.db.ExecContext(ctx, `DELETE FROM pastes WHERE expires_at < ?`, now)
	if err != nil {
		slog.Error("cleanup pastes", "err", err)
	} else if n, _ := pastes.RowsAffected(); n > 0 {
		slog.Info("cleanup deleted expired pastes", "count", n)
	}

	sessions, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ?`, now)
	if err != nil {
		slog.Error("cleanup sessions", "err", err)
	} else if n, _ := sessions.RowsAffected(); n > 0 {
		slog.Info("cleanup deleted expired sessions", "count", n)
	}

	access, err := s.db.ExecContext(ctx, `DELETE FROM paste_access_sessions WHERE expires_at < ?`, now)
	if err != nil {
		slog.Error("cleanup paste access sessions", "err", err)
	} else if n, _ := access.RowsAffected(); n > 0 {
		slog.Info("cleanup deleted expired paste access sessions", "count", n)
	}

	// Expired invites (unused or past expiry); used/revoked rows past expiry also go.
	invites, err := s.db.ExecContext(ctx, `DELETE FROM invites WHERE expires_at < ?`, now)
	if err != nil {
		slog.Error("cleanup invites", "err", err)
	} else if n, _ := invites.RowsAffected(); n > 0 {
		slog.Info("cleanup deleted expired invites", "count", n)
	}
}
