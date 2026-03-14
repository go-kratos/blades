package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveInitPathsUsesHomeAndWorkspaceFlag(t *testing.T) {
	preserveRootState(t)

	oldHome := os.Getenv("HOME")
	newHome := t.TempDir()
	_ = os.Setenv("HOME", newHome)
	t.Cleanup(func() {
		_ = os.Setenv("HOME", oldHome)
	})

	flagWorkspace = "~/my-agent"

	homeDir, workspaceDir, isCustom := resolveInitPaths()
	if got, want := homeDir, filepath.Join(newHome, ".blades"); got != want {
		t.Fatalf("home dir = %q, want %q", got, want)
	}
	if got, want := workspaceDir, filepath.Join(newHome, "my-agent"); got != want {
		t.Fatalf("workspace dir = %q, want %q", got, want)
	}
	if !isCustom {
		t.Fatal("expected custom workspace flag to be true")
	}
}

func TestResolveInitPathsDefaultWorkspace(t *testing.T) {
	preserveRootState(t)

	oldHome := os.Getenv("HOME")
	newHome := t.TempDir()
	_ = os.Setenv("HOME", newHome)
	t.Cleanup(func() {
		_ = os.Setenv("HOME", oldHome)
	})

	flagWorkspace = ""

	homeDir, workspaceDir, isCustom := resolveInitPaths()
	if got, want := homeDir, filepath.Join(newHome, ".blades"); got != want {
		t.Fatalf("home dir = %q, want %q", got, want)
	}
	if got, want := workspaceDir, filepath.Join(newHome, ".blades", "workspace"); got != want {
		t.Fatalf("workspace dir = %q, want %q", got, want)
	}
	if isCustom {
		t.Fatal("expected custom workspace flag to be false")
	}
}

func TestInitWithCustomWorkspaceSeparatesHomeAndWorkspace(t *testing.T) {
	preserveRootState(t)

	oldHome := os.Getenv("HOME")
	newHome := t.TempDir()
	_ = os.Setenv("HOME", newHome)
	t.Cleanup(func() {
		_ = os.Setenv("HOME", oldHome)
	})

	homeDir := filepath.Join(newHome, ".blades")
	workspaceDir := filepath.Join(newHome, "my-agent")

	flagConfig = ""
	flagWorkspace = "~/my-agent"

	initCmd := newInitCmd()
	initCmd.SetArgs([]string{})
	if err := initCmd.Execute(); err != nil {
		t.Fatalf("init command failed: %v", err)
	}

	homePaths := []string{
		filepath.Join(homeDir, "config.yaml"),
		filepath.Join(homeDir, "mcp.json"),
		filepath.Join(homeDir, "skills"),
		filepath.Join(homeDir, "sessions"),
		filepath.Join(homeDir, "log"),
	}
	for _, p := range homePaths {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected home path %q to exist: %v", p, err)
		}
	}

	workspacePaths := []string{
		filepath.Join(workspaceDir, "AGENTS.md"),
		filepath.Join(workspaceDir, "SOUL.md"),
		filepath.Join(workspaceDir, "USER.md"),
		filepath.Join(workspaceDir, "MEMORY.md"),
		filepath.Join(workspaceDir, "skills"),
		filepath.Join(workspaceDir, "memory"),
		filepath.Join(workspaceDir, "knowledges"),
		filepath.Join(workspaceDir, "outputs"),
	}
	for _, p := range workspacePaths {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected workspace path %q to exist: %v", p, err)
		}
	}

	if _, err := os.Stat(filepath.Join(homeDir, "workspace")); err == nil {
		t.Fatalf("did not expect default workspace dir %q when custom workspace is set", filepath.Join(homeDir, "workspace"))
	}
}
