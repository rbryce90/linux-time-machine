package vectorstore

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestUpsertAndLen(t *testing.T) {
	t.Parallel()
	s := NewBruteForceStore()
	defer s.Close()
	ctx := context.Background()

	if got := s.Len(); got != 0 {
		t.Fatalf("empty Len = %d, want 0", got)
	}
	mustUpsert(t, s, "a", []float32{1, 0, 0}, nil)
	mustUpsert(t, s, "b", []float32{0, 1, 0}, Metadata{"k": "v"})
	if got := s.Len(); got != 2 {
		t.Fatalf("Len = %d, want 2", got)
	}
	// Update existing — count stays at 2.
	mustUpsert(t, s, "a", []float32{0, 0, 1}, nil)
	if got := s.Len(); got != 2 {
		t.Fatalf("Len after update = %d, want 2", got)
	}

	// Updated vector should be reflected in search.
	hits, err := s.Search(ctx, []float32{0, 0, 1}, SearchOpts{K: 1})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 || hits[0].ID != "a" {
		t.Fatalf("after update, top hit = %+v, want id=a", hits)
	}
}

func TestUpsertEmptyVectorRejected(t *testing.T) {
	t.Parallel()
	s := NewBruteForceStore()
	defer s.Close()
	err := s.Upsert(context.Background(), "x", []float32{}, nil)
	if !errors.Is(err, ErrEmptyQuery) {
		t.Fatalf("err = %v, want ErrEmptyQuery", err)
	}
}

func TestUpsertZeroVectorRejected(t *testing.T) {
	t.Parallel()
	s := NewBruteForceStore()
	defer s.Close()
	err := s.Upsert(context.Background(), "x", []float32{0, 0, 0}, nil)
	if !errors.Is(err, ErrEmptyQuery) {
		t.Fatalf("err = %v, want ErrEmptyQuery", err)
	}
}

func TestDimensionMismatchOnUpsert(t *testing.T) {
	t.Parallel()
	s := NewBruteForceStore()
	defer s.Close()
	ctx := context.Background()
	mustUpsert(t, s, "a", []float32{1, 0, 0}, nil)
	err := s.Upsert(ctx, "b", []float32{1, 0}, nil)
	if !errors.Is(err, ErrDimensionMismatch) {
		t.Fatalf("err = %v, want ErrDimensionMismatch", err)
	}
}

func TestDimensionMismatchOnSearch(t *testing.T) {
	t.Parallel()
	s := NewBruteForceStore()
	defer s.Close()
	ctx := context.Background()
	mustUpsert(t, s, "a", []float32{1, 0, 0}, nil)
	_, err := s.Search(ctx, []float32{1, 0}, SearchOpts{K: 1})
	if !errors.Is(err, ErrDimensionMismatch) {
		t.Fatalf("err = %v, want ErrDimensionMismatch", err)
	}
}

func TestSearchEmptyStore(t *testing.T) {
	t.Parallel()
	s := NewBruteForceStore()
	defer s.Close()
	hits, err := s.Search(context.Background(), []float32{1, 0, 0}, SearchOpts{K: 5})
	if err != nil {
		t.Fatalf("Search empty: %v", err)
	}
	if hits != nil {
		t.Fatalf("hits = %v, want nil", hits)
	}
}

func TestSearchOrdering(t *testing.T) {
	t.Parallel()
	s := NewBruteForceStore()
	defer s.Close()
	ctx := context.Background()
	mustUpsert(t, s, "x", []float32{1, 0, 0}, nil)
	mustUpsert(t, s, "y", []float32{0.9, 0.1, 0}, nil)
	mustUpsert(t, s, "z", []float32{0, 1, 0}, nil)
	mustUpsert(t, s, "w", []float32{-1, 0, 0}, nil)

	// Use a permissive MinScore so negative scores aren't filtered;
	// the zero value of MinScore filters any score < 0.
	hits, err := s.Search(ctx, []float32{1, 0, 0}, SearchOpts{K: 4, MinScore: -2})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 4 {
		t.Fatalf("len(hits) = %d, want 4", len(hits))
	}
	wantOrder := []string{"x", "y", "z", "w"}
	for i, want := range wantOrder {
		if hits[i].ID != want {
			t.Fatalf("hits[%d].ID = %q, want %q (full=%+v)", i, hits[i].ID, want, hits)
		}
	}
	// Scores must be descending.
	for i := 1; i < len(hits); i++ {
		if hits[i].Score > hits[i-1].Score {
			t.Fatalf("score not descending at %d: %v then %v", i, hits[i-1].Score, hits[i].Score)
		}
	}
	// Top score should be very close to 1.0 (exact match).
	if hits[0].Score < 0.999 {
		t.Fatalf("top score = %v, want ~1.0", hits[0].Score)
	}
}

