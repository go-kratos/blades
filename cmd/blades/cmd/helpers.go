package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/context/window"
	bladesmcp "github.com/go-kratos/blades/contrib/mcp"
	bladeskills "github.com/go-kratos/blades/skills"
	bladestools "github.com/go-kratos/blades/tools"

	"github.com/go-kratos/blades/cmd/blades/internal/config"
	"github.com/go-kratos/blades/cmd/blades/internal/cron"
	"github.com/go-kratos/blades/cmd/blades/internal/memory"
	"github.com/go-kratos/blades/cmd/blades/internal/model"
	"github.com/go-kratos/blades/cmd/blades/internal/session"
	bldtools "github.com/go-kratos/blades/cmd/blades/internal/tools"
	"github.com/go-kratos/blades/cmd/blades/internal/workspace"
)

// loadConfigForFlags loads config using CLI flags.
//
// Configuration precedence (highest to lowest):
//  1. --config flag: use specified config file directly
//  2. ~/.blades/config.yaml (default location)
//  3. Built-in defaults (anthropic, claude-sonnet-4-6)
//
// After loading, if --workspace flag is set, it overrides config.Workspace.
func loadConfigForFlags() (*config.Config, error) {
	configPath := flagConfig

	// Config is always loaded from --config or ~/.blades/config.yaml
	// (NOT from workspace directory)

	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}

	// --workspace flag always overrides config.Workspace
	if flagWorkspace != "" {
		cfg.Workspace = config.ExpandTilde(flagWorkspace)
	}
	return cfg, nil
}

// bladesHomeDir returns the global blades home directory (~/.blades).
func bladesHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return filepath.Join(".", ".blades")
	}
	return filepath.Join(home, ".blades")
}

// workspaceForConfig builds a workspace using a fixed home dir (~/.blades)
// and the active workspace directory from config/flags.
func workspaceForConfig(cfg *config.Config) *workspace.Workspace {
	workspaceDir := ""
	if cfg != nil {
		workspaceDir = strings.TrimSpace(cfg.Workspace)
	}
	return workspace.NewWithWorkspace(bladesHomeDir(), workspaceDir)
}

// loadAll loads config + workspace + memory store.
// MCP server lists from ~/.blades/mcp.json and workspace/mcp.json are merged
// together with any servers declared inline in config.yaml.
func loadAll() (*config.Config, *workspace.Workspace, *memory.Store, error) {
	cfg, err := loadConfigForFlags()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("config: %w", err)
	}

	// Create workspace with separated home and workspace directories
	ws := workspaceForConfig(cfg)
	if err := ws.Load(); err != nil {
		return nil, nil, nil, err
	}

	// Merge MCP servers: config.yaml inline → ~/.blades/mcp.json → workspace/mcp.json.
	for _, path := range []string{ws.MCPPath(), ws.WorkspaceMCPPath()} {
		servers, err := config.LoadMCPFile(path)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("mcp: %w", err)
		}
		cfg.MCP = append(cfg.MCP, servers...)
	}

	mem, err := memory.New(ws.MemoryPath(), ws.MemoriesDir(), ws.KnowledgesDir())
	if err != nil {
		return nil, nil, nil, fmt.Errorf("memory: %w", err)
	}
	return cfg, ws, mem, nil
}

