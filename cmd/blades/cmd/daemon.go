package cmd

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/go-kratos/blades/cmd/blades/internal/cron"
	"github.com/go-kratos/blades/cmd/blades/internal/session"
	bldtools "github.com/go-kratos/blades/cmd/blades/internal/tools"
)

func newDaemonCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "daemon",
		Short: "Run the cron scheduler as a long-lived process",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			cfg, ws, _, err := loadAll()
			if err != nil {
				return err
			}

			sessMgr := session.NewManager(ws.SessionsDir())

			svc := cron.NewService(ws.CronStorePath(), nil)
			cronTool := bldtools.NewCronTool(svc)

			runner, err := buildRunner(cfg, ws, cronTool)
			if err != nil {
				return err
			}

			svc.SetHandler(cron.NewAgentHandlerWithExecWorkDir(
				makeTrigger(runner, sessMgr),
				60*time.Second,
				defaultExecWorkingDir(ws),
			))

			if err := svc.Start(ctx); err != nil {
				return fmt.Errorf("cron: %w", err)
			}
			defer svc.Stop()

			fmt.Printf("blades daemon running (workspace: %s) — Ctrl-C to stop\n", cfg.Workspace)
			<-ctx.Done()
			fmt.Println("\nShutting down…")
			return nil
		},
	}
}
