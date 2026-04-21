# Resume — next session

Context handoff for picking up `linux-time-machine` in a fresh Claude conversation.

## Current state (end of session 2026-04-20)

Everything below is committed and builds clean:

- **Domain-driven Go project** — `cmd/linux-time-machine/` + `internal/{app,storage,mcp,tui,types,accessor,llm,agent,domains/{system,events}}/`
- **Tokyo Night pink-accent theme** matching user's alacritty/i3 config
- **System domain** — gopsutil collector → SQLite, rates/sparklines/top-processes in the TUI, 3 MCP tools
- **Events domain** — journald capture via `journalctl -f -o json` subprocess, SQLite store with embedding BLOB column, background embedder using Ollama `nomic-embed-text`, 4 MCP tools including `events_semantic_search`
- **History scrubbing** — `h` to enter, arrow keys to scrub, `esc`/`end` to return live; both panels re-render at cursor
- **Multi-metric trends block** — 4-line stacked sparkline (CPU/MEM/DISK/NET), responsive to terminal width
- **MCP server** — stdio transport wired via `github.com/modelcontextprotocol/go-sdk`; works with Claude Desktop and `claude mcp add`
- **LLM + agent backend** (new, backend-only — no UI yet) — `internal/llm`, `internal/agent`, `internal/accessor/ollama/chat.go`. Compiles and runs via `cmd/agent-smoke`.

## What was being built at handoff

**In-app chat panel** — talk to a local LLM (Ollama) inside the TUI, with the LLM able to call the same MCP tools via the agent loop.

Backend is done. UI is not.

## Where to pick up

The remaining work to finish the in-app chat feature:

### 1. Confirm the agent loop works end-to-end

The smoke harness at `cmd/agent-smoke` hit a context deadline with `llama3.1:8b` on CPU. Try the smaller models first to verify tool calling works correctly at all:

```bash
LTM_CHAT_MODEL=llama3.2:3b go run ./cmd/agent-smoke
LTM_CHAT_MODEL=qwen2.5:3b  go run ./cmd/agent-smoke
```

If 3B models return a coherent answer with tool calls, the pipeline is fine — the issue was just inference speed on the bigger model. If 3B models also fail, debug the tool-calling serialization in `internal/accessor/ollama/chat.go` against Ollama's actual wire format.

### 2. Build the chat panel in `internal/tui`

Needs:
- `github.com/charmbracelet/bubbles/textinput` for the prompt box
- `github.com/charmbracelet/bubbles/viewport` for the scrolling conversation
- Message types: user / assistant / tool-call-indicator
- `/` or `a` key to focus the input, `esc` to blur
- Enter sends; while waiting, show a spinner

### 3. Wire it to the agent

- New `internal/tui/chat.go` that owns the panel + holds an `*agent.Agent`
- On submit, run the agent in a goroutine
- Stream events back to the TUI via `tea.Msg` (each `agent.Event` → a custom msg)
- Render tool calls inline so demos show "called system_top_processes"
- Render the final answer

### 4. Config plumbing

- Add `ChatConfig{Enabled, Provider, Model}` to `internal/app/config.go`
- Add `--chat-model` flag + `LTM_CHAT_MODEL` env to `cmd/linux-time-machine/main.go`
- Default: ollama + `llama3.2:3b` (fast enough on CPU)
- Skip chat panel if Ollama unavailable at startup

### 5. Provider interface cleanup

The `llm.Provider` interface is already provider-agnostic. When you add the
Anthropic SDK provider later, it's a new file under `internal/accessor/claude/`
implementing `llm.Provider` and a config branch to select between them.

## Smaller parallel work items

- **Retention policy for events table** — rows accumulate forever. Add a periodic DELETE of events older than N days, configurable. Same for `system_samples` and `system_processes`.
- **Tests + CI** — unit tests for the stores, collector fakes, `agent` mock-provider loop. GitHub Actions on push.
- **GitHub push + demo GIF + README rewrite** — the `ai-native local observability` angle per the earlier research.
- **Network domain** — `/proc/net/tcp` poller + PID attribution + GeoLite2 enrichment. Follows the same shape as `system/` and `events/`.
- **Config file** — TOML in `~/.config/linux-time-machine/config.toml` loaded at startup.

## Open questions to decide next session

1. **Ship v0.1 to GitHub before adding chat?** Arguments both ways — chat is the high-visibility feature; v0.1 already works and is shippable.
2. **Default chat model?** Probably `llama3.2:3b` for responsiveness, `llama3.1:8b` for quality. User has both.
3. **Where does the chat panel live in the layout?** Third panel below system+events, or a toggled full-screen mode?
4. **Streaming?** Ollama `/api/chat` supports `stream: true`. Harder to wire into bubbletea but dramatically better UX. Worth it for v0.2 probably.

## Key files touched

```
internal/accessor/ollama/
├── client.go          # added ChatModel option, bumped default timeout
├── chat.go            # NEW: /api/chat with tool support, implements llm.Provider
└── ...
internal/llm/llm.go    # NEW: Provider interface + Message/Tool/Response types
internal/agent/
├── agent.go           # NEW: tool-calling loop with Event emission for UI hooks
└── mcp_bridge.go      # NEW: wraps registered mcp.Tools as llm.Tools + invoker
cmd/agent-smoke/       # NEW: standalone tester for the agent loop
```

## First command when resuming

```bash
cd ~/dev/personal/pulse
git log --oneline -15   # see the recent history
go build ./...          # verify clean checkout
LTM_CHAT_MODEL=llama3.2:3b go run ./cmd/agent-smoke
```

If that works, proceed to building the chat panel. If not, debug the Ollama
chat wire format first.
