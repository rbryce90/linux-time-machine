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

func OpenSQLite(path string) (*SQLite, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", path, err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite %q: %w", path, err)
	}
	return &SQLite{DB: db, Path: path}, nil
}

func (s *SQLite) Close() error {
	if s == nil || s.DB == nil {
		return nil
	}
	return s.DB.Close()
}
