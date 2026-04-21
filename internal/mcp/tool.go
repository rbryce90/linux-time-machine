// Package mcp is the shared MCP integration. Each domain contributes Tool
// implementations via its ToolProvider method; this package knows nothing
// about any specific domain.
package mcp

import "context"

type Tool interface {
	Name() string
	Description() string
	// Schema returns a JSON-Schema object for the tool's arguments.
	// Return nil for "no arguments" or to accept anything.
	Schema() map[string]any
	Invoke(ctx context.Context, args map[string]any) (any, error)
}

type ToolProvider interface {
	Tools() []Tool
}

// ObjectSchema is a helper for building argument schemas in tool definitions.
// required may be nil.
func ObjectSchema(properties map[string]any, required []string) map[string]any {
	s := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

// IntProp / StringProp build JSON-Schema property entries with a description.
func IntProp(description string) map[string]any {
	return map[string]any{"type": "integer", "description": description}
}
func StringProp(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}
func StringEnumProp(description string, values ...string) map[string]any {
	return map[string]any{"type": "string", "description": description, "enum": values}
}
