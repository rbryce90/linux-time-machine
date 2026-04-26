package vectorstore

import (
	"container/heap"
	"context"
	"math"
	"sync"
)

// BruteForceStore is an in-memory Store that scans every vector on each
// Search. It normalizes vectors at insertion time so similarity reduces to a
// dot product. It is safe for concurrent use; see package documentation for
// the locking model.
//
// BruteForceStore is appropriate for collections up to roughly a few hundred
// thousand vectors of moderate dimension (~128–1024) on a single machine.
// Beyond that, prefer an approximate-nearest-neighbor index (not yet
// implemented; see the package roadmap).
type BruteForceStore struct {
	mu     sync.RWMutex
	dim    int
	closed bool

	// Parallel slices keyed by an internal slot index. ids[slot] is the
	// caller-supplied ID, vecs[slot] is the L2-normalized vector, metas[slot]
	// is the metadata. index maps ID -> slot for O(1) lookup.
	ids   []string
	vecs  [][]float32
	metas []Metadata
	index map[string]int
}

// NewBruteForceStore returns an empty in-memory store. The dimension is
// inferred from the first Upsert; until then the store accepts any
// dimension.
func NewBruteForceStore() *BruteForceStore {
	return &BruteForceStore{
		index: make(map[string]int),
	}
}

// Upsert implements Store.
func (s *BruteForceStore) Upsert(_ context.Context, id string, vec []float32, meta Metadata) error {
	if len(vec) == 0 {
		return ErrEmptyQuery
	}
	// Copy + normalize before taking the write lock so we hold it briefly.
	normalized, ok := normalize(vec)
	if !ok {
		return ErrEmptyQuery
	}
	metaCopy := copyMetadata(meta)

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	if s.dim == 0 {
		s.dim = len(vec)
	} else if s.dim != len(vec) {
		return ErrDimensionMismatch
	}
	if slot, exists := s.index[id]; exists {
		s.vecs[slot] = normalized
		s.metas[slot] = metaCopy
		return nil
	}
	slot := len(s.ids)
	s.ids = append(s.ids, id)
	s.vecs = append(s.vecs, normalized)
	s.metas = append(s.metas, metaCopy)
	s.index[id] = slot
	return nil
}

// Search implements Store.
func (s *BruteForceStore) Search(ctx context.Context, query []float32, opts SearchOpts) ([]Hit, error) {
	if len(query) == 0 {
		return nil, ErrEmptyQuery
	}
	normalizedQuery, ok := normalize(query)
	if !ok {
		return nil, ErrEmptyQuery
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, ErrClosed
	}
	if s.dim != 0 && len(query) != s.dim {
		return nil, ErrDimensionMismatch
	}
	if opts.K <= 0 || len(s.ids) == 0 {
		return nil, nil
	}

	// Periodically check ctx so very large scans can be cancelled. Checking
	// every iteration is overkill; once per cache-friendly chunk is plenty.
	const cancelCheckInterval = 1024

	h := &minHitHeap{}
	heap.Init(h)
	for i, vec := range s.vecs {
		if i%cancelCheckInterval == 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
		}
		if opts.Filter != nil && !opts.Filter(s.metas[i]) {
			continue
		}
		score := dot(normalizedQuery, vec)
		if score < opts.MinScore {
			continue
		}
		if h.Len() < opts.K {
			heap.Push(h, scoredHit{score: score, slot: i})
			continue
		}
		if score > (*h)[0].score {
			(*h)[0] = scoredHit{score: score, slot: i}
			heap.Fix(h, 0)
		}
	}
	// Drain the heap into a descending-score slice.
	out := make([]Hit, h.Len())
	for i := len(out) - 1; i >= 0; i-- {
		sh := heap.Pop(h).(scoredHit)
		out[i] = Hit{
			ID:       s.ids[sh.slot],
			Score:    sh.score,
			Metadata: copyMetadata(s.metas[sh.slot]),
		}
	}
	return out, nil
}

// Delete implements Store.
func (s *BruteForceStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	slot, ok := s.index[id]
	if !ok {
		return ErrNotFound
	}
	last := len(s.ids) - 1
	if slot != last {
		// Swap the last element into the freed slot to keep the slices
		// dense. Update the moved element's index entry.
		s.ids[slot] = s.ids[last]
		s.vecs[slot] = s.vecs[last]
		s.metas[slot] = s.metas[last]
		s.index[s.ids[slot]] = slot
	}
	s.ids = s.ids[:last]
	s.vecs = s.vecs[:last]
	s.metas = s.metas[:last]
	delete(s.index, id)
	return nil
}

// Len implements Store.
func (s *BruteForceStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return 0
	}
	return len(s.ids)
}

// Contains implements Store.
func (s *BruteForceStore) Contains(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return false
	}
	_, ok := s.index[id]
	return ok
}

// Close implements Store. Releases the in-memory backing slices and marks
// the store unusable. Idempotent.
func (s *BruteForceStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	s.ids = nil
	s.vecs = nil
	s.metas = nil
	s.index = nil
	return nil
}

// Dim returns the vector dimension this store has been configured for, or 0
// if no vector has been inserted yet.
func (s *BruteForceStore) Dim() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dim
}

// normalize returns the L2-normalized copy of v. It returns (nil, false) if
// v is empty or has zero magnitude (the zero vector cannot be normalized
// and cosine similarity against it is undefined).
func normalize(v []float32) ([]float32, bool) {
	if len(v) == 0 {
		return nil, false
	}
	var sumSq float64
	for _, x := range v {
		sumSq += float64(x) * float64(x)
	}
	if sumSq == 0 {
		return nil, false
	}
	inv := float32(1.0 / math.Sqrt(sumSq))
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = x * inv
	}
	return out, true
}

// dot returns the dot product of two equal-length vectors. The caller is
// responsible for ensuring the lengths match; this is internal and the
// public surface validates dimensions before invoking it.
func dot(a, b []float32) float32 {
	var s float64
	for i := range a {
		s += float64(a[i]) * float64(b[i])
	}
	return float32(s)
}

// copyMetadata returns a shallow copy of m. Metadata values themselves are
// not deep-copied: callers passing pointer or slice values share the
// underlying storage. This is consistent with Go map semantics and avoids
// expensive cloning on every Search hit.
func copyMetadata(m Metadata) Metadata {
	if m == nil {
		return nil
	}
	out := make(Metadata, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// scoredHit is the heap element used during Search.
type scoredHit struct {
	score float32
	slot  int
}

// minHitHeap is a min-heap of scoredHit ordered by score. It is used to
// maintain the running top-K during a scan: the smallest of the current K
// best is always at index 0, so a new candidate need only be compared
// against (*h)[0] before being inserted.
type minHitHeap []scoredHit

func (h minHitHeap) Len() int            { return len(h) }
func (h minHitHeap) Less(i, j int) bool  { return h[i].score < h[j].score }
func (h minHitHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *minHitHeap) Push(x any)         { *h = append(*h, x.(scoredHit)) }
func (h *minHitHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}
