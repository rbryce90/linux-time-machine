// Package agent runs the LLM tool-calling loop: send messages, execute any
// tool calls the model wants via the supplied invoker, feed results back,
// repeat until the model returns a plain text answer or MaxTurns is hit.
package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rbryce90/linux-time-machine/internal/llm"
)

// ToolInvoker executes one tool call and returns its result as a string
// (serialized however the tool wants — typically JSON).
type ToolInvoker func(ctx context.Context, name string, args map[string]any) (string, error)

type Agent struct {
	Provider     llm.Provider
	Tools        []llm.Tool
	Invoker      ToolInvoker
	MaxTurns     int
	SystemPrompt string
}

// Event is emitted during Run so callers (e.g. TUI) can show progress.
type Event struct {
	Kind       EventKind
	Content    string
	ToolName   string
	ToolArgs   map[string]any
	ToolResult string
	Err        error
}

type EventKind int

const (
	EventTurn EventKind = iota
	EventToolCall
	EventToolResult
	EventAnswer
	EventError
)

// Run executes the agent loop. `onEvent` may be nil. Returns the final
// assistant content or an error.
func (a *Agent) Run(ctx context.Context, userInput string, onEvent func(Event)) (string, error) {
	if a.MaxTurns <= 0 {
		a.MaxTurns = 6
	}
	emit := func(e Event) {
		if onEvent != nil {
			onEvent(e)
		}
	}

	messages := []llm.Message{}
	if a.SystemPrompt != "" {
		messages = append(messages, llm.Message{Role: llm.RoleSystem, Content: a.SystemPrompt})
	}
	messages = append(messages, llm.Message{Role: llm.RoleUser, Content: userInput})

	for turn := 0; turn < a.MaxTurns; turn++ {
		emit(Event{Kind: EventTurn, Content: fmt.Sprintf("turn %d", turn+1)})

		resp, err := a.Provider.Chat(ctx, messages, a.Tools)
		if err != nil {
			emit(Event{Kind: EventError, Err: err})
			return "", err
		}

		// No tool calls → this is the final answer.
		if len(resp.ToolCalls) == 0 {
			emit(Event{Kind: EventAnswer, Content: resp.Content})
			return resp.Content, nil
		}

		// Model wants to call tools. Record assistant turn first.
		messages = append(messages, llm.Message{
			Role:      llm.RoleAssistant,
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		for _, tc := range resp.ToolCalls {
			emit(Event{Kind: EventToolCall, ToolName: tc.Name, ToolArgs: tc.Arguments})

			result, err := a.Invoker(ctx, tc.Name, tc.Arguments)
			if err != nil {
				result = fmt.Sprintf(`{"error": %q}`, err.Error())
				emit(Event{Kind: EventError, Err: err, ToolName: tc.Name})
			} else {
				emit(Event{Kind: EventToolResult, ToolName: tc.Name, ToolResult: result})
			}

			messages = append(messages, llm.Message{
				Role:       llm.RoleTool,
				Content:    result,
				ToolCallID: tc.ID,
				ToolName:   tc.Name,
			})
		}
	}

	return "", fmt.Errorf("agent: exceeded max turns (%d)", a.MaxTurns)
}

// JSONResult is a convenience for tool invokers that return structured data.
func JSONResult(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
