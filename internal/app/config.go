// Package app owns process-wide concerns: config, domain registry, lifecycle.
// It does not know about any specific domain.
package app

type Config struct {
	DBPath  string        `json:"db_path"`
	Mode    RunMode       `json:"-"` // set from flags, not file
	Domains DomainToggles `json:"domains"`
}

type RunMode int

const (
	ModeTUI RunMode = iota // default: live TUI, human operator
	ModeMCP                // serve MCP over stdio, for Claude Desktop
)

type DomainToggles struct {
	System  DomainConfig `json:"system"`
	Events  DomainConfig `json:"events"`
	Network DomainConfig `json:"network"`
}

type DomainConfig struct {
	Enabled        bool `json:"enabled"`
	SampleInterval int  `json:"sample_interval_seconds"`
}

func DefaultConfig() Config {
	return Config{
		DBPath: "./" + Name + ".db",
		Domains: DomainToggles{
			System:  DomainConfig{Enabled: true, SampleInterval: 1},
			Events:  DomainConfig{Enabled: true, SampleInterval: 0},
			Network: DomainConfig{Enabled: false, SampleInterval: 2},
		},
	}
}
