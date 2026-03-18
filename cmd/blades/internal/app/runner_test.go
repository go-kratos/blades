package app

import (
	"strings"
	"testing"

	"github.com/go-kratos/blades/recipe"

	"github.com/go-kratos/blades/cmd/blades/internal/config"
	bldtools "github.com/go-kratos/blades/cmd/blades/internal/tools"
	"github.com/go-kratos/blades/cmd/blades/internal/workspace"
)

func TestDefaultExecWorkingDir(t *testing.T) {
	homeDir := t.TempDir()
	workspaceDir := t.TempDir()
	ws := workspace.NewWithWorkspace(homeDir, workspaceDir)

	if got, want := DefaultExecWorkingDir(ws), workspaceDir; got != want {
		t.Fatalf("DefaultExecWorkingDir = %q, want %q", got, want)
	}
	if got, want := DefaultExecWorkingDir(nil), "."; got != want {
		t.Fatalf("DefaultExecWorkingDir(nil) = %q, want %q", got, want)
	}
}

func TestBuildToolAndMiddlewareRegistry(t *testing.T) {
	registry := BuildToolRegistry(bldtools.DefaultExecConfig(t.TempDir()), nil)
	for _, name := range []string{"exec", "cron", "exit"} {
		if _, err := registry.Resolve(name); err != nil {
			t.Fatalf("expected tool %q to be registered: %v", name, err)
		}
	}

	middlewareRegistry := BuildMiddlewareRegistry()
	if _, err := middlewareRegistry.Resolve("retry", map[string]any{"attempts": 3}); err != nil {
		t.Fatalf("expected retry middleware to resolve: %v", err)
	}
}

func TestLoadAgentSpecAndBuildRunner(t *testing.T) {
	ws := workspace.New(t.TempDir())
	if err := ws.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	spec, err := LoadAgentSpec(ws)
	if err != nil {
		t.Fatalf("LoadAgentSpec: %v", err)
	}
	if err := recipe.Validate(spec); err != nil {
		t.Fatalf("default spec should be valid: %v", err)
	}
	if spec.Execution != recipe.ExecutionLoop {
		t.Fatalf("default execution = %q, want %q", spec.Execution, recipe.ExecutionLoop)
	}
	if len(spec.SubAgents) != 2 {
		t.Fatalf("default sub_agents = %d, want 2", len(spec.SubAgents))
	}

	cfg := &config.Config{
		Providers: []config.Provider{
			{
				Name:     "openai",
				Provider: "openai",
				Models:   []string{"gpt-4o"},
				APIKey:   "test-key",
			},
		},
	}

	if _, err := BuildRunner(cfg, ws, nil); err != nil {
		t.Fatalf("BuildRunner: %v", err)
	}
}

func TestApplyWorkspaceInstructionToLoopSubAgents(t *testing.T) {
	spec := &recipe.AgentSpec{
		Name:      "blades",
		Execution: recipe.ExecutionLoop,
		SubAgents: []recipe.SubAgentSpec{
			{Name: "action", Instruction: "action body"},
			{Name: "review", Instruction: "review body"},
		},
	}

	applyWorkspaceInstruction(spec, "workspace rules")

	for _, sub := range spec.SubAgents {
		if !strings.Contains(sub.Instruction, "workspace rules") {
			t.Fatalf("sub_agent %q instruction = %q", sub.Name, sub.Instruction)
		}
	}
}
