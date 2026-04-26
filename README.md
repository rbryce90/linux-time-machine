# linux-time-machine

**AI-native local system observability.** A single Go binary. No cloud.

Replaces the fragmented stack of `btop` + `iotop` + `nethogs` + `journalctl` with a unified tool that keeps **history**, supports **semantic log search**, and exposes everything to Claude via **MCP** for natural-language investigation of your own machine.

## Why

Existing tools each answer one question and only about *right now*. You notice a CPU spike at 3am — btop can't tell you what happened. You search for "authentication issues" — `journalctl | grep` only matches keywords, not meaning.

`linux-time-machine` keeps a rolling time-series of every metric and every journal line on your machine, and exposes them as MCP tools so Claude Desktop or Claude Code can scrub backwards and reason over it in English.

## What it does today

### TUI

- Live CPU / memory / disk / network rates, top processes, 2-minute multi-metric trend sparklines
- **History scrubbing**: press `h`, arrow-keys back in time, watch past state replay — the panel re-renders at whatever moment you land on
- Tokyo Night pink-accent theme, responsive to terminal width

### MCP server (7 tools)

System:
- `system_current_metrics` — snapshot
- `system_metrics_history` — samples over a time range
- `system_top_processes` — top-N by CPU or memory

Events (systemd journal):
- `events_latest` — tail
- `events_search` — case-insensitive substring over message + unit
- `events_near_time` — events in a window around a past moment
- `events_semantic_search` — embedding-backed RAG over the journal (requires Ollama)

All tools advertise typed JSON schemas so even smaller local models (Llama 3B, Qwen 3B) can call them reliably.

## Install

### Prerequisites

- Linux with systemd
- Go 1.25+
- *Optional*: [Ollama](https://ollama.com) with `nomic-embed-text` pulled, for semantic search

### Build from source

```bash
git clone https://github.com/rbryce90/linux-time-machine
cd linux-time-machine
go build -o ~/.local/bin/linux-time-machine ./cmd/linux-time-machine
```

### Run the TUI

```bash
~/.local/bin/linux-time-machine
```

Data collects into `./linux-time-machine.db`. Press `h` to scrub history, `q` to quit.

### Wire into Claude Code

```bash
claude mcp add linux-time-machine \
  --scope user \
  -- \
  ~/.local/bin/linux-time-machine \
  --mcp \
  --db ~/.local/share/linux-time-machine.db
```

Then in any Claude Code session:

> *What's eating my CPU right now?*
>
> *Any authentication issues in my journal today?*
>
> *Summarize CPU usage over the last 5 minutes — any spikes?*

### Wire into Claude Desktop

Add to `~/.config/Claude/claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "linux-time-machine": {
      "command": "/home/you/.local/bin/linux-time-machine",
      "args": ["--mcp", "--db", "/home/you/.local/share/linux-time-machine.db"]
    }
  }
}
```

## Architecture

Domain-driven Go with each subsystem self-contained under `internal/domains/*`:

```
internal/
├── app/          # config, registry, lifecycle
├── storage/      # shared SQLite with WAL
├── vectorstore/  # embedded vector index (brute-force cosine, snapshot-to-disk);
│                 # designed for extraction to its own module
├── mcp/          # Tool interface + server on top of the official MCP SDK
├── tui/          # bubbletea host + Tokyo Night theme
├── accessor/
│   └── ollama/   # embedding + chat client
├── llm/          # provider-agnostic chat + tool types
├── agent/        # tool-calling loop with parallel invocation
├── types/        # shared primitives
└── domains/
    ├── system/   # gopsutil collector → SQLite, TUI panel, MCP tools
    └── events/   # journalctl subprocess + background embedder,
                  # SQLite for event metadata, vectorstore for embeddings
```

Adding a new domain is one folder + one line in `cmd/linux-time-machine/main.go`.

## Design decisions

A few choices worth calling out, because they bound the project's identity:

- **Pure Go, no CGO.** Uses `modernc/sqlite` (pure-Go SQLite reimplementation) rather than `mattn/go-sqlite3`. The cost is some performance and no SQLite extensions; the win is `go install` works on any Linux box without a C toolchain, and `goreleaser` cross-compiles cleanly.
- **Embedded vector store, not a library.** Rather than depending on LanceDB / Qdrant / pgvector, `internal/vectorstore` is a small in-house brute-force index with snapshot-to-disk persistence. At single-machine scale (< ~50k vectors) this is faster than an ANN index and keeps the single-binary distribution intact. `Store` is an interface, so swapping in HNSW or `sqlite-vec` later is one implementation away.
- **Separate domains own their tables and queries.** No shared "ORM" layer; each domain (`system`, `events`) owns its schema and access patterns. Adding a new domain doesn't require touching anything but its own folder and one line in `main.go`.
- **MCP as the integration point, not a REST API.** The tool-call surface is what an LLM consumes; HTTP endpoints would just be a second adaptation layer. v0.2 may add `--http` for multi-client deployments, but stdio MCP is the v0.1 contract.

## Status

**v0.1** — working, shippable for single-user use. Test suite in place, CI green on every PR, race-clean.

### Working
- TUI with live + historical modes
- SQLite time-series with WAL concurrency
- Embedded vector store (`internal/vectorstore`) with snapshot persistence
- Semantic search over journald via Ollama embeddings
- MCP stdio server with typed tool schemas
- Parallel tool invocation in the agent layer
- Stdlib-only test suite (≈80% coverage on testable logic), `go vet` clean, race-tested
- Per-PR GitHub Actions check (`go vet`, `go test`, `go build`)
- Goreleaser config for Linux amd64/arm64 release binaries

### In-flight (next release)
- In-app chat panel (talk to a local LLM inside the TUI)
- `--http` transport so a persistent collector can serve multiple clients

### Planned
- Network flow tracking (`/proc/net` + GeoIP + threat intel)
- Retention policies for time-series tables
- Precompiled release binaries published to GitHub Releases

## Known limitations

- Linux-only (uses systemd's `journalctl`; `/proc` for per-process CPU). macOS + Windows would need different collectors.
- Event semantic search requires Ollama running locally with an embedding model.
- Single-writer SQLite; fine for one laptop.
- MCP stdio-only today — the collector only runs while a client is connected. HTTP transport is the fix (planned).

## License

MIT — see [LICENSE](./LICENSE).
