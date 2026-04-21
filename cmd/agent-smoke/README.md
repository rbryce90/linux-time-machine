# agent-smoke

Standalone harness for testing the agent tool-calling loop against Ollama.
Starts the full stack (collectors + MCP tools + Ollama chat) outside the TUI
so you can iterate on prompts, models, and tool invocation without bubbletea.

## Usage

```
# Default: llama3.1:8b with a built-in question
go run ./cmd/agent-smoke

# Custom question
go run ./cmd/agent-smoke "Any auth failures in the last 10 minutes?"

# Smaller/faster model while iterating (useful on CPU-only machines)
LTM_CHAT_MODEL=llama3.2:3b go run ./cmd/agent-smoke
LTM_CHAT_MODEL=qwen2.5:3b  go run ./cmd/agent-smoke
```

## Notes

- Collects metrics and events for 5s before asking the model, so tool calls
  have real data to return.
- Prints each agent event (turn, tool call, tool result, final answer) to
  stdout so you can see what the loop is actually doing.
- On CPU-only inference, 8B models may need >60s per turn. Use 3B models
  for iteration.
