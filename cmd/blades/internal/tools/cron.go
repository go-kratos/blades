package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/cmd/blades/internal/cron"
	bladestools "github.com/go-kratos/blades/tools"
	"github.com/google/jsonschema-go/jsonschema"
	robfigcron "github.com/robfig/cron/v3"
)

type cronInput struct {
	// Action is one of: add, list, remove, run
	Action string `json:"action" jsonschema:"What to do. Use exact values: add, list, remove, or run."`

	Name           string  `json:"name,omitempty" jsonschema:"Job name. Required for add. Also accepted as an identifier for remove or run when job_id is unknown."`
	ScheduleType   string  `json:"schedule_type,omitempty" jsonschema:"Schedule type. Use exact values: at, every, or cron. It may be omitted when using delay_seconds, delay_minutes, at, every_seconds, every_minutes, or cron_expr shorthand."`
	At             string  `json:"at,omitempty" jsonschema:"Absolute RFC3339 time for schedule_type=at, for example 2026-03-19T10:00:00Z."`
	DelaySeconds   float64 `json:"delay_seconds,omitempty" jsonschema:"One-shot shorthand: run once after N seconds. Prefer this for simple delays."`
	DelayMinutes   float64 `json:"delay_minutes,omitempty" jsonschema:"One-shot shorthand: run once after N minutes."`
	EverySeconds   float64 `json:"every_seconds,omitempty" jsonschema:"Repeat every N seconds for schedule_type=every."`
	EveryMinutes   float64 `json:"every_minutes,omitempty" jsonschema:"Repeat every N minutes for schedule_type=every."`
	CronExpr       string  `json:"cron_expr,omitempty" jsonschema:"Standard 5-field cron expression for schedule_type=cron: min hour dom month dow."`
	TZ             string  `json:"tz,omitempty" jsonschema:"Optional IANA timezone for cron_expr, for example Asia/Shanghai."`
	TaskType       string  `json:"task_type,omitempty" jsonschema:"Task type. Use exact values: exec, agent, or notify. If omitted, it is inferred from command, prompt, or text."`
	Command        string  `json:"command,omitempty" jsonschema:"Shell command for task_type=exec. This runs in the terminal/workspace."`
	Prompt         string  `json:"prompt,omitempty" jsonschema:"Prompt text for task_type=agent. This asks the assistant to run a turn."`
	Text           string  `json:"text,omitempty" jsonschema:"Direct text message for task_type=notify. This sends to a chat/session without invoking the assistant."`
	AgentSessionID string  `json:"agent_session_id,omitempty" jsonschema:"Optional conversation/session identifier for task_type=agent. If omitted, a job-specific session is created automatically."`
	ChatSessionID  string  `json:"chat_session_id,omitempty" jsonschema:"Optional social/chat session identifier. For notify it is the target chat. For exec or agent it receives the job output when set. If omitted and a current chat session exists, that current chat is used automatically."`
	DeleteAfterRun bool    `json:"delete_after_run,omitempty" jsonschema:"If true, remove the job after it runs once."`

	// --- remove / run ---
	JobID string `json:"job_id,omitempty" jsonschema:"Preferred job identifier for remove or run."`
}

type cronTool struct {
	svc *cron.Service
}

// NewCronTool creates a tool that lets the agent manage scheduled jobs.
func NewCronTool(svc *cron.Service) bladestools.Tool {
	t := &cronTool{svc: svc}
	inputSchema := newCronInputSchema()
	outputSchema, _ := jsonschema.For[string](nil)
	return bladestools.NewTool(
		"cron",
		`Manage scheduled jobs.
Use action=add with a flat payload: schedule_type / at / delay_seconds / delay_minutes / every_seconds / every_minutes / cron_expr and task_type / command / prompt / text.
Task type=exec runs a shell command, type=agent asks the assistant, type=notify sends a chat message directly.
Use action=list to see all jobs and their last status.
Use action=remove to delete a job by job_id.
Use action=run to execute a job immediately.
Prefer delay_minutes or delay_seconds for simple one-shot delays.
For recurring schedules use a standard 5-field cron expression.
Use exact enum values.`,
		bladestools.HandleFunc(t.handle),
		bladestools.WithInputSchema(inputSchema),
		bladestools.WithOutputSchema(outputSchema),
	)
}

