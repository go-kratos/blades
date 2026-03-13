package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/go-kratos/blades/cmd/blades/internal/cron"
	bladestools "github.com/go-kratos/blades/tools"
	"github.com/google/jsonschema-go/jsonschema"
	robfigcron "github.com/robfig/cron/v3"
)

type cronInput struct {
	// Action is one of: add, list, remove, run
	Action string `json:"action"`

	// --- add ---
	Name           string  `json:"name,omitempty"`
	PayloadKind    string  `json:"payload_kind,omitempty"` // "exec" | "agent_turn"
	Command        string  `json:"command,omitempty"`      // exec payload
	Message        string  `json:"message,omitempty"`      // agent_turn payload
	SessionID      string  `json:"session_id,omitempty"`
	ScheduleKind   string  `json:"schedule_kind,omitempty"`
	AtMs           int64   `json:"at_ms,omitempty"`
	DelaySeconds   float64 `json:"delay_seconds,omitempty"`
	EverySeconds   float64 `json:"every_seconds,omitempty"`
	CronExpr       string  `json:"cron_expr,omitempty"`
	TZ             string  `json:"tz,omitempty"`
	DeleteAfterRun bool    `json:"delete_after_run,omitempty"`

	// --- remove / run ---
	JobID string `json:"job_id,omitempty"`
}

type cronTool struct {
	svc *cron.Service
}

// NewCronTool creates a tool that lets the agent manage scheduled jobs.
func NewCronTool(svc *cron.Service) bladestools.Tool {
	t := &cronTool{svc: svc}
	inputSchema, _ := jsonschema.For[cronInput](nil)
	outputSchema, _ := jsonschema.For[string](nil)
	return bladestools.NewTool(
		"cron",
		"Manage scheduled jobs. Use action=add to schedule a shell command or an agent message, "+
			"action=list to see all jobs and their last status, "+
			"action=remove to delete a job by job_id, "+
			"action=run to execute a job immediately.",
		bladestools.HandleFunc(t.handle),
		bladestools.WithInputSchema(inputSchema),
		bladestools.WithOutputSchema(outputSchema),
	)
}

func (t *cronTool) handle(ctx context.Context, raw string) (string, error) {
	var in cronInput
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return "", fmt.Errorf("cron: parse input: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(in.Action)) {
	case "add":
		return t.add(in)
	case "list":
		return t.list()
	case "remove":
		return t.remove(ctx, in.JobID)
	case "run":
		return t.run(ctx, in.JobID)
	default:
		return "", fmt.Errorf("unknown action %q; valid: add, list, remove, run", in.Action)
	}
}

func (t *cronTool) add(a cronInput) (string, error) {
	// delay_seconds shorthand.
	if a.DelaySeconds > 0 && a.AtMs == 0 {
		a.AtMs = time.Now().UnixMilli() + int64(a.DelaySeconds*1000)
		if a.ScheduleKind == "" {
			a.ScheduleKind = "at"
		}
	}

	sk := normScheduleKind(a.ScheduleKind)
	if sk == "" {
		switch {
		case a.CronExpr != "":
			sk = cron.ScheduleCron
		case a.EverySeconds > 0:
			sk = cron.ScheduleEvery
		case a.AtMs > 0:
			sk = cron.ScheduleAt
		default:
			return "", fmt.Errorf("specify schedule_kind (at/every/cron) or delay_seconds / every_seconds / cron_expr / at_ms")
		}
	}

	sched := cron.Schedule{Kind: sk}
	switch sk {
	case cron.ScheduleAt:
		if a.AtMs <= 0 {
			return "", fmt.Errorf("at_ms is required for schedule_kind=at")
		}
		sched.AtMs = a.AtMs
	case cron.ScheduleEvery:
		if a.EverySeconds <= 0 {
			return "", fmt.Errorf("every_seconds is required for schedule_kind=every")
		}
		sched.EveryMs = int64(a.EverySeconds * 1000)
	case cron.ScheduleCron:
		if a.CronExpr == "" {
			return "", fmt.Errorf("cron_expr is required for schedule_kind=cron")
		}
		parser := robfigcron.NewParser(robfigcron.Minute | robfigcron.Hour | robfigcron.Dom | robfigcron.Month | robfigcron.Dow)
		if _, err := parser.Parse(a.CronExpr); err != nil {
			return "", fmt.Errorf("invalid cron_expr %q: %w", a.CronExpr, err)
		}
		sched.Expr = a.CronExpr
		sched.TZ = a.TZ
	}

	pk := normPayloadKind(a.PayloadKind)
	if pk == "" {
		if a.Command != "" {
			pk = cron.PayloadExec
		} else {
			pk = cron.PayloadAgentTurn
		}
	}

	payload := cron.Payload{Kind: pk, SessionID: a.SessionID}
	switch pk {
	case cron.PayloadExec:
		if a.Command == "" {
			return "", fmt.Errorf("command is required for payload_kind=exec")
		}
		payload.Command = a.Command
	case cron.PayloadAgentTurn:
		if a.Message == "" {
			return "", fmt.Errorf("message is required for payload_kind=agent_turn")
		}
		payload.Message = a.Message
	default:
		return "", fmt.Errorf("unknown payload_kind %q; valid: exec, agent_turn", pk)
	}

	name := strings.TrimSpace(a.Name)
	if name == "" {
		name = payload.Command + payload.Message
		if len(name) > 40 {
			name = name[:40]
		}
	}

	job, err := t.svc.AddJob(context.Background(), name, sched, payload, a.DeleteAfterRun)
	if err != nil {
		return "", err
	}

	next := "never"
	if job.State.NextRunAtMs > 0 {
		next = time.UnixMilli(job.State.NextRunAtMs).Format(time.RFC3339)
	}
	return fmt.Sprintf("Job added. id=%s name=%q next=%s", job.ID, job.Name, next), nil
}

