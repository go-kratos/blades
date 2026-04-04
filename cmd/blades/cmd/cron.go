package cmd

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	appcore "github.com/go-kratos/blades/cmd/blades/internal/app"
	robfigcron "github.com/robfig/cron/v3"
	"github.com/spf13/cobra"

	"github.com/go-kratos/blades/cmd/blades/internal/cron"
)

const (
	defaultHeartbeatJobName   = "heartbeat"
	defaultHeartbeatMessage   = "heartbeat poll"
	defaultHeartbeatSessionID = "heartbeat"
	defaultHeartbeatEvery     = 30 * time.Minute
)

func newCronCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cron",
		Short: "Manage scheduled jobs",
	}
	cmd.AddCommand(
		newCronListCmd(),
		newCronAddCmd(),
		newCronHeartbeatCmd(),
		newCronRemoveCmd(),
		newCronRunCmd(),
	)
	return cmd
}

// cronService creates a Service with no handler for read-only operations
// (list, add, remove). It only validates config and workspace paths because
// a full runtime load is not needed for these commands.
func cronService() (*cron.Service, error) {
	return cronServiceForOptions(appcore.Options{})
}

func newCronListCmd() *cobra.Command {
	var all bool
	cmd := newCronServiceCmd("list", "List scheduled jobs", cobra.NoArgs, func(cmd *cobra.Command, svc *cron.Service, args []string) error {
		jobs, err := svc.ListJobs(all)
		if err != nil {
			return err
		}
		if len(jobs) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "(no jobs)")
			return nil
		}
		for _, j := range jobs {
			fmt.Fprintln(cmd.OutOrStdout(), cron.FormatJob(j))
		}
		return nil
	})
	cmd.Flags().BoolVar(&all, "all", false, "include disabled jobs")
	return cmd
}

func newCronAddCmd() *cobra.Command {
	var (
		name           string
		cronExpr       string
		everyStr       string
		delayStr       string
		taskType       string
		prompt         string
		command        string
		text           string
		agentSessionID string
		chatSessionID  string
		deleteAfter    bool
		tz             string
	)
	cmd := newCronServiceCmd("add", "Add a scheduled job", cobra.NoArgs, func(cmd *cobra.Command, svc *cron.Service, args []string) error {
		if name == "" {
			return fmt.Errorf("--name is required")
		}
		if err := validateCronAddFlags(cronExpr, everyStr, delayStr, taskType, prompt, command, text, chatSessionID); err != nil {
			return err
		}

		sched, err := parseScheduleFlags(cronExpr, everyStr, delayStr, tz, scheduleFlagOptions{AllowDelay: true})
		if err != nil {
			return err
		}

		payload, err := cronPayloadFromFlags(taskType, prompt, command, text, agentSessionID, chatSessionID)
		if err != nil {
			return err
		}

		job, err := svc.AddJob(cmd.Context(), name, sched, payload, deleteAfter)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "✓ job added: %s\n", cron.FormatJob(job))
		return nil
	})
	cmd.Example = `  blades cron add --name "daily-brief" --type agent --cron "0 8 * * *" --prompt "generate morning brief"
  blades cron add --name "check" --type exec --every 1h --command "echo ok"
  blades cron add --name "post-reminder" --type notify --every 1h --text "remember to post" --chat-session "chat-id"
  blades cron add --name "test ls" --type exec --delay 10 --command "ls . > outputs/test.txt"`
	cmd.Flags().StringVar(&name, "name", "", "job name")
	cmd.Flags().StringVar(&cronExpr, "cron", "", "cron expression (5-field)")
	cmd.Flags().StringVar(&everyStr, "every", "", "repeat interval, e.g. 1h, 30m")
	cmd.Flags().StringVar(&delayStr, "delay", "", "run once after delay (seconds when unit omitted, e.g. 10 or 10s)")
	cmd.Flags().StringVar(&taskType, "type", "", "task type: exec, agent, or notify (inferred from payload field when omitted)")
	cmd.Flags().StringVar(&prompt, "prompt", "", "agent prompt for --type agent")
	cmd.Flags().StringVar(&command, "command", "", "shell command for --type exec")
	cmd.Flags().StringVar(&text, "text", "", "direct chat message for --type notify")
	cmd.Flags().StringVar(&agentSessionID, "agent-session", "", "agent session ID for --type agent (default: isolated per job)")
	cmd.Flags().StringVar(&chatSessionID, "chat-session", "", "chat/session target for notify, or output sink for exec/agent")
	cmd.Flags().StringVar(&tz, "tz", "", "timezone for cron expression")
	cmd.Flags().BoolVar(&deleteAfter, "delete-after-run", false, "delete job after first execution")
	return cmd
}

