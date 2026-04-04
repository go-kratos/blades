package cmd

import (
	"context"

	appcore "github.com/go-kratos/blades/cmd/blades/internal/app"
	"github.com/spf13/cobra"
)

type commandOptionsKey struct{}

func commandOptions(cmd *cobra.Command) appcore.Options {
	if cmd != nil {
		if ctx := cmd.Context(); ctx != nil {
			if opts, ok := ctx.Value(commandOptionsKey{}).(appcore.Options); ok {
				return opts
			}
		}

		var opts appcore.Options
		if flag := cmd.Flags().Lookup("config"); flag != nil {
			opts.ConfigPath, _ = cmd.Flags().GetString("config")
		}
		if flag := cmd.Flags().Lookup("workspace"); flag != nil {
			opts.WorkspaceDir, _ = cmd.Flags().GetString("workspace")
		}
		if flag := cmd.Flags().Lookup("debug"); flag != nil {
			opts.Debug, _ = cmd.Flags().GetBool("debug")
		}
		return opts
	}
	return appcore.Options{}
}

func withCommandOptions(cmd *cobra.Command, opts appcore.Options) {
	base := context.Background()
	if cmd != nil && cmd.Context() != nil {
		base = cmd.Context()
	}
	cmd.SetContext(context.WithValue(base, commandOptionsKey{}, opts))
}
