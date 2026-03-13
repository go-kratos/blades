package main

import (
	"context"
	"fmt"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	robfigcron "github.com/robfig/cron/v3"
	"github.com/spf13/cobra"

	blades "github.com/go-kratos/blades"
	"github.com/go-kratos/blades/cmd/blades/internal/cron"
	"github.com/go-kratos/blades/cmd/blades/internal/session"
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

// cronService creates a Service with no handler (for read-only operations).
func cronService() (*cron.Service, error) {
	cfg, err := loadConfigForFlags()
	if err != nil {
		return nil, err
	}
	storePath := filepath.Join(cfg.Workspace, "cron.json")
	return cron.NewService(storePath, nil), nil
}

func findHeartbeatJob(svc *cron.Service, name, message, sessionID string) *cron.Job {
	trimmedName := strings.TrimSpace(name)
	trimmedMessage := strings.TrimSpace(message)
	trimmedSessionID := strings.TrimSpace(sessionID)
	for _, job := range svc.ListJobs(true) {
		if strings.TrimSpace(job.Name) != trimmedName {
			continue
		}
		if job.Payload.Kind != cron.PayloadAgentTurn {
			continue
		}
		if strings.TrimSpace(job.Payload.Message) != trimmedMessage {
			continue
		}
		if strings.TrimSpace(job.Payload.SessionID) != trimmedSessionID {
			continue
		}
		return job
	}
	return nil
}

func ensureHeartbeatJob(ctx context.Context, svc *cron.Service, schedule cron.Schedule, name, message, sessionID string) (*cron.Job, bool, error) {
	if job := findHeartbeatJob(svc, name, message, sessionID); job != nil {
		return job, true, nil
	}
	job, err := svc.AddJob(ctx, name, schedule, cron.Payload{
		Kind:      cron.PayloadAgentTurn,
		Message:   message,
		SessionID: sessionID,
	}, false)
	if err != nil {
		return nil, false, err
	}
	return job, false, nil
}

func runScheduledJobNow(ctx context.Context, id string) (string, error) {
	cfg, ws, _, err := loadAll()
	if err != nil {
		return "", err
	}
	runner, err := buildRunner(cfg, ws)
	if err != nil {
		return "", err
	}
	sessMgr := session.NewManager(ws.SessionsDir())
	trigger := makeTrigger(runner, sessMgr)
	svc := cron.NewService(ws.CronStorePath(), cron.NewAgentHandler(trigger, 60*time.Second))
	return svc.RunNow(ctx, id)
}

func newCronListCmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List scheduled jobs",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := cronService()
			if err != nil {
				return err
			}
			jobs := svc.ListJobs(all)
			if len(jobs) == 0 {
				fmt.Println("(no jobs)")
				return nil
			}
			for _, j := range jobs {
				fmt.Println(cron.FormatJob(j))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "include disabled jobs")
	return cmd
}

