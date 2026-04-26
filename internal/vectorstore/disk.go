package vectorstore

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
)

// On-disk file format (little-endian throughout).
//
//   magic        [4]byte   "VSTR"
//   version      uint16    currently 1
//   dim          uint32    vector dimension
//   count        uint64    number of records
//   records      [count]record
//
// record:
//   idLen        uint16
//   id           [idLen]byte (UTF-8)
//   vec          [dim]float32 (4 bytes each, IEEE-754 little-endian)
//   metaLen      uint32     length in bytes of the metadata blob that follows
//   meta         [metaLen]byte (see encodeMetadata)
//
// metadata blob:
//   entries      uint32    number of key/value pairs
//   for each entry:
//     keyLen     uint16
//     key        [keyLen]byte (UTF-8)
//     typeTag    uint8     one of metaTypeXxx below
//     value      depends on tag (see encodeMetaValue)
//
// Supported metadata value types:
//   metaTypeNil    : (no payload)
//   metaTypeBool   : uint8 (0 or 1)
//   metaTypeInt64  : int64
//   metaTypeUint64 : uint64
//   metaTypeFloat64: float64
//   metaTypeString : uint32 length, then UTF-8 bytes
//   metaTypeBytes  : uint32 length, then raw bytes
//
// Anything else (slices, nested maps, structs, channels) causes Save to
// return an error rather than silently dropping data. Round-tripping
// non-supported types is an explicit non-goal of v0.1.

const (
	diskMagic   = "VSTR"
	diskVersion = uint16(1)
)

const (
	metaTypeNil     uint8 = 0
	metaTypeBool    uint8 = 1
	metaTypeInt64   uint8 = 2
	metaTypeUint64  uint8 = 3
	metaTypeFloat64 uint8 = 4
	metaTypeString  uint8 = 5
	metaTypeBytes   uint8 = 6
)

// ErrCorruptFile is returned by Load when the file fails magic, version, or
// length checks. It wraps the underlying io error when one is available.
var ErrCorruptFile = errors.New("vectorstore: corrupt or truncated file")

// ErrUnsupportedMetadata is returned by Save when a metadata value's type
// cannot be encoded by the on-disk format.
var ErrUnsupportedMetadata = errors.New("vectorstore: unsupported metadata value type")

