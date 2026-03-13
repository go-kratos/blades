package cmd

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func preserveRootState(t *testing.T) {
	t.Helper()

	oldConfig := flagConfig
	oldWorkspace := flagWorkspace
	oldDebug := flagDebug
	oldWriter := log.Writer()
	oldFlags := log.Flags()
	oldDebugEnv, hasDebugEnv := os.LookupEnv("BLADES_DEBUG")

	t.Cleanup(func() {
		flagConfig = oldConfig
		flagWorkspace = oldWorkspace
		flagDebug = oldDebug
		log.SetOutput(oldWriter)
		log.SetFlags(oldFlags)
		if hasDebugEnv {
			_ = os.Setenv("BLADES_DEBUG", oldDebugEnv)
		} else {
			_ = os.Unsetenv("BLADES_DEBUG")
		}
	})
}

func TestOpenRootLogFileUsesWorkspaceFlag(t *testing.T) {
	preserveRootState(t)

	root := t.TempDir()
	flagWorkspace = root
	flagConfig = ""

	now := time.Date(2026, time.March, 13, 10, 0, 0, 0, time.UTC)
	f, path, err := openRootLogFile(now)
	if err != nil {
		t.Fatalf("openRootLogFile: %v", err)
	}
	defer f.Close()

	want := filepath.Join(root, "log", "2026-03-13.log")
	if path != want {
		t.Fatalf("log path = %q, want %q", path, want)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected log file %q to exist: %v", path, err)
	}
}

func TestResolveLogRootDirUsesConfigWorkspace(t *testing.T) {
	preserveRootState(t)

	root := t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	cfgContent := "workspace: " + root + "\n"
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	flagWorkspace = ""
	flagConfig = cfgPath

	got := resolveLogRootDir()
	if got != root {
		t.Fatalf("resolveLogRootDir() = %q, want %q", got, root)
	}
}

func TestConfigureRootLoggerWritesToDailyLogFile(t *testing.T) {
	preserveRootState(t)

	root := t.TempDir()
	flagWorkspace = root
	flagConfig = ""
	flagDebug = false

	now := time.Date(2026, time.March, 13, 10, 0, 0, 0, time.UTC)
	configureRootLogger(now)

	log.Print("runtime log check")
	if f, ok := log.Writer().(*os.File); ok {
		_ = f.Sync()
		_ = f.Close()
		log.SetOutput(io.Discard)
	}

	logPath := filepath.Join(root, "log", "2026-03-13.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if !strings.Contains(string(data), "runtime log check") {
		t.Fatalf("expected log file to contain test message, got: %s", string(data))
	}
}
