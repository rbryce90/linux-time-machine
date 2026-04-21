// Package agent runs the LLM tool-calling loop: send messages, execute any
// tool calls the model wants via the supplied invoker, feed results back,
// repeat until the model returns a plain text answer or MaxTurns is hit.
package agent

import (
	"context"
	"fmt"
	"sync"

	"github.com/rbryce90/linux-time-machine/internal/llm"
	"golang.org/x/sync/errgroup"
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
	EventTurn       EventKind = iota
	EventToolCall             // model requested a tool invocation
	EventToolResult           // tool invocation returned successfully
	EventToolError            // tool invocation failed (loop continues; result fed back to model as {"error": ...})
	EventAnswer               // final assistant content
	EventError                // fatal: provider.Chat failed or max turns exceeded
)

// Run executes the agent loop. onEvent may be nil. Returns the final
// assistant content or an error.
func (a *Agent) Run(ctx context.Context, userInput string, onEvent func(Event)) (string, error) {
	maxTurns := a.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 6
	}
	emit := onEvent
	if emit == nil {
		emit = func(Event) {}
	}

	messages := make([]llm.Message, 0, 2+maxTurns*3)
	if a.SystemPrompt != "" {
		messages = append(messages, llm.Message{Role: llm.RoleSystem, Content: a.SystemPrompt})
	}
	messages = append(messages, llm.Message{Role: llm.RoleUser, Content: userInput})

	for turn := 0; turn < maxTurns; turn++ {
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

		// Record the assistant turn before invoking tools so the message
		// order seen by the next model call is correct.
		messages = append(messages, llm.Message{
			Role:      llm.RoleAssistant,
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		results := a.invokeParallel(ctx, resp.ToolCalls, emit)
		for i, r := range results {
			messages = append(messages, llm.Message{
				Role:       llm.RoleTool,
				Content:    r,
				ToolCallID: resp.ToolCalls[i].ID,
				ToolName:   resp.ToolCalls[i].Name,
			})
		}
	}

	err := fmt.Errorf("agent: exceeded max turns (%d)", maxTurns)
	emit(Event{Kind: EventError, Err: err})
	return "", err
}

// invokeParallel fans out tool invocations with errgroup, preserving the
// original order in the returned results slice. Emits EventToolCall for
// every call first (so the UI can render "calling X, Y, Z" at once), then
// EventToolResult / EventToolError as each completes.
func (a *Agent) invokeParallel(ctx context.Context, calls []llm.ToolCall, emit func(Event)) []string {
	for _, tc := range calls {
		emit(Event{Kind: EventToolCall, ToolName: tc.Name, ToolArgs: tc.Arguments})
	}

	results := make([]string, len(calls))
	var mu sync.Mutex // serialize emits so UI sees a coherent order

	eg, egCtx := errgroup.WithContext(ctx)
	for i, tc := range calls {
		i, tc := i, tc
		eg.Go(func() error {
			result, err := a.Invoker(egCtx, tc.Name, tc.Arguments)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				results[i] = fmt.Sprintf(`{"error": %q}`, err.Error())
				emit(Event{Kind: EventToolError, ToolName: tc.Name, Err: err})
				return nil // don't fail the whole batch
			}
			results[i] = result
			emit(Event{Kind: EventToolResult, ToolName: tc.Name, ToolResult: result})
			return nil
		})
	}
	_ = eg.Wait()
	return results
}
