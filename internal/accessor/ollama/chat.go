package ollama

import (
	"context"
	"fmt"

	"github.com/rbryce90/linux-time-machine/internal/llm"
)

func (c *Client) Name() string  { return "ollama" }
func (c *Client) Model() string { return c.chatModel }

// Chat sends a conversation with optional tools and returns the model's
// next turn. Non-streaming — returns once the full turn is ready.
func (c *Client) Chat(ctx context.Context, messages []llm.Message, tools []llm.Tool) (*llm.Response, error) {
	req := chatRequest{
		Model:    c.chatModel,
		Messages: toOllamaMessages(messages),
		Tools:    toOllamaTools(tools),
		Stream:   false,
	}
	var cr chatResponse
	if err := c.postJSON(ctx, "/api/chat", req, &cr); err != nil {
		return nil, fmt.Errorf("ollama chat: %w", err)
	}

	out := &llm.Response{Content: cr.Message.Content}
	for _, tc := range cr.Message.ToolCalls {
		// Ollama doesn't return tool-call IDs. Leaving ID empty signals
		// "no correlation needed" to the agent loop.
		out.ToolCalls = append(out.ToolCalls, llm.ToolCall{
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		})
	}
	return out, nil
}

type chatRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Tools    []ollamaTool    `json:"tools,omitempty"`
	Stream   bool            `json:"stream"`
}

type ollamaMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	ToolCalls []ollamaToolCall `json:"tool_calls,omitempty"`
	ToolName  string           `json:"tool_name,omitempty"`
}

type ollamaToolCall struct {
	Function ollamaFunctionCall `json:"function"`
}

type ollamaFunctionCall struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type ollamaTool struct {
	Type     string       `json:"type"`
	Function ollamaToolFn `json:"function"`
}

type ollamaToolFn struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type chatResponse struct {
	Message struct {
		Role      string           `json:"role"`
		Content   string           `json:"content"`
		ToolCalls []ollamaToolCall `json:"tool_calls,omitempty"`
	} `json:"message"`
	Done bool `json:"done"`
}

func toOllamaMessages(in []llm.Message) []ollamaMessage {
	out := make([]ollamaMessage, 0, len(in))
	for _, m := range in {
		om := ollamaMessage{
			Role:    string(m.Role),
			Content: m.Content,
		}
		if m.Role == llm.RoleTool {
			om.ToolName = m.ToolName
		}
		if len(m.ToolCalls) > 0 {
			om.ToolCalls = make([]ollamaToolCall, 0, len(m.ToolCalls))
			for _, tc := range m.ToolCalls {
				om.ToolCalls = append(om.ToolCalls, ollamaToolCall{
					Function: ollamaFunctionCall{
						Name:      tc.Name,
						Arguments: tc.Arguments,
					},
				})
			}
		}
		out = append(out, om)
	}
	return out
}

func toOllamaTools(in []llm.Tool) []ollamaTool {
	out := make([]ollamaTool, 0, len(in))
	for _, t := range in {
		schema := t.Schema
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		out = append(out, ollamaTool{
			Type: "function",
			Function: ollamaToolFn{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  schema,
			},
		})
	}
	return out
}
