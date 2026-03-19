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
	// PayloadNotify sends a text message directly to a chat/session.
	PayloadNotify PayloadKind = "notify"
)

// Schedule describes when a job should run.
type Schedule struct {
	Kind ScheduleKind `json:"kind" yaml:"kind"`

	// AtMs is the target Unix timestamp in milliseconds (kind=at).
	AtMs int64 `json:"atMs,omitempty" yaml:"atMs,omitempty"`

	// EveryMs is the repeat interval in milliseconds (kind=every).
	EveryMs int64 `json:"everyMs,omitempty" yaml:"everyMs,omitempty"`

	// Expr is a standard 5-field cron expression (kind=cron), e.g. "0 9 * * *".
	Expr string `json:"expr,omitempty" yaml:"expr,omitempty"`

	// TZ is an IANA timezone name used when evaluating Expr (kind=cron only).
	TZ string `json:"tz,omitempty" yaml:"tz,omitempty"`
}

// Payload describes what happens when a job fires.
type Payload struct {
	Kind PayloadKind `json:"kind" yaml:"kind"`

	// Command is the shell command to run (kind=exec).
	Command string `json:"command,omitempty" yaml:"command,omitempty"`

	// Message is injected as a user turn into the agent (kind=agent_turn),
	// or sent directly to chat/session (kind=notify).
	Message string `json:"message,omitempty" yaml:"message,omitempty"`

	// SessionID scopes the agent_turn to a specific conversation.
	// It is ignored for exec and notify jobs.
	SessionID string `json:"sessionID,omitempty" yaml:"sessionID,omitempty"`

	// ReplySessionID, when set, tells the handler to forward the job's output
	// back to this session (e.g. via the channel's proactive send path).
	// For notify jobs this is the target session that receives Message.
	ReplySessionID string `json:"replySessionID,omitempty" yaml:"replySessionID,omitempty"`
}

// JobState holds mutable runtime information updated after each execution.
type JobState struct {
	NextRunAtMs int64  `json:"nextRunAtMs,omitempty" yaml:"nextRunAtMs,omitempty"`
	LastRunAtMs int64  `json:"lastRunAtMs,omitempty" yaml:"lastRunAtMs,omitempty"`
	LastStatus  string `json:"lastStatus,omitempty" yaml:"lastStatus,omitempty"` // "ok" | "error"
	LastError   string `json:"lastError,omitempty" yaml:"lastError,omitempty"`
	LastOutput  string `json:"lastOutput,omitempty" yaml:"lastOutput,omitempty"`
}

// Job is a fully-described scheduled task.
type Job struct {
	ID             string   `json:"id" yaml:"id"`
	Name           string   `json:"name" yaml:"name"`
	Enabled        bool     `json:"enabled" yaml:"enabled"`
	Schedule       Schedule `json:"schedule" yaml:"schedule"`
	Payload        Payload  `json:"payload" yaml:"payload"`
	State          JobState `json:"state" yaml:"state"`
	CreatedAtMs    int64    `json:"createdAtMs" yaml:"createdAtMs"`
	UpdatedAtMs    int64    `json:"updatedAtMs" yaml:"updatedAtMs"`
	DeleteAfterRun bool     `json:"deleteAfterRun" yaml:"deleteAfterRun"`
}

// store is the on-disk cron store envelope.
type store struct {
	Version int    `json:"version" yaml:"version"`
	Jobs    []*Job `json:"jobs" yaml:"jobs"`
}
