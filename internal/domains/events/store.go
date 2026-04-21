package events

import (
	"database/sql"
	_ "embed"
	"fmt"
	"strings"
	"time"
)

//go:embed schema.sql
var schemaSQL string

type Store interface {
	EnsureSchema() error
	WriteEvents([]Event) error
	Search(query string, limit int, since time.Time) ([]Event, error)
	Latest(limit int) ([]Event, error)
	EventsNear(at time.Time, window time.Duration, limit int) ([]Event, error)
}

type sqliteStore struct {
	db *sql.DB
}

func NewSQLiteStore(db *sql.DB) Store {
	return &sqliteStore{db: db}
}

func (s *sqliteStore) EnsureSchema() error {
	if _, err := s.db.Exec(schemaSQL); err != nil {
		return fmt.Errorf("events schema: %w", err)
	}
	return nil
}

func (s *sqliteStore) WriteEvents(events []Event) error {
	if len(events) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("tx begin: %w", err)
	}
	stmt, err := tx.Prepare(
		`INSERT INTO events (ts, priority, unit, source, pid, message)
		 VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("tx prepare: %w", err)
	}
	defer stmt.Close()
	for _, e := range events {
		if _, err := stmt.Exec(e.At.UnixNano(), e.Priority, e.Unit, e.Source, e.PID, e.Message); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("tx exec: %w", err)
		}
	}
	return tx.Commit()
}

func (s *sqliteStore) Latest(limit int) ([]Event, error) {
	rows, err := s.db.Query(
		`SELECT ts, priority, unit, source, pid, message
		   FROM events
		  ORDER BY ts DESC
		  LIMIT ?`, limit)
	return scanEvents(rows, err)
}

// Search does a case-insensitive substring match on message + unit.
// Sufficient for v0.1; will be replaced by embedding-backed semantic search.
func (s *sqliteStore) Search(query string, limit int, since time.Time) ([]Event, error) {
	like := "%" + strings.ToLower(query) + "%"
	rows, err := s.db.Query(
		`SELECT ts, priority, unit, source, pid, message
		   FROM events
		  WHERE ts >= ?
		    AND (LOWER(message) LIKE ? OR LOWER(unit) LIKE ?)
		  ORDER BY ts DESC
		  LIMIT ?`, since.UnixNano(), like, like, limit)
	return scanEvents(rows, err)
}

func (s *sqliteStore) EventsNear(at time.Time, window time.Duration, limit int) ([]Event, error) {
	start := at.Add(-window)
	end := at.Add(window)
	rows, err := s.db.Query(
		`SELECT ts, priority, unit, source, pid, message
		   FROM events
		  WHERE ts BETWEEN ? AND ?
		  ORDER BY ts ASC
		  LIMIT ?`, start.UnixNano(), end.UnixNano(), limit)
	return scanEvents(rows, err)
}

func scanEvents(rows *sql.Rows, qErr error) ([]Event, error) {
	if qErr != nil {
		return nil, fmt.Errorf("query events: %w", qErr)
	}
	defer rows.Close()

	var out []Event
	for rows.Next() {
		var ts int64
		var pid sql.NullInt64
		var unit, source sql.NullString
		var e Event
		if err := rows.Scan(&ts, &e.Priority, &unit, &source, &pid, &e.Message); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		e.At = time.Unix(0, ts)
		if unit.Valid {
			e.Unit = unit.String
		}
		if source.Valid {
			e.Source = source.String
		}
		if pid.Valid {
			e.PID = int32(pid.Int64)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
