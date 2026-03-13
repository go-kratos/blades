// Package cmd defines the blades CLI command tree.
package cmd

import (
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

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
	├── log/                 runtime logs (YYYY-MM-DD.log)
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
			configureRootLogger(time.Now())
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

func configureRootLogger(now time.Time) {
	if flagDebug {
		log.SetFlags(log.LstdFlags | log.Lshortfile)
		_ = os.Setenv("BLADES_DEBUG", "1")
	} else {
		log.SetFlags(log.LstdFlags)
	}

	f, path, err := openRootLogFile(now)
	if err != nil {
		log.SetOutput(os.Stderr)
		if path != "" {
			log.Printf("blades: use stderr logging (open %s failed): %v", path, err)
		} else {
			log.Printf("blades: use stderr logging: %v", err)
		}
		return
	}

	if flagDebug {
		log.SetOutput(io.MultiWriter(os.Stderr, f))
		return
	}
	log.SetOutput(f)
}

func openRootLogFile(now time.Time) (*os.File, string, error) {
	root := resolveLogRootDir()
	if root == "" {
		return nil, "", errors.New("workspace root is empty")
	}

	logDir := filepath.Join(root, "log")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, logDir, err
	}

	logPath := filepath.Join(logDir, now.Format("2006-01-02")+".log")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, logPath, err
	}
	return f, logPath, nil
}

func resolveLogRootDir() string {
	cfg, err := loadConfigForFlags()
	if err == nil && cfg != nil && cfg.Workspace != "" {
		return cfg.Workspace
	}

	if flagWorkspace != "" {
		return flagWorkspace
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".blades")
}
