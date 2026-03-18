package cmd

import (
	"bytes"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appcore "github.com/go-kratos/blades/cmd/blades/internal/app"
	"github.com/go-kratos/blades/cmd/blades/internal/workspace"
)

func preserveRootState(t *testing.T) {
	t.Helper()

	oldWriter := log.Writer()
	oldFlags := log.Flags()
	oldDebugEnv, hasDebugEnv := os.LookupEnv("BLADES_DEBUG")

	t.Cleanup(func() {
		log.SetOutput(oldWriter)
		log.SetFlags(oldFlags)
		if hasDebugEnv {
			_ = os.Setenv("BLADES_DEBUG", oldDebugEnv)
		} else {
			_ = os.Unsetenv("BLADES_DEBUG")
		}
	})
}

func TestOpenRootLogFileUsesHomeDir(t *testing.T) {
	preserveRootState(t)

	// Save and restore HOME environment
	oldHome := os.Getenv("HOME")
	newHome := t.TempDir()
	_ = os.Setenv("HOME", newHome)
	t.Cleanup(func() {
		_ = os.Setenv("HOME", oldHome)
	})

	// Create .blades directory
	homeDir := filepath.Join(newHome, ".blades")
	if err := os.MkdirAll(homeDir, 0o755); err != nil {
		t.Fatalf("create home dir: %v", err)
	}

	now := time.Date(2026, time.March, 13, 10, 0, 0, 0, time.UTC)
	f, path, err := openRootLogFileForOptions(now, appcore.Options{WorkspaceDir: t.TempDir()})
	if err != nil {
		t.Fatalf("openRootLogFile: %v", err)
	}
	defer f.Close()

	want := filepath.Join(homeDir, "logs", "2026-03-13.log")
	if path != want {
		t.Fatalf("log path = %q, want %q", path, want)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected log file %q to exist: %v", path, err)
	}
}

func TestResolveLogRootDirAlwaysUsesHomeBlades(t *testing.T) {
	preserveRootState(t)

	// Save and restore HOME environment
	oldHome := os.Getenv("HOME")
	newHome := t.TempDir()
	_ = os.Setenv("HOME", newHome)
	t.Cleanup(func() {
		_ = os.Setenv("HOME", oldHome)
	})

	// Even with config specifying different workspace, logs go to ~/.blades
	root := t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "agent.yaml")
	cfgContent := "workspace: " + root + "\n"
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	got := resolveLogRootDirForOptions(appcore.Options{ConfigPath: cfgPath})
	want := filepath.Join(newHome, ".blades")
	if got != want {
		t.Fatalf("resolveLogRootDir() = %q, want %q", got, want)
	}
}

func TestConfigureRootLoggerWritesToDailyLogFile(t *testing.T) {
	preserveRootState(t)

	// Save and restore HOME environment
	oldHome := os.Getenv("HOME")
	newHome := t.TempDir()
	_ = os.Setenv("HOME", newHome)
	t.Cleanup(func() {
		_ = os.Setenv("HOME", oldHome)
	})

	// Create .blades directory
	homeDir := filepath.Join(newHome, ".blades")
	if err := os.MkdirAll(homeDir, 0o755); err != nil {
		t.Fatalf("create home dir: %v", err)
	}

	now := time.Date(2026, time.March, 13, 10, 0, 0, 0, time.UTC)
	configureRootLoggerForOptions(now, appcore.Options{})

	log.Print("runtime log check")
	if f, ok := log.Writer().(*os.File); ok {
		_ = f.Sync()
		_ = f.Close()
		log.SetOutput(io.Discard)
	}

	logPath := filepath.Join(homeDir, "logs", "2026-03-13.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if !strings.Contains(string(data), "runtime log check") {
		t.Fatalf("expected log file to contain test message, got: %s", string(data))
	}
}

func TestDoctorUsesExplicitConfigPath(t *testing.T) {
	preserveRootState(t)

	oldHome := os.Getenv("HOME")
	newHome := t.TempDir()
	_ = os.Setenv("HOME", newHome)
	t.Cleanup(func() {
		_ = os.Setenv("HOME", oldHome)
	})

	homeDir := filepath.Join(newHome, ".blades")
	workspaceDir := filepath.Join(newHome, "agent")
	ws := workspace.NewWithWorkspace(homeDir, workspaceDir)
	if err := ws.InitHome(); err != nil {
		t.Fatalf("InitHome: %v", err)
	}
	if err := ws.InitWorkspace(); err != nil {
		t.Fatalf("InitWorkspace: %v", err)
	}
	if err := os.Remove(ws.ConfigPath()); err != nil {
		t.Fatalf("remove default config: %v", err)
	}

	customConfig := filepath.Join(t.TempDir(), "custom-config.yaml")
	if err := os.WriteFile(customConfig, []byte(`providers:
  - name: openai
    provider: openai
    models: [gpt-4o]
    apiKey: test-key
exec:
  restrictToWorkspace: true
`), 0o644); err != nil {
		t.Fatalf("write custom config: %v", err)
	}

	cmd := newDoctorCmd()
	withCommandOptions(cmd, appcore.Options{
		ConfigPath:   customConfig,
		WorkspaceDir: workspaceDir,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("doctor with explicit config should succeed, got %v", err)
	}
}

func TestRunDoctorChecksAggregatesResultsInOrder(t *testing.T) {
	results, err := appcore.RunDoctorChecks(&appcore.DoctorContext{}, []appcore.DoctorCheck{
		{
			Name: "first",
			Run: func(*appcore.DoctorContext) ([]appcore.DoctorResult, error) {
				return []appcore.DoctorResult{{Label: "one", Detail: "ok", OK: true}}, nil
			},
		},
		{
			Name: "second",
			Run: func(*appcore.DoctorContext) ([]appcore.DoctorResult, error) {
				return []appcore.DoctorResult{
					{Label: "two", Detail: "warn", OK: false},
					{Label: "three", Detail: "ok", OK: true},
				}, nil
			},
		},
	})
	if err != nil {
		t.Fatalf("runDoctorChecks: %v", err)
	}
	if got, want := len(results), 3; got != want {
		t.Fatalf("len(results) = %d, want %d", got, want)
	}
	if results[0].Label != "one" || results[1].Label != "two" || results[2].Label != "three" {
		t.Fatalf("unexpected result order: %+v", results)
	}
}

func TestPrintDoctorResultsReportsFailures(t *testing.T) {
	var buf bytes.Buffer
	ok := appcore.PrintDoctorResults(&buf, []appcore.DoctorResult{
		{Label: "config.yaml", Detail: "/tmp/config.yaml", OK: true},
		{Label: "workspace/TOOLS.md", Detail: "/tmp/TOOLS.md (missing)", OK: false},
		{Label: "stale", Detail: "job-x", OK: false},
	})
	if ok {
		t.Fatal("printDoctorResults should report failure when a check fails")
	}
	out := buf.String()
	if !strings.Contains(out, "config.yaml") || !strings.Contains(out, "(missing)") || !strings.Contains(out, "stale: job-x") {
		t.Fatalf("printDoctorResults output = %q", out)
	}
}
