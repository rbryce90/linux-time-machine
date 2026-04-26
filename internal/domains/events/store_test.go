package events

import (
	"math"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/rbryce90/linux-time-machine/internal/storage"
)

// newTestStore builds an isolated SQLite-backed Store with schema applied.
func newTestStore(t *testing.T) Store {
	t.Helper()
	s, err := storage.OpenSQLite(filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	store := NewSQLiteStore(s.DB)
	if err := store.EnsureSchema(); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	return store
}

func TestEncodeDecodeVector_Roundtrip(t *testing.T) {
	cases := [][]float32{
		{1, 2, 3},
		{0},
		{-1.5, 0, 1.5, 3.14},
		{float32(math.Pi), float32(math.E)},
	}
	for _, vec := range cases {
		blob := encodeVector(vec)
		if len(blob) != 4*len(vec) {
			t.Errorf("encodeVector(%v) blob len = %d, want %d", vec, len(blob), 4*len(vec))
		}
		got, ok := decodeVector(blob)
		if !ok {
			t.Errorf("decodeVector(blob) reported failure for %v", vec)
			continue
		}
		if !reflect.DeepEqual(got, vec) {
			t.Errorf("roundtrip = %v, want %v", got, vec)
		}
	}
}

func TestDecodeVector_RejectsBadInput(t *testing.T) {
	cases := map[string][]byte{
		"empty":           {},
		"non-multiple-of-4": {1, 2, 3},
	}
	for name, b := range cases {
		t.Run(name, func(t *testing.T) {
			if _, ok := decodeVector(b); ok {
				t.Errorf("decodeVector(%v) should have failed", b)
			}
		})
	}
}

func TestNorm(t *testing.T) {
	cases := []struct {
		in   []float32
		want float32
	}{
		{[]float32{3, 4}, 5},          // classic 3-4-5
		{[]float32{0, 0, 0}, 0},
		{[]float32{1}, 1},
	}
	for _, c := range cases {
		got := norm(c.in)
		if math.Abs(float64(got-c.want)) > 1e-5 {
			t.Errorf("norm(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestCosine(t *testing.T) {
	cases := []struct {
		name string
		q, r []float32
		want float32
	}{
		{"identical vectors", []float32{1, 0, 0}, []float32{1, 0, 0}, 1},
		{"orthogonal", []float32{1, 0}, []float32{0, 1}, 0},
		{"opposite", []float32{1, 0}, []float32{-1, 0}, -1},
		{"length mismatch returns 0", []float32{1, 2}, []float32{1}, 0},
		{"zero r returns 0", []float32{1, 0}, []float32{0, 0}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			qNorm := norm(c.q)
			got := cosine(c.q, c.r, qNorm)
			if math.Abs(float64(got-c.want)) > 1e-5 {
				t.Errorf("cosine = %v, want %v", got, c.want)
			}
		})
	}
}

func TestCosine_ZeroQNormReturnsZero(t *testing.T) {
	// Caller passes their pre-computed query norm; if q is the zero vector the
	// passed norm will be 0 and we must not divide by it.
	got := cosine([]float32{0, 0}, []float32{1, 1}, 0)
	if got != 0 {
		t.Errorf("cosine with qNorm=0 = %v, want 0", got)
	}
}

func TestSQLiteStore_WriteAndLatest(t *testing.T) {
	store := newTestStore(t)

	now := time.Now().Truncate(time.Microsecond) // SQLite ts is UnixNano; truncate avoids float drift assertions
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
	// Latest is DESC by ts; index 0 should be `now` event.
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

	// Match by unit, not message.
	got, err = store.Search("sshd", 10, since)
	if err != nil {
		t.Fatalf("Search by unit: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("Search('sshd') returned %d, want 2", len(got))
	}

	// since filter excludes older rows.
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
	// Returned ASC by ts.
	wantOrder := []string{"in window early", "at anchor", "in window late"}
	for i, e := range got {
		if e.Message != wantOrder[i] {
			t.Errorf("EventsNear[%d] = %q, want %q", i, e.Message, wantOrder[i])
		}
	}
}

func TestSQLiteStore_NeedsEmbedding_AndSetEmbedding(t *testing.T) {
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
	// NeedsEmbedding orders newest-first per the contract in store.go.
	if rows[0].Message != "newest" {
		t.Errorf("NeedsEmbedding[0] = %q, want 'newest' (DESC ordering)", rows[0].Message)
	}

	// Embedding the newest row should remove it from NeedsEmbedding output.
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

func TestSQLiteStore_SemanticSearch_RanksByCosine(t *testing.T) {
	store := newTestStore(t)
	now := time.Now()

	// Write three rows we'll embed by hand.
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

	// Map message → embedding so we can score deterministically.
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

func TestSQLiteStore_SemanticSearch_SkipsRowsWithoutEmbedding(t *testing.T) {
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
		t.Errorf("SemanticSearch should skip rows with NULL embedding, got %v", got)
	}
}

func TestSQLiteStore_EnsureSchema_Idempotent(t *testing.T) {
	store := newTestStore(t) // EnsureSchema already called once
	if err := store.EnsureSchema(); err != nil {
		t.Errorf("second EnsureSchema = %v, want nil (idempotent migration)", err)
	}
	// Sanity: the embedding-column migration check must not have broken state.
	if err := store.WriteEvents([]Event{{At: time.Now(), Priority: 6, Message: "x"}}); err != nil {
		t.Errorf("WriteEvents after re-EnsureSchema: %v", err)
	}
}

