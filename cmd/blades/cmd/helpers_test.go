package cmd

import (
	"testing"

	bladeskills "github.com/go-kratos/blades/skills"

	"github.com/go-kratos/blades/cmd/blades/internal/workspace"
)

func TestInitCreatesLoadableBuiltInSkills(t *testing.T) {
	t.Parallel()

	homeDir := t.TempDir()
	ws := workspace.New(homeDir)
	if err := ws.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	skillList, err := bladeskills.NewFromDir(ws.SkillsDir())
	if err != nil {
		t.Fatalf("load built-in skills: %v", err)
	}

	for _, skill := range skillList {
		if skill.Name() == "blades-cron" {
			return
		}
	}

	t.Fatalf("expected built-in skill %q in %s", "blades-cron", ws.SkillsDir())
}

func TestDefaultExecWorkingDirUsesWorkspaceDir(t *testing.T) {
	t.Parallel()

	homeDir := t.TempDir()
	workspaceDir := t.TempDir()
	ws := workspace.NewWithWorkspace(homeDir, workspaceDir)

	// defaultExecWorkingDir should return the workspace directory, not the home directory
	if got, want := defaultExecWorkingDir(ws), workspaceDir; got != want {
		t.Fatalf("default exec working dir = %q, want %q", got, want)
	}
}

func TestDefaultExecWorkingDirFallsBackToDot(t *testing.T) {
	t.Parallel()

	if got, want := defaultExecWorkingDir(nil), "."; got != want {
		t.Fatalf("default exec working dir for nil workspace = %q, want %q", got, want)
	}
}
