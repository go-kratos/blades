package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	appcore "github.com/go-kratos/blades/cmd/blades/internal/app"
	"github.com/spf13/cobra"
)

func resolveRunSessionID(sessionID string, now func() time.Time) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID != "" {
		return sessionID
	}
	if now == nil {
		now = time.Now
	}
	return fmt.Sprintf("run-%d", now().UnixNano())
}

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

			return runWithRuntimeCommand(cmd, func(ctx context.Context, cmd *cobra.Command, rt *appcore.Runtime) error {
				runSessionID := resolveRunSessionID(sessionID, time.Now)

				_, err := appcore.NewTurnExecutor(rt.Runner, rt.Sessions, appcore.TurnOptions{
					Writer:          textWriter{writeText: func(chunk string) { fmt.Fprint(cmd.OutOrStdout(), chunk) }},
					Memory:          rt.Memory,
					LogConversation: true,
				}).Run(ctx, runSessionID, message)
				if err != nil {
					return err
				}
				printCommandln(cmd)
				return nil
			})
		},
	}
	cmd.Flags().StringVarP(&message, "message", "m", "", "message to send to the agent")
	cmd.Flags().StringVar(&sessionID, "session", "", "session ID for conversation continuity")
	return cmd
}