func newCronInputSchema() *jsonschema.Schema {
	schema, err := jsonschema.For[cronInput](nil)
	if err != nil || schema == nil {
		return schema
	}

	schema.Description = "Cron job requests. Use one flat object. Prefer delay_minutes or delay_seconds for simple one-shot delays. Use RFC3339 in at. Use exact enum values. For recurring schedules use a standard 5-field cron expression."
	schema.Examples = []any{
		map[string]any{
			"action":          "add",
			"name":            "morning-brief",
			"schedule_type":   "cron",
			"cron_expr":       "0 8 * * *",
			"tz":              "Asia/Shanghai",
			"task_type":       "agent",
			"prompt":          "Summarize my pending tasks.",
			"chat_session_id": "chat-123",
		},
		map[string]any{
			"action":        "add",
			"name":          "list-files-later",
			"delay_minutes": 10,
			"task_type":     "exec",
			"command":       "ls .",
		},
	}

	if action := schema.Properties["action"]; action != nil {
		action.Enum = []any{"add", "list", "remove", "run"}
	}
	if scheduleType := schema.Properties["schedule_type"]; scheduleType != nil {
		scheduleType.Enum = []any{"at", "every", "cron"}
	}
	if taskType := schema.Properties["task_type"]; taskType != nil {
		taskType.Enum = []any{"exec", "agent", "notify"}
	}
	for _, key := range []string{"delay_seconds", "delay_minutes", "every_seconds", "every_minutes"} {
		if prop := schema.Properties[key]; prop != nil {
			min := float64(0)
			prop.ExclusiveMinimum = &min
		}
	}

	return schema
}

func currentChatSessionID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if sess, ok := blades.SessionFromContext(ctx); ok && sess != nil {
		return strings.TrimSpace(sess.ID())
	}
	return ""
}

func fillChatSessionID(ctx context.Context, chatSessionID string) string {
	chatSessionID = strings.TrimSpace(chatSessionID)
	if chatSessionID != "" {
		log.Printf("cron add: chat_session_id provided by caller: %s", chatSessionID)
		return chatSessionID
	}
	if sessionID := currentChatSessionID(ctx); sessionID != "" {
		log.Printf("cron add: chat_session_id filled from context session_id=%s", sessionID)
		return sessionID
	}
	log.Printf("cron add: no chat_session_id available")
	return ""
}

func buildSchedule(in cronInput) (cron.Schedule, error) {
	delaySeconds := in.DelaySeconds
	everySeconds := in.EverySeconds
	if in.DelayMinutes > 0 && delaySeconds == 0 {
		delaySeconds = in.DelayMinutes * 60
	}
	if in.EveryMinutes > 0 && everySeconds == 0 {
		everySeconds = in.EveryMinutes * 60
	}
	atText := strings.TrimSpace(in.At)
	scheduleType := strings.TrimSpace(in.ScheduleType)
	if delaySeconds > 0 && atText == "" {
		atText = time.Now().Add(time.Duration(delaySeconds * float64(time.Second))).Format(time.RFC3339Nano)
		if scheduleType == "" {
			scheduleType = "at"
		}
	}

	sk := normScheduleKind(scheduleType)
	if sk == "" {
		switch {
		case strings.TrimSpace(in.CronExpr) != "":
			sk = cron.ScheduleCron
		case everySeconds > 0:
			sk = cron.ScheduleEvery
		case atText != "":
			sk = cron.ScheduleAt
		default:
			return cron.Schedule{}, fmt.Errorf("add requires one of at, delay_seconds, delay_minutes, every_seconds, every_minutes, or cron_expr")
		}
	}

	out := cron.Schedule{Kind: sk}
	switch sk {
	case cron.ScheduleAt:
		if atText == "" {
			return cron.Schedule{}, fmt.Errorf("at is required for schedule_type=at")
		}
		at, err := time.Parse(time.RFC3339Nano, atText)
		if err != nil {
			return cron.Schedule{}, fmt.Errorf("invalid at value %q: use RFC3339 time", atText)
		}
		out.At = at
	case cron.ScheduleEvery:
		if everySeconds <= 0 {
			return cron.Schedule{}, fmt.Errorf("every_seconds or every_minutes is required for schedule_type=every")
		}
		out.EveryMs = int64(everySeconds * 1000)
	case cron.ScheduleCron:
		if strings.TrimSpace(in.CronExpr) == "" {
			return cron.Schedule{}, fmt.Errorf("cron_expr is required for schedule_type=cron")
		}
		parser := robfigcron.NewParser(robfigcron.Minute | robfigcron.Hour | robfigcron.Dom | robfigcron.Month | robfigcron.Dow)
		if _, err := parser.Parse(in.CronExpr); err != nil {
			return cron.Schedule{}, fmt.Errorf("invalid cron_expr %q: %w", in.CronExpr, err)
		}
		out.Expr = strings.TrimSpace(in.CronExpr)
		out.TZ = strings.TrimSpace(in.TZ)
	default:
		return cron.Schedule{}, fmt.Errorf("unknown schedule_type %q; valid: at, every, cron", in.ScheduleType)
	}

	return out, nil
}

