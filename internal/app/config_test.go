package app

import (
	"strings"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if !strings.HasSuffix(cfg.DBPath, Name+".db") {
		t.Errorf("DBPath %q should end in %q.db", cfg.DBPath, Name)
	}
	if cfg.Mode != ModeTUI {
		t.Errorf("default Mode = %v, want ModeTUI", cfg.Mode)
	}

	if !cfg.Domains.System.Enabled {
		t.Error("System domain should be enabled by default")
	}
	if cfg.Domains.System.SampleInterval != 1 {
		t.Errorf("System.SampleInterval = %d, want 1", cfg.Domains.System.SampleInterval)
	}
	if !cfg.Domains.Events.Enabled {
		t.Error("Events domain should be enabled by default")
	}
	if cfg.Domains.Network.Enabled {
		t.Error("Network domain should be disabled by default (not implemented yet)")
	}
}

func TestRunMode_DistinctValues(t *testing.T) {
	if ModeTUI == ModeMCP {
		t.Fatal("ModeTUI and ModeMCP collapsed to same value")
	}
}
