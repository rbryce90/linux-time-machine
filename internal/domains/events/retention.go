package events

import (
	"context"
	"log"
	"time"
)

// runRetention is the events domain's background retention loop. It deletes
// events older than retentionDays from both SQLite and the vectorstore on a
// daily cadence.
//
// Behavior:
//   - retentionDays <= 0 disables the loop and returns immediately.
//   - The first pass runs synchronously on entry so the loop self-tests under
//     the same conditions a scheduled run will encounter.
//   - On each tick the cutoff is recomputed from time.Now so a long-running
//     process always uses a wall-clock window relative to the current moment.
//   - Errors are logged but never returned — retention is best-effort and a
//     single bad pass should not stop subsequent passes.
func runRetention(ctx context.Context, store Store, retentionDays int) {
	if retentionDays <= 0 {
		log.Printf("events retention: disabled (retention_days=%d)", retentionDays)
		return
	}

	purge := func() {
		cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)
		n, err := store.PurgeOlderThan(cutoff)
		if err != nil {
			log.Printf("events retention: purge failed cutoff=%s err=%v", cutoff.Format(time.RFC3339), err)
			return
		}
		log.Printf("events retention: purged %d events older than %s", n, cutoff.Format(time.RFC3339))
	}

	purge()

	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			purge()
		}
	}
}
