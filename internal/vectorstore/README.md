# vectorstore

A small, dependency-free vector index for single-machine workloads. Pure Go,
standard library only, no CGO. Designed to be lifted out of `internal/` into
its own module once the API has stabilized against real use; nothing in this
package imports anything else from `linux-time-machine`.

## Status

Lives in `internal/` while the API stabilizes against real use; will move to
its own module when stable.

## Purpose

`vectorstore` provides a brute-force in-memory vector index with cosine
similarity, optional metadata filters, and a length-prefixed binary
snapshot/restore format. It is intentionally minimal — appropriate for tens
to hundreds of thousands of vectors of moderate dimension on a single host.
Beyond that scale, prefer an approximate-nearest-neighbor index (see
roadmap).

## Public API

```go
type Store interface {
    Upsert(ctx context.Context, id string, vec []float32, meta Metadata) error
    Search(ctx context.Context, query []float32, opts SearchOpts) ([]Hit, error)
    Delete(ctx context.Context, id string) error
    Len() int
    Close() error
}

type Metadata map[string]any

type Hit struct {
    ID       string
    Score    float32
    Metadata Metadata
}

type SearchOpts struct {
    K        int
    MinScore float32
    Filter   func(Metadata) bool
}

// Sentinel errors (compare with errors.Is):
var (
    ErrNotFound          = errors.New("vectorstore: id not found")
    ErrDimensionMismatch = errors.New("vectorstore: vector dimension mismatch")
    ErrEmptyQuery        = errors.New("vectorstore: empty or zero-magnitude query")
    ErrClosed            = errors.New("vectorstore: store is closed")
    ErrCorruptFile       = errors.New("vectorstore: corrupt or truncated file")
    ErrUnsupportedMetadata = errors.New("vectorstore: unsupported metadata value type")
)

// Implementation:
func NewBruteForceStore() *BruteForceStore
func (s *BruteForceStore) Save(path string) error
func Load(path string) (*BruteForceStore, error)
```

The `BruteForceStore` is the v0.1 reference implementation: in-memory,
brute-force cosine similarity over normalized vectors, K-best selection via
a min-heap (O(N log K)), and optional metadata filters applied before the
score check. Vector dimension is fixed by the first successful `Upsert` and
enforced thereafter.

### Concurrency

All `Store` methods are safe for concurrent use. The `BruteForceStore`
protects its state with a `sync.RWMutex`: reads (`Search`, `Len`, `Dim`,
`Save`) take a read lock; writes (`Upsert`, `Delete`, `Close`) take a write
lock. Callers do not need their own synchronization.

## File format

All integers are little-endian.

```
header:
  magic    [4]byte   "VSTR"
  version  uint16    1
  dim      uint32    vector dimension
  count    uint64    number of records

record (repeated `count` times):
  idLen    uint16
  id       [idLen]byte (UTF-8)
  vec      [dim]float32 (IEEE-754, 4 bytes each)
  metaLen  uint32     length of the metadata blob in bytes
  meta     [metaLen]byte

metadata blob:
  entries  uint32
  for each entry:
    keyLen uint16
    key    [keyLen]byte (UTF-8)
    tag    uint8       (see below)
    value  variable    (depends on tag)

metadata value tags:
  0x00 nil      no payload
  0x01 bool     uint8 (0/1)
  0x02 int64    int64
  0x03 uint64   uint64
  0x04 float64  float64 (IEEE-754)
  0x05 string   uint32 length, UTF-8 bytes
  0x06 bytes    uint32 length, raw bytes
```

`Save` writes to a temp file in the destination directory and `os.Rename`s
into place so an aborted save cannot corrupt an existing snapshot. The
process keeps the snapshot on the same filesystem as the destination so
rename is atomic. Unsupported metadata value types (slices, nested maps,
arbitrary structs) cause `Save` to return `ErrUnsupportedMetadata` rather
than silently dropping the value; the destination file is not modified.

All integer-typed metadata values widen to `int64` on the wire (or `uint64`
for unsigned). All floats widen to `float64`. Round-tripping therefore
returns `int64` / `uint64` / `float64` rather than the original narrower
type. This is by design — the wire format is intentionally tiny and
ambiguity-free.

## Roadmap when extracted

Deliberately deferred from v0.1:

- **HNSW (or other ANN indexes)** — brute-force is fine to a few hundred
  thousand vectors; beyond that we'll need a graph-based index.
- **Hybrid filter+vector queries pushed into the index** — currently
  `Filter` is a post-filter callback; a real implementation would push
  metadata predicates into the index itself for selectivity.
- **Batch APIs** — `UpsertBatch`, `SearchBatch` for amortizing lock and
  allocation overhead over many ops.
- **Observability hooks** — counters / latency hooks via a tiny Stats
  interface or `expvar` integration.
- **Pluggable distance metrics** — currently cosine only; dot-product,
  Euclidean, and learned metrics are obvious extensions.
- **Streaming Save / Load** — current Save buffers writes through
  `bufio.Writer`; very large stores may want a streaming-from-disk Search
  path that doesn't require loading everything into memory.
- **Versioned migration** — the on-disk version is `1`; future versions
  will need a forward-compatible upgrade path.
- **Integrity check on save** — a CRC32 over the body would catch silent
  corruption that currently only manifests as a decode error mid-load.
- **Richer metadata types** — slices, nested maps, time.Time. Punted to
  keep the wire format trivial; users who need them can JSON-encode into a
  `string` value today.
