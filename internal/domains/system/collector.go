package system

import (
	"context"
	"log"
	"time"
)

type Collector struct {
	store    Store
	interval time.Duration
}

func NewCollector(store Store, interval time.Duration) *Collector {
	return &Collector{store: store, interval: interval}
}

// Run samples on an interval until ctx is cancelled. Sampling logic lives
// here. For the scaffold it just logs a heartbeat; gopsutil plugs in next.
func (c *Collector) Run(ctx context.Context) {
	t := time.NewTicker(c.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			sample := Sample{At: now}
			if err := c.store.WriteSample(sample); err != nil {
				log.Printf("system collector: write: %v", err)
			}
		}
	}
}
