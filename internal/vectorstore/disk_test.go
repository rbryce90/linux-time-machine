package vectorstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSaveLoadRoundtrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "store.vstr")

	src := NewBruteForceStore()
	defer src.Close()
	ctx := context.Background()

	cases := []struct {
		id   string
		vec  []float32
		meta Metadata
	}{
		{"a", []float32{1, 0, 0}, Metadata{"s": "alpha", "i": int64(42), "b": true}},
		{"b", []float32{0, 1, 0}, Metadata{"f": 3.14, "by": []byte{1, 2, 3}}},
		{"c", []float32{0.5, 0.5, 0.707}, nil},
		{"d", []float32{-1, 2, -3}, Metadata{"u": uint64(100), "n": nil}},
	}
	for _, c := range cases {
		if err := src.Upsert(ctx, c.id, c.vec, c.meta); err != nil {
			t.Fatalf("Upsert %s: %v", c.id, err)
		}
	}

	if err := src.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer loaded.Close()

	if loaded.Len() != src.Len() {
		t.Fatalf("Len = %d, want %d", loaded.Len(), src.Len())
	}
	if loaded.Dim() != src.Dim() {
		t.Fatalf("Dim = %d, want %d", loaded.Dim(), src.Dim())
	}

	// Search behavior should match between source and loaded for each
	// inserted vector.
	for _, c := range cases {
		srcHits, err := src.Search(ctx, c.vec, SearchOpts{K: 1})
		if err != nil {
			t.Fatalf("src Search: %v", err)
		}
		loadHits, err := loaded.Search(ctx, c.vec, SearchOpts{K: 1})
		if err != nil {
			t.Fatalf("loaded Search: %v", err)
		}
		if len(srcHits) != 1 || len(loadHits) != 1 {
			t.Fatalf("missing hits: src=%v loaded=%v", srcHits, loadHits)
		}
		if srcHits[0].ID != loadHits[0].ID {
			t.Fatalf("id mismatch: src=%s loaded=%s", srcHits[0].ID, loadHits[0].ID)
		}
		// Scores should be effectively identical (vectors are normalized
		// once at insert; both stores normalized the same input bytes).
		if d := srcHits[0].Score - loadHits[0].Score; d > 1e-5 || d < -1e-5 {
			t.Fatalf("score drift: src=%v loaded=%v", srcHits[0].Score, loadHits[0].Score)
		}
	}

	// Verify metadata round-tripped correctly (use a search that returns
	// the entry with metadata).
	hits, err := loaded.Search(ctx, []float32{1, 0, 0}, SearchOpts{K: 1})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if hits[0].ID != "a" {
		t.Fatalf("expected id=a, got %s", hits[0].ID)
	}
	if hits[0].Metadata["s"] != "alpha" {
		t.Fatalf("string roundtrip: %v", hits[0].Metadata)
	}
	if hits[0].Metadata["i"] != int64(42) {
		t.Fatalf("int64 roundtrip: %v (%T)", hits[0].Metadata["i"], hits[0].Metadata["i"])
	}
	if hits[0].Metadata["b"] != true {
		t.Fatalf("bool roundtrip: %v", hits[0].Metadata)
	}

	// Bytes value
	hitsB, _ := loaded.Search(ctx, []float32{0, 1, 0}, SearchOpts{K: 1})
	wantBytes := []byte{1, 2, 3}
	gotBytes, ok := hitsB[0].Metadata["by"].([]byte)
	if !ok || !reflect.DeepEqual(gotBytes, wantBytes) {
		t.Fatalf("bytes roundtrip: got %v (%T), want %v", hitsB[0].Metadata["by"], hitsB[0].Metadata["by"], wantBytes)
	}
}

func TestSaveEmptyStore(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.vstr")

	s := NewBruteForceStore()
	defer s.Close()
	if err := s.Save(path); err != nil {
		t.Fatalf("Save empty: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load empty: %v", err)
	}
	defer loaded.Close()
	if loaded.Len() != 0 {
		t.Fatalf("loaded.Len() = %d, want 0", loaded.Len())
	}
}

func TestSaveAfterCloseFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "closed.vstr")
	s := NewBruteForceStore()
	_ = s.Close()
	if err := s.Save(path); !errors.Is(err, ErrClosed) {
		t.Fatalf("err = %v, want ErrClosed", err)
	}
}

func TestLoadMissingFile(t *testing.T) {
	t.Parallel()
	_, err := Load(filepath.Join(t.TempDir(), "nope.vstr"))
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	// Missing-file errors are wrapped from os.Open; not ErrCorruptFile.
	if errors.Is(err, ErrCorruptFile) {
		t.Fatalf("missing file should not be reported as corrupt: %v", err)
	}
}

