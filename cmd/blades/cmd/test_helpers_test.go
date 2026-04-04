package cmd

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	appcore "github.com/go-kratos/blades/cmd/blades/internal/app"
	"github.com/go-kratos/blades/cmd/blades/internal/workspace"
)

func setupCommandWorkspace(t *testing.T) *workspace.Workspace {
	t.Helper()
	preserveRootState(t)

	oldHome := os.Getenv("HOME")
	newHome := t.TempDir()
	if err := os.Setenv("HOME", newHome); err != nil {
		t.Fatalf("set HOME: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Setenv("HOME", oldHome)
	})

	ws := workspace.NewWithWorkspace(
		filepath.Join(newHome, ".blades"),
		filepath.Join(newHome, "agent"),
	)
	if err := ws.InitHome(); err != nil {
		t.Fatalf("InitHome: %v", err)
	}
	if err := ws.InitWorkspace(); err != nil {
		t.Fatalf("InitWorkspace: %v", err)
	}

	return ws
}

func workspaceOptions(ws *workspace.Workspace) appcore.Options {
	return appcore.Options{WorkspaceDir: ws.WorkspaceDir()}
}

func workspaceOptionsWithConfig(ws *workspace.Workspace, cfgPath string) appcore.Options {
	opts := workspaceOptions(ws)
	opts.ConfigPath = cfgPath
	return opts
}

func quietCommand(cmd *cobra.Command, opts appcore.Options) *cobra.Command {
	withCommandOptions(cmd, opts)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	return cmd
}

func writeProviderConfig(t *testing.T, ws *workspace.Workspace) {
	t.Helper()

	const cfg = `providers:
  - name: openai
    provider: openai
    models: [gpt-4o]
    apiKey: test-key
`
	if err := os.WriteFile(ws.ConfigPath(), []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	t.Cleanup(func() {
		os.Stdout = oldStdout
	})

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	os.Stdout = oldStdout

	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	return string(data)
}

func subcommandNames(cmd interface{ Commands() []*cobra.Command }) []string {
	names := make([]string, 0, len(cmd.Commands()))
	for _, sub := range cmd.Commands() {
		names = append(names, sub.Name())
	}
	return names
}

func mustSubcommand(t *testing.T, cmd interface{ Commands() []*cobra.Command }, name string) *cobra.Command {
	t.Helper()
	for _, sub := range cmd.Commands() {
		if sub.Name() == name {
			return sub
		}
	}
	t.Fatalf("subcommand %q not found", name)
	return nil
}
