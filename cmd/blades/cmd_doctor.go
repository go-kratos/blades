package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/go-kratos/blades/cmd/blades/internal/cron"
	"github.com/go-kratos/blades/cmd/blades/internal/workspace"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check workspace health",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfigForFlags()
			if err != nil {
				return err
			}
			ws := workspace.New(cfg.Workspace)

			ok := true
			check := func(label, path string) {
				if _, err := os.Stat(path); err == nil {
					fmt.Printf("✓ %-30s %s\n", label, path)
				} else {
					fmt.Printf("✗ %-30s %s (missing)\n", label, path)
					ok = false
				}
			}

			check("Workspace", ws.Root())
			check("config.yaml", ws.ConfigPath())
			check("AGENTS.md", ws.AgentsPath())
			check("SOUL.md", ws.SoulPath())
			check("IDENTITY.md", ws.IdentityPath())
			check("USER.md", ws.UserPath())
			check("MEMORY.md", ws.MemoryPath())
			check("TOOLS.md", ws.ToolsPath())
			check("HEARTBEAT.md", ws.HeartbeatPath())
			check("skills/", ws.SkillsDir())
			check("memories/", ws.MemoriesDir())

			// Cron health.
			storePath := ws.CronStorePath()
			if _, err := os.Stat(storePath); err == nil {
				cronSvc := cron.NewService(storePath, nil)
				jobs := cronSvc.ListJobs(false)
				stale := cronSvc.StaleJobs(26 * time.Hour)
				fmt.Printf("✓ %-30s %d jobs, %d stale\n", "Cron", len(jobs), len(stale))
				for _, j := range stale {
					fmt.Printf("  ✗ stale: %s\n", cron.FormatJob(j))
					ok = false
				}
			} else {
				fmt.Printf("  Cron: no cron.json (add jobs with 'blades cron add')\n")
			}

			if !ok {
				fmt.Println("\nRun 'blades init' to create missing files.")
			}
			return nil
		},
	}
}
