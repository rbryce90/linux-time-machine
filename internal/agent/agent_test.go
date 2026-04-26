package agent

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rbryce90/linux-time-machine/internal/llm"
)

// fakeProvider scripts a sequence of responses. Each Chat call consumes the
// next item; if there are extra calls they receive the final scripted entry.
type fakeProvider struct {
	mu        sync.Mutex
	responses []*llm.Response
	calls     [][]llm.Message // per-call message snapshot, for assertions
	chatErr   error
}

func (p *fakeProvider) Name() string  { return "fake" }
func (p *fakeProvider) Model() string { return "fake-model" }

func (p *fakeProvider) Chat(_ context.Context, messages []llm.Message, _ []llm.Tool) (*llm.Response, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	snap := make([]llm.Message, len(messages))
	copy(snap, messages)
	p.calls = append(p.calls, snap)
	if p.chatErr != nil {
		return nil, p.chatErr
	}
	if len(p.responses) == 0 {
		return &llm.Response{Content: "default"}, nil
	}
	r := p.responses[0]
	if len(p.responses) > 1 {
		p.responses = p.responses[1:]
	}
	return r, nil
}

func TestAgent_ImmediateFinalAnswerHasNoToolLoop(t *testing.T) {
	prov := &fakeProvider{
		responses: []*llm.Response{{Content: "the answer is 42"}},
	}
	a := &Agent{
		Provider: prov,
		Invoker:  func(context.Context, string, map[string]any) (string, error) {
			t.Fatal("invoker should not be called when there are no tool calls")
			return "", nil
		},
	}

	var events []Event
	got, err := a.Run(context.Background(), "what is the answer?", func(e Event) {
		events = append(events, e)
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got != "the answer is 42" {
		t.Errorf("Run answer = %q, want 'the answer is 42'", got)
	}

	// We should see exactly one EventTurn followed by EventAnswer.
	if len(events) != 2 {
		t.Fatalf("event count = %d, want 2 (turn + answer); got %+v", len(events), events)
	}
	if events[0].Kind != EventTurn || events[1].Kind != EventAnswer {
		t.Errorf("event kinds = %v, %v; want EventTurn, EventAnswer", events[0].Kind, events[1].Kind)
	}
}

func TestAgent_SystemPromptPrefixed(t *testing.T) {
	prov := &fakeProvider{responses: []*llm.Response{{Content: "ok"}}}
	a := &Agent{
		Provider:     prov,
		SystemPrompt: "you are pulse",
	}
	if _, err := a.Run(context.Background(), "hi", nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(prov.calls) != 1 {
		t.Fatalf("provider called %d times, want 1", len(prov.calls))
	}
	first := prov.calls[0]
	if len(first) != 2 {
		t.Fatalf("messages = %d, want 2 (system + user)", len(first))
	}
	if first[0].Role != llm.RoleSystem || first[0].Content != "you are pulse" {
		t.Errorf("system prompt missing or wrong: %+v", first[0])
	}
	if first[1].Role != llm.RoleUser || first[1].Content != "hi" {
		t.Errorf("user message missing or wrong: %+v", first[1])
	}
}

func TestAgent_ToolCallThenFinalAnswer(t *testing.T) {
	prov := &fakeProvider{
		responses: []*llm.Response{
			{ToolCalls: []llm.ToolCall{{
				ID:        "t1",
				Name:      "lookup",
				Arguments: map[string]any{"q": "cpu"},
			}}},
			{Content: "cpu is 12%"},
		},
	}

	var invoked atomic.Int32
	a := &Agent{
		Provider: prov,
		Invoker: func(_ context.Context, name string, args map[string]any) (string, error) {
			invoked.Add(1)
			if name != "lookup" {
				t.Errorf("invoked name = %q, want lookup", name)
			}
			if args["q"] != "cpu" {
				t.Errorf("invoked arg q = %v, want cpu", args["q"])
			}
			return `{"ok":true}`, nil
		},
	}

	var kinds []EventKind
	got, err := a.Run(context.Background(), "what is cpu?", func(e Event) {
		kinds = append(kinds, e.Kind)
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got != "cpu is 12%" {
		t.Errorf("answer = %q", got)
	}
	if invoked.Load() != 1 {
		t.Errorf("invoker called %d times, want 1", invoked.Load())
	}

	// Sequence: Turn, ToolCall, ToolResult, Turn, Answer.
	want := []EventKind{EventTurn, EventToolCall, EventToolResult, EventTurn, EventAnswer}
	if !reflect.DeepEqual(kinds, want) {
		t.Errorf("event kinds = %v, want %v", kinds, want)
	}

	// Second provider call should have seen the assistant tool-call turn and
	// the tool-reply message in order.
	if len(prov.calls) != 2 {
		t.Fatalf("provider called %d times, want 2", len(prov.calls))
	}
	second := prov.calls[1]
	// [user, assistant(tool_calls), tool]
	if len(second) != 3 {
		t.Fatalf("second-call messages = %d, want 3; got %+v", len(second), second)
	}
	if second[1].Role != llm.RoleAssistant || len(second[1].ToolCalls) != 1 {
		t.Errorf("expected assistant message with tool_calls at index 1, got %+v", second[1])
	}
	if second[2].Role != llm.RoleTool || second[2].ToolName != "lookup" || second[2].ToolCallID != "t1" {
		t.Errorf("expected tool reply at index 2, got %+v", second[2])
	}
	if second[2].Content != `{"ok":true}` {
		t.Errorf("tool reply content = %q, want JSON", second[2].Content)
	}
}

func TestAgent_ToolErrorFedBackAsJSONErrorAndLoopContinues(t *testing.T) {
	prov := &fakeProvider{
		responses: []*llm.Response{
			{ToolCalls: []llm.ToolCall{{ID: "t1", Name: "boom"}}},
			{Content: "I noted the failure"},
		},
	}
	a := &Agent{
		Provider: prov,
		Invoker: func(context.Context, string, map[string]any) (string, error) {
			return "", errors.New("kaboom")
		},
	}

	var sawErrorEvent bool
	got, err := a.Run(context.Background(), "go", func(e Event) {
		if e.Kind == EventToolError {
			sawErrorEvent = true
			if e.Err == nil || !strings.Contains(e.Err.Error(), "kaboom") {
				t.Errorf("EventToolError should carry the underlying err, got %v", e.Err)
			}
		}
	})
	if err != nil {
		t.Fatalf("Run should not fail when an individual tool fails: %v", err)
	}
	if got != "I noted the failure" {
		t.Errorf("answer = %q", got)
	}
	if !sawErrorEvent {
		t.Error("expected an EventToolError emit")
	}

	// The follow-up provider call should have received a tool reply whose
	// Content is a JSON object containing "error".
	second := prov.calls[1]
	toolReply := second[len(second)-1]
	if toolReply.Role != llm.RoleTool {
		t.Fatalf("last message should be tool reply, got %v", toolReply.Role)
	}
	if !strings.Contains(toolReply.Content, `"error"`) || !strings.Contains(toolReply.Content, "kaboom") {
		t.Errorf("tool reply should contain JSON error with original message, got %q", toolReply.Content)
	}
}

func TestAgent_MaxTurnsExceeded(t *testing.T) {
	// Provider keeps requesting tools forever — the loop must give up.
	infiniteToolCall := &llm.Response{
		ToolCalls: []llm.ToolCall{{ID: "t", Name: "noop"}},
	}
	prov := &fakeProvider{responses: []*llm.Response{infiniteToolCall}}
	a := &Agent{
		Provider: prov,
		MaxTurns: 3,
		Invoker: func(context.Context, string, map[string]any) (string, error) {
			return "{}", nil
		},
	}

	var fatal Event
	_, err := a.Run(context.Background(), "go", func(e Event) {
		if e.Kind == EventError {
			fatal = e
		}
	})
	if err == nil {
		t.Fatal("expected error when MaxTurns exceeded")
	}
	if !strings.Contains(err.Error(), "max turns") {
		t.Errorf("error %q should mention max turns", err.Error())
	}
	if fatal.Kind != EventError || fatal.Err == nil {
		t.Error("expected a final EventError to be emitted")
	}
	// MaxTurns=3 means the provider is called exactly 3 times.
	if len(prov.calls) != 3 {
		t.Errorf("provider called %d times, want 3 (MaxTurns)", len(prov.calls))
	}
}

func TestAgent_DefaultMaxTurnsAppliesWhenZero(t *testing.T) {
	infiniteToolCall := &llm.Response{
		ToolCalls: []llm.ToolCall{{ID: "t", Name: "noop"}},
	}
	prov := &fakeProvider{responses: []*llm.Response{infiniteToolCall}}
	a := &Agent{ // MaxTurns omitted → loop should default to 6
		Provider: prov,
		Invoker:  func(context.Context, string, map[string]any) (string, error) { return "{}", nil },
	}
	if _, err := a.Run(context.Background(), "go", nil); err == nil {
		t.Fatal("expected error when default max turns exhausted")
	}
	if len(prov.calls) != 6 {
		t.Errorf("provider called %d times, want default 6", len(prov.calls))
	}
}

func TestAgent_ProviderErrorIsFatal(t *testing.T) {
	want := errors.New("network gone")
	prov := &fakeProvider{chatErr: want}
	a := &Agent{
		Provider: prov,
		Invoker:  func(context.Context, string, map[string]any) (string, error) { return "", nil },
	}
	_, err := a.Run(context.Background(), "go", nil)
	if !errors.Is(err, want) {
		t.Errorf("Run err = %v, want chain to %v", err, want)
	}
}

func TestAgent_ParallelToolInvocationPreservesOrder(t *testing.T) {
	// Three tool calls; we deliberately make the *first* one slowest so that
	// if the implementation accidentally serialised order would still pass.
	// What we really test: results[i] corresponds to calls[i] regardless of
	// completion order.
	calls := []llm.ToolCall{
		{ID: "1", Name: "slow", Arguments: map[string]any{}},
		{ID: "2", Name: "med"},
		{ID: "3", Name: "fast"},
	}
	prov := &fakeProvider{
		responses: []*llm.Response{
			{ToolCalls: calls},
			{Content: "done"},
		},
	}
	a := &Agent{
		Provider: prov,
		Invoker: func(_ context.Context, name string, _ map[string]any) (string, error) {
			switch name {
			case "slow":
				time.Sleep(20 * time.Millisecond)
			case "med":
				time.Sleep(10 * time.Millisecond)
			}
			return `{"name":"` + name + `"}`, nil
		},
	}
	if _, err := a.Run(context.Background(), "go", nil); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Inspect the second provider-call message slice: tool replies must be
	// indexed parallel to the original tool-call order.
	second := prov.calls[1]
	// [user, assistant, tool, tool, tool]
	if len(second) != 2+len(calls) {
		t.Fatalf("messages = %d, want %d", len(second), 2+len(calls))
	}
	for i, expected := range calls {
		reply := second[2+i]
		if reply.Role != llm.RoleTool {
			t.Fatalf("message[%d] role = %v, want tool", 2+i, reply.Role)
		}
		if reply.ToolCallID != expected.ID {
			t.Errorf("reply[%d] id = %q, want %q (order not preserved)", i, reply.ToolCallID, expected.ID)
		}
		if !strings.Contains(reply.Content, expected.Name) {
			t.Errorf("reply[%d] content = %q, expected to mention %q", i, reply.Content, expected.Name)
		}
	}
}

func TestAgent_RunWithNilEventCallbackDoesNotPanic(t *testing.T) {
	prov := &fakeProvider{responses: []*llm.Response{{Content: "ok"}}}
	a := &Agent{Provider: prov}
	if _, err := a.Run(context.Background(), "go", nil); err != nil {
		t.Errorf("Run with nil callback: %v", err)
	}
}
