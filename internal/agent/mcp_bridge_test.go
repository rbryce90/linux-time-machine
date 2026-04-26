package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rbryce90/linux-time-machine/internal/mcp"
)

// fakeMCPTool is a domain-free Tool implementation for bridge tests.
type fakeMCPTool struct {
	name        string
	description string
	schema      map[string]any
	result      any
	err         error
}

func (f *fakeMCPTool) Name() string                                            { return f.name }
func (f *fakeMCPTool) Description() string                                     { return f.description }
func (f *fakeMCPTool) Schema() map[string]any                                  { return f.schema }
func (f *fakeMCPTool) Invoke(_ context.Context, _ map[string]any) (any, error) { return f.result, f.err }

func TestFromMCPTools_TranslatesNameDescriptionSchema(t *testing.T) {
	server := mcp.NewServer("test", "v0.0.0")
	schema := map[string]any{"type": "object", "properties": map[string]any{}}
	if err := server.Register(&fakeMCPTool{
		name:        "alpha",
		description: "the alpha tool",
		schema:      schema,
		result:      map[string]any{"ok": true},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	tools, _ := FromMCPTools(server)
	if len(tools) != 1 {
		t.Fatalf("tools len = %d, want 1", len(tools))
	}
	got := tools[0]
	if got.Name != "alpha" {
		t.Errorf("Name = %q, want alpha", got.Name)
	}
	if got.Description != "the alpha tool" {
		t.Errorf("Description = %q", got.Description)
	}
	if got.Schema == nil || got.Schema["type"] != "object" {
		t.Errorf("Schema not preserved: %v", got.Schema)
	}
}

func TestFromMCPTools_InvokerDispatchesByName(t *testing.T) {
	server := mcp.NewServer("test", "v0.0.0")
	if err := server.Register(&fakeMCPTool{
		name:   "echo",
		result: map[string]any{"value": 42},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	_, invoker := FromMCPTools(server)
	out, err := invoker(context.Background(), "echo", nil)
	if err != nil {
		t.Fatalf("invoker: %v", err)
	}
	// Result must be JSON-encoded; the agent feeds this back to the model
	// as the tool's reply Content.
	if !strings.Contains(out, `"value"`) || !strings.Contains(out, "42") {
		t.Errorf("invoker output = %q, want JSON with value=42", out)
	}
}

func TestFromMCPTools_InvokerUnknownToolErrors(t *testing.T) {
	server := mcp.NewServer("test", "v0.0.0")
	_, invoker := FromMCPTools(server)
	_, err := invoker(context.Background(), "missing", nil)
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("error %q should reference the tool name", err.Error())
	}
}

func TestFromMCPTools_InvokerPropagatesToolError(t *testing.T) {
	server := mcp.NewServer("test", "v0.0.0")
	want := errors.New("disk full")
	if err := server.Register(&fakeMCPTool{name: "fail", err: want}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	_, invoker := FromMCPTools(server)
	_, err := invoker(context.Background(), "fail", nil)
	if !errors.Is(err, want) {
		t.Errorf("invoker err = %v, want chain to %v", err, want)
	}
}
