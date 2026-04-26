package events

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/binary"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rbryce90/linux-time-machine/internal/vectorstore"
)

//go:embed schema.sql
var schemaSQL string

// Store is the persistence surface of the events domain. Embedding storage
// is delegated to a vectorstore.Store; SemanticSearch queries the vectorstore
// then joins matching ids back to event rows by rowid.
type Store interface {
	EnsureSchema() error
	WriteEvents([]Event) error
	Search(query string, limit int, since time.Time) ([]Event, error)
	Latest(limit int) ([]Event, error)
	EventsNear(at time.Time, window time.Duration, limit int) ([]Event, error)

	NeedsEmbedding(limit int) ([]embeddingRow, error)
	SetEmbedding(rowid int64, vec []float32) error
	SemanticSearch(query []float32, limit int, since time.Time) ([]Event, error)

	// SaveVectorstore persists the vectorstore snapshot. Called from the
	// domain's Stop hook.
	SaveVectorstore() error
}

type embeddingRow struct {
	RowID   int64
	Message string
	Unit    string
}

// vectorSnapshotPath returns the snapshot location alongside the SQLite db,
// e.g. "./linux-time-machine.db" -> "./linux-time-machine.vec". Deriving it
// from DBPath keeps the two files in the same directory by construction.
func vectorSnapshotPath(dbPath string) string {
	if dbPath == "" {
		return ""
	}
	if ext := filepath.Ext(dbPath); ext != "" {
		return dbPath[:len(dbPath)-len(ext)] + ".vec"
	}
	return dbPath + ".vec"
}

type sqliteStore struct {
	db           *sql.DB
	vec          vectorstore.Store
	snapshotPath string
}

// NewSQLiteStore returns a Store backed by db with the supplied vectorstore
// and snapshot path. The vectorstore is expected to be already hydrated (or
// empty); migration of any pre-existing embedding BLOB column happens inside
// EnsureSchema and is idempotent.
func NewSQLiteStore(db *sql.DB, vec vectorstore.Store, snapshotPath string) Store {
	return &sqliteStore{db: db, vec: vec, snapshotPath: snapshotPath}
}

// EnsureSchema creates the events table on first run, then runs the one-time
// migration from the legacy embedding-BLOB layout to vectorstore-backed
// embeddings. The migration is idempotent: if no embedding column exists, it
// is a no-op.
func (s *sqliteStore) EnsureSchema() error {
	if _, err := s.db.Exec(schemaSQL); err != nil {
		return fmt.Errorf("events schema: %w", err)
	}
	return s.migrateEmbeddingColumn()
}

