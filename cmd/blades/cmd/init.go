package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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
			homeDir, workspaceDir, isCustomWorkspace := resolveInitPaths()

			// Create workspace with separated home and workspace directories
			ws := workspace.NewWithWorkspace(homeDir, workspaceDir)

			// Initialize home directory first (config, global skills, sessions)
			if err := ws.InitHome(); err != nil {
				return err
			}
			fmt.Printf("✓ Home initialised: %s\n", homeDir)

			// Initialize workspace directory
			if err := ws.InitWorkspace(); err != nil {
				return err
			}
			fmt.Printf("✓ Workspace initialised: %s\n", workspaceDir)

			if gitInit {
				if err := initGit(workspaceDir); err != nil {
					fmt.Fprintf(os.Stderr, "warn: git: %v\n", err)
				}
			}

			fmt.Printf("\nNext steps:\n")
			fmt.Printf("  1. Edit %s — set providers, API keys, and defaults\n", ws.ConfigPath())
			fmt.Printf("  2. Edit %s — define model/workflow/tools for this workspace\n", ws.AgentPath())
			fmt.Printf("  3. Edit %s — define startup rules and file-loading behaviour\n", ws.AgentsPath())
			fmt.Printf("  4. Edit %s and %s — define the assistant and the user\n", ws.SoulPath(), ws.UserPath())
			fmt.Printf("  5. Run 'blades chat' to start a conversation\n")
			if isCustomWorkspace {
				fmt.Printf("\nNote: Using custom workspace at %s\n", workspaceDir)
			}
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
	homeDir = bladesHomeDir()

	if flagWorkspace != "" {
		// Expand ~ in workspace path
		workspaceDir = expandTilde(flagWorkspace)
		isCustomWorkspace = true
	} else {
		workspaceDir = filepath.Join(homeDir, "workspace")
		isCustomWorkspace = false
	}

	return homeDir, workspaceDir, isCustomWorkspace
}

// expandTilde expands ~ to the user's home directory.
func expandTilde(path string) string {
	if strings.HasPrefix(path, "~/") || path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		if path == "~" {
			return home
		}
		return filepath.Join(home, path[2:])
	}
	return path
}

// initGit initialises a git repository in dir and creates a .gitignore.
func initGit(dir string) error {
	c := exec.Command("git", "init")
	c.Dir = dir
	if out, err := c.CombinedOutput(); err != nil {
		return fmt.Errorf("git init: %w\n%s", err, out)
	}

	ignPath := filepath.Join(dir, ".gitignore")
	if _, err := os.Stat(ignPath); os.IsNotExist(err) {
		if err := os.WriteFile(ignPath, []byte("*.tmp\n"), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "warn: failed to write .gitignore: %v\n", err)
		}
	}

	fmt.Printf("✓ git init: %s\n", dir)
	fmt.Printf(
		"  Tip: automate git backups with:\n"+
			"  blades cron add --name git-backup --cron \"0 */6 * * *\" "+
			"--command \"git -C %s add -A && git -C %s commit -m 'backup' --allow-empty && git -C %s push\"\n",
		dir, dir, dir,
	)
	return nil
}
