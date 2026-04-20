// Package system is the "system metrics" domain: CPU, memory, disk, network
// counters, processes.
//
// public.go defines the surface that OTHER domains are allowed to consume.
// Anything not exported here is private to this domain, even though Go
// package visibility technically allows it.
package system

import (
	"time"

	"github.com/rbryce90/pulse/internal/types"
)

type ProcessInfo struct {
	PID  types.ProcessID
	Name string
}

// API is what other domains (network, events) may call. Keep it small.
type API interface {
	ProcessAt(pid types.ProcessID, at time.Time) (ProcessInfo, error)
}
