package app

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/rbryce90/changeName/internal/mcp"
	"github.com/rbryce90/changeName/internal/tui"
)

// Deps is the shared plumbing each domain may use. A domain that doesn't
// need something simply ignores it.
type Deps struct {
	DB  *sql.DB
	MCP *mcp.Server
	TUI *tui.App
}

// Domain is the contract every domain implements. One folder under
// internal/domains/* equals one Domain.
//
// Start should return quickly; long-running loops belong on their own
// goroutine tied to ctx. Stop must be idempotent.
type Domain interface {
	Name() string
	Start(ctx context.Context, deps Deps) error
	Stop() error
}

type Registry struct {
	domains []Domain
}

func NewRegistry() *Registry { return &Registry{} }

func (r *Registry) Register(d Domain) {
	r.domains = append(r.domains, d)
}

func (r *Registry) StartAll(ctx context.Context, deps Deps) error {
	for _, d := range r.domains {
		if err := d.Start(ctx, deps); err != nil {
			return fmt.Errorf("start domain %q: %w", d.Name(), err)
		}
	}
	return nil
}

func (r *Registry) StopAll() {
	for i := len(r.domains) - 1; i >= 0; i-- {
		_ = r.domains[i].Stop()
	}
}

func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.domains))
	for _, d := range r.domains {
		out = append(out, d.Name())
	}
	return out
}