// migrateEmbeddingColumn hydrates the vectorstore from any pre-existing
// embedding BLOB column, saves a snapshot, then drops the column. If no
// embedding column exists this is a no-op (idempotent — running again on an
// already-migrated database does nothing).
//
// SQLite 3.35+ supports ALTER TABLE ... DROP COLUMN natively; modernc/sqlite
// ships a recent SQLite so we attempt that path. If it fails we surface the
// error rather than silently fall back — the rebuild-table dance is
// reachable via that error path if needed.
func (s *sqliteStore) migrateEmbeddingColumn() error {
	var count int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('events') WHERE name='embedding'`).
		Scan(&count); err != nil {
		return fmt.Errorf("events migration check: %w", err)
	}
	if count == 0 {
		return nil
	}

	rows, err := s.db.Query(
		`SELECT rowid, embedding FROM events WHERE embedding IS NOT NULL`)
	if err != nil {
		return fmt.Errorf("events migration scan: %w", err)
	}
	defer rows.Close()

	hydrated := 0
	for rows.Next() {
		var rowid int64
		var blob []byte
		if err := rows.Scan(&rowid, &blob); err != nil {
			return fmt.Errorf("events migration scan row: %w", err)
		}
		vec, ok := decodeLegacyVector(blob)
		if !ok {
			continue
		}
		id := strconv.FormatInt(rowid, 10)
		if err := s.vec.Upsert(context.Background(), id, vec, nil); err != nil {
			return fmt.Errorf("events migration upsert rowid=%d: %w", rowid, err)
		}
		hydrated++
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("events migration iter: %w", err)
	}

	// Persist the snapshot before dropping the column so a crash mid-migration
	// leaves the vectors recoverable from the BLOB column on next startup.
	if hydrated > 0 && s.snapshotPath != "" {
		if err := s.vec.Save(s.snapshotPath); err != nil {
			return fmt.Errorf("events migration save snapshot: %w", err)
		}
	}

	if _, err := s.db.Exec(`ALTER TABLE events DROP COLUMN embedding`); err != nil {
		return fmt.Errorf("events migration drop column: %w", err)
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

// NeedsEmbedding returns rows whose rowid does not yet appear in the
// vectorstore, ordered newest-first. Pulls a window of recent candidates
// from SQLite and filters out rowids already present in the vectorstore via
// O(1) Contains checks.
func (s *sqliteStore) NeedsEmbedding(limit int) ([]embeddingRow, error) {
	// Pull more rows than `limit` so we can skip already-embedded ones
	// without an extra round trip. 4x headroom is plenty in practice.
	const headroom = 4
	rows, err := s.db.Query(
		`SELECT rowid, COALESCE(unit, ''), message
		   FROM events
		  ORDER BY ts DESC
		  LIMIT ?`, limit*headroom)
	if err != nil {
		return nil, fmt.Errorf("needs embedding: %w", err)
	}
	defer rows.Close()

	out := make([]embeddingRow, 0, limit)
	for rows.Next() {
		var r embeddingRow
		if err := rows.Scan(&r.RowID, &r.Unit, &r.Message); err != nil {
			return nil, err
		}
		if s.vec.Contains(strconv.FormatInt(r.RowID, 10)) {
			continue
		}
		out = append(out, r)
		if len(out) >= limit {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *sqliteStore) SetEmbedding(rowid int64, vec []float32) error {
	id := strconv.FormatInt(rowid, 10)
	if err := s.vec.Upsert(context.Background(), id, vec, nil); err != nil {
		return fmt.Errorf("set embedding: %w", err)
	}
	return nil
}

// SemanticSearch queries the vectorstore for the top matching ids, then
// batch-fetches the matching event rows from SQLite by rowid and applies the
// `since` filter. Vector results are returned in vectorstore-rank order.
func (s *sqliteStore) SemanticSearch(query []float32, limit int, since time.Time) ([]Event, error) {
	// Over-fetch from the vectorstore so the SQLite-side `since` filter
	// cannot starve callers of results when many top hits are old.
	const overfetch = 4
	hits, err := s.vec.Search(context.Background(), query, vectorstore.SearchOpts{K: limit * overfetch})
	if err != nil {
		return nil, fmt.Errorf("semantic search: %w", err)
	}
	if len(hits) == 0 {
		return nil, nil
	}

	// Convert hit IDs -> rowids preserving rank order.
	rowids := make([]int64, 0, len(hits))
	for _, h := range hits {
		id, err := strconv.ParseInt(h.ID, 10, 64)
		if err != nil {
			continue // malformed id — skip rather than fail the whole query
		}
		rowids = append(rowids, id)
	}
	if len(rowids) == 0 {
		return nil, nil
	}

	// Single query: fetch rows by rowid IN (...) with ts >= since, capturing
	// rowid alongside event fields so we can restore vectorstore-rank order.
	byRowID, err := s.eventsByRowIDs(rowids, since)
	if err != nil {
		return nil, err
	}
	// Walk rowids in rank order; emit events present in the result set.
	out := make([]Event, 0, len(rowids))
	for _, id := range rowids {
		if e, ok := byRowID[id]; ok {
			out = append(out, e)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

// eventsByRowIDs fetches the event rows for the given rowids, filtered by
// ts >= since, and returns them keyed by rowid. Callers walk their original
// rowid list to restore order.
func (s *sqliteStore) eventsByRowIDs(rowids []int64, since time.Time) (map[int64]Event, error) {
	if len(rowids) == 0 {
		return nil, nil
	}
	// Build an IN clause. SQLite's default SQLITE_LIMIT_VARIABLE_NUMBER is
	// 32766 in modern builds — well above our overfetch ceiling.
	placeholders := strings.Repeat("?,", len(rowids))
	placeholders = placeholders[:len(placeholders)-1]

	args := make([]any, 0, len(rowids)+1)
	args = append(args, since.UnixNano())
	for _, id := range rowids {
		args = append(args, id)
	}

	rows, err := s.db.Query(
		`SELECT rowid, ts, priority, unit, source, pid, message
		   FROM events
		  WHERE ts >= ? AND rowid IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("semantic events fetch: %w", err)
	}
	defer rows.Close()
	out := make(map[int64]Event, len(rowids))
	for rows.Next() {
		var rowid, ts int64
		var pid sql.NullInt64
		var unit, source sql.NullString
		var e Event
		if err := rows.Scan(&rowid, &ts, &e.Priority, &unit, &source, &pid, &e.Message); err != nil {
			return nil, fmt.Errorf("semantic scan row: %w", err)
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
		out[rowid] = e
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// SaveVectorstore writes the vectorstore snapshot to disk. Called from the
// events Domain's Stop lifecycle hook. No-op if no snapshot path is set.
func (s *sqliteStore) SaveVectorstore() error {
	if s.snapshotPath == "" {
		return nil
	}
	return s.vec.Save(s.snapshotPath)
}

// decodeLegacyVector decodes the old []float32 BLOB layout used by the
// pre-vectorstore embedding column: little-endian IEEE-754 float32 packed
// 4 bytes per value. Used only by the one-time migration; not exposed.
func decodeLegacyVector(b []byte) ([]float32, bool) {
	if len(b)%4 != 0 || len(b) == 0 {
		return nil, false
	}
	out := make([]float32, len(b)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return out, true
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
