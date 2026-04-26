package system

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/rbryce90/linux-time-machine/internal/storage"
)

func newTestStore(t *testing.T) Store {
	t.Helper()
	s, err := storage.OpenSQLite(filepath.Join(t.TempDir(), "system.db"))
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

func TestSQLiteStore_LatestSample_ErrNoRowsWhenEmpty(t *testing.T) {
	store := newTestStore(t)
	_, err := store.LatestSample()
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("LatestSample on empty db: err = %v, want sql.ErrNoRows", err)
	}
}

func TestSQLiteStore_WriteAndLatestSample(t *testing.T) {
	store := newTestStore(t)
	now := time.Now()

	earlier := Sample{
		At: now.Add(-time.Second), CPUPct: 10, MemUsed: 100, MemTotal: 1000,
		DiskRead: 1, DiskWrite: 2, NetRx: 3, NetTx: 4,
	}
	later := Sample{
		At: now, CPUPct: 50, MemUsed: 500, MemTotal: 1000,
		DiskRead: 10, DiskWrite: 20, NetRx: 30, NetTx: 40,
	}
	if err := store.WriteSample(earlier); err != nil {
		t.Fatalf("WriteSample earlier: %v", err)
	}
	if err := store.WriteSample(later); err != nil {
		t.Fatalf("WriteSample later: %v", err)
	}

	got, err := store.LatestSample()
	if err != nil {
		t.Fatalf("LatestSample: %v", err)
	}
	if got.CPUPct != 50 || got.MemUsed != 500 {
		t.Errorf("LatestSample = %+v, want the 'later' sample (cpu=50)", got)
	}
}

func TestSQLiteStore_WriteSample_DuplicateTimestampIgnored(t *testing.T) {
	// schema declares ts as PRIMARY KEY with ON CONFLICT DO NOTHING — verify
	// that contract because the collector relies on it (a clock that produces
	// the same UnixNano twice shouldn't crash).
	store := newTestStore(t)
	at := time.Now()
	if err := store.WriteSample(Sample{At: at, CPUPct: 1, MemTotal: 1}); err != nil {
		t.Fatalf("first WriteSample: %v", err)
	}
	if err := store.WriteSample(Sample{At: at, CPUPct: 99, MemTotal: 1}); err != nil {
		t.Errorf("duplicate WriteSample: %v (should be ignored, not error)", err)
	}

	got, err := store.LatestSample()
	if err != nil {
		t.Fatalf("LatestSample: %v", err)
	}
	if got.CPUPct != 1 {
		t.Errorf("LatestSample.CPUPct = %v, want 1 (DO NOTHING preserves first row)", got.CPUPct)
	}
}

func TestSQLiteStore_SamplesInRange_BoundsAreInclusive(t *testing.T) {
	store := newTestStore(t)
	t0 := time.Now().Truncate(time.Second)

	samples := []Sample{
		{At: t0, CPUPct: 1, MemTotal: 1},
		{At: t0.Add(1 * time.Second), CPUPct: 2, MemTotal: 1},
		{At: t0.Add(2 * time.Second), CPUPct: 3, MemTotal: 1},
		{At: t0.Add(3 * time.Second), CPUPct: 4, MemTotal: 1},
	}
	for _, s := range samples {
		if err := store.WriteSample(s); err != nil {
			t.Fatalf("WriteSample: %v", err)
		}
	}

	// Half-open look: only the middle two.
	got, err := store.SamplesInRange(t0.Add(1*time.Second), t0.Add(2*time.Second))
	if err != nil {
		t.Fatalf("SamplesInRange: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("SamplesInRange returned %d, want 2", len(got))
	}
	if got[0].CPUPct != 2 || got[1].CPUPct != 3 {
		t.Errorf("SamplesInRange returned cpu=[%v %v], want [2 3] (ASC)",
			got[0].CPUPct, got[1].CPUPct)
	}
}

func TestSQLiteStore_SampleAt_ReturnsMostRecentAtOrBefore(t *testing.T) {
	store := newTestStore(t)
	t0 := time.Now().Truncate(time.Second)

	for i, cpu := range []float64{10, 20, 30} {
		if err := store.WriteSample(Sample{
			At: t0.Add(time.Duration(i) * time.Second), CPUPct: cpu, MemTotal: 1,
		}); err != nil {
			t.Fatalf("WriteSample %d: %v", i, err)
		}
	}

	// Cursor falls between the first and second sample → expect first.
	got, err := store.SampleAt(t0.Add(500 * time.Millisecond))
	if err != nil {
		t.Fatalf("SampleAt: %v", err)
	}
	if got.CPUPct != 10 {
		t.Errorf("SampleAt mid-gap CPUPct = %v, want 10", got.CPUPct)
	}

	// Cursor at exact sample time → that sample.
	got, err = store.SampleAt(t0.Add(time.Second))
	if err != nil {
		t.Fatalf("SampleAt exact: %v", err)
	}
	if got.CPUPct != 20 {
		t.Errorf("SampleAt exact CPUPct = %v, want 20", got.CPUPct)
	}

	// Cursor before any sample → ErrNoRows.
	_, err = store.SampleAt(t0.Add(-time.Hour))
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("SampleAt before-data err = %v, want sql.ErrNoRows", err)
	}
}