// Save writes the contents of s to path atomically (temp file + rename).
// It returns ErrUnsupportedMetadata if any stored metadata value has a type
// the on-disk format does not understand; in that case the destination file
// is not modified.
func (s *BruteForceStore) Save(path string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return ErrClosed
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".vstr-*")
	if err != nil {
		return fmt.Errorf("vectorstore: create temp: %w", err)
	}
	tmpName := tmp.Name()
	// Best-effort cleanup if anything below fails.
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}

	w := bufio.NewWriter(tmp)
	if _, err := w.WriteString(diskMagic); err != nil {
		cleanup()
		return fmt.Errorf("vectorstore: write magic: %w", err)
	}
	if err := binary.Write(w, binary.LittleEndian, diskVersion); err != nil {
		cleanup()
		return fmt.Errorf("vectorstore: write version: %w", err)
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(s.dim)); err != nil {
		cleanup()
		return fmt.Errorf("vectorstore: write dim: %w", err)
	}
	if err := binary.Write(w, binary.LittleEndian, uint64(len(s.ids))); err != nil {
		cleanup()
		return fmt.Errorf("vectorstore: write count: %w", err)
	}

	for i, id := range s.ids {
		if len(id) > math.MaxUint16 {
			cleanup()
			return fmt.Errorf("vectorstore: id too long (%d > %d)", len(id), math.MaxUint16)
		}
		if err := binary.Write(w, binary.LittleEndian, uint16(len(id))); err != nil {
			cleanup()
			return fmt.Errorf("vectorstore: write id len: %w", err)
		}
		if _, err := w.WriteString(id); err != nil {
			cleanup()
			return fmt.Errorf("vectorstore: write id: %w", err)
		}
		for _, v := range s.vecs[i] {
			if err := binary.Write(w, binary.LittleEndian, math.Float32bits(v)); err != nil {
				cleanup()
				return fmt.Errorf("vectorstore: write vec: %w", err)
			}
		}
		blob, err := encodeMetadata(s.metas[i])
		if err != nil {
			cleanup()
			return err
		}
		if err := binary.Write(w, binary.LittleEndian, uint32(len(blob))); err != nil {
			cleanup()
			return fmt.Errorf("vectorstore: write meta len: %w", err)
		}
		if _, err := w.Write(blob); err != nil {
			cleanup()
			return fmt.Errorf("vectorstore: write meta: %w", err)
		}
	}
	if err := w.Flush(); err != nil {
		cleanup()
		return fmt.Errorf("vectorstore: flush: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("vectorstore: fsync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("vectorstore: close temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("vectorstore: rename: %w", err)
	}
	return nil
}

// Load reads a snapshot previously written by Save and returns a fully
// populated BruteForceStore. The returned store is independent of the file:
// further changes are in-memory only until another Save.
//
// Load returns ErrCorruptFile (wrapped) if the file's magic, version, or
// internal length fields do not match expectations or if the file is
// truncated.
func Load(path string) (*BruteForceStore, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("vectorstore: open: %w", err)
	}
	defer f.Close()

	r := bufio.NewReader(f)

	magic := make([]byte, 4)
	if _, err := io.ReadFull(r, magic); err != nil {
		return nil, fmt.Errorf("%w: read magic: %w", ErrCorruptFile, err)
	}
	if string(magic) != diskMagic {
		return nil, fmt.Errorf("%w: bad magic %q", ErrCorruptFile, magic)
	}
	var version uint16
	if err := binary.Read(r, binary.LittleEndian, &version); err != nil {
		return nil, fmt.Errorf("%w: read version: %w", ErrCorruptFile, err)
	}
	if version != diskVersion {
		return nil, fmt.Errorf("%w: unsupported version %d", ErrCorruptFile, version)
	}
	var dim uint32
	if err := binary.Read(r, binary.LittleEndian, &dim); err != nil {
		return nil, fmt.Errorf("%w: read dim: %w", ErrCorruptFile, err)
	}
	var count uint64
	if err := binary.Read(r, binary.LittleEndian, &count); err != nil {
		return nil, fmt.Errorf("%w: read count: %w", ErrCorruptFile, err)
	}

	store := NewBruteForceStore()
	store.dim = int(dim)
	store.ids = make([]string, 0, count)
	store.vecs = make([][]float32, 0, count)
	store.metas = make([]Metadata, 0, count)

	for i := uint64(0); i < count; i++ {
		var idLen uint16
		if err := binary.Read(r, binary.LittleEndian, &idLen); err != nil {
			return nil, fmt.Errorf("%w: read id len at %d: %w", ErrCorruptFile, i, err)
		}
		idBuf := make([]byte, idLen)
		if _, err := io.ReadFull(r, idBuf); err != nil {
			return nil, fmt.Errorf("%w: read id at %d: %w", ErrCorruptFile, i, err)
		}
		vec := make([]float32, dim)
		for j := uint32(0); j < dim; j++ {
			var bits uint32
			if err := binary.Read(r, binary.LittleEndian, &bits); err != nil {
				return nil, fmt.Errorf("%w: read vec[%d] at %d: %w", ErrCorruptFile, j, i, err)
			}
			vec[j] = math.Float32frombits(bits)
		}
		var metaLen uint32
		if err := binary.Read(r, binary.LittleEndian, &metaLen); err != nil {
			return nil, fmt.Errorf("%w: read meta len at %d: %w", ErrCorruptFile, i, err)
		}
		metaBuf := make([]byte, metaLen)
		if _, err := io.ReadFull(r, metaBuf); err != nil {
			return nil, fmt.Errorf("%w: read meta at %d: %w", ErrCorruptFile, i, err)
		}
		meta, err := decodeMetadata(metaBuf)
		if err != nil {
			return nil, fmt.Errorf("%w: decode meta at %d: %w", ErrCorruptFile, i, err)
		}
		id := string(idBuf)
		store.ids = append(store.ids, id)
		store.vecs = append(store.vecs, vec)
		store.metas = append(store.metas, meta)
		store.index[id] = len(store.ids) - 1
	}
	return store, nil
}

func encodeMetadata(m Metadata) ([]byte, error) {
	// Write to a bytes buffer-equivalent via append; avoids importing
	// bytes.Buffer for such a tiny payload.
	var buf []byte
	buf = binary.LittleEndian.AppendUint32(buf, uint32(len(m)))
	for k, v := range m {
		if len(k) > math.MaxUint16 {
			return nil, fmt.Errorf("vectorstore: metadata key too long (%d > %d)", len(k), math.MaxUint16)
		}
		buf = binary.LittleEndian.AppendUint16(buf, uint16(len(k)))
		buf = append(buf, []byte(k)...)
		var err error
		buf, err = encodeMetaValue(buf, v)
		if err != nil {
			return nil, fmt.Errorf("vectorstore: metadata key %q: %w", k, err)
		}
	}
	return buf, nil
}