func buildTask(ctx context.Context, in cronInput) (cron.Payload, error) {
	taskType := strings.ToLower(strings.TrimSpace(in.TaskType))
	if taskType == "" {
		switch {
		case strings.TrimSpace(in.Command) != "":
			taskType = "exec"
		case strings.TrimSpace(in.Prompt) != "":
			taskType = "agent"
		case strings.TrimSpace(in.Text) != "":
			taskType = "notify"
		default:
			return cron.Payload{}, fmt.Errorf("add requires task_type, command, prompt, or text")
		}
	}
	chatSessionID := fillChatSessionID(ctx, in.ChatSessionID)

	switch taskType {
	case "exec":
		command := strings.TrimSpace(in.Command)
		if command == "" {
			return cron.Payload{}, fmt.Errorf("command is required for task_type=exec")
		}
		return cron.Payload{
			Kind:           cron.PayloadExec,
			Command:        command,
			ReplySessionID: chatSessionID,
		}, nil
	case "agent":
		prompt := strings.TrimSpace(in.Prompt)
		if prompt == "" {
			return cron.Payload{}, fmt.Errorf("prompt is required for task_type=agent")
		}
		return cron.Payload{
			Kind:           cron.PayloadAgentTurn,
			Message:        prompt,
			SessionID:      strings.TrimSpace(in.AgentSessionID),
			ReplySessionID: chatSessionID,
		}, nil
	case "notify":
		text := strings.TrimSpace(in.Text)
		if text == "" {
			return cron.Payload{}, fmt.Errorf("text is required for task_type=notify")
		}
		if chatSessionID == "" {
			return cron.Payload{}, fmt.Errorf("chat_session_id is required for task_type=notify")
		}
		return cron.Payload{
			Kind:           cron.PayloadNotify,
			Message:        text,
			ReplySessionID: chatSessionID,
		}, nil
	default:
		return cron.Payload{}, fmt.Errorf("unknown task_type %q; valid: exec, agent, notify", in.TaskType)
	}
}

func defaultJobName(payload cron.Payload) string {
	switch payload.Kind {
	case cron.PayloadExec:
		return payload.Command
	case cron.PayloadAgentTurn, cron.PayloadNotify:
		return payload.Message
	default:
		return ""
	}
}

func taskListLabel(payload cron.Payload) string {
	switch payload.Kind {
	case cron.PayloadExec:
		label := "exec:" + truncStr(payload.Command, 28)
		if payload.ReplySessionID != "" {
			label += " -> chat:" + truncStr(payload.ReplySessionID, 12)
		}
		return label
	case cron.PayloadAgentTurn:
		label := "agent:" + truncStr(payload.Message, 28)
		if payload.ReplySessionID != "" {
			label += " -> chat:" + truncStr(payload.ReplySessionID, 12)
		}
		return label
	case cron.PayloadNotify:
		return "notify -> chat:" + truncStr(payload.ReplySessionID, 12)
	default:
		return string(payload.Kind)
	}
}

func (t *cronTool) handle(ctx context.Context, raw string) (string, error) {
	var in cronInput
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return "", fmt.Errorf("cron: parse input: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(in.Action)) {
	case "add":
		return t.add(ctx, in)
	case "list":
		return t.list()
	case "remove":
		id, err := t.resolveJobID(in)
		if err != nil {
			return "", err
		}
		return t.remove(ctx, id)
	case "run":
		id, err := t.resolveJobID(in)
		if err != nil {
			return "", err
		}
		return t.run(ctx, id)
	default:
		return "", fmt.Errorf("unknown action %q; valid: add, list, remove, run", in.Action)
	}
}

