package llm

import (
	"testing"
)

func TestRoleConstantsAreStable(t *testing.T) {
	// These string values are part of the wire contract with provider
	// implementations (see ollama.toOllamaMessages). Pin them so a refactor
	// can't silently shift them.
	cases := map[Role]string{
		RoleSystem:    "system",
		RoleUser:      "user",
		RoleAssistant: "assistant",
		RoleTool:      "tool",
	}
	for r, want := range cases {
		if string(r) != want {
			t.Errorf("Role %q != wire string %q", r, want)
		}
	}
}

func TestMessage_FieldsZeroValues(t *testing.T) {
	// A user-typed message has no tool calls and no tool reply metadata.
	m := Message{Role: RoleUser, Content: "hi"}
	if len(m.ToolCalls) != 0 {
		t.Errorf("ToolCalls should be empty by default, got %d", len(m.ToolCalls))
	}
	if m.ToolCallID != "" || m.ToolName != "" {
		t.Errorf("Tool reply fields should be empty on user message, got id=%q name=%q",
			m.ToolCallID, m.ToolName)
	}
}

func TestToolCall_ArgumentsRoundtrip(t *testing.T) {
	tc := ToolCall{
		ID:   "call_1",
		Name: "system_top_processes",
		Arguments: map[string]any{
			"metric": "cpu",
			"limit":  10,
		},
	}
	if tc.Arguments["metric"] != "cpu" {
		t.Errorf("metric = %v, want cpu", tc.Arguments["metric"])
	}
	if tc.Arguments["limit"] != 10 {
		t.Errorf("limit = %v, want 10", tc.Arguments["limit"])
	}
}

func TestTool_SchemaCanBeNil(t *testing.T) {
	// Tools with no arguments use a nil schema; provider adapters must not
	// crash on nil. Document the contract here so regressions surface early.
	tool := Tool{Name: "noop", Description: "does nothing"}
	if tool.Schema != nil {
		t.Errorf("zero-value Tool.Schema should be nil, got %v", tool.Schema)
	}
}

func TestResponse_FinalAnswerHasNoToolCalls(t *testing.T) {
	// The agent loop relies on len(ToolCalls)==0 to detect a final answer.
	// Pin this contract so a future change can't silently break the loop.
	r := Response{Content: "done"}
	if len(r.ToolCalls) != 0 {
		t.Errorf("final-answer Response should have no ToolCalls, got %d", len(r.ToolCalls))
	}
}
