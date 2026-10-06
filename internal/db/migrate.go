package db

import (
	"database/sql"
	"fmt"
)

// Migration is a named, ordered schema change.
type Migration struct {
	Version int
	Name    string
	SQL     string
}

// Migrations is the ordered list applied on startup.
var Migrations = []Migration{
	{
		Version: 1,
		Name:    "app_meta",
		SQL: `
CREATE TABLE app_meta (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
`,
	},
	{
		Version: 2,
		Name:    "users",
		SQL: `
CREATE TABLE users (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	username TEXT NOT NULL COLLATE NOCASE UNIQUE,
	password_hash TEXT NOT NULL,
	role TEXT NOT NULL CHECK (role IN ('user', 'admin')),
	created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
	updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
`,
	},
	{
		Version: 3,
		Name:    "sessions",
		SQL: `
CREATE TABLE sessions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	token_hash TEXT NOT NULL UNIQUE,
	csrf_token TEXT NOT NULL,
	created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
	expires_at TEXT NOT NULL,
	last_seen_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX sessions_user_id_idx ON sessions(user_id);
`,
	},
	{
		Version: 4,
		Name:    "invites",
		SQL: `
CREATE TABLE invites (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	created_by_admin_id INTEGER NOT NULL REFERENCES users(id),
	token_hash TEXT NOT NULL UNIQUE,
	email TEXT,
	created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
	expires_at TEXT NOT NULL,
	used_at TEXT,
	used_by_user_id INTEGER REFERENCES users(id),
	revoked_at TEXT
);
CREATE INDEX invites_token_hash_idx ON invites(token_hash);
`,
	},
}

// Migrate applies all pending migrations in order. Safe to call repeatedly.
func Migrate(database *sql.DB) error {
	if _, err := database.Exec(`
CREATE TABLE IF NOT EXISTS schema_migrations (
	version INTEGER PRIMARY KEY,
	name TEXT NOT NULL,
	applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
`); err != nil {
		return fmt.Errorf("ensure schema_migrations: %w", err)
	}

	for _, m := range Migrations {
		var exists int
		err := database.QueryRow(`SELECT 1 FROM schema_migrations WHERE version = ?`, m.Version).Scan(&exists)
		if err == nil {
			continue
		}
		if err != sql.ErrNoRows {
			return fmt.Errorf("check migration %d: %w", m.Version, err)
		}

		tx, err := database.Begin()
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", m.Version, err)
		}
		if _, err := tx.Exec(m.SQL); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %d (%s): %w", m.Version, m.Name, err)
		}
		if _, err := tx.Exec(
			`INSERT INTO schema_migrations (version, name) VALUES (?, ?)`,
			m.Version, m.Name,
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %d: %w", m.Version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", m.Version, err)
		}
	}
	return nil
}
