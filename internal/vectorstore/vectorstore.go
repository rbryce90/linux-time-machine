// Package vectorstore is a small, dependency-free vector index for
// single-machine workloads.
//
// It exposes a minimal Store interface backed (in v0.1) by a brute-force
// in-memory index that performs cosine similarity over normalized float32
// vectors. Vectors are normalized once at insertion so search reduces to a
// dot product, and the top-K results are extracted with a min-heap so the
// search cost is O(N log K) rather than O(N log N).
//
// The package is intentionally self-contained: it imports only the Go
// standard library and has no knowledge of the surrounding project. It is
// safe to copy or extract into its own module without modification.
//
// # Concurrency
//
// All operations on Store implementations in this package are safe for
// concurrent use by multiple goroutines. Implementations protect their
// internal state with a sync.RWMutex: reads (Search, Len) take a read lock
// and writes (Upsert, Delete, Close) take a write lock. Callers do not need
// to add their own synchronization.
//
// # Roadmap
//
// v0.1 is deliberately small. Approximate-nearest-neighbor (HNSW), hybrid
// metadata+vector queries pushed into the index, batch APIs, and pluggable
// distance metrics are all out of scope until the API has been validated
// against real workloads.
package vectorstore

import (
	"context"
	"errors"
)

// Sentinel errors returned by Store implementations. Callers should compare
// with errors.Is rather than direct equality, since wrapping is permitted.
var (
	// ErrNotFound is returned by Delete when the requested ID is not in the
	// store.
	ErrNotFound = errors.New("vectorstore: id not found")

	// ErrDimensionMismatch is returned when a vector's length does not match
	// the dimension established by the first inserted vector (for Upsert) or
	// the index's current dimension (for Search).
	ErrDimensionMismatch = errors.New("vectorstore: vector dimension mismatch")

	// ErrEmptyQuery is returned by Search when the query vector is empty or
	// has zero magnitude (cosine similarity is undefined for the zero
	// vector).
	ErrEmptyQuery = errors.New("vectorstore: empty or zero-magnitude query")

	// ErrClosed is returned by any operation invoked on a Store after Close
	// has returned.
	ErrClosed = errors.New("vectorstore: store is closed")
)

// Metadata is an arbitrary key/value bag attached to a stored vector. Values
// must be JSON-/gob-friendly if the caller intends to persist the store with
// Save; the on-disk format encodes a small set of primitive types (see
// internal/vectorstore/disk.go for the exact list). Unknown types are
// rejected at Save time rather than silently dropped.
type Metadata map[string]any

// Hit is a single search result. Score is the cosine similarity of the query
// against the stored vector, in the range [-1, 1]; for normalized inputs the
// effective range is [0, 1] when the embedding model produces non-negative
// dot products, otherwise [-1, 1].
type Hit struct {
	// ID is the caller-supplied identifier passed to Upsert.
	ID string

	// Score is the cosine similarity. Higher is more similar.
	Score float32

	// Metadata is the metadata associated with the hit at insertion time. It
	// is a copy: callers may mutate it freely without affecting the store.
	Metadata Metadata
}

// SearchOpts controls the behavior of Store.Search. The zero value is valid
// and returns no results (K defaults to 0); callers should always set K
// explicitly.
type SearchOpts struct {
	// K is the maximum number of hits to return. If K <= 0, Search returns
	// an empty slice with no error. If K exceeds the number of stored
	// vectors, all vectors are returned.
	K int

	// MinScore is an inclusive cosine-similarity floor. Hits with a score
	// strictly less than MinScore are discarded before K is applied. The
	// zero value (0.0) accepts any non-negative similarity.
	MinScore float32

	// Filter, if non-nil, is called for every candidate vector during
	// search; the candidate is included only if Filter returns true. The
	// filter sees the metadata as stored, not a copy, so implementations
	// must not mutate the map. Filter is called under a read lock and
	// should not call back into the Store.
	Filter func(Metadata) bool
}

// Store is the public interface of the package. All methods are safe for
// concurrent use; see the package documentation for the locking model.
//
// A Store has a fixed vector dimension established by the first successful
// Upsert. Subsequent Upserts with a different dimension return
// ErrDimensionMismatch. The dimension cannot be changed after the first
// insert; create a new Store instead.
type Store interface {
	// Upsert inserts a new vector or replaces an existing one with the same
	// ID. The vector is copied; callers may reuse the input slice. Metadata
	// is shallow-copied. Returns ErrDimensionMismatch if vec's length does
	// not match the store's established dimension, ErrEmptyQuery if vec is
	// empty, and ErrClosed if the store has been closed.
	Upsert(ctx context.Context, id string, vec []float32, meta Metadata) error

	// Search returns up to opts.K hits ordered by descending similarity.
	// Returns ErrDimensionMismatch if query's length does not match the
	// store's dimension, ErrEmptyQuery if query is empty or has zero
	// magnitude, and ErrClosed if the store has been closed. An empty store
	// returns (nil, nil).
	Search(ctx context.Context, query []float32, opts SearchOpts) ([]Hit, error)

	// Delete removes the vector with the given ID. Returns ErrNotFound if no
	// such ID exists, and ErrClosed if the store has been closed.
	Delete(ctx context.Context, id string) error

	// Len returns the number of vectors currently stored. After Close, Len
	// returns 0.
	Len() int

	// Contains reports whether an entry with the given ID is in the store.
	// After Close, returns false.
	Contains(id string) bool

	// Save persists the store to path. The exact format is implementation-
	// defined; callers should treat the file as opaque and pair Save with the
	// matching package-level Load function. Implementations should write
	// atomically (temp file + rename) so that a crash mid-Save does not
	// corrupt an existing snapshot.
	Save(path string) error

	// Close releases resources held by the store. After Close, all other
	// methods return ErrClosed. Close is idempotent.
	Close() error
}
