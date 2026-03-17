package cmd

import (
	"context"
	"fmt"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/cmd/blades/internal/channel"
	clichi "github.com/go-kratos/blades/cmd/blades/internal/channel/cli"
	"github.com/go-kratos/blades/cmd/blades/internal/cron"
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

			// handler is called once per user message. It streams text tokens and
			// tool lifecycle events to w so the channel can render them.
			handler := func(ctx context.Context, sid, text string, w channel.Writer) (string, error) {
				mu.RLock()
				runner := currentRunner
				mu.RUnlock()

				sess := sessMgr.GetOrNew(sid)
				msg := blades.UserMessage(text)
				var buf strings.Builder
				toolEventKey := func(tp blades.ToolPart) string {
					if strings.TrimSpace(tp.ID) != "" {
						return tp.ID
					}
					return tp.Name + "\n" + tp.Request
				}
				// Track which tool call IDs have already been announced so that
				// partial-argument streaming tokens don't open a new box every tick.
				startedTools := make(map[string]bool)
				endedTools := make(map[string]bool)

				for m, err := range runner.RunStream(ctx, msg, blades.WithSession(sess)) {
					if err != nil {
						return buf.String(), err
					}
					if m == nil {
						continue
					}
					// StatusCompleted is the final assembled message — its text is the
					// union of all prior InProgress deltas. Don't re-stream it to the
					// writer (that would show every token twice). Use it only as the
					// canonical return value. If no deltas were streamed at all (e.g.
					// non-streaming provider), fall back to writing the full text once.
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
					// Emit tool call events — one Start and one End per unique tp.ID.
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
							// Some providers only surface completed tool parts. Emit a
							// synthetic start so the CLI can render the tool lifecycle.
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

				// Persist turn to daily log and session.
				_ = mem.AppendDailyLog("user", text)
				_ = mem.AppendDailyLog("assistant", buf.String())
				_ = sessMgr.Save(sess)
				return buf.String(), nil
			}

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
