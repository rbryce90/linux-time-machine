package system

import (
	"database/sql"
	_ "embed"
	"fmt"
	"time"

	"github.com/rbryce90/linux-time-machine/internal/types"
)

//go:embed schema.sql
var schemaSQL string

type Sample struct {
	At        time.Time
	CPUPct    float64
	MemUsed   int64
	MemTotal  int64
	DiskRead  int64
	DiskWrite int64
	NetRx     int64
	NetTx     int64
}

// Store is the storage contract for this domain. Collector writes; tools
// and panel read. A fake can be substituted for tests.
type Store interface {
	EnsureSchema() error
	WriteSample(Sample) error
	ProcessAt(pid types.ProcessID, at time.Time) (ProcessInfo, error)
}

type sqliteStore struct {
	db *sql.DB
}

func NewSQLiteStore(db *sql.DB) Store {
	return &sqliteStore{db: db}
}

func (s *sqliteStore) EnsureSchema() error {
	_, err := s.db.Exec(schemaSQL)
	if err != nil {
		return fmt.Errorf("system schema: %w", err)
	}
	return nil
}

func (s *sqliteStore) WriteSample(_ Sample) error {
	return nil
}

func (s *sqliteStore) ProcessAt(_ types.ProcessID, _ time.Time) (ProcessInfo, error) {
	return ProcessInfo{}, nil
}
