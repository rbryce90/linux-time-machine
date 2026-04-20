// Package mcp is the shared MCP integration. Each domain contributes Tool
// implementations via its ToolProvider method; this package knows nothing
// about any specific domain.
//
// The real MCP SDK will be plugged in when we wire tool transport. For now
// Tool is an internal interface so domains can register without taking a
// dependency on the external SDK yet.
package mcp

import "context"

type Tool interface {
	Name() string
	Description() string
	Invoke(ctx context.Context, args map[string]any) (any, error)
}

type ToolProvider interface {
	Tools() []Tool
}
