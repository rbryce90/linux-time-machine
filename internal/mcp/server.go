package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Server wraps the official MCP SDK. Domains register via our Tool interface;
// this package bridges to the SDK's typed handler shape.
type Server struct {
	impl  *mcpsdk.Implementation
	sdk   *mcpsdk.Server
	tools map[string]Tool
}

func NewServer(name, version string) *Server {
	impl := &mcpsdk.Implementation{Name: name, Version: version}
	return &Server{
		impl:  impl,
		sdk:   mcpsdk.NewServer(impl, nil),
		tools: make(map[string]Tool),
	}
}

func (s *Server) Register(t Tool) error {
	if _, exists := s.tools[t.Name()]; exists {
		return fmt.Errorf("mcp: tool %q already registered", t.Name())
	}
	s.tools[t.Name()] = t

	handler := func(ctx context.Context, req *mcpsdk.CallToolRequest, input map[string]any) (*mcpsdk.CallToolResult, map[string]any, error) {
		out, err := t.Invoke(ctx, input)
		if err != nil {
			return &mcpsdk.CallToolResult{
				IsError: true,
				Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: err.Error()}},
			}, nil, nil
		}
		payload, _ := json.MarshalIndent(out, "", "  ")
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: string(payload)}},
		}, nil, nil
	}

	mcpsdk.AddTool(s.sdk, &mcpsdk.Tool{
		Name:        t.Name(),
		Description: t.Description(),
	}, handler)
	return nil
}

func (s *Server) RegisterProvider(p ToolProvider) error {
	for _, t := range p.Tools() {
		if err := s.Register(t); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) Tools() []Tool {
	out := make([]Tool, 0, len(s.tools))
	for _, t := range s.tools {
		out = append(out, t)
	}
	return out
}

// ServeStdio runs the MCP server over stdin/stdout. Blocks until the client
// disconnects or ctx is cancelled. Intended for spawning by Claude Desktop.
func (s *Server) ServeStdio(ctx context.Context) error {
	return s.sdk.Run(ctx, &mcpsdk.StdioTransport{})
}
