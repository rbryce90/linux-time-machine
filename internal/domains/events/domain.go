package events

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/rbryce90/linux-time-machine/internal/accessor/ollama"
	"github.com/rbryce90/linux-time-machine/internal/app"
	"github.com/rbryce90/linux-time-machine/internal/vectorstore"
)

type Config struct {
	// RetentionDays bounds how long events stay in SQLite + vectorstore.
	// A daily background pass deletes anything older. Zero or negative
	// disables retention entirely.
	RetentionDays int
}

type Domain struct {
	cfg          Config
	store        Store
	vec          vectorstore.Store
	snapshotPath string
	collector    *Collector
	embedder     *embedder
	ollama       *ollama.Client
	cancel       context.CancelFunc
}

func New(cfg Config) *Domain {
	return &Domain{cfg: cfg}
}

func (d *Domain) Name() string { return "events" }

func (d *Domain) Start(ctx context.Context, deps app.Deps) error {
	// Hydrate the vectorstore: prefer the on-disk snapshot if present,
	// otherwise start empty. The legacy embedding-BLOB column is migrated
	// inside store.EnsureSchema (idempotent — no-op if already migrated).
	d.snapshotPath = vectorSnapshotPath(deps.DBPath)
	bf, err := loadOrNewVectorstore(d.snapshotPath)
	if err != nil {
		return fmt.Errorf("events: vectorstore load: %w", err)
	}
	d.vec = bf

	d.store = NewSQLiteStore(deps.DB, d.vec, d.snapshotPath)
	if err := d.store.EnsureSchema(); err != nil {
		return fmt.Errorf("events: schema: %w", err)
	}

	d.collector = NewCollector(d.store)
	d.ollama = deps.Ollama
	d.embedder = newEmbedder(d.store, deps.Ollama)

	if err := deps.MCP.RegisterProvider(d); err != nil {
		return fmt.Errorf("events: mcp register: %w", err)
	}
	deps.TUI.AddProvider(d)

	runCtx, cancel := context.WithCancel(ctx)
	d.cancel = cancel
	go d.collector.Run(runCtx)
	go d.embedder.run(runCtx)
	go runRetention(runCtx, d.store, d.cfg.RetentionDays)
	return nil
}

func (d *Domain) Stop() error {
	if d.cancel != nil {
		d.cancel()
	}
	// Persist the vectorstore snapshot so embeddings survive restart. Best-
	// effort: log and continue on error. Stop is called from the registry
	// during shutdown; failing here would just spam an error log.
	if d.store != nil {
		if err := d.store.SaveVectorstore(); err != nil {
			log.Printf("events: save vectorstore snapshot: %v", err)
		}
	}
	if d.vec != nil {
		_ = d.vec.Close()
	}
	return nil
}

// loadOrNewVectorstore returns a hydrated *BruteForceStore loaded from path
// if it exists, or a fresh empty store if it doesn't. Any other error
// (corrupt file, permission denied) is returned to the caller — we'd rather
// fail loudly than silently lose embeddings.
func loadOrNewVectorstore(path string) (*vectorstore.BruteForceStore, error) {
	if path == "" {
		return vectorstore.NewBruteForceStore(), nil
	}
	bf, err := vectorstore.Load(path)
	if err == nil {
		return bf, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return vectorstore.NewBruteForceStore(), nil
	}
	return nil, err
}
