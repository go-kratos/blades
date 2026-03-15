package cmd

import (
	"context"
	"fmt"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/go-kratos/blades/cmd/blades/internal/cron"
	"github.com/go-kratos/blades/cmd/blades/internal/session"
)

// runScheduledJobNow loads a full runner and triggers job id immediately,
// returning the assembled output. Used by "cron run" and "cron heartbeat --run-now".
func runScheduledJobNow(ctx context.Context, id string) (string, error) {
	cfg, ws, _, mcpServers, err := loadAll()
	if err != nil {
		return "", err
	}
	runner, err := buildRunner(cfg, ws, mcpServers)
	if err != nil {
		return "", err
	}
	sessMgr := session.NewManager(ws.SessionsDir())
	trigger := makeTrigger(runner, sessMgr)
	svc := cron.NewService(
		ws.CronStorePath(),
		cron.NewAgentHandlerWithExecWorkDir(trigger, 60*time.Second, defaultExecWorkingDir(ws)),
	)
	return svc.RunNow(ctx, id)
}

func newCronRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run <id>",
		Short: "Execute a job immediately",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			output, err := runScheduledJobNow(ctx, args[0])
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
