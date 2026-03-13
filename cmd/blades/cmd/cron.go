package cmd

import (
	"fmt"
	"time"

	robfigcron "github.com/robfig/cron/v3"
	"github.com/spf13/cobra"

	"github.com/go-kratos/blades/cmd/blades/internal/cron"
	"github.com/go-kratos/blades/cmd/blades/internal/workspace"
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
// (list, add, remove). It intentionally uses loadConfigForFlags rather than
// loadAll because a full workspace load is not needed for these commands.
func cronService() (*cron.Service, error) {
	cfg, err := loadConfigForFlags()
	if err != nil {
		return nil, err
	}
	return cron.NewService(workspace.New(cfg.Workspace).CronStorePath(), nil), nil
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
				parser := robfigcron.NewParser(robfigcron.Minute | robfigcron.Hour | robfigcron.Dom | robfigcron.Month | robfigcron.Dow)
				if _, err := parser.Parse(cronExpr); err != nil {
					return fmt.Errorf("invalid --cron expression: %w", err)
				}
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
