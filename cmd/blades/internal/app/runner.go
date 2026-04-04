package app

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/go-kratos/blades"
	recipeMiddleware "github.com/go-kratos/blades/middleware"
	"github.com/go-kratos/blades/recipe"
	bladeskills "github.com/go-kratos/blades/skills"
	bladestools "github.com/go-kratos/blades/tools"
	coretools "github.com/go-kratos/blades/tools"

	"github.com/go-kratos/blades/cmd/blades/internal/config"
	"github.com/go-kratos/blades/cmd/blades/internal/cron"
	"github.com/go-kratos/blades/cmd/blades/internal/model"
	"github.com/go-kratos/blades/cmd/blades/internal/session"
	bldtools "github.com/go-kratos/blades/cmd/blades/internal/tools"
	"github.com/go-kratos/blades/cmd/blades/internal/workspace"
)

func BuildRunner(cfg *config.Config, ws *workspace.Workspace, cronSvc *cron.Service, extraTools ...bladestools.Tool) (*blades.Runner, error) {
	spec, err := LoadAgentSpec(ws)
	if err != nil {
		return nil, err
	}

	if instruction, err := ws.ReadFile("AGENTS.md"); err == nil && instruction != "" {
		applyWorkspaceInstruction(spec, instruction)
	}

	skillList, err := LoadSkills(ws)
	if err != nil {
		return nil, err
	}
	skillTools := []bladestools.Tool{}
	if len(skillList) > 0 {
		toolset, err := bladeskills.NewToolset(skillList)
		if err != nil {
			return nil, err
		}
		applyWorkspaceInstruction(spec, toolset.Instruction())
		skillTools = append(skillTools, toolset.Tools()...)
	}
	extraTools = append(skillTools, extraTools...)

	toolRegistry := BuildToolRegistry(ExecConfigFromDefaults(DefaultExecWorkingDir(ws), cfg.Exec), cronSvc, extraTools...)
	middlewareRegistry := BuildMiddlewareRegistry()
	reg := model.NewRegistry(cfg.Providers)
	agent, err := recipe.Build(spec,
		recipe.WithModelRegistry(reg),
		recipe.WithToolRegistry(toolRegistry),
		recipe.WithMiddlewareRegistry(middlewareRegistry),
		recipe.WithContext(spec.Context != nil),
	)
	if err != nil {
		return nil, fmt.Errorf("recipe: %w", err)
	}
	return blades.NewRunner(agent), nil
}

func BuildSessionManager(cfg *config.Config, ws *workspace.Workspace) (*session.Manager, error) {
	spec, err := LoadAgentSpec(ws)
	if err != nil {
		return nil, err
	}
	var providers []config.Provider
	if cfg != nil {
		providers = cfg.Providers
	}
	reg := model.NewRegistry(providers)
	sessOpt, err := recipe.BuildSessionOption(spec, recipe.WithModelRegistry(reg))
	if err != nil {
		return nil, fmt.Errorf("recipe context: %w", err)
	}
	if sessOpt == nil {
		return session.NewManager(ws.SessionsDir()), nil
	}
	return session.NewManager(ws.SessionsDir(), sessOpt), nil
}

func BuildToolRegistry(execCfg bldtools.ExecConfig, cronSvc *cron.Service, extraTools ...bladestools.Tool) *bldtools.Registry {
	if cronSvc == nil {
		cronSvc = cron.NewService("", nil)
	}
	toolsByName := map[string]bladestools.Tool{
		"read":  bldtools.NewReadTool(execCfg),
		"write": bldtools.NewWriteTool(execCfg),
		"edit":  bldtools.NewEditTool(execCfg),
		"bash":  bldtools.NewBashTool(execCfg),
		"cron":  bldtools.NewCronTool(cronSvc),
		"exit":  coretools.NewExitTool(),
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
			Version:       "1.0",
			Name:          "blades",
			Description:   "Personal AI assistant running in your local workspace.",
			Model:         "anthropic/claude-sonnet-4-6",
			Execution:     recipe.ExecutionLoop,
			MaxIterations: 3,
			Context: &recipe.ContextSpec{
				Strategy:    recipe.ContextStrategyWindow,
				MaxTokens:   80000,
				MaxMessages: 50,
			},
			SubAgents: []recipe.SubAgentSpec{
				{
					Name:        "action",
					Description: "Execute the next concrete step toward the user's goal.",
					Instruction: "You are the action agent.\nProduce the best next response or action for the user's request.\nUse the available tools only when they are actually needed.\nIf prior review feedback exists, address it carefully.\nDo not rewrite a good answer just to phrase it differently.\nFor simple greetings, acknowledgements, or casual chat, answer naturally and briefly.\n\nPrevious review feedback:\n{{.review_feedback}}",
					Tools:       []string{"read", "write", "edit", "bash", "cron"},
					OutputKey:   "action_result",
					Middlewares: []recipe.MiddlewareSpec{
						{
							Name: "retry",
							Options: map[string]any{
								"attempts": 3,
							},
						},
					},
				},
				{
					Name:        "review",
					Description: "Review the latest action result and decide whether to stop.",
					Instruction: "You are the review agent.\nBias strongly toward stopping.\nIf the latest action result already answers the user well enough, call the exit tool immediately with a brief reason.\nThis includes greetings, casual chat, short factual answers, and responses that are already good enough.\nDo not request another iteration just for stylistic rewrites or alternate phrasing.\nOnly continue when the latest action result is clearly incomplete, incorrect, unsafe, or missed an obvious required tool/action.\nIf another iteration is needed, explain exactly what the next action iteration must improve.",
					Prompt:      "Review the latest action result below.\n\nACTION_RESULT_BEGIN\n{{.action_result}}\nACTION_RESULT_END\n\nPlease review the action output above. Decide whether to stop or continue.",
					Tools:       []string{"exit"},
					OutputKey:   "review_feedback",
					Middlewares: []recipe.MiddlewareSpec{
						{
							Name: "retry",
							Options: map[string]any{
								"attempts": 3,
							},
						},
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

func applyWorkspaceInstruction(spec *recipe.AgentSpec, instruction string) {
	instruction = strings.TrimSpace(instruction)
	if spec == nil || instruction == "" {
		return
	}
	if len(spec.SubAgents) == 0 {
		spec.Instruction = instruction
		return
	}
	for i := range spec.SubAgents {
		subInstruction := strings.TrimSpace(spec.SubAgents[i].Instruction)
		if subInstruction == "" {
			spec.SubAgents[i].Instruction = instruction
			continue
		}
		spec.SubAgents[i].Instruction = instruction + "\n\n" + subInstruction
	}
}
