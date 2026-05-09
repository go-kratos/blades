package event

import "github.com/go-kratos/blades/content"

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

// LoopExit signals that a tool requested loop termination.
type LoopExit struct {
	ToolID   string
	ToolName string
	Escalate bool
}

func (LoopExit) output() {}

// Handoff signals that a tool requested delegation to another agent.
type Handoff struct {
	ToolID   string
	ToolName string
	Agent    string
	Carry    *content.ToolResult
}

func (Handoff) output() {}
