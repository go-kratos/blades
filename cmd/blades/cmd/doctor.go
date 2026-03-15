package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/go-kratos/blades/cmd/blades/internal/config"
	"github.com/go-kratos/blades/cmd/blades/internal/cron"
	bladesmcp "github.com/go-kratos/blades/contrib/mcp"
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
			ws := workspaceForConfig(cfg)
			if err := ws.Load(); err != nil {
				return err
			}

			ok := true
			check := func(label, path string) {
				if _, err := os.Stat(path); err == nil {
					fmt.Printf("✓ %-30s %s\n", label, path)
				} else {
					fmt.Printf("✗ %-30s %s (missing)\n", label, path)
					ok = false
				}
			}

			check("Blades home (root)", ws.Home())
			check("Workspace directory", ws.WorkspaceDir())
			check("config.yaml", ws.ConfigPath())
			check("workspace/AGENTS.md", ws.AgentsPath())
			check("workspace/SOUL.md", ws.SoulPath())
			check("workspace/IDENTITY.md", ws.IdentityPath())
			check("workspace/USER.md", ws.UserPath())
			check("workspace/MEMORY.md", ws.MemoryPath())
			check("workspace/TOOLS.md", ws.ToolsPath())
			check("workspace/HEARTBEAT.md", ws.HeartbeatPath())
			check("skills/", ws.SkillsDir())
			check("workspace/skills/", ws.WorkspaceSkillsDir())
			check("workspace/memory/", ws.MemoriesDir())

			storePath := ws.CronStorePath()
			if _, err := os.Stat(storePath); err == nil {
				cronSvc := cron.NewService(storePath, nil)
				jobs, err := cronSvc.ListJobs(false)
				if err != nil {
					fmt.Printf("✗ %-30s %v\n", "Cron", err)
					ok = false
				} else {
					stale := cronSvc.StaleJobs(26 * time.Hour)
					fmt.Printf("✓ %-30s %d jobs, %d stale\n", "Cron", len(jobs), len(stale))
					for _, j := range stale {
						fmt.Printf("  ✗ stale: %s\n", cron.FormatJob(j))
						ok = false
					}
				}
			} else {
				fmt.Printf("  Cron: no cron.json (add jobs with 'blades cron add')\n")
			}

			// Check MCP servers
			var allMCP []bladesmcp.ClientConfig
			for _, path := range []string{ws.MCPPath(), ws.WorkspaceMCPPath()} {
				servers, err := config.LoadMCPFile(path)
				if err != nil {
					fmt.Printf("✗ %-30s %v\n", "mcp: "+path, err)
					ok = false
					continue
				}
				allMCP = append(allMCP, servers...)
			}
			if len(allMCP) == 0 {
				fmt.Printf("  MCP: no servers configured\n")
			} else {
				ctx := context.Background()
				for _, mc := range allMCP {
					client, err := bladesmcp.NewClient(mc)
					if err != nil {
						fmt.Printf("✗ %-30s %v\n", "mcp: "+mc.Name, err)
						ok = false
						continue
					}
					if err := client.Connect(ctx); err != nil {
						fmt.Printf("✗ %-30s %v\n", "mcp: "+mc.Name, err)
						ok = false
						_ = client.Close()
					} else {
						tools, err := client.ListTools(ctx)
						if err != nil {
							fmt.Printf("✗ %-30s %v\n", "mcp: "+mc.Name+" (ListTools)", err)
							ok = false
						} else {
							fmt.Printf("✓ %-30s %d tools\n", "mcp: "+mc.Name, len(tools))
						}
						_ = client.Close()
					}
				}
			}

			if !ok {
				fmt.Println("\nRun 'blades init' to create missing files.")
			}
			return nil
		},
	}
}
