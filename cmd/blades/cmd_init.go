package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/go-kratos/blades/cmd/blades/internal/workspace"
)

func newInitCmd() *cobra.Command {
	var gitInit bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialise a workspace directory",
		Example: `  blades init
  blades init --workspace ~/my-agent
  blades init --git`,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := flagWorkspace
			if dir == "" {
				home, err := os.UserHomeDir()
				if err != nil {
					return err
				}
				dir = filepath.Join(home, ".blades")
			}

			ws := workspace.New(dir)
			if err := ws.Init(); err != nil {
				return err
			}
			fmt.Printf("✓ Workspace initialised: %s\n", dir)

			if gitInit {
				if err := initGit(dir); err != nil {
					fmt.Fprintf(os.Stderr, "warn: git: %v\n", err)
				}
			}

			fmt.Printf("\nNext steps:\n")
			fmt.Printf("  1. Edit %s — set your LLM provider and API key\n", ws.ConfigPath())
			fmt.Printf("  2. Edit %s — define startup rules and file-loading behaviour\n", ws.AgentsPath())
			fmt.Printf("  3. Edit %s and %s — define the assistant and the user\n", ws.SoulPath(), ws.UserPath())
			fmt.Printf("  4. Run 'blades chat' to start a conversation\n")
			return nil
		},
	}
	cmd.Flags().BoolVar(&gitInit, "git", false, "run git init in the workspace")
	return cmd
}

// initGit initialises a git repository in dir and creates a .gitignore.
// Cron-based git backups are added separately via 'blades cron add'.
func initGit(dir string) error {
	c := exec.Command("git", "init")
	c.Dir = dir
	if out, err := c.CombinedOutput(); err != nil {
		return fmt.Errorf("git init: %w\n%s", err, out)
	}

	ignPath := filepath.Join(dir, ".gitignore")
	if _, err := os.Stat(ignPath); os.IsNotExist(err) {
		_ = os.WriteFile(ignPath, []byte("sessions/\n*.tmp\n"), 0o644)
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