func TestSQLiteStore_TimeBounds(t *testing.T) {
	store := newTestStore(t)

	// Empty db → ErrNoRows.
	_, _, err := store.TimeBounds()
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("TimeBounds empty: err = %v, want sql.ErrNoRows", err)
	}

	t0 := time.Now().Truncate(time.Second)
	for i := 0; i < 3; i++ {
		if err := store.WriteSample(Sample{
			At: t0.Add(time.Duration(i) * time.Second), CPUPct: 1, MemTotal: 1,
		}); err != nil {
			t.Fatalf("WriteSample: %v", err)
		}
	}

	minT, maxT, err := store.TimeBounds()
	if err != nil {
		t.Fatalf("TimeBounds: %v", err)
	}
	if !minT.Equal(t0) {
		t.Errorf("min = %v, want %v", minT, t0)
	}
	if !maxT.Equal(t0.Add(2 * time.Second)) {
		t.Errorf("max = %v, want %v", maxT, t0.Add(2*time.Second))
	}
}

func TestSQLiteStore_TopProcessesRecent_OrderingByMetric(t *testing.T) {
	store := newTestStore(t)
	t0 := time.Now()

	procs := []ProcessSample{
		{At: t0, PID: 1, Name: "alpha", CPUPct: 5, MemRSS: 100},
		{At: t0, PID: 2, Name: "beta", CPUPct: 50, MemRSS: 50},
		{At: t0, PID: 3, Name: "gamma", CPUPct: 25, MemRSS: 200},
	}
	if err := store.WriteProcesses(procs); err != nil {
		t.Fatalf("WriteProcesses: %v", err)
	}

	t.Run("by cpu", func(t *testing.T) {
		got, err := store.TopProcessesRecent("cpu", 10)
		if err != nil {
			t.Fatalf("TopProcessesRecent cpu: %v", err)
		}
		if len(got) != 3 {
			t.Fatalf("got %d procs, want 3", len(got))
		}
		want := []string{"beta", "gamma", "alpha"} // 50, 25, 5
		for i, n := range want {
			if got[i].Name != n {
				t.Errorf("by cpu [%d] = %q, want %q", i, got[i].Name, n)
			}
		}
	})

	t.Run("by mem", func(t *testing.T) {
		got, err := store.TopProcessesRecent("mem", 10)
		if err != nil {
			t.Fatalf("TopProcessesRecent mem: %v", err)
		}
		want := []string{"gamma", "alpha", "beta"} // 200, 100, 50
		for i, n := range want {
			if got[i].Name != n {
				t.Errorf("by mem [%d] = %q, want %q", i, got[i].Name, n)
			}
		}
	})

	t.Run("limit honored", func(t *testing.T) {
		got, err := store.TopProcessesRecent("cpu", 1)
		if err != nil {
			t.Fatalf("TopProcessesRecent limit: %v", err)
		}
		if len(got) != 1 || got[0].Name != "beta" {
			t.Errorf("limit=1 returned %+v, want [beta]", got)
		}
	})
}

