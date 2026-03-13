// Package cron implements a persistent job scheduler for the blades CLI.
package cron

// ScheduleKind identifies how a job is scheduled.
type ScheduleKind string

const (
	ScheduleAt    ScheduleKind = "at"
	ScheduleEvery ScheduleKind = "every"
	ScheduleCron  ScheduleKind = "cron"
)

// PayloadKind identifies what a job does when it fires.
type PayloadKind string

const (
	PayloadExec      PayloadKind = "exec"
	PayloadAgentTurn PayloadKind = "agent_turn"
)

// Schedule describes when a job should run.
type Schedule struct {
	Kind    ScheduleKind `json:"kind"`
	AtMs    int64        `json:"atMs,omitempty"`
	EveryMs int64        `json:"everyMs,omitempty"`
	Expr    string       `json:"expr,omitempty"`
	TZ      string       `json:"tz,omitempty"`
}

// Payload describes what happens when a job fires.
type Payload struct {
	Kind      PayloadKind `json:"kind"`
	Command   string      `json:"command,omitempty"`
	Message   string      `json:"message,omitempty"`
	SessionID string      `json:"sessionID,omitempty"`
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
