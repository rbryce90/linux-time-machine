package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rbryce90/linux-time-machine/internal/llm"
	"github.com/rbryce90/linux-time-machine/internal/mcp"
)

// FromMCPTools wraps the MCP server's registered tools as llm.Tools plus an
// invoker that dispatches by name. No schema info yet — LLMs degrade to
// "object with arbitrary properties," which is good enough for our current
// tool set.
func FromMCPTools(server *mcp.Server) (tools []llm.Tool, invoker ToolInvoker) {
	registered := server.Tools()
	byName := make(map[string]mcp.Tool, len(registered))
	tools = make([]llm.Tool, 0, len(registered))

	for _, t := range registered {
		byName[t.Name()] = t
		tools = append(tools, llm.Tool{
			Name:        t.Name(),
			Description: t.Description(),
			// Generic schema — LLMs can send any args map. Individual tools
			// validate what they need via intArg/stringArg helpers.
			Schema: map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"additionalProperties": true,
			},
		})
	}

	invoker = func(ctx context.Context, name string, args map[string]any) (string, error) {
		t, ok := byName[name]
		if !ok {
			return "", fmt.Errorf("unknown tool %q", name)
		}
		result, err := t.Invoke(ctx, args)
		if err != nil {
			return "", err
		}
		b, err := json.Marshal(result)
		if err != nil {
			return "", fmt.Errorf("marshal tool result: %w", err)
		}
		return string(b), nil
	}
	return tools, invoker
}
