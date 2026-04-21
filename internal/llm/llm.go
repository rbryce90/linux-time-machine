// Package llm defines provider-agnostic types for chat + tool calling.
// Concrete backends (Ollama, Anthropic, OpenAI) implement Provider.
package llm

import "context"

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type Message struct {
	Role       Role
	Content    string
	ToolCalls  []ToolCall // assistant messages may include these
	ToolCallID string     // for RoleTool replies
	ToolName   string     // for RoleTool replies
}

type ToolCall struct {
	ID        string
	Name      string
	Arguments map[string]any
}

// Tool is the LLM-facing description of a callable capability.
type Tool struct {
	Name        string
	Description string
	// Schema is a JSON-Schema object describing tool arguments. Minimal
	// accepted form: {"type":"object","properties":{...},"required":[...]}
	Schema map[string]any
}

// Response is one LLM turn's output. Either Content is present and no tool
// calls (final answer), tool calls are present (agent loop should invoke and
// continue), or both.
type Response struct {
	Content   string
	ToolCalls []ToolCall
}

// Provider is the LLM backend abstraction.
type Provider interface {
	Chat(ctx context.Context, messages []Message, tools []Tool) (*Response, error)
	Name() string  // e.g. "ollama"
	Model() string // e.g. "llama3.1:8b"
}