func TestLoadBadMagic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.vstr")
	if err := os.WriteFile(path, []byte("XXXX\x01\x00\x00\x00\x00\x00\x00"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := Load(path)
	if !errors.Is(err, ErrCorruptFile) {
		t.Fatalf("err = %v, want ErrCorruptFile", err)
	}
}

func TestLoadTruncatedAfterHeader(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "trunc.vstr")

	full := filepath.Join(dir, "full.vstr")
	s := NewBruteForceStore()
	defer s.Close()
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if err := s.Upsert(ctx, string(rune('a'+i)), []float32{float32(i + 1), 1, 1}, Metadata{"i": int64(i)}); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
	}
	if err := s.Save(full); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	// Truncate to roughly half — past the header but before all records.
	if err := os.WriteFile(path, data[:len(data)/2], 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err = Load(path)
	if !errors.Is(err, ErrCorruptFile) {
		t.Fatalf("err = %v, want ErrCorruptFile", err)
	}
}

func TestLoadBadVersion(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "ver.vstr")
	// magic + version=99 + dim=3 + count=0
	buf := []byte("VSTR\x63\x00\x03\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00")
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := Load(path)
	if !errors.Is(err, ErrCorruptFile) {
		t.Fatalf("err = %v, want ErrCorruptFile", err)
	}
}

func TestSaveUnsupportedMetadata(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "bad-meta.vstr")
	s := NewBruteForceStore()
	defer s.Close()
	type custom struct{ X int }
	if err := s.Upsert(context.Background(), "a", []float32{1, 0, 0}, Metadata{"k": custom{X: 1}}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	err := s.Save(path)
	if !errors.Is(err, ErrUnsupportedMetadata) {
		t.Fatalf("err = %v, want ErrUnsupportedMetadata", err)
	}
	// Save must not have left the file behind on failure.
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("dest file exists after failed Save")
	}
}

func TestMetadataAllPrimitiveTypesRoundtrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "all-types.vstr")

	src := NewBruteForceStore()
	defer src.Close()
	ctx := context.Background()

	meta := Metadata{
		"nil":     nil,
		"bool":    true,
		"int":     int(-7),
		"int8":    int8(-8),
		"int16":   int16(-16),
		"int32":   int32(-32),
		"int64":   int64(-64),
		"uint":    uint(7),
		"uint8":   uint8(8),
		"uint16":  uint16(16),
		"uint32":  uint32(32),
		"uint64":  uint64(64),
		"float32": float32(1.5),
		"float64": float64(2.5),
		"string":  "hello",
		"bytes":   []byte{0xde, 0xad, 0xbe, 0xef},
	}
	if err := src.Upsert(ctx, "a", []float32{1, 0, 0}, meta); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := src.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer loaded.Close()

	hits, err := loaded.Search(ctx, []float32{1, 0, 0}, SearchOpts{K: 1})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %v", hits)
	}
	got := hits[0].Metadata
	// All integer types decode as int64; all unsigned types decode as
	// uint64; float32 decodes as float64. This is documented in the file
	// format spec.
	checks := map[string]any{
		"nil":     nil,
		"bool":    true,
		"int":     int64(-7),
		"int8":    int64(-8),
		"int16":   int64(-16),
		"int32":   int64(-32),
		"int64":   int64(-64),
		"uint":    uint64(7),
		"uint8":   uint64(8),
		"uint16":  uint64(16),
		"uint32":  uint64(32),
		"uint64":  uint64(64),
		"float32": float64(1.5),
		"float64": float64(2.5),
		"string":  "hello",
		"bytes":   []byte{0xde, 0xad, 0xbe, 0xef},
	}
	for k, want := range checks {
		gotVal, ok := got[k]
		if !ok {
			t.Errorf("missing key %q", k)
			continue
		}
		if !reflect.DeepEqual(gotVal, want) {
			t.Errorf("key %q: got %v (%T), want %v (%T)", k, gotVal, gotVal, want, want)
		}
	}
}

func TestLoadCorruptMetadataTypeTag(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "bad-tag.vstr")

	// Build a valid file then corrupt the metadata type tag for the only
	// record so decodeMetaValue hits its default branch.
	src := NewBruteForceStore()
	defer src.Close()
	if err := src.Upsert(context.Background(), "a", []float32{1, 0, 0}, Metadata{"k": "v"}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := src.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	// Find the "v" string payload by searching for the literal byte sequence.
	// Easier: scan for byte 0x05 (metaTypeString) and overwrite with 0xFF.
	for i, b := range data {
		if b == metaTypeString {
			data[i] = 0xFF
			break
		}
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := Load(path); !errors.Is(err, ErrCorruptFile) {
		t.Fatalf("err = %v, want ErrCorruptFile", err)
	}
}

func TestDirOf(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"/tmp/foo.vstr":     "/tmp",
		"/a/b/c/d.vstr":     "/a/b/c",
		"plainfile":         ".",
		"":                  ".",
	}
	for in, want := range cases {
		if got := dirOf(in); got != want {
			t.Errorf("dirOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSaveAtomicReplace(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "store.vstr")
	if err := os.WriteFile(path, []byte("preexisting"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	s := NewBruteForceStore()
	defer s.Close()
	if err := s.Upsert(context.Background(), "a", []float32{1, 0, 0}, nil); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := s.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer loaded.Close()
	if loaded.Len() != 1 {
		t.Fatalf("loaded.Len() = %d, want 1", loaded.Len())
	}
}
