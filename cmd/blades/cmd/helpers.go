package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

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
//  2. ~/.blades/agent.yaml (default location)
//  3. Built-in defaults (anthropic, claude-sonnet-4-6)
func loadConfigForFlags() (*config.Config, error) {
	configPath := flagConfig

	// Config is always loaded from --config or ~/.blades/agent.yaml
	// (NOT from workspace directory)

	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
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
// and the active workspace directory from the --workspace flag.
func workspaceForConfig(cfg *config.Config) *workspace.Workspace {
	workspaceDir := ""
	if flagWorkspace != "" {
		workspaceDir = config.ExpandTilde(flagWorkspace)
	}
	return workspace.NewWithWorkspace(bladesHomeDir(), workspaceDir)
}

// loadAll loads config, workspace, memory store, and MCP servers.
// MCP servers are loaded only from ~/.blades/mcp.json (not from agent.yaml).
func loadAll() (*config.Config, *workspace.Workspace, *memory.Store, []bladesmcp.ClientConfig, error) {
	cfg, err := loadConfigForFlags()
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("config: %w", err)
	}

	ws := workspaceForConfig(cfg)
	if err := ws.Load(); err != nil {
		return nil, nil, nil, nil, err
	}

	var mcpServers []bladesmcp.ClientConfig
	servers, err := config.LoadMCPFile(ws.MCPPath())
	if err != nil {
		log.Printf("mcp: load %s: %v (continuing without)", ws.MCPPath(), err)
	} else {
		mcpServers = append(mcpServers, servers...)
	}

	mem, err := memory.New(ws.MemoryPath(), ws.MemoriesDir(), ws.KnowledgesDir())
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("memory: %w", err)
	}
	return cfg, ws, mem, mcpServers, nil
}

// buildRunner constructs a blades Agent + Runner from config, workspace, and MCP list.
// extraTools are appended in addition to the built-in exec tool.
func buildRunner(cfg *config.Config, ws *workspace.Workspace, mcpServers []bladesmcp.ClientConfig, extraTools ...bladestools.Tool) (*blades.Runner, error) {
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

	maxMessages := contextMessageLimit(cfg)
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

	execCfg := execConfigFromDefaults(defaultExecWorkingDir(ws), cfg.Exec)
	builtinTools := []bladestools.Tool{
		bldtools.NewExecTool(execCfg),
	}
	allTools := append(builtinTools, extraTools...)

	if len(mcpServers) > 0 {
		resolver, err := bladesmcp.NewToolsResolver(mcpServers...)
		if err != nil {
			log.Printf("mcp: init resolver: %v (continuing without MCP tools)", err)
		} else {
			agentOpts = append(agentOpts, blades.WithToolsResolver(resolver))
		}
	}

	agentOpts = append(agentOpts, blades.WithTools(allTools...))

	agent, err := blades.NewAgent("blades", agentOpts...)
	if err != nil {
		return nil, fmt.Errorf("agent: %w", err)
	}
	return blades.NewRunner(agent), nil
}

// execConfigFromDefaults merges config.Exec with built-in defaults.
func execConfigFromDefaults(workingDir string, exec config.ExecConfig) bldtools.ExecConfig {
	base := bldtools.DefaultExecConfig(workingDir)
	base.Timeout = exec.ExecTimeout()
	if exec.RestrictToWorkspace {
		base.RestrictToWorkspace = true
	}
	if len(exec.DenyPatterns) > 0 {
		base.DenyPatterns = append(base.DenyPatterns, exec.DenyPatterns...)
	}
	if len(exec.AllowPatterns) > 0 {
		base.AllowPatterns = exec.AllowPatterns
	}
	return base
}

// loadSkills loads skills from ~/.blades/skills.
// Missing directory or load errors are logged and skipped.
func loadSkills(ws *workspace.Workspace) ([]bladeskills.Skill, error) {
	dir := ws.SkillsDir() // ~/.blades/skills

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		return nil, nil
	}
	list, err := bladeskills.NewFromDir(dir)
	if err != nil {
		log.Printf("skills: load %s: %v (skipping)", dir, err)
		return nil, nil
	}
	return list, nil
}

// makeTrigger returns a cron.TriggerFn that runs a single agent turn and
// returns the assembled reply text. Used by chat, daemon, and cron run.
// If getRunner is nil, runner is used; otherwise getRunner is called each time.
func makeTrigger(runner *blades.Runner, sessMgr *session.Manager) cron.TriggerFn {
	return makeTriggerWithGetter(func() *blades.Runner { return runner }, sessMgr)
}

// makeTriggerWithGetter is like makeTrigger but takes a getter for the current runner (for daemon reload).
func makeTriggerWithGetter(getRunner func() *blades.Runner, sessMgr *session.Manager) cron.TriggerFn {
	return func(ctx context.Context, sessionID, text string) (string, error) {
		r := getRunner()
		if r == nil {
			return "", fmt.Errorf("no runner")
		}
		sess := sessMgr.GetOrNew(sessionID)
		msg := blades.UserMessage(text)
		var buf strings.Builder
		for m, err := range r.RunStream(ctx, msg, blades.WithSession(sess)) {
			if err != nil {
				return buf.String(), err
			}
			if m == nil {
				continue
			}

			// Keep one canonical final response: streamed deltas are accumulated,
			// and a completed message replaces them when provided by the model.
			if m.Status == blades.StatusCompleted {
				if finalText := m.Text(); finalText != "" {
					buf.Reset()
					buf.WriteString(finalText)
				}
				continue
			}

			if chunk := m.Text(); chunk != "" {
				buf.WriteString(chunk)
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
