package system

import (
	"context"
	"log"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
)

type Collector struct {
	store    Store
	interval time.Duration
}

func NewCollector(store Store, interval time.Duration) *Collector {
	return &Collector{store: store, interval: interval}
}

// Run samples on an interval until ctx is cancelled. The first cpu.Percent
// call seeds the rolling percentage; subsequent calls return real values.
func (c *Collector) Run(ctx context.Context) {
	_, _ = cpu.Percent(0, false) // prime cpu sampling baseline

	t := time.NewTicker(c.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			sample, err := sampleNow(now)
			if err != nil {
				log.Printf("system collector: sample: %v", err)
				continue
			}
			if err := c.store.WriteSample(sample); err != nil {
				log.Printf("system collector: write: %v", err)
			}
		}
	}
}

func sampleNow(at time.Time) (Sample, error) {
	s := Sample{At: at}

	if pcts, err := cpu.Percent(0, false); err == nil && len(pcts) > 0 {
		s.CPUPct = pcts[0]
	}

	if vm, err := mem.VirtualMemory(); err == nil {
		s.MemUsed = int64(vm.Used)
		s.MemTotal = int64(vm.Total)
	}

	if ios, err := disk.IOCounters(); err == nil {
		var rd, wr uint64
		for _, io := range ios {
			rd += io.ReadBytes
			wr += io.WriteBytes
		}
		s.DiskRead = int64(rd)
		s.DiskWrite = int64(wr)
	}

	if ios, err := net.IOCounters(false); err == nil && len(ios) > 0 {
		s.NetRx = int64(ios[0].BytesRecv)
		s.NetTx = int64(ios[0].BytesSent)
	}

	return s, nil
}
