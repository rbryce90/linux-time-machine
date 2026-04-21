// Package storage provides the shared SQLite connection used by every domain.
// Domains own their own tables and queries; storage just hands out a *sql.DB.
package storage

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

type SQLite struct {
	DB   *sql.DB
	Path string
}

// OpenSQLite opens a SQLite database and configures it for concurrent
// reader/writer access: WAL journal (readers don't block a writer),
// NORMAL synchronous (safe with WAL, much faster), and a generous
// busy_timeout so contending goroutines wait instead of erroring.
func OpenSQLite(path string) (*SQLite, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", path, err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite %q: %w", path, err)
	}

	pragmas := []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA foreign_keys = ON",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("apply pragma %q: %w", p, err)
		}
	}
	// Single writer serialised through one connection avoids WAL
	// write-contention between collector goroutines. Readers still
	// fan out via the pool (SetMaxOpenConns default is unlimited).
	db.SetMaxOpenConns(8)

	return &SQLite{DB: db, Path: path}, nil
}

func (s *SQLite) Close() error {
	if s == nil || s.DB == nil {
		return nil
	}
	return s.DB.Close()
}
