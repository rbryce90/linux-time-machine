package chat

import "github.com/rbryce90/linux-time-machine/internal/agent"

type agentEventMsg struct {
	Event agent.Event
}

type agentDoneMsg struct {
	Answer string
}

type agentErrMsg struct {
	Err error
}
