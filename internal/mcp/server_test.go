package mcp

import (
	"context"
	"errors"
	"testing"
)

// fakeTool is a stand-in for domain tools so we don't pull in events/system.
type fakeTool struct {
	name   string
	desc   string
	schema map[string]any
	result any
	err    error

	calls int
}

func (f *fakeTool) Name() string                 { return f.name }
func (f *fakeTool) Description() string          { return f.desc }
func (f *fakeTool) Schema() map[string]any       { return f.schema }
func (f *fakeTool) Invoke(_ context.Context, _ map[string]any) (any, error) {
	f.calls++
	return f.result, f.err
}

func TestServer_Register(t *testing.T) {
	s := NewServer("test", "v0.0.0")
	tool := &fakeTool{name: "do_thing", desc: "does the thing"}

	if err := s.Register(tool); err != nil {
		t.Fatalf("Register: %v", err)
	}

	got := s.Tools()
	if len(got) != 1 {
		t.Fatalf("Tools() len = %d, want 1", len(got))
	}
	if got[0].Name() != "do_thing" {
		t.Errorf("registered tool name = %q, want do_thing", got[0].Name())
	}
}

func TestServer_RegisterDuplicateRejected(t *testing.T) {
	s := NewServer("test", "v0.0.0")
	if err := s.Register(&fakeTool{name: "dup"}); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	err := s.Register(&fakeTool{name: "dup"})
	if err == nil {
		t.Fatal("duplicate Register should return error")
	}
}

func TestServer_RegisterProvider(t *testing.T) {
	s := NewServer("test", "v0.0.0")
	prov := fakeProvider{tools: []Tool{
		&fakeTool{name: "alpha"},
		&fakeTool{name: "beta"},
	}}
	if err := s.RegisterProvider(prov); err != nil {
		t.Fatalf("RegisterProvider: %v", err)
	}

	names := map[string]bool{}
	for _, t := range s.Tools() {
		names[t.Name()] = true
	}
	if !names["alpha"] || !names["beta"] {
		t.Errorf("Tools() = %v, want both alpha and beta", names)
	}
}

func TestServer_RegisterProviderStopsOnError(t *testing.T) {
	s := NewServer("test", "v0.0.0")
	if err := s.Register(&fakeTool{name: "already"}); err != nil {
		t.Fatalf("seed Register: %v", err)
	}

	prov := fakeProvider{tools: []Tool{
		&fakeTool{name: "fresh"},
		&fakeTool{name: "already"}, // collides with seeded one
		&fakeTool{name: "never_reached"},
	}}
	err := s.RegisterProvider(prov)
	if err == nil {
		t.Fatal("RegisterProvider should fail on collision")
	}
	for _, tool := range s.Tools() {
		if tool.Name() == "never_reached" {
			t.Error("RegisterProvider should stop registering after first error")
		}
	}
}

func TestServer_ToolInvokePropagatesError(t *testing.T) {
	// Verify that the fakeTool wiring we rely on in agent tests actually
	// propagates errors as expected — keeps the helper honest.
	want := errors.New("tool blew up")
	tool := &fakeTool{name: "boom", err: want}

	_, err := tool.Invoke(context.Background(), nil)
	if !errors.Is(err, want) {
		t.Errorf("Invoke err = %v, want %v", err, want)
	}
	if tool.calls != 1 {
		t.Errorf("calls = %d, want 1", tool.calls)
	}
}

type fakeProvider struct{ tools []Tool }

func (p fakeProvider) Tools() []Tool { return p.tools }
