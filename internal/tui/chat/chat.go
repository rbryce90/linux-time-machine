// Package chat implements the in-app chat panel. The chat view does NOT
// satisfy tui.Panel: panels assume synchronous Refresh()/View() against a
// live domain, but chat is input-driven, async, and full-screen. Instead it
// exposes its own small surface (Init/Update/View + Activate/Deactivate)
// that internal/tui/app.go calls directly when in modeChat.
package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rbryce90/linux-time-machine/internal/agent"
	"github.com/rbryce90/linux-time-machine/internal/tui/theme"
)

const systemPrompt = "You are a diagnostic assistant for a Linux workstation. You have tools that query live and historical system metrics and journald events. Prefer calling tools over guessing. Be concise and specific in your answers."

// NOTE: multi-turn conversation is not yet threaded through Agent.Run — each
// submit is a fresh run with just that user input. Prior Q/A stays visible
// in the scrollback so the user has context. True multi-turn history is a
// follow-up once the llm.Provider interface grows to support it.

type Model struct {
	ag       *agent.Agent
	disabled bool // true when Ollama was unreachable at startup

	// Parent context for every submit; cancelled by Close on TUI shutdown so
	// the agent goroutine exits before the app tears down its dependencies.
	ctx       context.Context
	cancelAll context.CancelFunc

	width  int
	height int

	vp      viewport.Model
	input   textinput.Model
	spinner spinner.Model

	lines []string // rendered scrollback; re-joined into viewport content on change

	// A run is active iff cancel != nil. events/done are non-nil together with cancel.
	cancel context.CancelFunc
	events chan tea.Msg
	done   chan tea.Msg

	// cursor set by the host when activating from history mode
	historyCursor *time.Time
}

func (m *Model) running() bool { return m.cancel != nil }

func (m *Model) finishRun() {
	m.cancel = nil
	m.events = nil
	m.done = nil
}

// Close cancels any in-flight agent run and prevents new ones. Idempotent.
// Called by the TUI host when the program exits so the goroutine doesn't
// outlive the DB / MCP tools it depends on.
func (m *Model) Close() {
	if m.cancelAll != nil {
		m.cancelAll()
	}
}

func New(ag *agent.Agent, disabledReason string) *Model {
	ti := textinput.New()
	ti.Placeholder = "Ask about your system..."
	ti.Prompt = "› "
	ti.CharLimit = 2000

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(theme.Purple)

	vp := viewport.New(80, 20)

	ctx, cancelAll := context.WithCancel(context.Background())
	m := &Model{
		ag:        ag,
		ctx:       ctx,
		cancelAll: cancelAll,
		vp:        vp,
		input:     ti,
		spinner:   sp,
	}

	if disabledReason != "" {
		m.disabled = true
		m.appendLine(theme.Bad.Render("! " + disabledReason))
		m.input.Placeholder = "(chat unavailable)"
	} else {
		m.appendLine(theme.Dim.Render("Ask about CPU, memory, processes, or journald events. Enter to submit, esc to exit."))
	}
	return m
}

// Activate is called by the host every time the user enters chat mode.
// historyCursor != nil means the user came from history mode; we thread the
// timestamp into the system prompt so the model knows.
func (m *Model) Activate(historyCursor *time.Time) tea.Cmd {
	m.historyCursor = historyCursor
	if !m.disabled {
		m.input.Focus()
	}
	// spinner only animates while a request is in flight, but we still need
	// the initial tick if one happens to be outstanding (e.g., reactivate)
	if m.running() {
		return m.spinner.Tick
	}
	return nil
}

// Deactivate is called when leaving chat mode. We keep scrollback intact;
// any in-flight request keeps running until the user cancels or it finishes.
func (m *Model) Deactivate() {
	m.input.Blur()
}

func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h

	// leave 1 line for the input, 1 for the in-flight hint, and a small
	// breathing margin. viewport fills the rest.
	vpHeight := h - 3
	if vpHeight < 3 {
		vpHeight = 3
	}
	m.vp.Width = w
	m.vp.Height = vpHeight
	m.input.Width = w - 4
	m.rerenderViewport()
}

// Interrupt cancels an in-flight request if there is one. Returns true iff
// something was cancelled. Host uses this to implement ctrl+c → cancel-or-exit:
//
//	if !chat.Interrupt() { exitChat() }
//
// Note: m.cancel is NOT nilled here — finishRun is the sole niller, so
// running() stays true until the goroutine actually delivers done/err. This
// prevents the user from submitting a new request against orphaned channels
// while the cancelled goroutine is still winding down.
func (m *Model) Interrupt() bool {
	if m.cancel == nil {
		return false
	}
	m.cancel()
	return true
}

