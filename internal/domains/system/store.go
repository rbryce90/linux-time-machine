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

type ProcessSample struct {
	At     time.Time
	PID    int32
	Name   string
	CPUPct float64
	MemRSS int64
}

// Store is the storage contract for this domain. Collector writes; tools
// and panel read. A fake can be substituted for tests.
type Store interface {
	EnsureSchema() error
	WriteSample(Sample) error
	WriteProcesses([]ProcessSample) error
	LatestSample() (Sample, error)
	SamplesInRange(start, end time.Time) ([]Sample, error)
	TopProcessesRecent(metric string, limit int) ([]ProcessSample, error)
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

func (s *sqliteStore) WriteSample(sample Sample) error {
	_, err := s.db.Exec(
		`INSERT INTO system_samples
		   (ts, cpu_pct, mem_used, mem_total, disk_read, disk_write, net_rx, net_tx)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(ts) DO NOTHING`,
		sample.At.UnixNano(),
		sample.CPUPct,
		sample.MemUsed,
		sample.MemTotal,
		sample.DiskRead,
		sample.DiskWrite,
		sample.NetRx,
		sample.NetTx,
	)
	if err != nil {
		return fmt.Errorf("write sample: %w", err)
	}
	return nil
}

func (s *sqliteStore) LatestSample() (Sample, error) {
	row := s.db.QueryRow(
		`SELECT ts, cpu_pct, mem_used, mem_total, disk_read, disk_write, net_rx, net_tx
		   FROM system_samples
		  ORDER BY ts DESC
		  LIMIT 1`)
	return scanSample(row)
}

func (s *sqliteStore) SamplesInRange(start, end time.Time) ([]Sample, error) {
	rows, err := s.db.Query(
		`SELECT ts, cpu_pct, mem_used, mem_total, disk_read, disk_write, net_rx, net_tx
		   FROM system_samples
		  WHERE ts BETWEEN ? AND ?
		  ORDER BY ts ASC`,
		start.UnixNano(), end.UnixNano())
	if err != nil {
		return nil, fmt.Errorf("samples range: %w", err)
	}
	defer rows.Close()

	var out []Sample
	for rows.Next() {
		sample, err := scanSample(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sample)
	}
	return out, rows.Err()
}

func (s *sqliteStore) WriteProcesses(ps []ProcessSample) error {
	if len(ps) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("tx begin: %w", err)
	}
	stmt, err := tx.Prepare(
		`INSERT INTO system_processes (ts, pid, name, cpu_pct, mem_rss)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(ts, pid) DO NOTHING`)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("tx prepare: %w", err)
	}
	defer stmt.Close()
	for _, p := range ps {
		if _, err := stmt.Exec(p.At.UnixNano(), p.PID, p.Name, p.CPUPct, p.MemRSS); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("tx exec: %w", err)
		}
	}
	return tx.Commit()
}

// TopProcessesRecent returns the N processes with the highest metric from
// the most recent batch written. metric is "cpu" or "mem".
func (s *sqliteStore) TopProcessesRecent(metric string, limit int) ([]ProcessSample, error) {
	orderCol := "cpu_pct"
	if metric == "mem" {
		orderCol = "mem_rss"
	}
	q := fmt.Sprintf(
		`SELECT ts, pid, name, cpu_pct, mem_rss
		   FROM system_processes
		  WHERE ts = (SELECT MAX(ts) FROM system_processes)
		  ORDER BY %s DESC
		  LIMIT ?`, orderCol)
	rows, err := s.db.Query(q, limit)
	if err != nil {
		return nil, fmt.Errorf("top processes: %w", err)
	}
	defer rows.Close()

	var out []ProcessSample
	for rows.Next() {
		var ts int64
		var p ProcessSample
		if err := rows.Scan(&ts, &p.PID, &p.Name, &p.CPUPct, &p.MemRSS); err != nil {
			return nil, fmt.Errorf("scan process: %w", err)
		}
		p.At = time.Unix(0, ts)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *sqliteStore) ProcessAt(_ types.ProcessID, _ time.Time) (ProcessInfo, error) {
	return ProcessInfo{}, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanSample(r rowScanner) (Sample, error) {
	var ts int64
	var s Sample
	if err := r.Scan(&ts, &s.CPUPct, &s.MemUsed, &s.MemTotal,
		&s.DiskRead, &s.DiskWrite, &s.NetRx, &s.NetTx); err != nil {
		if err == sql.ErrNoRows {
			return Sample{}, err
		}
		return Sample{}, fmt.Errorf("scan sample: %w", err)
	}
	s.At = time.Unix(0, ts)
	return s, nil
}
