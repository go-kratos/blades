package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWorkspaceInitLoadAndReadFile(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".blades")
	workspaceDir := filepath.Join(t.TempDir(), "agent")
	ws := NewWithWorkspace(home, workspaceDir)

	if !ws.IsCustomWorkspace() {
		t.Fatal("expected custom workspace to be true")
	}

	if err := ws.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := ws.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := ws.LoadHome(); err != nil {
		t.Fatalf("LoadHome: %v", err)
	}

	for _, path := range []string{
		ws.ConfigPath(),
		ws.AgentPath(),
		ws.SkillsDir(),
		ws.SessionsDir(),
		ws.LogDir(),
		ws.AgentsPath(),
		ws.SoulPath(),
		ws.IdentityPath(),
		ws.UserPath(),
		ws.MemoryPath(),
		ws.HeartbeatPath(),
		ws.ToolsPath(),
		ws.MemoriesDir(),
		ws.KnowledgesDir(),
		ws.OutputsDir(),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected path %s: %v", path, err)
		}
	}

	content, err := ws.ReadFile("AGENTS.md")
	if err != nil {
		t.Fatalf("ReadFile existing: %v", err)
	}
	if strings.TrimSpace(content) == "" {
		t.Fatal("expected AGENTS.md content")
	}

	content, err = ws.ReadFile("missing.md")
	if err != nil {
		t.Fatalf("ReadFile missing: %v", err)
	}
	if content != "" {
		t.Fatalf("expected empty content for missing file, got %q", content)
	}

	if got := filepath.Base(ws.DailyLogPath()); got != time.Now().Format("2006-01-02")+".md" {
		t.Fatalf("DailyLogPath filename = %q", got)
	}
}

func TestWorkspaceDefaultLayout(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".blades")
	ws := New(home)

	if ws.IsCustomWorkspace() {
		t.Fatal("expected default workspace to not be custom")
	}
	if got, want := ws.WorkspaceDir(), filepath.Join(home, "workspace"); got != want {
		t.Fatalf("WorkspaceDir = %q, want %q", got, want)
	}
	if got := ws.Home(); got != home {
		t.Fatalf("Home = %q, want %q", got, home)
	}
	if got := ws.Root(); got != ws.WorkspaceDir() {
		t.Fatalf("Root = %q, want %q", got, ws.WorkspaceDir())
	}
	if got := ws.CronStorePath(); got != filepath.Join(home, "cron.yaml") {
		t.Fatalf("CronStorePath = %q", got)
	}
}

func TestWorkspaceInitHomeAndWorkspaceSeparately(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".blades")
	workspaceDir := filepath.Join(t.TempDir(), "agent")
	ws := NewWithWorkspace(home, workspaceDir)

	if err := ws.InitHome(); err != nil {
		t.Fatalf("InitHome: %v", err)
	}
	if err := ws.InitWorkspace(); err != nil {
		t.Fatalf("InitWorkspace: %v", err)
	}
	if err := ws.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := ws.LoadHome(); err != nil {
		t.Fatalf("LoadHome: %v", err)
	}
}
