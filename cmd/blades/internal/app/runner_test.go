package app

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/go-kratos/blades"
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
	for _, name := range []string{"read", "write", "edit", "bash", "cron", "exit"} {
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
	if spec.Context == nil {
		t.Fatal("default spec should define top-level context")
	}
	if len(spec.SubAgents) != 2 {
		t.Fatalf("default sub_agents = %d, want 2", len(spec.SubAgents))
	}
	for _, sub := range spec.SubAgents {
		if sub.Context != nil {
			t.Fatalf("default sub_agent %q should not define its own context", sub.Name)
		}
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

func TestBuildSessionManagerAppliesTopLevelContext(t *testing.T) {
	ws := workspace.New(t.TempDir())
	if err := ws.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := os.WriteFile(ws.AgentPath(), []byte(`version: "1.0"
name: blades
model: openai/gpt-4o
execution: loop
max_iterations: 3
context:
  strategy: window
  max_messages: 1
sub_agents:
  - name: action
    instruction: |
      Action.
    tools:
      - read
    output_key: action_result
  - name: review
    instruction: |
      Review {{.action_result}}.
    tools:
      - exit
    output_key: review_feedback
`), 0o644); err != nil {
		t.Fatalf("Write agent.yaml: %v", err)
	}

	sessMgr, err := BuildSessionManager(&config.Config{}, ws)
	if err != nil {
		t.Fatalf("BuildSessionManager: %v", err)
	}
	sess := sessMgr.GetOrNew("ctx")
	if err := sess.Append(context.Background(), blades.UserMessage("one")); err != nil {
		t.Fatalf("append one: %v", err)
	}
	if err := sess.Append(context.Background(), blades.UserMessage("two")); err != nil {
		t.Fatalf("append two: %v", err)
	}
	history, err := sess.History(context.Background())
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if got, want := len(history), 1; got != want {
		t.Fatalf("history len = %d, want %d", got, want)
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