func newCronAddCmd() *cobra.Command {
	var (
		name        string
		cronExpr    string
		everyStr    string
		message     string
		command     string
		deleteAfter bool
		tz          string
	)
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a scheduled job",
		Example: `  blades cron add --name "daily-brief" --cron "0 8 * * *" --message "generate morning brief"
  blades cron add --name "check" --every 1h --command "echo ok"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if name == "" {
				return fmt.Errorf("--name is required")
			}

			var sched cron.Schedule
			switch {
			case cronExpr != "":
				sched = cron.Schedule{Kind: cron.ScheduleCron, Expr: cronExpr, TZ: tz}
			case everyStr != "":
				d, err := time.ParseDuration(everyStr)
				if err != nil {
					return fmt.Errorf("invalid --every duration: %w", err)
				}
				sched = cron.Schedule{Kind: cron.ScheduleEvery, EveryMs: d.Milliseconds()}
			default:
				return fmt.Errorf("one of --cron or --every is required")
			}

			var payload cron.Payload
			switch {
			case message != "":
				payload = cron.Payload{Kind: cron.PayloadAgentTurn, Message: message, SessionID: "cron"}
			case command != "":
				payload = cron.Payload{Kind: cron.PayloadExec, Command: command}
			default:
				return fmt.Errorf("one of --message or --command is required")
			}

			svc, err := cronService()
			if err != nil {
				return err
			}
			job, err := svc.AddJob(cmd.Context(), name, sched, payload, deleteAfter)
			if err != nil {
				return err
			}
			fmt.Printf("✓ job added: %s\n", cron.FormatJob(job))
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "job name")
	cmd.Flags().StringVar(&cronExpr, "cron", "", "cron expression (5-field)")
	cmd.Flags().StringVar(&everyStr, "every", "", "repeat interval, e.g. 1h, 30m")
	cmd.Flags().StringVar(&message, "message", "", "agent message payload")
	cmd.Flags().StringVar(&command, "command", "", "shell command payload")
	cmd.Flags().StringVar(&tz, "tz", "", "timezone for cron expression")
	cmd.Flags().BoolVar(&deleteAfter, "delete-after-run", false, "delete job after first execution")
	return cmd
}

func newCronHeartbeatCmd() *cobra.Command {
	var (
		name      string
		cronExpr  string
		everyStr  string
		message   string
		sessionID string
		tz        string
		runNow    bool
	)
	cmd := &cobra.Command{
		Use:   "heartbeat",
		Short: "Ensure a heartbeat job exists",
		Example: `  blades cron heartbeat
  blades cron heartbeat --every 15m
  blades cron heartbeat --run-now`,
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := cronService()
			if err != nil {
				return err
			}

			var schedule cron.Schedule
			switch {
			case cronExpr != "":
				parser := robfigcron.NewParser(robfigcron.Minute | robfigcron.Hour | robfigcron.Dom | robfigcron.Month | robfigcron.Dow)
				if _, err := parser.Parse(cronExpr); err != nil {
					return fmt.Errorf("invalid --cron expression: %w", err)
				}
				schedule = cron.Schedule{Kind: cron.ScheduleCron, Expr: cronExpr, TZ: tz}
			case everyStr != "":
				d, err := time.ParseDuration(everyStr)
				if err != nil {
					return fmt.Errorf("invalid --every duration: %w", err)
				}
				schedule = cron.Schedule{Kind: cron.ScheduleEvery, EveryMs: d.Milliseconds()}
			default:
				schedule = cron.Schedule{Kind: cron.ScheduleEvery, EveryMs: defaultHeartbeatEvery.Milliseconds()}
			}

			job, existed, err := ensureHeartbeatJob(cmd.Context(), svc, schedule, name, message, sessionID)
			if err != nil {
				return err
			}
			if existed {
				fmt.Printf("✓ heartbeat job already exists: %s\n", cron.FormatJob(job))
			} else {
				fmt.Printf("✓ heartbeat job ready: %s\n", cron.FormatJob(job))
			}

			if runNow {
				ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
				defer cancel()
				output, err := runScheduledJobNow(ctx, job.ID)
				if err != nil {
					return err
				}
				fmt.Printf("✓ heartbeat triggered: %s\n", job.ID)
				if strings.TrimSpace(output) != "" {
					fmt.Print(output)
					if !strings.HasSuffix(output, "\n") {
						fmt.Println()
					}
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", defaultHeartbeatJobName, "heartbeat job name")
	cmd.Flags().StringVar(&cronExpr, "cron", "", "cron expression (5-field)")
	cmd.Flags().StringVar(&everyStr, "every", defaultHeartbeatEvery.String(), "repeat interval, e.g. 30m, 1h")
	cmd.Flags().StringVar(&message, "message", defaultHeartbeatMessage, "agent message payload")
	cmd.Flags().StringVar(&sessionID, "session", defaultHeartbeatSessionID, "session ID used for heartbeat turns")
	cmd.Flags().StringVar(&tz, "tz", "", "timezone for cron expression")
	cmd.Flags().BoolVar(&runNow, "run-now", false, "trigger the heartbeat job immediately after ensuring it exists")
	return cmd
}

func newCronRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <id>",
		Short: "Remove a scheduled job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := cronService()
			if err != nil {
				return err
			}
			if !svc.RemoveJob(cmd.Context(), args[0]) {
				return fmt.Errorf("job %q not found", args[0])
			}
			fmt.Printf("✓ removed job %s\n", args[0])
			return nil
		},
	}
}

func newCronRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run <id>",
		Short: "Execute a job immediately",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			cfg, ws, _, err := loadAll()
			if err != nil {
				return err
			}
			runner, err := buildRunner(cfg, ws)
			if err != nil {
				return err
			}

			sessMgr := session.NewManager(ws.SessionsDir())
			trigger := makeTrigger(runner, sessMgr)

			svc := cron.NewService(ws.CronStorePath(), cron.NewAgentHandler(trigger, 60*time.Second))
			output, err := svc.RunNow(ctx, args[0])
			if strings.TrimSpace(output) != "" {
				fmt.Print(output)
				if !strings.HasSuffix(output, "\n") {
					fmt.Println()
				}
			}
			return err
		},
	}
}

// makeTrigger returns a TriggerFn that runs a single agent turn and returns the
// assembled reply text. Used by both the cron run command and the daemon.
func makeTrigger(runner *blades.Runner, sessMgr *session.Manager) cron.TriggerFn {
	return func(ctx context.Context, sessionID, text string) (string, error) {
		sess := sessMgr.GetOrNew(sessionID)
		msg := blades.UserMessage(text)
		var buf strings.Builder
		for m, err := range runner.RunStream(ctx, msg, blades.WithSession(sess)) {
			if err != nil {
				return buf.String(), err
			}
			if m != nil {
				buf.WriteString(m.Text())
			}
		}
		return buf.String(), nil
	}
}
