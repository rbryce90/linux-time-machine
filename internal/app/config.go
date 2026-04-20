// Package app owns process-wide concerns: config, domain registry, lifecycle.
// It does not know about any specific domain.
package app

type Config struct {
	DBPath  string        `json:"db_path"`
	Domains DomainToggles `json:"domains"`
}

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
		DBPath: "./pulse.db",
		Domains: DomainToggles{
			System:  DomainConfig{Enabled: true, SampleInterval: 1},
			Events:  DomainConfig{Enabled: false, SampleInterval: 0},
			Network: DomainConfig{Enabled: false, SampleInterval: 2},
		},
	}
}
