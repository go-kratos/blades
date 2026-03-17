package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/cmd/blades/internal/channel"
	"github.com/go-kratos/blades/cmd/blades/internal/channel/lark"
	"github.com/go-kratos/blades/cmd/blades/internal/config"
	"github.com/go-kratos/blades/cmd/blades/internal/cron"
	"github.com/go-kratos/blades/cmd/blades/internal/logger"
	"github.com/go-kratos/blades/cmd/blades/internal/memory"
	"github.com/go-kratos/blades/cmd/blades/internal/session"
	bldtools "github.com/go-kratos/blades/cmd/blades/internal/tools"
)

var (
	daemonChannels   = channel.NewRegistry()
	daemonForeground bool
)

func init() {
	daemonChannels.Register("lark", func(cfg interface{}) (channel.Channel, error) {
		c := cfg.(*config.Config)
		appID := c.Channels.Lark.AppID
		if appID == "" {
			appID = os.Getenv("LARK_APP_ID")
		}
		if appID == "" {
			return nil, fmt.Errorf("lark.appID or LARK_APP_ID is required")
		}
		appSecret := c.Channels.Lark.AppSecret
		if appSecret == "" {
			appSecret = os.Getenv("LARK_APP_SECRET")
		}
		if appSecret == "" {
			return nil, fmt.Errorf("lark.appSecret or LARK_APP_SECRET is required")
		}
		opts := []lark.Option{
			lark.WithAppID(appID),
			lark.WithAppSecret(appSecret),
		}
		if c.Channels.Lark.EncryptKey != "" {
			opts = append(opts, lark.WithEncryptKey(c.Channels.Lark.EncryptKey))
		}
		if c.Channels.Lark.VerificationToken != "" {
			opts = append(opts, lark.WithVerificationToken(c.Channels.Lark.VerificationToken))
		}
		if c.Channels.Lark.Debug {
			opts = append(opts, lark.WithDebug(true))
		}
		return lark.New(opts...), nil
	})
}

func newDaemonCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "daemon",
		Short: "Run as a long-lived process (cron + lark channel)",
		RunE: func(cmd *cobra.Command, args []string) error {
			// When foreground, all logs go to terminal; otherwise to ~/.blades/log/
			if daemonForeground {
				log.SetOutput(os.Stderr)
			}

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			cfg, ws, mem, mcpServers, err := loadAll()
			if err != nil {
				return err
			}

			sessMgr := session.NewManager(ws.SessionsDir())

			svc := cron.NewService(ws.CronStorePath(), nil)
			cronTool := bldtools.NewCronTool(svc)

			var mu sync.RWMutex
			currentRunner, err := buildRunner(cfg, ws, mcpServers, cronTool)
			if err != nil {
				return err
			}
			getRunner := func() *blades.Runner {
				mu.RLock()
				defer mu.RUnlock()
				return currentRunner
			}

			var notify cron.NotifyFn
			var larkCh channel.Channel
			if cfg.Channels.Lark.Enabled {
				var lerr error
				larkCh, lerr = daemonChannels.Build("lark", cfg)
				if lerr != nil {
					return fmt.Errorf("lark channel: %w", lerr)
				}
				if sn, ok := larkCh.(channel.SessionNotifier); ok {
					notify = sn.SendToSession
				}
			}
			svc.SetHandler(cron.NewBotHandlerWithExecWorkDir(
				makeTriggerWithGetter(getRunner, sessMgr),
				notify,
				60*time.Second,
				defaultExecWorkingDir(ws),
			))

			if err := svc.Start(ctx); err != nil {
				return fmt.Errorf("cron: %w", err)
			}
			defer svc.Stop()

			reload := func() error {
				r, err := buildRunner(cfg, ws, mcpServers, cronTool)
				if err != nil {
					return err
				}
				mu.Lock()
				currentRunner = r
				mu.Unlock()
				return nil
			}

			rtLog := logger.NewRuntime(ws.Home())
			baseHandler := createStreamHandlerWithGetter(getRunner, sessMgr, mem, rtLog, true)
			streamHandler := wrapChannelCommands(baseHandler, reload)

			var channelDone <-chan struct{}
			if cfg.Channels.Lark.Enabled && larkCh != nil {
				done := make(chan struct{})
				channelDone = done
				go func() {
					defer close(done)
					if err := larkCh.Start(ctx, streamHandler); err != nil && ctx.Err() == nil {
						fmt.Printf("lark channel error: %v\n", err)
					}
				}()
			}

			fmt.Printf("blades daemon running (workspace: %s) — Ctrl-C to stop\n", ws.WorkspaceDir())
			if cfg.Channels.Lark.Enabled {
				fmt.Println("lark channel enabled (websocket mode)")
			}
			<-ctx.Done()
			// Restore default signal behavior so a second Ctrl-C can terminate immediately.
			cancel()
			fmt.Println("\nShutting down…")
			if !waitForDone(channelDone, 5*time.Second) {
				fmt.Println("channel shutdown timed out; forcing exit")
			}
			return nil
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

// wrapChannelCommands wraps a StreamHandler to handle /help, /reload (and /stop) before calling the delegate.
func wrapChannelCommands(delegate channel.StreamHandler, reload func() error) channel.StreamHandler {
	helpText := "**Commands**\n- `/help` — show this help\n- `/reload` — hot-reload skills and MCP\n- `/stop` — (reserved) stop current reply"
	return func(ctx context.Context, sid, text string, w channel.Writer) (string, error) {
		cmd := strings.TrimSpace(strings.ToLower(text))
		switch cmd {
		case "/help":
			w.WriteText(helpText)
			return helpText, nil
		case "/reload":
			if reload == nil {
				w.WriteText("Reload not available.")
				return "Reload not available.", nil
			}
			if err := reload(); err != nil {
				msg := "Reload failed: " + err.Error()
				w.WriteText(msg)
				return msg, nil
			}
			w.WriteText("✓ Skills and MCP reloaded.")
			return "✓ Skills and MCP reloaded.", nil
		case "/stop":
			w.WriteText("Stop is not yet supported in this channel.")
			return "Stop is not yet supported in this channel.", nil
		default:
			return delegate(ctx, sid, text, w)
		}
	}
}

// createStreamHandlerWithGetter creates a StreamHandler that uses getRunner to get the current runner (for reload).
func createStreamHandlerWithGetter(getRunner func() *blades.Runner, sessMgr *session.Manager, mem *memory.Store, rtLog *logger.Runtime, logConversation bool) channel.StreamHandler {
	return func(ctx context.Context, sid, text string, w channel.Writer) (string, error) {
		r := getRunner()
		if r == nil {
			return "", fmt.Errorf("no runner")
		}

		sess := sessMgr.GetOrNew(sid)
		msg := blades.UserMessage(text)
		var buf strings.Builder
		toolEventKey := func(tp blades.ToolPart) string {
			if strings.TrimSpace(tp.ID) != "" {
				return tp.ID
			}
			return tp.Name + "\n" + tp.Request
		}
		startedTools := make(map[string]bool)
		endedTools := make(map[string]bool)

		if rtLog != nil {
			rtLog.WriteConversation(sid, "user", text)
		}

		for m, err := range r.RunStream(ctx, msg, blades.WithSession(sess)) {
			if err != nil {
				if rtLog != nil {
					rtLog.WriteConversation(sid, "assistant_error", err.Error())
				}
				return buf.String(), err
			}
			if m == nil {
				continue
			}
			if m.Status == blades.StatusCompleted {
				finalText := m.Text()
				if buf.Len() == 0 && finalText != "" {
					w.WriteText(finalText)
				}
				buf.Reset()
				buf.WriteString(finalText)
			} else if chunk := m.Text(); chunk != "" {
				w.WriteText(chunk)
				buf.WriteString(chunk)
			}
			for _, part := range m.Parts {
				tp, ok := part.(blades.ToolPart)
				if !ok {
					continue
				}
				key := toolEventKey(tp)
				if !tp.Completed {
					if !startedTools[key] {
						startedTools[key] = true
						w.WriteEvent(channel.Event{
							Kind:  channel.EventToolStart,
							ID:    key,
							Name:  tp.Name,
							Input: tp.Request,
						})
					}
				} else if !endedTools[key] {
					if !startedTools[key] {
						startedTools[key] = true
						w.WriteEvent(channel.Event{
							Kind:  channel.EventToolStart,
							ID:    key,
							Name:  tp.Name,
							Input: tp.Request,
						})
					}
					endedTools[key] = true
					w.WriteEvent(channel.Event{
						Kind:   channel.EventToolEnd,
						ID:     key,
						Name:   tp.Name,
						Input:  tp.Request,
						Output: tp.Response,
					})
				}
			}
		}

		if rtLog != nil {
			rtLog.WriteConversation(sid, "assistant", buf.String())
		}
		if logConversation {
			_ = mem.AppendDailyLog("user", text)
			_ = mem.AppendDailyLog("assistant", buf.String())
		}
		_ = sessMgr.Save(sess)
		return buf.String(), nil
	}
}
