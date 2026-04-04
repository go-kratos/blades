package cmd

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-kratos/blades"
	bladestools "github.com/go-kratos/blades/tools"

	appcore "github.com/go-kratos/blades/cmd/blades/internal/app"
	"github.com/go-kratos/blades/cmd/blades/internal/channel"
	"github.com/go-kratos/blades/cmd/blades/internal/channel/lark"
	"github.com/go-kratos/blades/cmd/blades/internal/config"
	"github.com/go-kratos/blades/cmd/blades/internal/memory"
	"github.com/go-kratos/blades/cmd/blades/internal/session"
	"github.com/go-kratos/blades/cmd/blades/internal/workspace"
	"github.com/spf13/cobra"
)

func TestMiddlewareAttemptsExecConfigAndToolRegistry(t *testing.T) {
	tests := []struct {
		name    string
		options map[string]any
		want    int
		wantErr string
	}{
		{name: "default", options: nil, want: 3},
		{name: "int64", options: map[string]any{"attempts": int64(4)}, want: 4},
		{name: "float64", options: map[string]any{"attempts": float64(5)}, want: 5},
		{name: "string", options: map[string]any{"attempts": " 6 "}, want: 6},
		{name: "invalid", options: map[string]any{"attempts": "nope"}, wantErr: "positive integer"},
	}
	for _, tt := range tests {
		got, err := appcore.MiddlewareAttempts(tt.options)
		if tt.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("%s: expected error containing %q, got %v", tt.name, tt.wantErr, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: middlewareAttempts: %v", tt.name, err)
		}
		if got != tt.want {
			t.Fatalf("%s: attempts = %d, want %d", tt.name, got, tt.want)
		}
	}

	execCfg := appcore.ExecConfigFromDefaults("/workspace", config.ExecConfig{
		TimeoutSeconds:      15,
		DenyPatterns:        []string{"danger"},
		AllowPatterns:       []string{"safe"},
		RestrictToWorkspace: true,
	})
	if execCfg.Timeout != 15*time.Second {
		t.Fatalf("exec timeout = %s, want 15s", execCfg.Timeout)
	}
	if !execCfg.RestrictToWorkspace {
		t.Fatal("expected RestrictToWorkspace to be true")
	}
	if !slices.Contains(execCfg.DenyPatterns, "danger") {
		t.Fatalf("deny patterns = %v", execCfg.DenyPatterns)
	}
	if len(execCfg.AllowPatterns) != 1 || execCfg.AllowPatterns[0] != "safe" {
		t.Fatalf("allow patterns = %v", execCfg.AllowPatterns)
	}

	extraTool := bladestools.NewTool("bash", "override", bladestools.HandleFunc(func(context.Context, string) (string, error) {
		return `{"ok":true}`, nil
	}))
	registry := appcore.BuildToolRegistry(execCfg, nil, nil, extraTool)
	gotTool, err := registry.Resolve("bash")
	if err != nil {
		t.Fatalf("Resolve(bash): %v", err)
	}
	if gotTool.Description() != "override" {
		t.Fatalf("bash tool description = %q, want override", gotTool.Description())
	}
}

func TestLarkFactoryUsesEnvFallback(t *testing.T) {
	preserveRootState(t)

	oldAppID, hasAppID := os.LookupEnv("LARK_APP_ID")
	oldAppSecret, hasAppSecret := os.LookupEnv("LARK_APP_SECRET")
	t.Cleanup(func() {
		if hasAppID {
			_ = os.Setenv("LARK_APP_ID", oldAppID)
		} else {
			_ = os.Unsetenv("LARK_APP_ID")
		}
		if hasAppSecret {
			_ = os.Setenv("LARK_APP_SECRET", oldAppSecret)
		} else {
			_ = os.Unsetenv("LARK_APP_SECRET")
		}
	})

	if err := os.Unsetenv("LARK_APP_ID"); err != nil {
		t.Fatalf("unset LARK_APP_ID: %v", err)
	}
	if err := os.Unsetenv("LARK_APP_SECRET"); err != nil {
		t.Fatalf("unset LARK_APP_SECRET: %v", err)
	}

	if _, err := lark.NewFromConfig(config.LarkConfig{}, nil); err == nil || !strings.Contains(err.Error(), "LARK_APP_ID") {
		t.Fatalf("expected missing app id error, got %v", err)
	}

	if err := os.Setenv("LARK_APP_ID", "env-app-id"); err != nil {
		t.Fatalf("set LARK_APP_ID: %v", err)
	}
	if err := os.Setenv("LARK_APP_SECRET", "env-app-secret"); err != nil {
		t.Fatalf("set LARK_APP_SECRET: %v", err)
	}

	cfg := &config.Config{}
	cfg.Channels.Lark.Enabled = true
	cfg.Channels.Lark.EncryptKey = "enc"
	cfg.Channels.Lark.VerificationToken = "verify"
	cfg.Channels.Lark.Debug = true

	ch, err := lark.NewFromConfig(cfg.Channels.Lark, nil)
	if err != nil {
		t.Fatalf("build lark channel from env: %v", err)
	}
	if got, want := ch.Name(), "lark"; got != want {
		t.Fatalf("channel name = %q, want %q", got, want)
	}
}

