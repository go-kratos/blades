package cmd

import (
	appcore "github.com/go-kratos/blades/cmd/blades/internal/app"
	"github.com/go-kratos/blades/cmd/blades/internal/config"
	"github.com/go-kratos/blades/cmd/blades/internal/cron"
	"github.com/go-kratos/blades/cmd/blades/internal/memory"
	"github.com/go-kratos/blades/cmd/blades/internal/workspace"
	"github.com/spf13/cobra"
)

func bootstrapFromOptions(opts appcore.Options) appcore.Bootstrap {
	return appcore.NewBootstrap(opts)
}

func bootstrapFromCommand(cmd *cobra.Command) appcore.Bootstrap {
	return bootstrapFromOptions(commandOptions(cmd))
}

func loadConfigForOptions(opts appcore.Options) (*config.Config, error) {
	return bootstrapFromOptions(opts).LoadConfig()
}

func loadConfigForCommand(cmd *cobra.Command) (*config.Config, error) {
	return loadConfigForOptions(commandOptions(cmd))
}

func loadAllForOptions(opts appcore.Options) (*config.Config, *workspace.Workspace, *memory.Store, error) {
	return bootstrapFromOptions(opts).LoadAll()
}

func loadAllForCommand(cmd *cobra.Command) (*config.Config, *workspace.Workspace, *memory.Store, error) {
	return loadAllForOptions(commandOptions(cmd))
}

func loadRuntimeForOptions(opts appcore.Options) (*appcore.Runtime, error) {
	return bootstrapFromOptions(opts).LoadRuntime()
}

func loadRuntimeForCommand(cmd *cobra.Command) (*appcore.Runtime, error) {
	return loadRuntimeForOptions(commandOptions(cmd))
}

func cronServiceForOptions(opts appcore.Options) (*cron.Service, error) {
	return bootstrapFromOptions(opts).CronService()
}

func cronServiceForCommand(cmd *cobra.Command) (*cron.Service, error) {
	return cronServiceForOptions(commandOptions(cmd))
}

func initPathsForOptions(opts appcore.Options) (homeDir, workspaceDir string, isCustomWorkspace bool) {
	return bootstrapFromOptions(opts).InitPaths()
}
