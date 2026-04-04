package app

import (
	"os"
	"path/filepath"
	"testing"
)

func writeProviderConfig(t *testing.T, path string) {
	t.Helper()
	const cfg = `providers:
  - name: openai
    provider: openai
    models: [gpt-4o]
    apiKey: test-key
`
	if err := os.WriteFile(path, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func withTempHome(t *testing.T) string {
	t.Helper()
	oldHome := os.Getenv("HOME")
	home := t.TempDir()
	if err := os.Setenv("HOME", home); err != nil {
		t.Fatalf("set HOME: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Setenv("HOME", oldHome)
	})
	return home
}

func TestBootstrapHomeWorkspaceAndInitPaths(t *testing.T) {
	home := withTempHome(t)
	b := NewBootstrap(Options{WorkspaceDir: "~/my-agent"})

	if got, want := b.HomeDir(), filepath.Join(home, ".blades"); got != want {
		t.Fatalf("HomeDir = %q, want %q", got, want)
	}
	if got, want := b.Workspace().WorkspaceDir(), filepath.Join(home, "my-agent"); got != want {
		t.Fatalf("WorkspaceDir = %q, want %q", got, want)
	}

	gotHome, gotWorkspace, gotCustom := b.InitPaths()
	if gotHome != filepath.Join(home, ".blades") || gotWorkspace != filepath.Join(home, "my-agent") || !gotCustom {
		t.Fatalf("InitPaths = (%q, %q, %v)", gotHome, gotWorkspace, gotCustom)
	}
}

func TestBootstrapLoadAllAndRuntime(t *testing.T) {
	home := withTempHome(t)
	b := NewBootstrap(Options{})
	ws := b.Workspace()

	if err := ws.InitHome(); err != nil {
		t.Fatalf("InitHome: %v", err)
	}
	if err := ws.InitWorkspace(); err != nil {
		t.Fatalf("InitWorkspace: %v", err)
	}
	writeProviderConfig(t, filepath.Join(home, ".blades", "config.yaml"))

	cfg, loadedWS, mem, err := b.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if cfg == nil || loadedWS == nil || mem == nil {
		t.Fatalf("LoadAll returned nil values: cfg=%v ws=%v mem=%v", cfg, loadedWS, mem)
	}

	rt, err := b.LoadRuntime()
	if err != nil {
		t.Fatalf("LoadRuntime: %v", err)
	}
	if rt.Runner == nil || rt.Cron == nil || rt.Sessions == nil {
		t.Fatalf("runtime not initialized: %+v", rt)
	}
}