func TestConfigureRootLoggerDebugAndRuntimeHelpers(t *testing.T) {
	ws := setupCommandWorkspace(t)
	writeProviderConfig(t, ws)

	now := time.Date(2026, time.March, 18, 9, 0, 0, 0, time.UTC)
	opts := workspaceOptions(ws)
	opts.Debug = true
	configureRootLoggerForOptions(now, opts)
	log.Print("debug branch check")

	logPath := filepath.Join(ws.Home(), "logs", "2026-03-18.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read debug log: %v", err)
	}
	if !strings.Contains(string(data), "debug branch check") {
		t.Fatalf("debug log = %q", string(data))
	}
	if got := os.Getenv("BLADES_DEBUG"); got != "1" {
		t.Fatalf("BLADES_DEBUG = %q, want 1", got)
	}
	log.SetOutput(io.Discard)

	cfg, err := loadConfigForOptions(workspaceOptions(ws))
	if err != nil {
		t.Fatalf("loadConfigForOptions: %v", err)
	}
	mem, err := memory.New(ws.MemoryPath(), ws.MemoriesDir(), ws.KnowledgesDir())
	if err != nil {
		t.Fatalf("memory.New: %v", err)
	}

	rt, err := appcore.BuildRuntime(cfg, ws, mem)
	if err != nil {
		t.Fatalf("buildRuntime: %v", err)
	}
	if rt.Runner == nil || rt.Cron == nil || rt.Sessions == nil {
		t.Fatalf("runtime not fully initialized: %+v", rt)
	}
	appcore.ConfigureRuntimeCron(rt, nil)

	loaded, err := loadRuntimeForOptions(workspaceOptions(ws))
	if err != nil {
		t.Fatalf("loadRuntime: %v", err)
	}
	if loaded.Runner == nil || loaded.Cron == nil || loaded.Sessions == nil {
		t.Fatalf("loaded runtime not fully initialized: %+v", loaded)
	}

	var chunks []string
	writer := textWriter{writeText: func(chunk string) {
		chunks = append(chunks, chunk)
	}}
	writer.WriteText("hello")
	writer.WriteEvent(channel.Event{})
	if strings.Join(chunks, "") != "hello" {
		t.Fatalf("textWriter chunks = %v", chunks)
	}

	if got := appcore.ToolEventKey(blades.ToolPart{ID: "known"}, 1); got != "known" {
		t.Fatalf("toolEventKey with ID = %q, want known", got)
	}
	if got := appcore.ToolEventKey(blades.ToolPart{Name: "search", Request: `{"q":"x"}`}, 2); got != "search\n#2" {
		t.Fatalf("toolEventKey without ID = %q", got)
	}

	handler := createStreamHandler(
		blades.NewRunner(&fixedReplyAgent{text: "streamed reply"}),
		session.NewManager(t.TempDir()),
		nil,
		nil,
		false,
	)
	reply, err := handler(context.Background(), "session-1", "hello", writer)
	if err != nil {
		t.Fatalf("stream handler: %v", err)
	}
	if reply != "streamed reply" {
		t.Fatalf("reply = %q, want %q", reply, "streamed reply")
	}
}

func TestRuntimeDependentCommandsReturnErrorsWithoutProviderConfig(t *testing.T) {
	ws := setupCommandWorkspace(t)
	cfgPath := filepath.Join(t.TempDir(), "empty-config.yaml")
	if err := os.WriteFile(cfgPath, []byte("providers: []\n"), 0o644); err != nil {
		t.Fatalf("write empty config: %v", err)
	}
	opts := workspaceOptionsWithConfig(ws, cfgPath)

	runCmd := newRunCmd()
	quietCommand(runCmd, opts)
	runCmd.SetArgs([]string{"--message", "hello"})
	if err := runCmd.Execute(); err == nil {
		t.Fatal("expected run command to fail without provider config")
	}

	cronRunCmd := newCronRunCmd()
	quietCommand(cronRunCmd, opts)
	cronRunCmd.SetArgs([]string{"job-id"})
	if err := cronRunCmd.Execute(); err == nil {
		t.Fatal("expected cron run command to fail without provider config")
	}

	chatCmd := newChatCmd()
	quietCommand(chatCmd, opts)
	chatCmd.SetArgs([]string{"--simple"})
	_ = captureStdout(t, func() {
		if err := chatCmd.Execute(); err == nil {
			t.Fatal("expected chat command to fail without provider config")
		}
	})

	oldForeground := daemonForeground
	daemonForeground = true
	t.Cleanup(func() {
		daemonForeground = oldForeground
	})

	daemonCmd := newDaemonCmd()
	quietCommand(daemonCmd, opts)
	if err := daemonCmd.Execute(); err == nil {
		t.Fatal("expected daemon command to fail without provider config")
	}
}

