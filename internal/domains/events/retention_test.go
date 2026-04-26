package events

import (
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/rbryce90/linux-time-machine/internal/storage"
	"github.com/rbryce90/linux-time-machine/internal/vectorstore"
)

// newRetentionTestStore returns a fresh sqliteStore plus the underlying
// vectorstore + snapshot path so retention tests can poke at internal state.
func newRetentionTestStore(t *testing.T) (Store, *vectorstore.BruteForceStore, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := storage.OpenSQLite(filepath.Join(dir, "events.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	vec := vectorstore.NewBruteForceStore()
	t.Cleanup(func() { _ = vec.Close() })

	snapshotPath := filepath.Join(dir, "events.vec")
	store := NewSQLiteStore(s.DB, vec, snapshotPath)
	if err := store.EnsureSchema(); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	return store, vec, snapshotPath
}

func TestPurgeOlderThan_DeletesOldEvents(t *testing.T) {
	store, vec, _ := newRetentionTestStore(t)

	now := time.Now()
	old := now.Add(-100 * 24 * time.Hour)
	recent := now.Add(-1 * time.Hour)

	if err := store.WriteEvents([]Event{
		{At: old, Priority: 6, Unit: "u-old", Message: "old-1"},
		{At: old.Add(time.Minute), Priority: 6, Unit: "u-old", Message: "old-2"},
		{At: recent, Priority: 6, Unit: "u-new", Message: "fresh"},
	}); err != nil {
		t.Fatalf("WriteEvents: %v", err)
	}

	rows, err := store.NeedsEmbedding(10)
	if err != nil {
		t.Fatalf("NeedsEmbedding: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("NeedsEmbedding rows = %d, want 3", len(rows))
	}
	for _, r := range rows {
		if err := store.SetEmbedding(r.RowID, []float32{1, 0, 0}); err != nil {
			t.Fatalf("SetEmbedding: %v", err)
		}
	}
	if vec.Len() != 3 {
		t.Fatalf("vectorstore Len = %d, want 3", vec.Len())
	}

	cutoff := now.Add(-90 * 24 * time.Hour)
	n, err := store.PurgeOlderThan(cutoff)
	if err != nil {
		t.Fatalf("PurgeOlderThan: %v", err)
	}
	if n != 2 {
		t.Errorf("PurgeOlderThan returned %d, want 2", n)
	}

	got, err := store.Latest(10)
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Latest after purge = %d, want 1", len(got))
	}
	if got[0].Message != "fresh" {
		t.Errorf("surviving event = %q, want 'fresh'", got[0].Message)
	}

	if vec.Len() != 1 {
		t.Errorf("vectorstore Len after purge = %d, want 1", vec.Len())
	}
}

func TestPurgeOlderThan_Idempotent(t *testing.T) {
	store, _, _ := newRetentionTestStore(t)

	now := time.Now()
	old := now.Add(-100 * 24 * time.Hour)
	if err := store.WriteEvents([]Event{
		{At: old, Priority: 6, Message: "old"},
		{At: now, Priority: 6, Message: "fresh"},
	}); err != nil {
		t.Fatalf("WriteEvents: %v", err)
	}

	cutoff := now.Add(-90 * 24 * time.Hour)
	n1, err := store.PurgeOlderThan(cutoff)
	if err != nil {
		t.Fatalf("first PurgeOlderThan: %v", err)
	}
	if n1 != 1 {
		t.Errorf("first purge = %d, want 1", n1)
	}

	n2, err := store.PurgeOlderThan(cutoff)
	if err != nil {
		t.Fatalf("second PurgeOlderThan: %v", err)
	}
	if n2 != 0 {
		t.Errorf("second purge = %d, want 0", n2)
	}
}

// TestPurgeOlderThan_NoEmbeddings verifies that events without any embedding
// in the vectorstore still get cleaned up from SQLite — the ErrNotFound from
// vectorstore.Delete must not bubble up.
func TestPurgeOlderThan_NoEmbeddings(t *testing.T) {
	store, vec, _ := newRetentionTestStore(t)

	now := time.Now()
	old := now.Add(-100 * 24 * time.Hour)
	if err := store.WriteEvents([]Event{
		{At: old, Priority: 6, Message: "no-embedding-1"},
		{At: old.Add(time.Minute), Priority: 6, Message: "no-embedding-2"},
	}); err != nil {
		t.Fatalf("WriteEvents: %v", err)
	}

	if vec.Len() != 0 {
		t.Fatalf("precondition: vectorstore should be empty, got Len=%d", vec.Len())
	}

	cutoff := now.Add(-90 * 24 * time.Hour)
	n, err := store.PurgeOlderThan(cutoff)
	if err != nil {
		t.Fatalf("PurgeOlderThan: %v", err)
	}
	if n != 2 {
		t.Errorf("purge = %d, want 2", n)
	}

	got, err := store.Latest(10)
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Latest after purge = %d, want 0", len(got))
	}
}

// TestPurgeOlderThan_SnapshotPersists verifies the on-disk snapshot reflects
// the deletion: reloading via vectorstore.Load must not see the purged ids.
func TestPurgeOlderThan_SnapshotPersists(t *testing.T) {
	store, _, snapshotPath := newRetentionTestStore(t)

	now := time.Now()
	old := now.Add(-100 * 24 * time.Hour)
	if err := store.WriteEvents([]Event{
		{At: old, Priority: 6, Message: "old"},
		{At: now, Priority: 6, Message: "fresh"},
	}); err != nil {
		t.Fatalf("WriteEvents: %v", err)
	}

	rows, err := store.NeedsEmbedding(10)
	if err != nil {
		t.Fatalf("NeedsEmbedding: %v", err)
	}
	var oldRowID int64
	for _, r := range rows {
		if err := store.SetEmbedding(r.RowID, []float32{1, 0, 0}); err != nil {
			t.Fatalf("SetEmbedding: %v", err)
		}
		if r.Message == "old" {
			oldRowID = r.RowID
		}
	}
	if oldRowID == 0 {
		t.Fatalf("could not locate rowid for the old event")
	}

	cutoff := now.Add(-90 * 24 * time.Hour)
	if _, err := store.PurgeOlderThan(cutoff); err != nil {
		t.Fatalf("PurgeOlderThan: %v", err)
	}

	loaded, err := vectorstore.Load(snapshotPath)
	if err != nil {
		t.Fatalf("vectorstore.Load: %v", err)
	}
	defer loaded.Close()

	if loaded.Contains(strconv.FormatInt(oldRowID, 10)) {
		t.Errorf("loaded snapshot still contains purged rowid=%d", oldRowID)
	}
	if loaded.Len() != 1 {
		t.Errorf("loaded snapshot Len = %d, want 1", loaded.Len())
	}
}