func (t *cronTool) list() (string, error) {
	jobs := t.svc.ListJobs(true)
	if len(jobs) == 0 {
		return "No scheduled jobs.", nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%-10s %-24s %-8s %-22s %-6s %s\n", "ID", "NAME", "KIND", "NEXT RUN", "ON", "PAYLOAD")
	sb.WriteString(strings.Repeat("-", 88) + "\n")
	for _, j := range jobs {
		next := "—"
		if j.State.NextRunAtMs > 0 {
			next = time.UnixMilli(j.State.NextRunAtMs).Format("2006-01-02 15:04:05")
		}
		on := "on"
		if !j.Enabled {
			on = "off"
		}
		payload := string(j.Payload.Kind)
		if j.Payload.Command != "" {
			payload += ":" + truncStr(j.Payload.Command, 28)
		} else if j.Payload.Message != "" {
			payload += ":" + truncStr(j.Payload.Message, 28)
		}
		fmt.Fprintf(&sb, "%-10s %-24s %-8s %-22s %-6s %s\n",
			j.ID, truncStr(j.Name, 23), string(j.Schedule.Kind), next, on, payload)
		if j.State.LastStatus != "" {
			fmt.Fprintf(&sb, "  last=%s %s\n", j.State.LastStatus, j.State.LastError)
		}
	}
	return sb.String(), nil
}

func (t *cronTool) remove(ctx context.Context, id string) (string, error) {
	if id == "" {
		return "", fmt.Errorf("job_id is required")
	}
	if !t.svc.RemoveJob(ctx, id) {
		return fmt.Sprintf("Job %q not found.", id), nil
	}
	return fmt.Sprintf("Job %q removed.", id), nil
}

func (t *cronTool) run(ctx context.Context, id string) (string, error) {
	if id == "" {
		return "", fmt.Errorf("job_id is required")
	}
	output, err := t.svc.RunNow(ctx, id)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(output) == "" {
		return fmt.Sprintf("Job %q triggered.", id), nil
	}
	return fmt.Sprintf("Job %q triggered.\n%s", id, output), nil
}

func truncStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func normScheduleKind(raw string) cron.ScheduleKind {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return ""
	case "delay":
		return cron.ScheduleAt
	default:
		return cron.ScheduleKind(strings.ToLower(strings.TrimSpace(raw)))
	}
}

func normPayloadKind(raw string) cron.PayloadKind {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return ""
	case "command", "shell":
		return cron.PayloadExec
	case "message", "agent_message":
		return cron.PayloadAgentTurn
	default:
		return cron.PayloadKind(strings.ToLower(strings.TrimSpace(raw)))
	}
}
