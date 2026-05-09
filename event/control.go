package event

// Abort signals the agent to terminate the current turn.
type Abort struct {
	Reason string
}

func (Abort) input() {}

// Pause signals the agent to pause tool execution.
type Pause struct{}

func (Pause) input() {}

// Resume signals the agent to resume tool execution.
type Resume struct{}

func (Resume) input() {}