func TestSQLiteStore_TopProcessesRecent_OnlyLatestBatch(t *testing.T) {
	// The query uses MAX(ts) so older batches must not bleed into results.
	store := newTestStore(t)
	t0 := time.Now()

	older := []ProcessSample{
		{At: t0.Add(-time.Minute), PID: 99, Name: "stale-cpu-king", CPUPct: 95, MemRSS: 1},
	}
	newer := []ProcessSample{
		{At: t0, PID: 1, Name: "current", CPUPct: 5, MemRSS: 50},
	}
	if err := store.WriteProcesses(older); err != nil {
		t.Fatalf("WriteProcesses older: %v", err)
	}
	if err := store.WriteProcesses(newer); err != nil {
		t.Fatalf("WriteProcesses newer: %v", err)
	}

	got, err := store.TopProcessesRecent("cpu", 10)
	if err != nil {
		t.Fatalf("TopProcessesRecent: %v", err)
	}
	if len(got) != 1 || got[0].Name != "current" {
		t.Errorf("TopProcessesRecent = %+v; expected only the newest batch (current)", got)
	}
}

func TestSQLiteStore_TopProcessesAt(t *testing.T) {
	store := newTestStore(t)
	t0 := time.Now().Truncate(time.Second)

	if err := store.WriteProcesses([]ProcessSample{
		{At: t0, PID: 1, Name: "a", CPUPct: 10, MemRSS: 100},
		{At: t0, PID: 2, Name: "b", CPUPct: 20, MemRSS: 200},
	}); err != nil {
		t.Fatalf("write batch 1: %v", err)
	}
	if err := store.WriteProcesses([]ProcessSample{
		{At: t0.Add(2 * time.Second), PID: 3, Name: "c", CPUPct: 99, MemRSS: 50},
	}); err != nil {
		t.Fatalf("write batch 2: %v", err)
	}

	// Anchor between batches → returns batch 1.
	got, err := store.TopProcessesAt(t0.Add(time.Second), "cpu", 10)
	if err != nil {
		t.Fatalf("TopProcessesAt: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("TopProcessesAt mid returned %d, want 2", len(got))
	}
	if got[0].Name != "b" {
		t.Errorf("TopProcessesAt mid top = %q, want b", got[0].Name)
	}

	// Anchor at/after batch 2 → returns batch 2.
	got, err = store.TopProcessesAt(t0.Add(5*time.Second), "cpu", 10)
	if err != nil {
		t.Fatalf("TopProcessesAt later: %v", err)
	}
	if len(got) != 1 || got[0].Name != "c" {
		t.Errorf("TopProcessesAt later = %+v, want [c]", got)
	}
}

func TestSQLiteStore_WriteProcesses_NoopForEmptySlice(t *testing.T) {
	store := newTestStore(t)
	if err := store.WriteProcesses(nil); err != nil {
		t.Errorf("WriteProcesses(nil) = %v, want nil", err)
	}
}

func TestSQLiteStore_WriteProcesses_DuplicatePidIgnored(t *testing.T) {
	// schema has PRIMARY KEY (ts, pid) ON CONFLICT DO NOTHING — collectors
	// occasionally produce duplicate PIDs in a single tick.
	store := newTestStore(t)
	t0 := time.Now()
	if err := store.WriteProcesses([]ProcessSample{
		{At: t0, PID: 1, Name: "first", CPUPct: 1, MemRSS: 1},
		{At: t0, PID: 1, Name: "second", CPUPct: 2, MemRSS: 2},
	}); err != nil {
		t.Errorf("WriteProcesses with dup pid: %v (should be ignored, not error)", err)
	}
	got, err := store.TopProcessesRecent("cpu", 10)
	if err != nil {
		t.Fatalf("TopProcessesRecent: %v", err)
	}
	if len(got) != 1 || got[0].Name != "first" {
		t.Errorf("TopProcessesRecent after dup write = %+v, want [first]", got)
	}
}
