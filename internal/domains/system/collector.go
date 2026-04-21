package system

import (
	"context"
	"log"
	"sort"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
	"github.com/shirou/gopsutil/v3/process"
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
	_, _ = cpu.Percent(0, false)
	_ = primeProcessCPU()

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
				log.Printf("system collector: write sample: %v", err)
			}

			procs := sampleProcesses(now, 10)
			if len(procs) > 0 {
				if err := c.store.WriteProcesses(procs); err != nil {
					log.Printf("system collector: write processes: %v", err)
				}
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

// primeProcessCPU warms the per-process CPU baseline so the first real
// sample has meaningful non-zero numbers.
func primeProcessCPU() error {
	procs, err := process.Processes()
	if err != nil {
		return err
	}
	for _, p := range procs {
		_, _ = p.CPUPercent()
	}
	return nil
}

// sampleProcesses returns the top N processes by CPU% at this moment.
// Reads are best-effort: processes can die between listing and querying,
// which we silently skip.
func sampleProcesses(at time.Time, topN int) []ProcessSample {
	procs, err := process.Processes()
	if err != nil {
		return nil
	}
	out := make([]ProcessSample, 0, len(procs))
	for _, p := range procs {
		name, err := p.Name()
		if err != nil {
			continue
		}
		cpuPct, _ := p.CPUPercent()
		memInfo, _ := p.MemoryInfo()
		var rss int64
		if memInfo != nil {
			rss = int64(memInfo.RSS)
		}
		out = append(out, ProcessSample{
			At:      at,
			PID:     p.Pid,
			Name:    name,
			CPUPct:  cpuPct,
			MemRSS:  rss,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CPUPct > out[j].CPUPct })
	if len(out) > topN {
		out = out[:topN]
	}
	return out
}
