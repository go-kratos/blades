package cmd

import (
	"context"
	"fmt"
	"os/signal"
	"strings"
	"syscall"
	"time"

	robfigcron "github.com/robfig/cron/v3"
	"github.com/spf13/cobra"

	"github.com/go-kratos/blades/cmd/blades/internal/cron"
)

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
				ctx, cancel := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
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
	cmd.Flags().StringVar(&everyStr, "every", "", "repeat interval, e.g. 30m, 1h (default 30m)")
	cmd.Flags().StringVar(&message, "message", defaultHeartbeatMessage, "agent message payload")
	cmd.Flags().StringVar(&sessionID, "session", defaultHeartbeatSessionID, "session ID used for heartbeat turns")
	cmd.Flags().StringVar(&tz, "tz", "", "timezone for cron expression")
	cmd.Flags().BoolVar(&runNow, "run-now", false, "trigger the heartbeat job immediately after ensuring it exists")
	return cmd
}
