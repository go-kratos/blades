package cmd

import (
	"context"
	"fmt"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/go-kratos/blades"
	clichi "github.com/go-kratos/blades/cmd/blades/internal/channel/cli"
	"github.com/go-kratos/blades/cmd/blades/internal/cron"
	"github.com/go-kratos/blades/cmd/blades/internal/logger"
	"github.com/go-kratos/blades/cmd/blades/internal/session"
	bldtools "github.com/go-kratos/blades/cmd/blades/internal/tools"
)

func newChatCmd() *cobra.Command {
	var sessionID string
	var simpleMode bool
	cmd := &cobra.Command{
		Use:   "chat",
		Short: "Start an interactive conversation",
		Example: `  blades chat
  blades chat --session my-project`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			cfg, ws, mem, mcpServers, err := loadAll()
			if err != nil {
				return err
			}

			sessMgr := session.NewManager(ws.SessionsDir())

			// Cron tool is available so the agent can add/list/remove/run jobs;
			// the cron ticker is not started in chat — only daemon runs scheduled jobs.
			cronSvc := cron.NewService(ws.CronStorePath(), nil)
			cronTool := bldtools.NewCronTool(cronSvc)

			var mu sync.RWMutex
			currentRunner, err := buildRunner(cfg, ws, mcpServers, cronTool)
			if err != nil {
				return err
			}

			// Handler is set so "cron run <id>" from the tool works immediately.
			cronSvc.SetHandler(cron.NewAgentHandlerWithExecWorkDir(
				makeTrigger(currentRunner, sessMgr),
				60*time.Second,
				defaultExecWorkingDir(ws),
			))

			if sessionID == "" {
				sessionID = fmt.Sprintf("chat-%d", time.Now().Unix())
			}

			getRunner := func() *blades.Runner {
				mu.RLock()
				defer mu.RUnlock()
				return currentRunner
			}
			rtLog := logger.NewRuntime(ws.Home())
			handler := createStreamHandlerWithGetter(getRunner, sessMgr, mem, rtLog, cfg.Defaults.LogConversation, false)

			// reload rebuilds the runner so skill changes are picked up live.
			reload := func() error {
				r, err := buildRunner(cfg, ws, mcpServers, cronTool)
				if err != nil {
					return err
				}
				mu.Lock()
				currentRunner = r
				mu.Unlock()
				cronSvc.SetHandler(cron.NewAgentHandlerWithExecWorkDir(
					makeTrigger(r, sessMgr),
					60*time.Second,
					defaultExecWorkingDir(ws),
				))
				return nil
			}

			opts := []clichi.Option{
				clichi.WithReload(reload),
				clichi.WithDebug(flagDebug),
			}
			if simpleMode {
				opts = append(opts, clichi.WithNoAltScreen())
			}
			ch := clichi.New(sessionID, opts...)
			return ch.Start(ctx, handler)
		},
	}
	cmd.Flags().StringVar(&sessionID, "session", "", "session ID (default: auto-generated)")
	cmd.Flags().BoolVar(&simpleMode, "simple", false, "plain line-based I/O (fixes Windows IME; output is selectable with the mouse)")
	return cmd
}
