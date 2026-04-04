package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/go-kratos/blades"
	appcore "github.com/go-kratos/blades/cmd/blades/internal/app"
	"github.com/go-kratos/blades/cmd/blades/internal/channel"
	"github.com/go-kratos/blades/cmd/blades/internal/channel/lark"
	weixinch "github.com/go-kratos/blades/cmd/blades/internal/channel/weixin"
	"github.com/go-kratos/blades/cmd/blades/internal/cron"
	"github.com/go-kratos/blades/cmd/blades/internal/logger"
	"github.com/go-kratos/blades/cmd/blades/internal/memory"
	"github.com/go-kratos/blades/cmd/blades/internal/session"
)

var (
	daemonForeground bool
)

func newDaemonCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "daemon",
		Short: "Run as a long-lived process (cron + lark channel)",
		RunE: func(cmd *cobra.Command, args []string) error {
			// When foreground, all logs go to terminal; otherwise to ~/.blades/log/
			if daemonForeground {
				log.SetOutput(os.Stderr)
			}

			return runWithRuntimeCommand(cmd, func(ctx context.Context, cmd *cobra.Command, rt *appcore.Runtime) error {
				var notify cron.NotifyFn
				var channels []channel.Channel
				if rt.Config.Channels.Lark.Enabled {
					var err error
					larkCh, err := lark.NewFromConfig(rt.Config.Channels.Lark, func(sessionID string) error {
						return rt.Sessions.Delete(sessionID)
					}, lark.WithOutput(commandOut(cmd)), lark.WithLogf(log.Printf))
					if err != nil {
						return err
					}
					channels = append(channels, larkCh)
					notify = larkCh.SendToSession
				}
				if rt.Config.Channels.Weixin.Enabled {
					weixinCh, err := weixinch.NewFromConfig(rt.Config.Channels.Weixin, func(sessionID string) error {
						return rt.Sessions.Delete(sessionID)
					}, weixinch.WithOutput(commandOut(cmd)), weixinch.WithLogf(log.Printf))
					if err != nil {
						return err
					}
					channels = append(channels, weixinCh)
					if notify == nil {
						notify = weixinCh.SendToSession
					}
				}
				appcore.ConfigureRuntimeCron(rt, notify)

				if err := rt.Cron.Start(ctx); err != nil {
					return fmt.Errorf("cron: %w", err)
				}
				defer rt.Cron.Stop()

				rtLog := logger.NewRuntime(rt.Workspace.Home())
				streamHandler := createStreamHandler(rt.Runner, rt.Sessions, rt.Memory, rtLog, true)

				var channelDone <-chan struct{}
				if len(channels) > 0 {
					done := make(chan struct{})
					channelDone = done
					go func() {
						defer close(done)
						errCh := make(chan error, len(channels))
						for _, ch := range channels {
							go func(ch channel.Channel) {
								if err := ch.Start(ctx, streamHandler); err != nil && ctx.Err() == nil {
									errCh <- fmt.Errorf("%s channel error: %w", ch.Name(), err)
								}
							}(ch)
						}
						select {
						case err := <-errCh:
							warnCommandf(cmd, "%v\n", err)
						case <-ctx.Done():
						}
					}()
				}

				printCommandf(cmd, "blades daemon running (workspace: %s) — Ctrl-C to stop\n", rt.Workspace.WorkspaceDir())
				if rt.Config.Channels.Lark.Enabled {
					printCommandln(cmd, "lark channel enabled (websocket mode)")
				}
				if rt.Config.Channels.Weixin.Enabled {
					printCommandln(cmd, "weixin channel enabled (long polling mode)")
				}
				<-ctx.Done()
				printCommandln(cmd, "\nShutting down…")
				if !waitForDone(channelDone, 5*time.Second) {
					printCommandln(cmd, "channel shutdown timed out; forcing exit")
				}
				return nil
			})
		},
	}
	c.Flags().BoolVar(&daemonForeground, "foreground", true, "if true, block terminal and print all logs to terminal; if false, logs go to ~/.blades/log/")
	return c
}

func waitForDone(done <-chan struct{}, timeout time.Duration) bool {
	if done == nil {
		return true
	}
	if timeout <= 0 {
		<-done
		return true
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}

// createStreamHandler creates a StreamHandler for a fixed runner.
func createStreamHandler(runner *blades.Runner, sessMgr *session.Manager, mem *memory.Store, rtLog *logger.Runtime, logConversation bool) channel.StreamHandler {
	return appcore.NewTurnExecutor(runner, sessMgr, appcore.TurnOptions{
		Memory:          mem,
		LogConversation: logConversation,
		RuntimeLog:      rtLog,
	}).StreamHandler()
}
