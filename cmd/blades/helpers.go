package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-kratos/blades"
	bladesmiddleware "github.com/go-kratos/blades/middleware"
	bladeskills "github.com/go-kratos/blades/skills"
	bladestools "github.com/go-kratos/blades/tools"

	"github.com/go-kratos/blades/cmd/blades/internal/config"
	"github.com/go-kratos/blades/cmd/blades/internal/memory"
	"github.com/go-kratos/blades/cmd/blades/internal/model"
	bldtools "github.com/go-kratos/blades/cmd/blades/internal/tools"
	"github.com/go-kratos/blades/cmd/blades/internal/workspace"
)

// loadConfigForFlags loads config using CLI flags.
// Precedence: --config > --workspace/config.yaml (if exists) > defaults.
func loadConfigForFlags() (*config.Config, error) {
	configPath := flagConfig
	if configPath == "" && flagWorkspace != "" {
		candidate := filepath.Join(flagWorkspace, "config.yaml")
		if _, err := os.Stat(candidate); err == nil {
			configPath = candidate
		} else if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("config: stat %q: %w", candidate, err)
		}
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}
	if flagWorkspace != "" {
		cfg.Workspace = flagWorkspace
	}
	return cfg, nil
}

// loadAll loads config + workspace + memory store.
// Most commands call this first.
func loadAll() (*config.Config, *workspace.Workspace, *memory.Store, error) {
	cfg, err := loadConfigForFlags()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("config: %w", err)
	}

	ws := workspace.New(cfg.Workspace)
	if err := ws.Load(); err != nil {
		return nil, nil, nil, err
	}

	mem, err := memory.New(ws.MemoryPath(), ws.MemoriesDir(), ws.KnowledgesDir())
	if err != nil {
		return nil, nil, nil, fmt.Errorf("memory: %w", err)
	}
	return cfg, ws, mem, nil
}

// buildInstruction assembles the initial system instruction.
// Only AGENTS.md is injected eagerly; the agent is expected to read other
// workspace files at runtime following the startup procedure defined there.
func buildInstruction(ws *workspace.Workspace) (string, error) {
	return ws.ReadFile("AGENTS.md")
}

// buildRunner constructs a blades Agent + Runner from the current config.
// extraTools are appended to the agent in addition to the built-in exec tool.
func buildRunner(cfg *config.Config, ws *workspace.Workspace, extraTools ...bladestools.Tool) (*blades.Runner, error) {
	provider, err := model.NewProvider(cfg.LLM)
	if err != nil {
		return nil, fmt.Errorf("model: %w", err)
	}

	instruction, err := buildInstruction(ws)
	if err != nil {
		return nil, fmt.Errorf("instruction: %w", err)
	}

	skillList, err := loadSkills(ws)
	if err != nil {
		return nil, err
	}

	agentOpts := []blades.AgentOption{
		blades.WithModel(provider),
		blades.WithMiddleware(bladesmiddleware.ConversationBuffered(100)),
	}
	if instruction != "" {
		agentOpts = append(agentOpts, blades.WithInstruction(instruction))
	}
	if len(skillList) > 0 {
		agentOpts = append(agentOpts, blades.WithSkills(skillList...))
	}
	if cfg.Defaults.MaxIterations > 0 {
		agentOpts = append(agentOpts, blades.WithMaxIterations(cfg.Defaults.MaxIterations))
	}

	// Built-in exec tool is always available; callers may pass extra tools (e.g. cron).
	builtinTools := []bladestools.Tool{
		bldtools.NewExecTool(bldtools.DefaultExecConfig(cfg.Workspace)),
	}
	allTools := append(builtinTools, extraTools...)
	agentOpts = append(agentOpts, blades.WithTools(allTools...))

	agent, err := blades.NewAgent("blades", agentOpts...)
	if err != nil {
		return nil, fmt.Errorf("agent: %w", err)
	}
	return blades.NewRunner(agent), nil
}

// loadSkills loads skills from the workspace skills directory.
func loadSkills(ws *workspace.Workspace) ([]bladeskills.Skill, error) {
	dir := ws.SkillsDir()
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil, nil
	}
	list, err := bladeskills.NewFromDir(dir)
	if err != nil {
		return nil, fmt.Errorf("skills: load: %w", err)
	}
	return list, nil
}