func encodeMetaValue(buf []byte, v any) ([]byte, error) {
	switch val := v.(type) {
	case nil:
		return append(buf, metaTypeNil), nil
	case bool:
		buf = append(buf, metaTypeBool)
		var b uint8
		if val {
			b = 1
		}
		return append(buf, b), nil
	case int:
		buf = append(buf, metaTypeInt64)
		return binary.LittleEndian.AppendUint64(buf, uint64(int64(val))), nil
	case int8:
		buf = append(buf, metaTypeInt64)
		return binary.LittleEndian.AppendUint64(buf, uint64(int64(val))), nil
	case int16:
		buf = append(buf, metaTypeInt64)
		return binary.LittleEndian.AppendUint64(buf, uint64(int64(val))), nil
	case int32:
		buf = append(buf, metaTypeInt64)
		return binary.LittleEndian.AppendUint64(buf, uint64(int64(val))), nil
	case int64:
		buf = append(buf, metaTypeInt64)
		return binary.LittleEndian.AppendUint64(buf, uint64(val)), nil
	case uint:
		buf = append(buf, metaTypeUint64)
		return binary.LittleEndian.AppendUint64(buf, uint64(val)), nil
	case uint8:
		buf = append(buf, metaTypeUint64)
		return binary.LittleEndian.AppendUint64(buf, uint64(val)), nil
	case uint16:
		buf = append(buf, metaTypeUint64)
		return binary.LittleEndian.AppendUint64(buf, uint64(val)), nil
	case uint32:
		buf = append(buf, metaTypeUint64)
		return binary.LittleEndian.AppendUint64(buf, uint64(val)), nil
	case uint64:
		buf = append(buf, metaTypeUint64)
		return binary.LittleEndian.AppendUint64(buf, val), nil
	case float32:
		buf = append(buf, metaTypeFloat64)
		return binary.LittleEndian.AppendUint64(buf, math.Float64bits(float64(val))), nil
	case float64:
		buf = append(buf, metaTypeFloat64)
		return binary.LittleEndian.AppendUint64(buf, math.Float64bits(val)), nil
	case string:
		buf = append(buf, metaTypeString)
		buf = binary.LittleEndian.AppendUint32(buf, uint32(len(val)))
		return append(buf, []byte(val)...), nil
	case []byte:
		buf = append(buf, metaTypeBytes)
		buf = binary.LittleEndian.AppendUint32(buf, uint32(len(val)))
		return append(buf, val...), nil
	default:
		return nil, fmt.Errorf("%w: %T", ErrUnsupportedMetadata, v)
	}
}

func decodeMetadata(buf []byte) (Metadata, error) {
	if len(buf) == 0 {
		return nil, nil
	}
	if len(buf) < 4 {
		return nil, fmt.Errorf("metadata blob too short")
	}
	entries := binary.LittleEndian.Uint32(buf[:4])
	pos := 4
	if entries == 0 {
		return Metadata{}, nil
	}
	out := make(Metadata, entries)
	for i := uint32(0); i < entries; i++ {
		if pos+2 > len(buf) {
			return nil, fmt.Errorf("truncated key len at entry %d", i)
		}
		keyLen := int(binary.LittleEndian.Uint16(buf[pos : pos+2]))
		pos += 2
		if pos+keyLen > len(buf) {
			return nil, fmt.Errorf("truncated key at entry %d", i)
		}
		key := string(buf[pos : pos+keyLen])
		pos += keyLen
		if pos+1 > len(buf) {
			return nil, fmt.Errorf("truncated type tag at entry %d", i)
		}
		tag := buf[pos]
		pos++
		val, n, err := decodeMetaValue(tag, buf[pos:])
		if err != nil {
			return nil, fmt.Errorf("entry %d (%q): %w", i, key, err)
		}
		pos += n
		out[key] = val
	}
	return out, nil
}

func decodeMetaValue(tag uint8, buf []byte) (any, int, error) {
	switch tag {
	case metaTypeNil:
		return nil, 0, nil
	case metaTypeBool:
		if len(buf) < 1 {
			return nil, 0, fmt.Errorf("truncated bool")
		}
		return buf[0] != 0, 1, nil
	case metaTypeInt64:
		if len(buf) < 8 {
			return nil, 0, fmt.Errorf("truncated int64")
		}
		return int64(binary.LittleEndian.Uint64(buf[:8])), 8, nil
	case metaTypeUint64:
		if len(buf) < 8 {
			return nil, 0, fmt.Errorf("truncated uint64")
		}
		return binary.LittleEndian.Uint64(buf[:8]), 8, nil
	case metaTypeFloat64:
		if len(buf) < 8 {
			return nil, 0, fmt.Errorf("truncated float64")
		}
		return math.Float64frombits(binary.LittleEndian.Uint64(buf[:8])), 8, nil
	case metaTypeString:
		if len(buf) < 4 {
			return nil, 0, fmt.Errorf("truncated string len")
		}
		n := int(binary.LittleEndian.Uint32(buf[:4]))
		if 4+n > len(buf) {
			return nil, 0, fmt.Errorf("truncated string body")
		}
		return string(buf[4 : 4+n]), 4 + n, nil
	case metaTypeBytes:
		if len(buf) < 4 {
			return nil, 0, fmt.Errorf("truncated bytes len")
		}
		n := int(binary.LittleEndian.Uint32(buf[:4]))
		if 4+n > len(buf) {
			return nil, 0, fmt.Errorf("truncated bytes body")
		}
		out := make([]byte, n)
		copy(out, buf[4:4+n])
		return out, 4 + n, nil
	default:
		return nil, 0, fmt.Errorf("unknown type tag 0x%02x", tag)
	}
}