func validateCronAddFlags(cronExpr, everyStr, delayStr, taskType, prompt, command, text, chatSessionID string) error {
	if _, err := parseScheduleFlags(cronExpr, everyStr, delayStr, "", scheduleFlagOptions{AllowDelay: true, ValidateOnly: true}); err != nil {
		return err
	}
	_, err := cronPayloadFromFlags(taskType, prompt, command, text, "cron-agent-session", chatSessionID)
	return err
}

type scheduleFlagOptions struct {
	AllowDelay   bool
	DefaultEvery time.Duration
	ValidateOnly bool
}

func parseScheduleFlags(cronExpr, everyStr, delayStr, tz string, opts scheduleFlagOptions) (cron.Schedule, error) {
	return parseScheduleFlagsWithNow(time.Now, cronExpr, everyStr, delayStr, tz, opts)
}

func parseScheduleFlagsWithNow(now func() time.Time, cronExpr, everyStr, delayStr, tz string, opts scheduleFlagOptions) (cron.Schedule, error) {
	scheduleFlags := 0
	for _, value := range []string{cronExpr, everyStr, delayStr} {
		if strings.TrimSpace(value) != "" {
			scheduleFlags++
		}
	}
	if scheduleFlags > 1 {
		return cron.Schedule{}, fmt.Errorf("--cron, --every, and --delay are mutually exclusive")
	}
	if scheduleFlags == 0 {
		if opts.DefaultEvery > 0 {
			return cron.Schedule{Kind: cron.ScheduleEvery, EveryMs: opts.DefaultEvery.Milliseconds()}, nil
		}
		return cron.Schedule{}, fmt.Errorf("one of --cron, --every, or --delay is required")
	}

	switch {
	case strings.TrimSpace(cronExpr) != "":
		parser := robfigcron.NewParser(robfigcron.Minute | robfigcron.Hour | robfigcron.Dom | robfigcron.Month | robfigcron.Dow)
		if _, err := parser.Parse(cronExpr); err != nil {
			return cron.Schedule{}, fmt.Errorf("invalid --cron expression: %w", err)
		}
		if opts.ValidateOnly {
			return cron.Schedule{}, nil
		}
		return cron.Schedule{Kind: cron.ScheduleCron, Expr: cronExpr, TZ: tz}, nil
	case strings.TrimSpace(everyStr) != "":
		d, err := time.ParseDuration(everyStr)
		if err != nil {
			return cron.Schedule{}, fmt.Errorf("invalid --every duration: %w", err)
		}
		if d <= 0 {
			return cron.Schedule{}, fmt.Errorf("--every must be > 0")
		}
		if opts.ValidateOnly {
			return cron.Schedule{}, nil
		}
		return cron.Schedule{Kind: cron.ScheduleEvery, EveryMs: d.Milliseconds()}, nil
	case strings.TrimSpace(delayStr) != "":
		if !opts.AllowDelay {
			return cron.Schedule{}, fmt.Errorf("--delay is not supported here")
		}
		d, err := parseDelayValue(delayStr)
		if err != nil {
			return cron.Schedule{}, err
		}
		if opts.ValidateOnly {
			return cron.Schedule{}, nil
		}
		if now == nil {
			now = time.Now
		}
		return cron.Schedule{Kind: cron.ScheduleAt, At: now().Add(d)}, nil
	default:
		return cron.Schedule{}, fmt.Errorf("one of --cron, --every, or --delay is required")
	}
}

func parseDelayValue(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, fmt.Errorf("--delay must not be empty")
	}

	if d, err := time.ParseDuration(raw); err == nil {
		if d <= 0 {
			return 0, fmt.Errorf("--delay must be > 0")
		}
		return d, nil
	}

	secs, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid --delay value %q: use seconds (e.g. 10) or duration (e.g. 10s, 5m)", raw)
	}
	if secs <= 0 {
		return 0, fmt.Errorf("--delay must be > 0")
	}

	return time.Duration(secs * float64(time.Second)), nil
}

func newCronRemoveCmd() *cobra.Command {
	return newCronServiceCmd("remove <id>", "Remove a scheduled job", cobra.ExactArgs(1), func(cmd *cobra.Command, svc *cron.Service, args []string) error {
		found, err := svc.RemoveJob(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("job %q not found", args[0])
		}
		fmt.Fprintf(cmd.OutOrStdout(), "✓ removed job %s\n", args[0])
		return nil
	})
}
