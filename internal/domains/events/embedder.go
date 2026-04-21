package events

import (
	"context"
	"log"
	"time"

	"github.com/rbryce90/linux-time-machine/internal/accessor/ollama"
)

// embedder polls the store for unembedded rows and fills them via Ollama.
// It runs independent of the collector so slow embeddings don't back-pressure
// journal ingestion. If Ollama is unreachable the worker sleeps and retries.
type embedder struct {
	store      Store
	ollama     *ollama.Client
	batchSize  int
	pollEmpty  time.Duration // sleep when no work
	pollError  time.Duration // sleep after error
	perRequest time.Duration // per-embedding timeout
}

func newEmbedder(store Store, oll *ollama.Client) *embedder {
	return &embedder{
		store:      store,
		ollama:     oll,
		batchSize:  16,
		pollEmpty:  5 * time.Second,
		pollError:  15 * time.Second,
		perRequest: 20 * time.Second,
	}
}

func (e *embedder) run(ctx context.Context) {
	if e.ollama == nil {
		log.Printf("events embedder: ollama client not configured, worker idle")
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		rows, err := e.store.NeedsEmbedding(e.batchSize)
		if err != nil {
			log.Printf("events embedder: needs embedding: %v", err)
			sleep(ctx, e.pollError)
			continue
		}
		if len(rows) == 0 {
			sleep(ctx, e.pollEmpty)
			continue
		}

		for _, r := range rows {
			if ctx.Err() != nil {
				return
			}
			text := r.Message
			if r.Unit != "" {
				text = r.Unit + ": " + text
			}
			reqCtx, cancel := context.WithTimeout(ctx, e.perRequest)
			vec, err := e.ollama.Embed(reqCtx, text)
			cancel()
			if err != nil {
				log.Printf("events embedder: embed row %d: %v", r.RowID, err)
				sleep(ctx, e.pollError)
				break // back off the whole batch on first error
			}
			if err := e.store.SetEmbedding(r.RowID, vec); err != nil {
				log.Printf("events embedder: set embedding row %d: %v", r.RowID, err)
			}
		}
	}
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}
