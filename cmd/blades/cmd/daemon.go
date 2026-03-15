package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
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
		appID := c.Lark.AppID
		if appID == "" {
			appID = os.Getenv("LARK_APP_ID")
		}
		if appID == "" {
			return nil, fmt.Errorf("lark.appID or LARK_APP_ID is required")
		}
		appSecret := c.Lark.AppSecret
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
		if c.Lark.EncryptKey != "" {
			opts = append(opts, lark.WithEncryptKey(c.Lark.EncryptKey))
		}
		if c.Lark.VerificationToken != "" {
			opts = append(opts, lark.WithVerificationToken(c.Lark.VerificationToken))
		}
		if c.Lark.Debug {
			opts = append(opts, lark.WithDebug(true))
		}
		return lark.New(opts...), nil
	})
}

// buildLarkChannel builds a Lark channel from config and optional reload (used by daemon so /reload works).
func buildLarkChannel(cfg *config.Config, reload func() error) (channel.Channel, error) {
	appID := cfg.Lark.AppID
	if appID == "" {
		appID = os.Getenv("LARK_APP_ID")
	}
	if appID == "" {
		return nil, fmt.Errorf("lark.appID or LARK_APP_ID is required")
	}
	appSecret := cfg.Lark.AppSecret
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
	if cfg.Lark.EncryptKey != "" {
		opts = append(opts, lark.WithEncryptKey(cfg.Lark.EncryptKey))
	}
	if cfg.Lark.VerificationToken != "" {
		opts = append(opts, lark.WithVerificationToken(cfg.Lark.VerificationToken))
	}
	if cfg.Lark.Debug {
		opts = append(opts, lark.WithDebug(true))
	}
	if reload != nil {
		opts = append(opts, lark.WithReload(reload))
	}
	return lark.New(opts...), nil
}

func newDaemonCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "daemon",
		Short: "Run as a long-lived process (cron + lark channel)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			cfg, ws, mem, mcpServers, err := loadAll()
			if err != nil {
				return err
			}

			// Setup logging based on foreground flag
			var rtLog *logger.Runtime
			if !daemonForeground {
				// Background mode: all logs go to ~/.blades/log/
				rtLog = logger.NewRuntime(ws.Home())
				// Redirect log package output to file
				logFile, err := os.OpenFile(
					filepath.Join(ws.Home(), "log", time.Now().Format("2006-01-02")+".log"),
					os.O_APPEND|os.O_CREATE|os.O_WRONLY,
					0o644,
				)
				if err == nil {
					log.SetOutput(logFile)
					defer logFile.Close()
				}
			} else {
				// Foreground mode: logs go to stderr (default)
				log.SetOutput(os.Stderr)
				rtLog = logger.NewRuntime(ws.Home())
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

			var notify cron.NotifyFn
			var larkCh channel.Channel
			if cfg.Lark.Enabled {
				var lerr error
				larkCh, lerr = buildLarkChannel(cfg, reload)
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

			baseHandler := createStreamHandlerWithGetter(getRunner, sessMgr, mem, rtLog, cfg.Defaults.LogConversation, !daemonForeground)
			// Lark handles /help, /reload, /stop via its own command.Processor; pass baseHandler only.
			var channelDone <-chan struct{}
			if cfg.Lark.Enabled && larkCh != nil {
				done := make(chan struct{})
				channelDone = done
				go func() {
					defer close(done)
					if err := larkCh.Start(ctx, baseHandler); err != nil && ctx.Err() == nil {
						if daemonForeground {
							fmt.Printf("lark channel error: %v\n", err)
						} else {
							log.Printf("lark channel error: %v", err)
						}
					}
				}()
			}

			if daemonForeground {
				fmt.Printf("blades daemon running (workspace: %s) — Ctrl-C to stop\n", ws.WorkspaceDir())
				if cfg.Lark.Enabled {
					fmt.Println("lark channel enabled (websocket mode)")
				}
			} else {
				log.Printf("blades daemon running (workspace: %s)", ws.WorkspaceDir())
				if cfg.Lark.Enabled {
					log.Println("lark channel enabled (websocket mode)")
				}
			}

			<-ctx.Done()
			// Restore default signal behavior so a second Ctrl-C can terminate immediately.
			cancel()

			if daemonForeground {
				fmt.Println("\nShutting down…")
			} else {
				log.Println("Shutting down…")
			}

			if !waitForDone(channelDone, 5*time.Second) {
				if daemonForeground {
					fmt.Println("channel shutdown timed out; forcing exit")
				} else {
					log.Println("channel shutdown timed out; forcing exit")
				}
			}
			return nil
		},
	}
	c.Flags().BoolVar(&daemonForeground, "foreground", false, "if true, block terminal and print all logs to stdout/stderr; if false, run in background with logs to ~/.blades/log/")
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

// wrapChannelCommands wraps a StreamHandler to handle commands before calling the delegate.
// This is deprecated in favor of the unified command.Processor in each channel.
// Kept for backward compatibility with daemon mode.
func wrapChannelCommands(delegate channel.StreamHandler, reload func() error) channel.StreamHandler {
	helpText := "**Commands**\n- `/help` — show this help\n- `/reload` — hot-reload skills and MCP\n- `/stop` — stop current reply"
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
// If writeAuditLog is false, audit logs are not written to ~/.blades/log/ (foreground mode).
func createStreamHandlerWithGetter(getRunner func() *blades.Runner, sessMgr *session.Manager, mem *memory.Store, rtLog *logger.Runtime, logConversation bool, writeAuditLog bool) channel.StreamHandler {
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

		if writeAuditLog && rtLog != nil {
			rtLog.WriteConversation(sid, "user", text)
		}

		for m, err := range r.RunStream(ctx, msg, blades.WithSession(sess)) {
			if err != nil {
				if writeAuditLog && rtLog != nil {
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

		if writeAuditLog && rtLog != nil {
			rtLog.WriteConversation(sid, "assistant", buf.String())
		}
		if logConversation {
			if err := mem.AppendDailyLog("user", text); err != nil {
				log.Printf("daemon: append daily log (user) failed: %v", err)
			}
			if err := mem.AppendDailyLog("assistant", buf.String()); err != nil {
				log.Printf("daemon: append daily log (assistant) failed: %v", err)
			}
		}
		if err := sessMgr.Save(sess); err != nil {
			log.Printf("daemon: save session failed: %v", err)
		}
		return buf.String(), nil
	}
}