func (m *Model) Update(msg tea.Msg) (*Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			if m.disabled || m.running() {
				return m, nil
			}
			text := strings.TrimSpace(m.input.Value())
			if text == "" {
				return m, nil
			}
			m.input.SetValue("")
			m.appendLine(theme.Title.Render("you ") + theme.Value.Render(text))
			cmds = append(cmds, m.submit(text), m.spinner.Tick)
			return m, tea.Batch(cmds...)
		case "pgup", "pgdown", "up", "down":
			var c tea.Cmd
			m.vp, c = m.vp.Update(msg)
			cmds = append(cmds, c)
			return m, tea.Batch(cmds...)
		}

	case agentEventMsg:
		m.handleEvent(msg.Event)
		cmds = append(cmds, m.drainCmd())
	case agentDoneMsg:
		// drain any events still buffered — done can arrive before the last
		// tool-call events, and finishRun would orphan them otherwise.
		m.drainBufferedEvents()
		m.finishRun()
		if msg.Answer == "" {
			m.appendLine(theme.Dim.Render("(no answer)"))
		} else {
			m.appendLine(theme.PanelTitle.Render("pulse ") + theme.Value.Render(msg.Answer))
		}
	case agentErrMsg:
		m.drainBufferedEvents()
		m.finishRun()
		if msg.Err == context.Canceled {
			m.appendLine(theme.Dim.Render("(cancelled)"))
		} else if msg.Err != nil {
			m.appendLine(theme.Bad.Render("! error: " + msg.Err.Error()))
		}
	case spinner.TickMsg:
		if m.running() {
			var c tea.Cmd
			m.spinner, c = m.spinner.Update(msg)
			cmds = append(cmds, c)
		}
	}

	// input receives everything else (keystrokes while not submitting)
	if !m.disabled && !m.running() {
		var c tea.Cmd
		m.input, c = m.input.Update(msg)
		cmds = append(cmds, c)
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) View() string {
	var b strings.Builder
	b.WriteString(m.vp.View())
	b.WriteString("\n")

	if m.running() {
		b.WriteString(theme.Dim.Render(m.spinner.View() + " thinking..."))
	} else if m.disabled {
		b.WriteString(theme.Dim.Render("(chat unavailable)"))
	} else {
		b.WriteString(theme.Dim.Render("press enter to submit · esc to exit · ctrl+c to cancel"))
	}
	b.WriteString("\n")
	b.WriteString(m.input.View())
	return b.String()
}

// submit starts the agent run in a goroutine. The goroutine pushes events
// into m.events; each call to drainCmd() reads one message. Update() re-arms
// drainCmd on every agentEventMsg until agentDoneMsg/agentErrMsg arrives.
func (m *Model) submit(userInput string) tea.Cmd {
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel

	m.events = make(chan tea.Msg, 32)
	m.done = make(chan tea.Msg, 1)
	events, done := m.events, m.done

	sp := systemPrompt
	if m.historyCursor != nil {
		sp += " The user is currently viewing state at " + m.historyCursor.Format(time.RFC3339) + "."
	}
	runAgent := *m.ag
	runAgent.SystemPrompt = sp

	go func() {
		defer close(events)
		answer, err := runAgent.Run(ctx, userInput, func(ev agent.Event) {
			// Block on backpressure so tool-call log lines are never lost,
			// but unblock promptly if the user cancels.
			select {
			case events <- agentEventMsg{Event: ev}:
			case <-ctx.Done():
			}
		})
		if err != nil {
			done <- agentErrMsg{Err: err}
		} else {
			done <- agentDoneMsg{Answer: answer}
		}
		close(done)
	}()

	return m.drainCmd()
}

// drainCmd returns a Cmd that blocks on the next event or completion signal.
// Update re-arms it after each agentEventMsg; after agentDoneMsg/agentErrMsg
// the channels are cleared and no further drain is issued.
func (m *Model) drainCmd() tea.Cmd {
	events, done := m.events, m.done
	if events == nil && done == nil {
		return nil
	}
	return func() tea.Msg {
		select {
		case ev, ok := <-events:
			if ok {
				return ev
			}
			// events closed → wait for the done signal
			return <-done
		case d := <-done:
			return d
		}
	}
}

// drainBufferedEvents non-blocking-pulls every remaining agentEventMsg from
// m.events and feeds each into handleEvent. Used on run termination so tool-
// call log lines emitted just before the goroutine finished are not dropped.
func (m *Model) drainBufferedEvents() {
	if m.events == nil {
		return
	}
	for {
		select {
		case msg, ok := <-m.events:
			if !ok {
				return
			}
			if em, isEvent := msg.(agentEventMsg); isEvent {
				m.handleEvent(em.Event)
			}
		default:
			return
		}
	}
}

func (m *Model) handleEvent(ev agent.Event) {
	switch ev.Kind {
	case agent.EventTurn:
		// deliberately silent — the turn counter would be noisy
	case agent.EventToolCall:
		argsJSON, _ := json.Marshal(ev.ToolArgs)
		m.appendLine(theme.Dim.Render(fmt.Sprintf("→ %s %s", ev.ToolName, string(argsJSON))))
	case agent.EventToolResult:
		m.appendLine(theme.Dim.Render(fmt.Sprintf("← %s %s", ev.ToolName, summarizeResult(ev.ToolResult))))
	case agent.EventToolError:
		errText := ""
		if ev.Err != nil {
			errText = ev.Err.Error()
		}
		m.appendLine(theme.Bad.Render(fmt.Sprintf("× %s: %s", ev.ToolName, errText)))
	case agent.EventAnswer:
		// final answer also arrives via agentDoneMsg; skip here to avoid dupes.
	case agent.EventError:
		// also arrives via agentErrMsg; skip.
	}
}

func summarizeResult(s string) string {
	const max = 100
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) == 0 {
		return "ok"
	}
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}

func (m *Model) appendLine(s string) {
	m.lines = append(m.lines, s)
	m.rerenderViewport()
}

func (m *Model) rerenderViewport() {
	m.vp.SetContent(strings.Join(m.lines, "\n"))
	m.vp.GotoBottom()
}
