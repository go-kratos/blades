package cmd

import (
	"context"

	appcore "github.com/go-kratos/blades/cmd/blades/internal/app"
	"github.com/spf13/cobra"
)

type runtimeCommandFunc func(context.Context, *cobra.Command, *appcore.Runtime) error

func runWithRuntimeCommand(cmd *cobra.Command, fn runtimeCommandFunc) error {
	parent := context.Background()
	if cmd != nil && cmd.Context() != nil {
		parent = cmd.Context()
	}
	return runWithSignalContext(parent, func(ctx context.Context) error {
		rt, err := loadRuntimeForCommand(cmd)
		if err != nil {
			return err
		}
		return fn(ctx, cmd, rt)
	})
}
