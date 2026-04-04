package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-kratos/blades/cmd/blades/internal/config"
	"github.com/go-kratos/blades/cmd/blades/internal/cron"
	"github.com/go-kratos/blades/cmd/blades/internal/memory"
	"github.com/go-kratos/blades/cmd/blades/internal/workspace"
)

type Options struct {
	ConfigPath   string
	WorkspaceDir string
	Debug        bool
}

type Bootstrap struct {
	Options Options
}

func NewBootstrap(opts Options) Bootstrap {
	return Bootstrap{Options: opts}
}

func (b Bootstrap) HomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return filepath.Join(".", ".blades")
	}
	return filepath.Join(home, ".blades")
}

func (b Bootstrap) Workspace() *workspace.Workspace {
	workspaceDir := ""
	if b.Options.WorkspaceDir != "" {
		workspaceDir = config.ExpandTilde(b.Options.WorkspaceDir)
	}
	return workspace.NewWithWorkspace(b.HomeDir(), workspaceDir)
}

func (b Bootstrap) LoadConfig() (*config.Config, error) {
	return config.Load(b.Options.ConfigPath)
}

func (b Bootstrap) LoadMemory(ws *workspace.Workspace) (*memory.Store, error) {
	mem, err := memory.New(ws.MemoryPath(), ws.MemoriesDir(), ws.KnowledgesDir())
	if err != nil {
		return nil, fmt.Errorf("memory: %w", err)
	}
	return mem, nil
}

func (b Bootstrap) LoadAll() (*config.Config, *workspace.Workspace, *memory.Store, error) {
	cfg, err := b.LoadConfig()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("config: %w", err)
	}

	ws := b.Workspace()
	if err := ws.Load(); err != nil {
		return nil, nil, nil, err
	}

	mem, err := b.LoadMemory(ws)
	if err != nil {
		return nil, nil, nil, err
	}
	return cfg, ws, mem, nil
}

func (b Bootstrap) LoadRuntime() (*Runtime, error) {
	cfg, ws, mem, err := b.LoadAll()
	if err != nil {
		return nil, err
	}
	return BuildRuntime(cfg, ws, mem)
}

func (b Bootstrap) CronService() (*cron.Service, error) {
	if _, err := b.LoadConfig(); err != nil {
		return nil, err
	}
	ws := b.Workspace()
	return cron.NewService(ws.CronStorePath(), nil), nil
}

func (b Bootstrap) InitPaths() (homeDir, workspaceDir string, isCustomWorkspace bool) {
	homeDir = b.HomeDir()
	if b.Options.WorkspaceDir != "" {
		return homeDir, config.ExpandTilde(b.Options.WorkspaceDir), true
	}
	return homeDir, filepath.Join(homeDir, "workspace"), false
}

func (b Bootstrap) LogRootDir() string {
	return b.HomeDir()
}