func TestSearchKLargerThanLen(t *testing.T) {
	t.Parallel()
	s := NewBruteForceStore()
	defer s.Close()
	ctx := context.Background()
	mustUpsert(t, s, "a", []float32{1, 0}, nil)
	mustUpsert(t, s, "b", []float32{0, 1}, nil)
	hits, err := s.Search(ctx, []float32{1, 0}, SearchOpts{K: 100})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("len(hits) = %d, want 2", len(hits))
	}
}

func TestSearchKZeroOrNegative(t *testing.T) {
	t.Parallel()
	s := NewBruteForceStore()
	defer s.Close()
	ctx := context.Background()
	mustUpsert(t, s, "a", []float32{1, 0}, nil)
	for _, k := range []int{0, -1, -100} {
		hits, err := s.Search(ctx, []float32{1, 0}, SearchOpts{K: k})
		if err != nil {
			t.Fatalf("K=%d: %v", k, err)
		}
		if hits != nil {
			t.Fatalf("K=%d: hits = %v, want nil", k, hits)
		}
	}
}

func TestSearchMinScoreCutoff(t *testing.T) {
	t.Parallel()
	s := NewBruteForceStore()
	defer s.Close()
	ctx := context.Background()
	mustUpsert(t, s, "x", []float32{1, 0, 0}, nil)
	mustUpsert(t, s, "y", []float32{0, 1, 0}, nil) // orthogonal -> 0
	mustUpsert(t, s, "w", []float32{-1, 0, 0}, nil) // antiparallel -> -1

	hits, err := s.Search(ctx, []float32{1, 0, 0}, SearchOpts{K: 10, MinScore: 0.5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 || hits[0].ID != "x" {
		t.Fatalf("hits = %+v, want [x]", hits)
	}
}

func TestSearchFilter(t *testing.T) {
	t.Parallel()
	s := NewBruteForceStore()
	defer s.Close()
	ctx := context.Background()
	mustUpsert(t, s, "a", []float32{1, 0}, Metadata{"kind": "alpha"})
	mustUpsert(t, s, "b", []float32{0.9, 0.1}, Metadata{"kind": "beta"})
	mustUpsert(t, s, "c", []float32{0.8, 0.2}, Metadata{"kind": "alpha"})

	hits, err := s.Search(ctx, []float32{1, 0}, SearchOpts{
		K: 10,
		Filter: func(m Metadata) bool {
			return m["kind"] == "alpha"
		},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("len(hits) = %d, want 2", len(hits))
	}
	for _, h := range hits {
		if h.Metadata["kind"] != "alpha" {
			t.Fatalf("filter leaked: %+v", h)
		}
	}
}

func TestSearchEmptyQuery(t *testing.T) {
	t.Parallel()
	s := NewBruteForceStore()
	defer s.Close()
	ctx := context.Background()
	mustUpsert(t, s, "a", []float32{1, 0}, nil)

	if _, err := s.Search(ctx, []float32{}, SearchOpts{K: 1}); !errors.Is(err, ErrEmptyQuery) {
		t.Fatalf("empty: err = %v, want ErrEmptyQuery", err)
	}
	if _, err := s.Search(ctx, []float32{0, 0}, SearchOpts{K: 1}); !errors.Is(err, ErrEmptyQuery) {
		t.Fatalf("zero-mag: err = %v, want ErrEmptyQuery", err)
	}
}

func TestDelete(t *testing.T) {
	t.Parallel()
	s := NewBruteForceStore()
	defer s.Close()
	ctx := context.Background()
	mustUpsert(t, s, "a", []float32{1, 0, 0}, nil)
	mustUpsert(t, s, "b", []float32{0, 1, 0}, nil)
	mustUpsert(t, s, "c", []float32{0, 0, 1}, nil)

	if err := s.Delete(ctx, "b"); err != nil {
		t.Fatalf("Delete b: %v", err)
	}
	if got := s.Len(); got != 2 {
		t.Fatalf("Len after delete = %d, want 2", got)
	}
	hits, err := s.Search(ctx, []float32{0, 1, 0}, SearchOpts{K: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, h := range hits {
		if h.ID == "b" {
			t.Fatalf("deleted id still appears: %+v", hits)
		}
	}

	// Delete missing.
	if err := s.Delete(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: err = %v, want ErrNotFound", err)
	}

	// Delete remaining and search.
	if err := s.Delete(ctx, "a"); err != nil {
		t.Fatalf("Delete a: %v", err)
	}
	if err := s.Delete(ctx, "c"); err != nil {
		t.Fatalf("Delete c: %v", err)
	}
	if got := s.Len(); got != 0 {
		t.Fatalf("Len = %d, want 0", got)
	}
}

func TestCloseIdempotent(t *testing.T) {
	t.Parallel()
	s := NewBruteForceStore()
	mustUpsert(t, s, "a", []float32{1, 0}, nil)
	if err := s.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestOperationsAfterCloseFail(t *testing.T) {
	t.Parallel()
	s := NewBruteForceStore()
	mustUpsert(t, s, "a", []float32{1, 0}, nil)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	ctx := context.Background()
	if err := s.Upsert(ctx, "b", []float32{1, 0}, nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("Upsert: err = %v, want ErrClosed", err)
	}
	if _, err := s.Search(ctx, []float32{1, 0}, SearchOpts{K: 1}); !errors.Is(err, ErrClosed) {
		t.Fatalf("Search: err = %v, want ErrClosed", err)
	}
	if err := s.Delete(ctx, "a"); !errors.Is(err, ErrClosed) {
		t.Fatalf("Delete: err = %v, want ErrClosed", err)
	}
	if got := s.Len(); got != 0 {
		t.Fatalf("Len after close = %d, want 0", got)
	}
}

func TestMetadataIsolation(t *testing.T) {
	t.Parallel()
	s := NewBruteForceStore()
	defer s.Close()
	ctx := context.Background()
	original := Metadata{"k": "v"}
	mustUpsert(t, s, "a", []float32{1, 0}, original)
	// Mutate the input map after upsert; store should not see it.
	original["k"] = "changed"
	original["new"] = "x"

	hits, err := s.Search(ctx, []float32{1, 0}, SearchOpts{K: 1})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %+v", hits)
	}
	if hits[0].Metadata["k"] != "v" {
		t.Fatalf("store leaked input mutation: got %v", hits[0].Metadata)
	}
	if _, ok := hits[0].Metadata["new"]; ok {
		t.Fatalf("store saw post-upsert key")
	}

	// Mutating a returned hit's metadata must not affect the store.
	hits[0].Metadata["k"] = "tampered"
	hits2, _ := s.Search(ctx, []float32{1, 0}, SearchOpts{K: 1})
	if hits2[0].Metadata["k"] != "v" {
		t.Fatalf("store mutated through returned hit: %v", hits2[0].Metadata)
	}
}

func TestContextCancellationDuringSearch(t *testing.T) {
	t.Parallel()
	s := NewBruteForceStore()
	defer s.Close()

	// Insert enough vectors to exceed cancelCheckInterval (1024).
	for i := 0; i < 4000; i++ {
		mustUpsert(t, s, fmt.Sprintf("v%d", i), []float32{float32(i + 1), 1, 1}, nil)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	_, err := s.Search(ctx, []float32{1, 1, 1}, SearchOpts{K: 5})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestConcurrentSafe(t *testing.T) {
	t.Parallel()
	s := NewBruteForceStore()
	defer s.Close()
	ctx := context.Background()

	const writers = 4
	const readers = 8
	const perWriter = 200

	var wg sync.WaitGroup
	wg.Add(writers + readers)

	for w := 0; w < writers; w++ {
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				id := fmt.Sprintf("w%d-%d", w, i)
				vec := []float32{float32(i + 1), float32(w + 1), 1}
				if err := s.Upsert(ctx, id, vec, Metadata{"w": w}); err != nil {
					t.Errorf("Upsert: %v", err)
					return
				}
			}
		}(w)
	}
	for r := 0; r < readers; r++ {
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				_, err := s.Search(ctx, []float32{1, 1, 1}, SearchOpts{K: 5})
				if err != nil && !errors.Is(err, ErrDimensionMismatch) {
					// ErrDimensionMismatch can race if dim isn't yet set;
					// after first writer succeeds, no further mismatch.
					t.Errorf("Search: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()

	if got := s.Len(); got != writers*perWriter {
		t.Fatalf("Len = %d, want %d", got, writers*perWriter)
	}
}

func mustUpsert(t *testing.T, s *BruteForceStore, id string, v []float32, m Metadata) {
	t.Helper()
	if err := s.Upsert(context.Background(), id, v, m); err != nil {
		t.Fatalf("Upsert(%s): %v", id, err)
	}
}