func TestLoadSkillsReturnsNilForEmptyOrMissingDir(t *testing.T) {
	ws := setupCommandWorkspace(t)

	skillList, err := appcore.LoadSkills(ws)
	if err != nil {
		t.Fatalf("loadSkills(existing): %v", err)
	}
	if skillList != nil {
		t.Fatalf("expected nil skill list for empty dir, got %v", skillList)
	}

	missing := workspace.NewWithWorkspace(filepath.Join(t.TempDir(), ".blades"), filepath.Join(t.TempDir(), "agent"))
	skillList, err = appcore.LoadSkills(missing)
	if err != nil {
		t.Fatalf("loadSkills(missing): %v", err)
	}
	if skillList != nil {
		t.Fatalf("expected nil skill list for missing dir, got %v", skillList)
	}
}

func TestDoctorCommandReportsMissingWorkspaceFiles(t *testing.T) {
	ws := setupCommandWorkspace(t)
	writeProviderConfig(t, ws)
	if err := os.Remove(ws.ToolsPath()); err != nil {
		t.Fatalf("remove TOOLS.md: %v", err)
	}

	var out strings.Builder
	cmd := newDoctorCmd()
	withCommandOptions(cmd, workspaceOptions(ws))
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "doctor found issues") {
		t.Fatalf("expected doctor failure, got %v", err)
	}
	if !strings.Contains(out.String(), "(missing)") {
		t.Fatalf("doctor output = %q", out.String())
	}
}

func TestChatCommandSimpleModeExitsOnEOF(t *testing.T) {
	ws := setupCommandWorkspace(t)
	cfgPath := filepath.Join(t.TempDir(), "provider-config.yaml")
	if err := os.WriteFile(cfgPath, []byte(`providers:
  - name: openai
    provider: openai
    models: [gpt-4o]
    apiKey: test-key
`), 0o644); err != nil {
		t.Fatalf("write provider config: %v", err)
	}
	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close stdin writer: %v", err)
	}
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = oldStdin
	})

	_ = captureStdout(t, func() {
		cmd := newChatCmd()
		withCommandOptions(cmd, workspaceOptionsWithConfig(ws, cfgPath))
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"--simple"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("chat simple mode: %v", err)
		}
	})
}

func TestDaemonCommandStartsAndStopsOnInterrupt(t *testing.T) {
	ws := setupCommandWorkspace(t)
	cfgPath := filepath.Join(t.TempDir(), "provider-config.yaml")
	if err := os.WriteFile(cfgPath, []byte(`providers:
  - name: openai
    provider: openai
    models: [gpt-4o]
    apiKey: test-key
`), 0o644); err != nil {
		t.Fatalf("write provider config: %v", err)
	}
	oldForeground := daemonForeground
	daemonForeground = false
	t.Cleanup(func() {
		daemonForeground = oldForeground
	})

	go func() {
		time.Sleep(50 * time.Millisecond)
		p, err := os.FindProcess(os.Getpid())
		if err == nil {
			_ = p.Signal(os.Interrupt)
		}
	}()

	cmd := newDaemonCmd()
	withCommandOptions(cmd, workspaceOptionsWithConfig(ws, cfgPath))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("daemon execute: %v", err)
	}
}

func TestResolveRunSessionIDUsesUniqueDefault(t *testing.T) {
	first := resolveRunSessionID("", func() time.Time { return time.Unix(0, 111) })
	second := resolveRunSessionID("", func() time.Time { return time.Unix(0, 222) })

	if first == "" || second == "" {
		t.Fatalf("expected non-empty session IDs, got %q and %q", first, second)
	}
	if first == second {
		t.Fatalf("expected unique default session IDs, got %q", first)
	}
	if got := resolveRunSessionID("custom", nil); got != "custom" {
		t.Fatalf("custom session ID = %q, want custom", got)
	}
}

func TestRunWithRuntimeCommandUsesCommandContext(t *testing.T) {
	ws := setupCommandWorkspace(t)
	writeProviderConfig(t, ws)

	parent, cancel := context.WithCancel(context.Background())
	cancel()

	cmd := &cobra.Command{}
	cmd.SetContext(parent)
	withCommandOptions(cmd, workspaceOptions(ws))

	called := false
	err := runWithRuntimeCommand(cmd, func(ctx context.Context, _ *cobra.Command, _ *appcore.Runtime) error {
		called = true
		return ctx.Err()
	})
	if !called {
		t.Fatal("expected runtime command callback to be called")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
}
