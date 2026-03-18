package cmd

import (
	"context"

	appcore "github.com/go-kratos/blades/cmd/blades/internal/app"
	"github.com/spf13/cobra"
)

// runScheduledJobNow loads a full runner and triggers job id immediately,
// returning the assembled output. Used by "cron run" and "cron heartbeat --run-now".
func runScheduledJobNow(ctx context.Context, id string) (string, error) {
	rt, err := loadRuntimeForOptions(appcore.Options{})
	if err != nil {
		return "", err
	}
	appcore.ConfigureRuntimeCron(rt, nil)
	return rt.Cron.RunNow(ctx, id)
}

func runScheduledJobNowForOptions(ctx context.Context, opts appcore.Options, id string) (string, error) {
	rt, err := loadRuntimeForOptions(opts)
	if err != nil {
		return "", err
	}
	appcore.ConfigureRuntimeCron(rt, nil)
	return rt.Cron.RunNow(ctx, id)
}

func newCronRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run <id>",
		Short: "Execute a job immediately",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWithSignalContext(context.Background(), func(ctx context.Context) error {
				output, err := runScheduledJobNowForOptions(ctx, commandOptions(cmd), args[0])
				writeCommandOutput(cmd.OutOrStdout(), output)
				return err
			})
		},
	}
}
