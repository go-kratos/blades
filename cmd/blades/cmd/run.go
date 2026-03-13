package cmd

import (
	"context"
	"fmt"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	blades "github.com/go-kratos/blades"
	"github.com/go-kratos/blades/cmd/blades/internal/session"
)

func newRunCmd() *cobra.Command {
	var message string
	var sessionID string
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Execute a single agent turn and exit",
		Example: `  blades run --message "summarise today's notes"
  blades run -m "@distill"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if message == "" {
				return fmt.Errorf("--message is required")
			}

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			cfg, ws, mem, err := loadAll()
			if err != nil {
				return err
			}

			runner, err := buildRunner(cfg, ws)
			if err != nil {
				return err
			}

			sessMgr := session.NewManager(ws.SessionsDir())
			if sessionID == "" {
				sessionID = "run"
			}
			sess := sessMgr.GetOrNew(sessionID)

			msg := blades.UserMessage(message)
			var buf strings.Builder

			for m, err := range runner.RunStream(ctx, msg, blades.WithSession(sess)) {
				if err != nil {
					return err
				}
				if m == nil {
					continue
				}
				// StatusCompleted is the final assembled message — its text is the
				// union of all prior InProgress deltas. Skip printing it to avoid
				// double-output when the provider streams deltas first. If no deltas
				// were streamed (non-streaming provider), fall back to writing it once.
				if m.Status == blades.StatusCompleted {
					finalText := m.Text()
					if buf.Len() == 0 && finalText != "" {
						fmt.Print(finalText)
					}
					buf.Reset()
					buf.WriteString(finalText)
				} else if chunk := m.Text(); chunk != "" {
					fmt.Print(chunk)
					buf.WriteString(chunk)
				}
			}
			fmt.Println()

			_ = mem.AppendDailyLog("user", message)
			_ = mem.AppendDailyLog("assistant", buf.String())
			_ = sessMgr.Save(sess)
			return nil
		},
	}
	cmd.Flags().StringVarP(&message, "message", "m", "", "message to send to the agent")
	cmd.Flags().StringVar(&sessionID, "session", "", "session ID for conversation continuity")
	return cmd
}
