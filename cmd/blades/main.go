// Command blades is a file-system-based local AI agent CLI.
package main

import (
	"log"
	"os"

	"github.com/spf13/cobra"
)

// Global flags available to all subcommands.
var (
	flagConfig    string
	flagWorkspace string
	flagDebug     bool
)

func main() {
	root := &cobra.Command{
		Use:   "blades",
		Short: "A file-system-based local AI agent",
		Long: `blades — personal AI assistant backed by your local workspace (~/.blades).

Commands:
  init     Initialise a workspace directory
  chat     Start an interactive conversation
  run      Execute a single agent turn
  memory   Manage long-term memory
  cron     Manage scheduled jobs
  daemon   Run the cron scheduler as a long-lived process
  doctor   Check workspace health`,
		SilenceUsage: true,
	}

	root.PersistentFlags().StringVar(&flagConfig, "config", "", "path to config.yaml (default: workspace/config.yaml)")
	root.PersistentFlags().StringVar(&flagWorkspace, "workspace", "", "workspace root (default: ~/.blades)")
	root.PersistentFlags().BoolVar(&flagDebug, "debug", false, "enable verbose debug logging")

	root.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		if flagDebug {
			log.SetFlags(log.LstdFlags | log.Lshortfile)
			os.Setenv("BLADES_DEBUG", "1")
		} else {
			log.SetOutput(os.Stderr)
		}
	}

	root.AddCommand(
		newInitCmd(),
		newChatCmd(),
		newRunCmd(),
		newMemoryCmd(),
		newCronCmd(),
		newDaemonCmd(),
		newDoctorCmd(),
	)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
