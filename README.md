# linux-time-machine

AI-native local system observability. One binary. No cloud.

## What it is

A single Go binary that replaces the fragmented stack of `btop` + `iotop` + `nethogs` + `journalctl` with:

- **Real-time TUI dashboard** — CPU, memory, disk, network, processes, temps
- **Historical scrubbing** — every metric recorded locally, scrub back in time
- **Semantic event search** — journald messages embedded locally, searchable by meaning
- **Claude-native investigation** — MCP server exposes metrics and events to Claude for natural-language diagnosis

You don't just watch your system. You can ask it questions.

## Why it exists

Existing tools each answer one question. You end up in tmux with three panes and still can't correlate what the disk was doing when the CPU spiked. `pulse` unifies collection and adds two things nobody ships well at the single-machine level: **history** and **semantic investigation**.

## Status

Early development. Architecture below is the v0.1 target.

## Architecture

```
┌──────────────────────────────────────────────────┐
│  TUI (bubbletea)   ◄── real-time + history UI    │
├──────────────────────────────────────────────────┤
│  MCP server        ◄── Claude asks structured    │
│                        and semantic questions    │
├──────────────────────────────────────────────────┤
│  Query layer                                     │
│   ├─ SQLite ◄── structured time-series metrics   │
│   └─ LanceDB ◄── embedded process + event context│
├──────────────────────────────────────────────────┤
│  Collector (gopsutil + journald reader)          │
└──────────────────────────────────────────────────┘
```

## Planned stack

- **Go 1.22+** — core language, single static binary
- **gopsutil** — cross-platform system metrics
- **bubbletea / lipgloss / bubbles** — TUI
- **modernc.org/sqlite** — pure-Go SQLite, no cgo
- **LanceDB** — vector store for semantic event search
- **Ollama** (`nomic-embed-text`) — local embeddings
- **MCP Go SDK** — Model Context Protocol server

## v0.1 goals

- [ ] Collector daemon sampling at 1s
- [ ] SQLite time-series store
- [ ] TUI with 4 panels (CPU / mem / disk / net)
- [ ] Process table with sort
- [ ] History scrubbing mode
- [ ] MCP server with 4 structured tools
- [ ] journald reader + LanceDB embedding pipeline
- [ ] 2 RAG-powered MCP tools (`search_events`, `find_similar_windows`)
- [ ] README with demo GIF
- [ ] Published binary release

## License

TBD
