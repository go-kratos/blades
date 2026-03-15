// Package channel defines the Channel interface and the StreamHandler type
// that all channel implementations use.
package channel

import "context"

// EventKind classifies an agent lifecycle event.
type EventKind string

const (
	// EventToolStart fires when a tool invocation begins.
	EventToolStart EventKind = "tool_start"
	// EventToolEnd fires when a tool invocation completes.
	EventToolEnd EventKind = "tool_end"
)

// Event is an agent lifecycle event (tool call, skill invocation, etc.).
type Event struct {
	Kind   EventKind
	ID     string // unique call ID; used to correlate Start→End pairs
	Name   string // tool or skill name
	Input  string // arguments (JSON or text)
	Output string // result (non-empty for EventToolEnd)
}

// Writer abstracts the output surface a StreamHandler writes to.
// Implementations must be safe for concurrent use.
type Writer interface {
	// WriteText delivers a streaming text token from the assistant.
	WriteText(chunk string)
	// WriteEvent signals an agent lifecycle event (tool call, etc.).
	WriteEvent(e Event)
}

// StreamHandler processes one user message, streaming output to w.
// It returns the full assembled reply text.
type StreamHandler func(ctx context.Context, sessionID, text string, w Writer) (string, error)

// Channel abstracts a messaging transport (CLI, HTTP, messaging app, etc.).
// Implementations block in Start until ctx is cancelled or a fatal error occurs.
type Channel interface {
	// Name returns a stable lower-case identifier, e.g. "cli".
	Name() string

	// Start begins receiving messages and dispatches each one to handler.
	Start(ctx context.Context, handler StreamHandler) error
}

// SessionNotifier is an optional interface for channels that can send
// proactive messages to a session (e.g. cron job results to a Feishu chat).
// sessionID is the same value passed to the StreamHandler (e.g. Lark chat ID).
type SessionNotifier interface {
	SendToSession(ctx context.Context, sessionID, text string) error
}
