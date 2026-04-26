package storage

import (
	"path/filepath"
	"testing"
)

func TestOpenSQLite_AppliesPragmas(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pragma.db")
	s, err := OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	cases := []struct {
		pragma string
		want   string
	}{
		{"journal_mode", "wal"},
		{"synchronous", "1"}, // NORMAL = 1
		{"busy_timeout", "5000"},
		{"foreign_keys", "1"},
	}
	for _, c := range cases {
		t.Run(c.pragma, func(t *testing.T) {
			var got string
			if err := s.DB.QueryRow("PRAGMA " + c.pragma).Scan(&got); err != nil {
				t.Fatalf("scan PRAGMA %s: %v", c.pragma, err)
			}
			if got != c.want {
				t.Errorf("PRAGMA %s = %q, want %q", c.pragma, got, c.want)
			}
		})
	}
}

func TestOpenSQLite_PathIsRecorded(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "stored-path.db")
	s, err := OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	if s.Path != dbPath {
		t.Errorf("Path = %q, want %q", s.Path, dbPath)
	}
	if s.DB == nil {
		t.Fatal("DB is nil")
	}
}

func TestOpenSQLite_PingsConnection(t *testing.T) {
	s, err := OpenSQLite(filepath.Join(t.TempDir(), "ping.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	if err := s.DB.Ping(); err != nil {
		t.Errorf("Ping: %v", err)
	}
}

func TestSQLite_CloseIsIdempotentOnNil(t *testing.T) {
	var s *SQLite
	if err := s.Close(); err != nil {
		t.Errorf("nil receiver Close: %v", err)
	}
	s = &SQLite{}
	if err := s.Close(); err != nil {
		t.Errorf("zero-value Close: %v", err)
	}
}

func TestOpenSQLite_FailsOnUnopenablePath(t *testing.T) {
	// A directory path can't be opened as a SQLite DB file. modernc/sqlite
	// reports the failure on Ping rather than Open, which is fine — we just
	// need OpenSQLite to surface *some* error rather than returning a usable
	// handle.
	dir := t.TempDir()
	_, err := OpenSQLite(dir)
	if err == nil {
		t.Fatal("expected error opening a directory as SQLite db")
	}
}