func (t *cronTool) resolveJobID(in cronInput) (string, error) {
	if id := strings.TrimSpace(in.JobID); id != "" {
		return id, nil
	}

	name := strings.TrimSpace(in.Name)
	if name == "" {
		return "", fmt.Errorf("job_id is required (name is also accepted)")
	}

	jobs, err := t.svc.ListJobs(true)
	if err != nil {
		return "", fmt.Errorf("list jobs: %w", err)
	}
	matches := make([]string, 0, 1)
	for _, j := range jobs {
		if strings.EqualFold(strings.TrimSpace(j.Name), name) {
			matches = append(matches, j.ID)
		}
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("job %q not found", name)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("multiple jobs named %q: %s; use job_id", name, strings.Join(matches, ", "))
	}
}

func (t *cronTool) add(ctx context.Context, a cronInput) (string, error) {
	sched, err := buildSchedule(a)
	if err != nil {
		return "", err
	}

	payload, err := buildTask(ctx, a)
	if err != nil {
		return "", err
	}

	name := strings.TrimSpace(a.Name)
	if name == "" {
		name = defaultJobName(payload)
		if len(name) > 40 {
			name = name[:40]
		}
	}
	if name == "" {
		return "", fmt.Errorf("name is required when task content is empty")
	}

	job, err := t.svc.AddJob(context.Background(), name, sched, payload, a.DeleteAfterRun)
	if err != nil {
		return "", err
	}

	next := "never"
	if !job.State.NextRunAt.IsZero() {
		next = job.State.NextRunAt.Format(time.RFC3339)
	}
	return fmt.Sprintf("Job added. id=%s name=%q next=%s", job.ID, job.Name, next), nil
}

func (t *cronTool) list() (string, error) {
	jobs, err := t.svc.ListJobs(true)
	if err != nil {
		return "", fmt.Errorf("list jobs: %w", err)
	}
	if len(jobs) == 0 {
		return "No scheduled jobs.", nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%-10s %-24s %-8s %-22s %-6s %s\n", "ID", "NAME", "WHEN", "NEXT RUN", "ON", "TASK")
	sb.WriteString(strings.Repeat("-", 100) + "\n")
	for _, j := range jobs {
		next := "—"
		if !j.State.NextRunAt.IsZero() {
			next = j.State.NextRunAt.Format("2006-01-02 15:04:05")
		}
		on := "on"
		if !j.Enabled {
			on = "off"
		}
		fmt.Fprintf(&sb, "%-10s %-24s %-8s %-22s %-6s %s\n",
			j.ID, truncStr(j.Name, 23), string(j.Schedule.Kind), next, on, taskListLabel(j.Payload))
		if j.State.LastStatus != "" {
			fmt.Fprintf(&sb, "  last=%s %s\n", j.State.LastStatus, j.State.LastError)
		}
	}
	return sb.String(), nil
}

func (t *cronTool) remove(ctx context.Context, id string) (string, error) {
	if id == "" {
		return "", fmt.Errorf("job_id is required (name is also accepted)")
	}
	found, err := t.svc.RemoveJob(ctx, id)
	if err != nil {
		return "", fmt.Errorf("remove job: %w", err)
	}
	if !found {
		return fmt.Sprintf("Job %q not found.", id), nil
	}
	return fmt.Sprintf("Job %q removed.", id), nil
}

// TODO: use this context will time out
func (t *cronTool) run(_ context.Context, id string) (string, error) {
	if id == "" {
		return "", fmt.Errorf("job_id is required (name is also accepted)")
	}
	// Run the job asynchronously so the agent is not blocked waiting for
	// potentially long-running commands or recursive agent turns.
	go func() {
		_, _ = t.svc.RunNow(context.Background(), id)
	}()
	return fmt.Sprintf("Job %q triggered (running in background).", id), nil
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
	case "delay", "once":
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
	case "chat", "social", "channel_message":
		return cron.PayloadNotify
	default:
		return cron.PayloadKind(strings.ToLower(strings.TrimSpace(raw)))
	}
}
