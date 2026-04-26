package events

import (
	"context"
	"encoding/binary"
	"math"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/rbryce90/linux-time-machine/internal/storage"
	"github.com/rbryce90/linux-time-machine/internal/vectorstore"
)

// newTestStore builds an isolated SQLite-backed Store with schema applied
// and a fresh empty vectorstore.
func newTestStore(t *testing.T) Store {
	t.Helper()
	dir := t.TempDir()
	s, err := storage.OpenSQLite(filepath.Join(dir, "events.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	vec := vectorstore.NewBruteForceStore()
	t.Cleanup(func() { _ = vec.Close() })

	store := NewSQLiteStore(s.DB, vec, filepath.Join(dir, "events.vec"))
	if err := store.EnsureSchema(); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	return store
}

func TestSQLiteStore_WriteAndLatest(t *testing.T) {
	store := newTestStore(t)

	now := time.Now().Truncate(time.Microsecond)
	earlier := now.Add(-time.Minute)
	events := []Event{
		{At: earlier, Priority: 6, Unit: "sshd.service", Source: "sshd", PID: 100, Message: "Accepted publickey"},
		{At: now, Priority: 3, Unit: "kernel", Source: "kernel", PID: 0, Message: "out of memory"},
	}
	if err := store.WriteEvents(events); err != nil {
		t.Fatalf("WriteEvents: %v", err)
	}

	got, err := store.Latest(10)
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Latest returned %d events, want 2", len(got))
	}
	if got[0].Message != "out of memory" {
		t.Errorf("Latest[0] message = %q, want 'out of memory'", got[0].Message)
	}
	if got[0].Priority != 3 || got[0].Unit != "kernel" {
		t.Errorf("Latest[0] fields wrong: %+v", got[0])
	}
}

func TestSQLiteStore_WriteEvents_NoopForEmptySlice(t *testing.T) {
	store := newTestStore(t)
	if err := store.WriteEvents(nil); err != nil {
		t.Errorf("WriteEvents(nil) = %v, want nil", err)
	}
	got, err := store.Latest(10)
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Latest after empty write = %d events, want 0", len(got))
	}
}

