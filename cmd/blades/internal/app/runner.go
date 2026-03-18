package app

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/recipe"
	recipeMiddleware "github.com/go-kratos/blades/recipe/middleware"
	bladeskills "github.com/go-kratos/blades/skills"
	bladestools "github.com/go-kratos/blades/tools"
	coretools "github.com/go-kratos/blades/tools"

	"github.com/go-kratos/blades/cmd/blades/internal/config"
	"github.com/go-kratos/blades/cmd/blades/internal/cron"
	"github.com/go-kratos/blades/cmd/blades/internal/model"
	bldtools "github.com/go-kratos/blades/cmd/blades/internal/tools"
	"github.com/go-kratos/blades/cmd/blades/internal/workspace"
)

func BuildRunner(cfg *config.Config, ws *workspace.Workspace, cronSvc *cron.Service, extraTools ...bladestools.Tool) (*blades.Runner, error) {
	spec, err := LoadAgentSpec(ws)
	if err != nil {
		return nil, err
	}

	if instruction, err := ws.ReadFile("AGENTS.md"); err == nil && instruction != "" {
		spec.Instruction = instruction
	}

	skillList, err := LoadSkills(ws)
	if err != nil {
		return nil, err
	}

	extraOpts := []blades.AgentOption{}
	if len(skillList) > 0 {
		extraOpts = append(extraOpts, blades.WithSkills(skillList...))
	}

	toolRegistry := BuildToolRegistry(ExecConfigFromDefaults(DefaultExecWorkingDir(ws), cfg.Exec), cronSvc, extraTools...)
	middlewareRegistry := BuildMiddlewareRegistry()
	reg := model.NewRegistry(cfg.Providers)
	agent, err := recipe.Build(spec,
		recipe.WithModelRegistry(reg),
		recipe.WithToolRegistry(toolRegistry),
		recipe.WithMiddlewareRegistry(middlewareRegistry),
		recipe.WithAgentOptions(extraOpts...),
	)
	if err != nil {
		return nil, fmt.Errorf("recipe: %w", err)
	}
	return blades.NewRunner(agent), nil
}

func BuildToolRegistry(execCfg bldtools.ExecConfig, cronSvc *cron.Service, extraTools ...bladestools.Tool) *bldtools.Registry {
	if cronSvc == nil {
		cronSvc = cron.NewService("", nil)
	}
	toolsByName := map[string]bladestools.Tool{
		"exec": bldtools.NewExecTool(execCfg),
		"cron": bldtools.NewCronTool(cronSvc),
		"exit": coretools.NewExitTool(),
	}
	for _, tool := range extraTools {
		if tool == nil {
			continue
		}
		toolsByName[tool.Name()] = tool
	}
	return bldtools.NewRegistry(toolsByName)
}

func BuildMiddlewareRegistry() *recipe.MiddlewareRegistry {
	registry := recipe.NewMiddlewareRegistry()
	registry.Register("retry", func(options map[string]any) (blades.Middleware, error) {
		attempts, err := MiddlewareAttempts(options)
		if err != nil {
			return nil, err
		}
		return recipeMiddleware.Retry(attempts), nil
	})
	return registry
}

func MiddlewareAttempts(options map[string]any) (int, error) {
	if len(options) == 0 {
		return 3, nil
	}
	raw, ok := options["attempts"]
	if !ok {
		return 3, nil
	}
	switch v := raw.(type) {
	case int:
		if v > 0 {
			return v, nil
		}
	case int64:
		if v > 0 {
			return int(v), nil
		}
	case float64:
		if v > 0 && v == float64(int(v)) {
			return int(v), nil
		}
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err == nil && n > 0 {
			return n, nil
		}
	}
	return 0, fmt.Errorf("middleware retry: attempts must be a positive integer")
}

func ExecConfigFromDefaults(workingDir string, exec config.ExecConfig) bldtools.ExecConfig {
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

func LoadSkills(ws *workspace.Workspace) ([]bladeskills.Skill, error) {
	dir := ws.SkillsDir()
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

func DefaultExecWorkingDir(ws *workspace.Workspace) string {
	if ws == nil {
		return "."
	}
	root := strings.TrimSpace(ws.WorkspaceDir())
	if root == "" {
		return "."
	}
	return root
}

func LoadAgentSpec(ws *workspace.Workspace) (*recipe.AgentSpec, error) {
	path := ws.AgentPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return &recipe.AgentSpec{
			Version:     "1.0",
			Name:        "blades",
			Description: "Personal AI assistant running in your local workspace.",
			Model:       "anthropic/claude-sonnet-4-6",
			Instruction: "You are a helpful personal AI assistant.",
			Tools:       []string{"exec", "cron"},
			Context: &recipe.ContextSpec{
				Strategy:    recipe.ContextStrategyWindow,
				MaxTokens:   80000,
				MaxMessages: 50,
			},
			Middlewares: []recipe.MiddlewareSpec{
				{
					Name: "retry",
					Options: map[string]any{
						"attempts": 3,
					},
				},
			},
		}, nil
	}
	spec, err := recipe.LoadFromFile(path)
	if err != nil {
		return nil, fmt.Errorf("agent.yaml: %w", err)
	}
	return spec, nil
}
