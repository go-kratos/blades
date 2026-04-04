package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/go-kratos/blades/cmd/blades/internal/cron"
)

func findHeartbeatJob(svc *cron.Service, name, message, sessionID string) *cron.Job {
	trimmedName := strings.TrimSpace(name)
	trimmedMessage := strings.TrimSpace(message)
	trimmedSessionID := strings.TrimSpace(sessionID)
	job, err := findMatchingCronJob(svc, func(job *cron.Job) bool {
		if strings.TrimSpace(job.Name) != trimmedName {
			return false
		}
		if job.Payload.Kind != cron.PayloadAgentTurn {
			return false
		}
		if strings.TrimSpace(job.Payload.Message) != trimmedMessage {
			return false
		}
		return strings.TrimSpace(job.Payload.SessionID) == trimmedSessionID
	})
	if err != nil {
		return nil
	}
	return job
}

func ensureHeartbeatJob(ctx context.Context, svc *cron.Service, schedule cron.Schedule, name, message, sessionID string) (*cron.Job, bool, error) {
	payload := cron.Payload{
		Kind:      cron.PayloadAgentTurn,
		Message:   message,
		SessionID: sessionID,
	}
	return upsertCronJob(ctx, svc, func(job *cron.Job) bool {
		return strings.TrimSpace(job.Name) == strings.TrimSpace(name) &&
			job.Payload.Kind == cron.PayloadAgentTurn &&
			strings.TrimSpace(job.Payload.Message) == strings.TrimSpace(message) &&
			strings.TrimSpace(job.Payload.SessionID) == strings.TrimSpace(sessionID)
	}, cronJobSpec{
		Name:           name,
		Schedule:       schedule,
		Payload:        payload,
		DeleteAfterRun: false,
	})
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
	cmd := newCronServiceCmd("heartbeat", "Ensure a heartbeat job exists", cobra.NoArgs, func(cmd *cobra.Command, svc *cron.Service, args []string) error {
		schedule, err := parseScheduleFlags(cronExpr, everyStr, "", tz, scheduleFlagOptions{
			DefaultEvery: defaultHeartbeatEvery,
		})
		if err != nil {
			return err
		}

		job, existed, err := ensureHeartbeatJob(cmd.Context(), svc, schedule, name, message, sessionID)
		if err != nil {
			return err
		}
		if existed {
			fmt.Fprintf(cmd.OutOrStdout(), "✓ heartbeat job already exists: %s\n", cron.FormatJob(job))
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "✓ heartbeat job ready: %s\n", cron.FormatJob(job))
		}

		if runNow {
			return runWithSignalContext(cmd.Context(), func(ctx context.Context) error {
				output, err := runScheduledJobNowForOptions(ctx, commandOptions(cmd), job.ID)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "✓ heartbeat triggered: %s\n", job.ID)
				writeCommandOutput(cmd.OutOrStdout(), output)
				return nil
			})
		}
		return nil
	})
	cmd.Example = `  blades cron heartbeat
  blades cron heartbeat --every 15m
  blades cron heartbeat --run-now`
	cmd.Flags().StringVar(&name, "name", defaultHeartbeatJobName, "heartbeat job name")
	cmd.Flags().StringVar(&cronExpr, "cron", "", "cron expression (5-field)")
	cmd.Flags().StringVar(&everyStr, "every", "", "repeat interval, e.g. 30m, 1h (default 30m)")
	cmd.Flags().StringVar(&message, "message", defaultHeartbeatMessage, "agent message payload")
	cmd.Flags().StringVar(&sessionID, "session", defaultHeartbeatSessionID, "session ID used for heartbeat turns")
	cmd.Flags().StringVar(&tz, "tz", "", "timezone for cron expression")
	cmd.Flags().BoolVar(&runNow, "run-now", false, "trigger the heartbeat job immediately after ensuring it exists")
	return cmd
}