func TestSQLiteStore_Search_CaseInsensitiveSubstring(t *testing.T) {
	store := newTestStore(t)
	now := time.Now()

	if err := store.WriteEvents([]Event{
		{At: now.Add(-30 * time.Second), Priority: 6, Unit: "sshd.service", Message: "Accepted publickey for alice"},
		{At: now.Add(-20 * time.Second), Priority: 3, Unit: "kernel", Message: "Out of memory: kill process"},
		{At: now.Add(-10 * time.Second), Priority: 6, Unit: "sshd.service", Message: "Failed password for bob"},
	}); err != nil {
		t.Fatalf("WriteEvents: %v", err)
	}

	since := now.Add(-time.Minute)

	got, err := store.Search("FAILED", 10, since)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 || got[0].Message != "Failed password for bob" {
		t.Errorf("Search('FAILED') returned %+v, want exactly the 'Failed password' event", got)
	}

	got, err = store.Search("sshd", 10, since)
	if err != nil {
		t.Fatalf("Search by unit: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("Search('sshd') returned %d, want 2", len(got))
	}

	got, err = store.Search("memory", 10, now.Add(-15*time.Second))
	if err != nil {
		t.Fatalf("Search since: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Search since filter failed: %+v", got)
	}
}

func TestSQLiteStore_EventsNear(t *testing.T) {
	store := newTestStore(t)
	anchor := time.Now()

	if err := store.WriteEvents([]Event{
		{At: anchor.Add(-2 * time.Minute), Priority: 6, Message: "way before"},
		{At: anchor.Add(-30 * time.Second), Priority: 6, Message: "in window early"},
		{At: anchor, Priority: 3, Message: "at anchor"},
		{At: anchor.Add(30 * time.Second), Priority: 6, Message: "in window late"},
		{At: anchor.Add(2 * time.Minute), Priority: 6, Message: "way after"},
	}); err != nil {
		t.Fatalf("WriteEvents: %v", err)
	}

	got, err := store.EventsNear(anchor, time.Minute, 100)
	if err != nil {
		t.Fatalf("EventsNear: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("EventsNear returned %d, want 3 (the in-window events)", len(got))
	}
	wantOrder := []string{"in window early", "at anchor", "in window late"}
	for i, e := range got {
		if e.Message != wantOrder[i] {
			t.Errorf("EventsNear[%d] = %q, want %q", i, e.Message, wantOrder[i])
		}
	}
}

// TestSQLiteStore_NeedsEmbedding_NewSemantics verifies the new vectorstore-
// backed semantics: "needs embedding" means "rowid is not in vectorstore",
// not "embedding column IS NULL".
func TestSQLiteStore_NeedsEmbedding_NewSemantics(t *testing.T) {
	store := newTestStore(t)
	now := time.Now()

	if err := store.WriteEvents([]Event{
		{At: now.Add(-3 * time.Second), Unit: "u1", Message: "older"},
		{At: now.Add(-2 * time.Second), Unit: "u2", Message: "middle"},
		{At: now.Add(-time.Second), Unit: "u3", Message: "newest"},
	}); err != nil {
		t.Fatalf("WriteEvents: %v", err)
	}

	rows, err := store.NeedsEmbedding(10)
	if err != nil {
		t.Fatalf("NeedsEmbedding: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("NeedsEmbedding returned %d rows, want 3", len(rows))
	}
	if rows[0].Message != "newest" {
		t.Errorf("NeedsEmbedding[0] = %q, want 'newest' (DESC ordering)", rows[0].Message)
	}

	// Embed the newest row -> it should drop out of NeedsEmbedding.
	if err := store.SetEmbedding(rows[0].RowID, []float32{1, 0, 0}); err != nil {
		t.Fatalf("SetEmbedding: %v", err)
	}

	leftover, err := store.NeedsEmbedding(10)
	if err != nil {
		t.Fatalf("NeedsEmbedding after set: %v", err)
	}
	if len(leftover) != 2 {
		t.Errorf("after SetEmbedding, NeedsEmbedding returned %d rows, want 2", len(leftover))
	}
	for _, r := range leftover {
		if r.RowID == rows[0].RowID {
			t.Errorf("row %d still appears in NeedsEmbedding after set", r.RowID)
		}
	}
}

// TestSQLiteStore_SemanticSearch_RanksByCosine verifies the vectorstore-
// backed search path: events are written, embedded via the store, and
// semantic search returns them ordered by cosine similarity.
func TestSQLiteStore_SemanticSearch_RanksByCosine(t *testing.T) {
	store := newTestStore(t)
	now := time.Now()

	if err := store.WriteEvents([]Event{
		{At: now.Add(-3 * time.Second), Message: "rowA"},
		{At: now.Add(-2 * time.Second), Message: "rowB"},
		{At: now.Add(-time.Second), Message: "rowC"},
	}); err != nil {
		t.Fatalf("WriteEvents: %v", err)
	}

	rows, err := store.NeedsEmbedding(10)
	if err != nil {
		t.Fatalf("NeedsEmbedding: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows to embed, got %d", len(rows))
	}

	// Query is [1, 0, 0]:
	//   rowA: [1, 0, 0]   → cosine 1.0  (best)
	//   rowB: [0.7, 0.7, 0]
	//   rowC: [0, 1, 0]   → cosine 0.0  (worst)
	embeddings := map[string][]float32{
		"rowA": {1, 0, 0},
		"rowB": {0.7, 0.7, 0},
		"rowC": {0, 1, 0},
	}
	for _, r := range rows {
		vec := embeddings[r.Message]
		if vec == nil {
			t.Fatalf("no embedding mapped for %q", r.Message)
		}
		if err := store.SetEmbedding(r.RowID, vec); err != nil {
			t.Fatalf("SetEmbedding: %v", err)
		}
	}

	got, err := store.SemanticSearch([]float32{1, 0, 0}, 2, now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("SemanticSearch: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("SemanticSearch returned %d, want 2 (limit)", len(got))
	}
	if got[0].Message != "rowA" {
		t.Errorf("top result = %q, want rowA", got[0].Message)
	}
	if got[1].Message != "rowB" {
		t.Errorf("second result = %q, want rowB", got[1].Message)
	}
}

// TestSQLiteStore_SemanticSearch_EmptyVectorstore returns no results without
// error when nothing has been embedded yet.
func TestSQLiteStore_SemanticSearch_EmptyVectorstore(t *testing.T) {
	store := newTestStore(t)
	now := time.Now()
	if err := store.WriteEvents([]Event{
		{At: now.Add(-time.Second), Message: "no-embedding"},
	}); err != nil {
		t.Fatalf("WriteEvents: %v", err)
	}
	got, err := store.SemanticSearch([]float32{1, 0}, 5, now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("SemanticSearch: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("SemanticSearch on empty vectorstore = %v, want []", got)
	}
}

// TestSQLiteStore_SemanticSearch_AppliesSinceFilter verifies the since
// timestamp filters out events whose ts is older than the cutoff, even if
// their vectorstore hit ranks high.
func TestSQLiteStore_SemanticSearch_AppliesSinceFilter(t *testing.T) {
	store := newTestStore(t)
	now := time.Now()
	if err := store.WriteEvents([]Event{
		{At: now.Add(-time.Hour), Message: "old"},
		{At: now.Add(-time.Second), Message: "fresh"},
	}); err != nil {
		t.Fatalf("WriteEvents: %v", err)
	}
	rows, err := store.NeedsEmbedding(10)
	if err != nil {
		t.Fatalf("NeedsEmbedding: %v", err)
	}
	for _, r := range rows {
		// Both rows get the same embedding so vectorstore rank is arbitrary
		// — the since filter is what matters.
		if err := store.SetEmbedding(r.RowID, []float32{1, 0, 0}); err != nil {
			t.Fatalf("SetEmbedding: %v", err)
		}
	}
	got, err := store.SemanticSearch([]float32{1, 0, 0}, 10, now.Add(-time.Minute))
	if err != nil {
		t.Fatalf("SemanticSearch: %v", err)
	}
	if len(got) != 1 || got[0].Message != "fresh" {
		t.Errorf("SemanticSearch after since filter = %v, want [fresh]", got)
	}
}

func TestSQLiteStore_EnsureSchema_Idempotent(t *testing.T) {
	store := newTestStore(t)
	if err := store.EnsureSchema(); err != nil {
		t.Errorf("second EnsureSchema = %v, want nil (idempotent migration)", err)
	}
	if err := store.WriteEvents([]Event{{At: time.Now(), Priority: 6, Message: "x"}}); err != nil {
		t.Errorf("WriteEvents after re-EnsureSchema: %v", err)
	}
}

func TestVectorSnapshotPath(t *testing.T) {
	cases := map[string]string{
		"./linux-time-machine.db":           "./linux-time-machine.vec",
		"/tmp/foo.db":                       "/tmp/foo.vec",
		"events.sqlite":                     "events.vec",
		"":                                  "",
		"/var/lib/pulse/data":               "/var/lib/pulse/data.vec", // no extension -> append
		"/etc.d/no-leading-dot-on-basename": "/etc.d/no-leading-dot-on-basename.vec",
	}
	for in, want := range cases {
		if got := vectorSnapshotPath(in); got != want {
			t.Errorf("vectorSnapshotPath(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestSQLiteStore_Migration_FromLegacyEmbeddingColumn verifies the one-time
// migration: a database carrying the old embedding BLOB column is hydrated
// into the vectorstore, the column is dropped, and subsequent searches work.
func TestSQLiteStore_Migration_FromLegacyEmbeddingColumn(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "events.db")

	// 1. Open a raw SQLite database and create the legacy schema (with
	//    embedding BLOB column). Insert a few rows whose embedding BLOB is
	//    encoded in the old little-endian float32 layout.
	rawDB, err := storage.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	if _, err := rawDB.DB.Exec(`CREATE TABLE events (
		ts INTEGER NOT NULL, priority INTEGER NOT NULL, unit TEXT, source TEXT,
		pid INTEGER, message TEXT NOT NULL, embedding BLOB)`); err != nil {
		t.Fatalf("create legacy table: %v", err)
	}
	now := time.Now()
	stmt, err := rawDB.DB.Prepare(
		`INSERT INTO events (ts, priority, unit, source, pid, message, embedding)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	defer stmt.Close()

	for _, e := range []struct {
		msg string
		vec []float32
	}{
		{"alpha", []float32{1, 0, 0}},
		{"beta", []float32{0, 1, 0}},
		{"gamma", nil}, // NULL embedding — must not break migration
	} {
		var blob any
		if e.vec != nil {
			blob = encodeLegacyVectorForTest(e.vec)
		}
		if _, err := stmt.Exec(now.UnixNano(), 6, "u", "s", 0, e.msg, blob); err != nil {
			t.Fatalf("insert legacy row: %v", err)
		}
	}
	if err := rawDB.Close(); err != nil {
		t.Fatalf("close raw db: %v", err)
	}

	// 2. Reopen via the events Store. Migration should run on EnsureSchema.
	s2, err := storage.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = s2.Close() })
	vec := vectorstore.NewBruteForceStore()
	t.Cleanup(func() { _ = vec.Close() })
	snapshotPath := filepath.Join(dir, "events.vec")
	store := NewSQLiteStore(s2.DB, vec, snapshotPath)
	if err := store.EnsureSchema(); err != nil {
		t.Fatalf("EnsureSchema (with migration): %v", err)
	}

	// 3. The embedding column must have been dropped.
	var col int
	if err := s2.DB.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('events') WHERE name='embedding'`).
		Scan(&col); err != nil {
		t.Fatalf("post-migration column check: %v", err)
	}
	if col != 0 {
		t.Errorf("embedding column still present after migration")
	}

	// 4. Vectorstore was hydrated with the two non-null rows.
	if vec.Len() != 2 {
		t.Errorf("vectorstore.Len after migration = %d, want 2", vec.Len())
	}

	// 5. Snapshot was written.
	if _, err := vectorstore.Load(snapshotPath); err != nil {
		t.Errorf("snapshot at %q not loadable: %v", snapshotPath, err)
	}

	// 6. Semantic search through the migrated path returns the expected row.
	got, err := store.SemanticSearch([]float32{1, 0, 0}, 5, now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("SemanticSearch post-migration: %v", err)
	}
	if len(got) == 0 {
		t.Fatalf("post-migration SemanticSearch returned no results")
	}
	if got[0].Message != "alpha" {
		t.Errorf("post-migration top result = %q, want alpha", got[0].Message)
	}

	// 7. Idempotency: a second EnsureSchema must be a no-op (column is gone).
	if err := store.EnsureSchema(); err != nil {
		t.Errorf("second EnsureSchema after migration = %v, want nil", err)
	}
}

// TestSQLiteStore_SaveVectorstore_RoundTrip verifies SaveVectorstore writes a
// snapshot that vectorstore.Load can read back and that contains all stored
// vectors.
func TestSQLiteStore_SaveVectorstore_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := storage.OpenSQLite(filepath.Join(dir, "events.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	vec := vectorstore.NewBruteForceStore()
	snapshotPath := filepath.Join(dir, "events.vec")
	store := NewSQLiteStore(s.DB, vec, snapshotPath)
	if err := store.EnsureSchema(); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	now := time.Now()
	if err := store.WriteEvents([]Event{{At: now, Message: "x"}}); err != nil {
		t.Fatalf("WriteEvents: %v", err)
	}
	rows, err := store.NeedsEmbedding(10)
	if err != nil {
		t.Fatalf("NeedsEmbedding: %v", err)
	}
	if err := store.SetEmbedding(rows[0].RowID, []float32{0.5, 0.5}); err != nil {
		t.Fatalf("SetEmbedding: %v", err)
	}

	if err := store.SaveVectorstore(); err != nil {
		t.Fatalf("SaveVectorstore: %v", err)
	}

	loaded, err := vectorstore.Load(snapshotPath)
	if err != nil {
		t.Fatalf("Load snapshot: %v", err)
	}
	defer loaded.Close()
	if loaded.Len() != 1 {
		t.Errorf("loaded.Len = %d, want 1", loaded.Len())
	}
	hits, err := loaded.Search(context.Background(), []float32{1, 1}, vectorstore.SearchOpts{K: 5})
	if err != nil {
		t.Fatalf("loaded.Search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("loaded.Search hits = %d, want 1", len(hits))
	}
	if hits[0].ID != strconv.FormatInt(rows[0].RowID, 10) {
		t.Errorf("hit ID = %q, want %q", hits[0].ID, strconv.FormatInt(rows[0].RowID, 10))
	}
}

// encodeLegacyVectorForTest mirrors the pre-vectorstore on-disk layout used
// by the legacy embedding BLOB column: little-endian IEEE-754 float32 packed
// 4 bytes per value. Test-only helper kept colocated with the migration test.
func encodeLegacyVectorForTest(vec []float32) []byte {
	buf := make([]byte, 4*len(vec))
	for i, v := range vec {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return buf
}
