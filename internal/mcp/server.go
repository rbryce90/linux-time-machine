package mcp

import (
	"context"
	"fmt"
)

// Server is a placeholder. It will host the real MCP transport (stdio today,
// HTTP later). For now it just holds registered tools so the wiring compiles.
type Server struct {
	tools map[string]Tool
}

func NewServer() *Server {
	return &Server{tools: make(map[string]Tool)}
}

func (s *Server) Register(t Tool) error {
	if _, exists := s.tools[t.Name()]; exists {
		return fmt.Errorf("mcp: tool %q already registered", t.Name())
	}
	s.tools[t.Name()] = t
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

func (s *Server) Start(ctx context.Context) error {
	<-ctx.Done()
	return nil
}
