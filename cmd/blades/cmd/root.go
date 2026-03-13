// Package cmd defines the blades CLI command tree.
package cmd

import (
	"log"
	"os"

	"github.com/spf13/cobra"
)

// Global flags registered on the root command and read by loadConfigForFlags.
var (
	flagConfig    string
	flagWorkspace string
	flagDebug     bool
)

// Execute runs the root command and exits the process on error.
func Execute() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "blades",
		Short: "A file-system-based local AI agent",
		Long: `blades — personal AI assistant backed by your local workspace (~/.blades).

Layout:
  ~/.blades/
  ├── config.yaml          LLM provider, model, API key
  ├── mcp.json             global MCP server connections
  ├── skills/              global skills (all workspaces)
  ├── sessions/            conversation history
  └── workspace/           agent operating directory
      ├── AGENTS.md        behaviour rules (loaded at startup)
      ├── SOUL.md / USER.md / IDENTITY.md / MEMORY.md
      ├── mcp.json         workspace-level MCP servers
      ├── skills/          workspace-local skills
      ├── memory/          daily session logs
      ├── knowledges/      domain reference files
      └── outputs/         agent-generated artifacts`,
		SilenceUsage: true,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if flagDebug {
				log.SetFlags(log.LstdFlags | log.Lshortfile)
				os.Setenv("BLADES_DEBUG", "1")
			} else {
				log.SetOutput(os.Stderr)
			}
		},
	}

	root.PersistentFlags().StringVar(&flagConfig, "config", "", "path to config.yaml (default: ~/.blades/config.yaml)")
	root.PersistentFlags().StringVar(&flagWorkspace, "workspace", "", "blades root directory (default: ~/.blades)")
	root.PersistentFlags().BoolVar(&flagDebug, "debug", false, "enable verbose debug logging")

	root.AddCommand(
		newInitCmd(),
		newChatCmd(),
		newRunCmd(),
		newMemoryCmd(),
		newCronCmd(),
		newDaemonCmd(),
		newDoctorCmd(),
	)
	return root
}
