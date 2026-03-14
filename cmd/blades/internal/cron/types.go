// Package cron implements a persistent job scheduler for the blades CLI.
// Each job has a schedule (one-shot "at", repeating "every", or cron expression "cron")
// and a payload describing what to execute when it fires.

package cron

// ScheduleKind identifies how a job is scheduled.
type ScheduleKind string

const (
	// ScheduleAt fires the job once at an absolute Unix-millisecond timestamp.
	ScheduleAt ScheduleKind = "at"
	// ScheduleEvery repeats the job on a fixed interval.
	ScheduleEvery ScheduleKind = "every"
	// ScheduleCron uses a standard 5-field cron expression.
	ScheduleCron ScheduleKind = "cron"
)

// PayloadKind identifies what a job does when it fires.
type PayloadKind string

const (
	// PayloadExec runs a shell command.
	PayloadExec PayloadKind = "exec"
	// PayloadAgentTurn injects a user message into the agent.
	PayloadAgentTurn PayloadKind = "agent_turn"
)

// Schedule describes when a job should run.
type Schedule struct {
	Kind ScheduleKind `json:"kind"`

	// AtMs is the target Unix timestamp in milliseconds (kind=at).
	AtMs int64 `json:"atMs,omitempty"`

	// EveryMs is the repeat interval in milliseconds (kind=every).
	EveryMs int64 `json:"everyMs,omitempty"`

	// Expr is a standard 5-field cron expression (kind=cron), e.g. "0 9 * * *".
	Expr string `json:"expr,omitempty"`

	// TZ is an IANA timezone name used when evaluating Expr (kind=cron only).
	TZ string `json:"tz,omitempty"`
}

// Payload describes what happens when a job fires.
type Payload struct {
	Kind PayloadKind `json:"kind"`

	// Command is the shell command to run (kind=exec).
	Command string `json:"command,omitempty"`

	// Message is injected as a user turn into the agent (kind=agent_turn).
	Message string `json:"message,omitempty"`

	// SessionID scopes the agent_turn to a specific conversation.
	SessionID string `json:"sessionID,omitempty"`

	// ReplySessionID, when set, tells the handler to forward the job's output
	// back to this session (e.g. via the channel's proactive send path).
	ReplySessionID string `json:"replySessionID,omitempty"`
}

// JobState holds mutable runtime information updated after each execution.
type JobState struct {
	NextRunAtMs int64  `json:"nextRunAtMs,omitempty"`
	LastRunAtMs int64  `json:"lastRunAtMs,omitempty"`
	LastStatus  string `json:"lastStatus,omitempty"` // "ok" | "error"
	LastError   string `json:"lastError,omitempty"`
	LastOutput  string `json:"lastOutput,omitempty"`
}

// Job is a fully-described scheduled task.
type Job struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Enabled        bool     `json:"enabled"`
	Schedule       Schedule `json:"schedule"`
	Payload        Payload  `json:"payload"`
	State          JobState `json:"state"`
	CreatedAtMs    int64    `json:"createdAtMs"`
	UpdatedAtMs    int64    `json:"updatedAtMs"`
	DeleteAfterRun bool     `json:"deleteAfterRun"`
}

// store is the on-disk JSON envelope.
type store struct {
	Version int    `json:"version"`
	Jobs    []*Job `json:"jobs"`
}
