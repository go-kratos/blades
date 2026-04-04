package cmd

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	appcore "github.com/go-kratos/blades/cmd/blades/internal/app"
	clichi "github.com/go-kratos/blades/cmd/blades/internal/channel/cli"
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
			return runWithRuntimeCommand(cmd, func(ctx context.Context, cmd *cobra.Command, rt *appcore.Runtime) error {
				appcore.ConfigureRuntimeCron(rt, nil)

				if sessionID == "" {
					sessionID = fmt.Sprintf("chat-%s", uuid.NewString()[:8])
				}

				handler := appcore.NewTurnExecutor(rt.Runner, rt.Sessions, appcore.TurnOptions{
					Memory:          rt.Memory,
					LogConversation: true,
				}).StreamHandler()

				opts := []clichi.Option{
					clichi.WithDebug(commandOptions(cmd).Debug),
					clichi.WithSwitchSession(func(string) error { return nil }),
					clichi.WithClearSession(func(sessionID string) error {
						return rt.Sessions.Delete(sessionID)
					}),
				}
				if simpleMode {
					opts = append(opts, clichi.WithNoAltScreen())
				}
				ch := clichi.New(sessionID, opts...)
				return ch.Start(ctx, handler)
			})
		},
	}
	cmd.Flags().StringVar(&sessionID, "session", "", "session ID (default: auto-generated)")
	cmd.Flags().BoolVar(&simpleMode, "simple", false, "plain line-based I/O (fixes Windows IME; output is selectable with the mouse)")
	return cmd
}
