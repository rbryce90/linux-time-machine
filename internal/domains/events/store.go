package events

import (
	"database/sql"
	_ "embed"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
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

	// Embedding pipeline
	NeedsEmbedding(limit int) ([]embeddingRow, error)
	SetEmbedding(rowid int64, vec []float32) error
	SemanticSearch(query []float32, limit int, since time.Time) ([]Event, error)
}

type embeddingRow struct {
	RowID   int64
	Message string
	Unit    string
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
	// Lightweight migration: add embedding column if upgrading from a pre-
	// embedding schema. SQLite has no IF NOT EXISTS on ADD COLUMN, so check
	// first via pragma_table_info.
	var count int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('events') WHERE name='embedding'`).
		Scan(&count); err != nil {
		return fmt.Errorf("events migration check: %w", err)
	}
	if count == 0 {
		if _, err := s.db.Exec(`ALTER TABLE events ADD COLUMN embedding BLOB`); err != nil {
			return fmt.Errorf("events add embedding column: %w", err)
		}
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

// NeedsEmbedding returns up to `limit` rows that have not yet been embedded.
// Ordered newest-first so the background worker catches up on recent data.
func (s *sqliteStore) NeedsEmbedding(limit int) ([]embeddingRow, error) {
	rows, err := s.db.Query(
		`SELECT rowid, COALESCE(unit, ''), message
		   FROM events
		  WHERE embedding IS NULL
		  ORDER BY ts DESC
		  LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("needs embedding: %w", err)
	}
	defer rows.Close()
	var out []embeddingRow
	for rows.Next() {
		var r embeddingRow
		if err := rows.Scan(&r.RowID, &r.Unit, &r.Message); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *sqliteStore) SetEmbedding(rowid int64, vec []float32) error {
	blob := encodeVector(vec)
	if _, err := s.db.Exec(`UPDATE events SET embedding = ? WHERE rowid = ?`, blob, rowid); err != nil {
		return fmt.Errorf("set embedding: %w", err)
	}
	return nil
}

// SemanticSearch performs a brute-force cosine similarity over rows that
// already have embeddings, within the since-now window. This is O(N) over
// the window; fine for single-machine scale. Returns top `limit` matches.
func (s *sqliteStore) SemanticSearch(query []float32, limit int, since time.Time) ([]Event, error) {
	rows, err := s.db.Query(
		`SELECT ts, priority, unit, source, pid, message, embedding
		   FROM events
		  WHERE ts >= ? AND embedding IS NOT NULL`, since.UnixNano())
	if err != nil {
		return nil, fmt.Errorf("semantic search: %w", err)
	}
	defer rows.Close()

	type scored struct {
		e     Event
		score float32
	}
	qNorm := norm(query)
	var results []scored
	for rows.Next() {
		var ts int64
		var pid sql.NullInt64
		var unit, source sql.NullString
		var blob []byte
		var e Event
		if err := rows.Scan(&ts, &e.Priority, &unit, &source, &pid, &e.Message, &blob); err != nil {
			return nil, fmt.Errorf("scan semantic row: %w", err)
		}
		vec, ok := decodeVector(blob)
		if !ok || len(vec) != len(query) {
			continue
		}
		score := cosine(query, vec, qNorm)
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
		results = append(results, scored{e: e, score: score})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Partial-sort top N.
	sort.Slice(results, func(i, j int) bool { return results[i].score > results[j].score })
	if len(results) > limit {
		results = results[:limit]
	}
	out := make([]Event, 0, len(results))
	for _, r := range results {
		out = append(out, r.e)
	}
	return out, nil
}

// encodeVector packs []float32 into a little-endian BLOB, 4 bytes per value.
func encodeVector(vec []float32) []byte {
	buf := make([]byte, 4*len(vec))
	for i, v := range vec {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return buf
}

func decodeVector(b []byte) ([]float32, bool) {
	if len(b)%4 != 0 || len(b) == 0 {
		return nil, false
	}
	out := make([]float32, len(b)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return out, true
}

// norm returns the L2 norm of a vector (for cosine similarity). Callers pass
// the query's norm once so it isn't recomputed per row.
func norm(v []float32) float32 {
	var s float64
	for _, x := range v {
		s += float64(x) * float64(x)
	}
	return float32(math.Sqrt(s))
}

// cosine returns cosine similarity of q and r. qNorm is ||q||.
func cosine(q, r []float32, qNorm float32) float32 {
	if len(q) != len(r) || qNorm == 0 {
		return 0
	}
	var dot, rNormSq float64
	for i := range q {
		dot += float64(q[i]) * float64(r[i])
		rNormSq += float64(r[i]) * float64(r[i])
	}
	rNorm := math.Sqrt(rNormSq)
	if rNorm == 0 {
		return 0
	}
	return float32(dot / (float64(qNorm) * rNorm))
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
