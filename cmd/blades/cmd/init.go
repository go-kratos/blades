package cmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	appcore "github.com/go-kratos/blades/cmd/blades/internal/app"
	"github.com/spf13/cobra"

	"github.com/go-kratos/blades/cmd/blades/internal/workspace"
)

func newInitCmd() *cobra.Command {
	var gitInit bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialise a workspace directory",
		Long: `Initialise blades configuration and workspace directories.

By default, home-level files are created in ~/.blades and workspace files in ~/.blades/workspace.
Use --workspace to create workspace files in a separate directory.

		Examples:
  blades init                           # Home in ~/.blades, workspace in ~/.blades/workspace
  blades init --workspace ~/my-agent    # Config in ~/.blades, workspace in ~/my-agent`,
		RunE: func(cmd *cobra.Command, args []string) error {
			homeDir, workspaceDir, isCustomWorkspace := initPathsForOptions(commandOptions(cmd))

			// Create workspace with separated home and workspace directories
			ws := workspace.NewWithWorkspace(homeDir, workspaceDir)

			// Initialize home directory first (config, global skills, sessions)
			if err := ws.InitHome(); err != nil {
				return err
			}
			printCommandf(cmd, "✓ Home initialised: %s\n", homeDir)

			// Initialize workspace directory
			if err := ws.InitWorkspace(); err != nil {
				return err
			}
			printCommandf(cmd, "✓ Workspace initialised: %s\n", workspaceDir)

			if gitInit {
				if err := initGitWithIO(workspaceDir, commandOut(cmd), commandErr(cmd)); err != nil {
					warnCommandf(cmd, "warn: git: %v\n", err)
				}
			}

			printCommandln(cmd, renderInitSummary(ws, workspaceDir, isCustomWorkspace))
			return nil
		},
	}
	cmd.Flags().BoolVar(&gitInit, "git", false, "run git init in the workspace")
	return cmd
}

// resolveInitPaths determines the home and workspace directories for init command.
// Returns (homeDir, workspaceDir, isCustomWorkspace).
//
// Logic:
//   - Home is always ~/.blades
//   - Workspace is --workspace flag value, or ~/.blades/workspace if not specified
func resolveInitPaths() (homeDir, workspaceDir string, isCustomWorkspace bool) {
	return initPathsForOptions(appcore.Options{})
}

// initGit initialises a git repository in dir and creates a .gitignore.
func initGit(dir string) error {
	return initGitWithIO(dir, os.Stdout, os.Stderr)
}

func renderInitSummary(ws *workspace.Workspace, workspaceDir string, isCustomWorkspace bool) string {
	summary := fmt.Sprintf(`
Next steps:
  1. Edit %s — set providers, API keys, and defaults
  2. Edit %s — define model/workflow/tools for this workspace
  3. Edit %s — define startup rules and file-loading behaviour
  4. Edit %s and %s — define the assistant and the user
  5. Run 'blades chat' to start a conversation`,
		ws.ConfigPath(),
		ws.AgentPath(),
		ws.AgentsPath(),
		ws.SoulPath(),
		ws.UserPath(),
	)
	if isCustomWorkspace {
		summary += fmt.Sprintf("\n\nNote: Using custom workspace at %s", workspaceDir)
	}
	return summary
}

func initGitWithIO(dir string, stdout, stderr io.Writer) error {
	if err := ensureGitRepo(dir, runGitInit); err != nil {
		return err
	}
	if err := ensureGitignore(dir); err != nil {
		fmt.Fprintf(stderr, "warn: failed to write .gitignore: %v\n", err)
	}
	fmt.Fprintf(stdout, "✓ git init: %s\n", dir)
	fmt.Fprint(stdout, renderGitBackupHint(dir))
	return nil
}

func ensureGitRepo(dir string, run func(string) ([]byte, error)) error {
	if run == nil {
		run = runGitInit
	}
	if out, err := run(dir); err != nil {
		return fmt.Errorf("git init: %w\n%s", err, out)
	}
	return nil
}

func runGitInit(dir string) ([]byte, error) {
	c := exec.Command("git", "init")
	c.Dir = dir
	return c.CombinedOutput()
}

func ensureGitignore(dir string) error {
	ignPath := filepath.Join(dir, ".gitignore")
	if _, err := os.Stat(ignPath); os.IsNotExist(err) {
		return os.WriteFile(ignPath, []byte("*.tmp\n"), 0o644)
	}
	return nil
}

func renderGitBackupHint(dir string) string {
	return fmt.Sprintf(
		"  Tip: automate git backups with:\n"+
			"  blades cron add --name git-backup --cron \"0 */6 * * *\" "+
			"--command \"git -C %s add -A && git -C %s commit -m 'backup' --allow-empty && git -C %s push\"\n",
		dir, dir, dir,
	)
}