// buildRunner constructs a blades Agent + Runner from the current config.
// extraTools are appended to the agent in addition to the built-in exec tool.
func buildRunner(cfg *config.Config, ws *workspace.Workspace, extraTools ...bladestools.Tool) (*blades.Runner, error) {
	provider, err := model.NewProvider(cfg.LLM)
	if err != nil {
		return nil, fmt.Errorf("model: %w", err)
	}

	instruction, err := ws.ReadFile("AGENTS.md")
	if err != nil {
		return nil, fmt.Errorf("instruction: %w", err)
	}

	skillList, err := loadSkills(ws)
	if err != nil {
		return nil, err
	}

	maxMessages := 100
	maxMessages = contextMessageLimit(cfg)
	windowCM := window.NewContextManager(
		window.WithMaxMessages(maxMessages),
		window.WithMaxTokens(int64(cfg.Defaults.CompressThreshold)),
	)

	agentOpts := []blades.AgentOption{
		blades.WithModel(provider),
		blades.WithContextManager(windowCM),
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

	// Exec tool uses the workspace root as its default working directory.
	builtinTools := []bladestools.Tool{
		bldtools.NewExecTool(bldtools.DefaultExecConfig(defaultExecWorkingDir(ws))),
	}
	allTools := append(builtinTools, extraTools...)

	if len(cfg.MCP) > 0 {
		mcpConfigs := make([]bladesmcp.ClientConfig, 0, len(cfg.MCP))
		for _, m := range cfg.MCP {
			cc := bladesmcp.ClientConfig{
				Name:      m.Name,
				Transport: bladesmcp.TransportType(m.Transport),
				Command:   m.Command,
				Args:      m.Args,
				Env:       m.Env,
				WorkDir:   m.WorkDir,
				Endpoint:  m.Endpoint,
				Headers:   m.Headers,
			}
			if m.TimeoutSeconds > 0 {
				cc.Timeout = time.Duration(m.TimeoutSeconds) * time.Second
			}
			mcpConfigs = append(mcpConfigs, cc)
		}
		resolver, err := bladesmcp.NewToolsResolver(mcpConfigs...)
		if err != nil {
			return nil, fmt.Errorf("mcp: %w", err)
		}
		agentOpts = append(agentOpts, blades.WithToolsResolver(resolver))
	}

	agentOpts = append(agentOpts, blades.WithTools(allTools...))

	agent, err := blades.NewAgent("blades", agentOpts...)
	if err != nil {
		return nil, fmt.Errorf("agent: %w", err)
	}
	return blades.NewRunner(agent), nil
}

// loadSkills loads skills from three directories in order:
//  1. ~/.agents/skills  — system-wide skills
//  2. ~/.blades/skills  — global blades skills
//  3. workspace/skills  — workspace-local skills
//
// All three are merged; later entries override nothing (skills are additive).
// Missing directories are silently skipped.
func loadSkills(ws *workspace.Workspace) ([]bladeskills.Skill, error) {
	home, _ := os.UserHomeDir()
	dirs := []string{
		filepath.Join(home, ".agents", "skills"), // system-wide
		ws.SkillsDir(),                           // ~/.blades/skills
		ws.WorkspaceSkillsDir(),                  // workspace/skills
	}

	var all []bladeskills.Skill
	for _, dir := range dirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue
		}
		// Skip directories that have no entries — NewFromDir errors on empty dirs.
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) == 0 {
			continue
		}
		list, err := bladeskills.NewFromDir(dir)
		if err != nil {
			return nil, fmt.Errorf("skills: load %s: %w", dir, err)
		}
		all = append(all, list...)
	}
	return all, nil
}

// makeTrigger returns a cron.TriggerFn that runs a single agent turn and
// returns the assembled reply text. Used by chat, daemon, and cron run.
func makeTrigger(runner *blades.Runner, sessMgr *session.Manager) cron.TriggerFn {
	return func(ctx context.Context, sessionID, text string) (string, error) {
		sess := sessMgr.GetOrNew(sessionID)
		msg := blades.UserMessage(text)
		var buf strings.Builder
		for m, err := range runner.RunStream(ctx, msg, blades.WithSession(sess)) {
			if err != nil {
				return buf.String(), err
			}
			if m != nil {
				buf.WriteString(m.Text())
			}
		}
		return buf.String(), nil
	}
}

func contextMessageLimit(cfg *config.Config) int {
	if cfg != nil && cfg.Defaults.MaxTurns > 0 {
		return cfg.Defaults.MaxTurns * 2
	}
	return 100
}

// defaultExecWorkingDir returns the workspace directory as the exec tool's working directory.
func defaultExecWorkingDir(ws *workspace.Workspace) string {
	if ws == nil {
		return "."
	}
	root := strings.TrimSpace(ws.WorkspaceDir())
	if root == "" {
		return "."
	}
	return root
}
