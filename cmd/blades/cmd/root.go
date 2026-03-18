// Package cmd defines the blades CLI command tree.
package cmd

import (
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	appcore "github.com/go-kratos/blades/cmd/blades/internal/app"
	"github.com/spf13/cobra"
)

type rootLoggerDeps struct {
	stderr   io.Writer
	setenv   func(string, string) error
	unsetenv func(string) error
}

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
		Long: `blades — personal AI assistant with configurable workspace.

Home Directory (~/.blades/):
	├── config.yaml          provider credentials and defaults
  ├── agent.yaml           agent recipe (model ref, workflow, tools)
  ├── skills/              global skills (shared across workspaces)
  ├── sessions/            conversation history
  └── logs/                runtime logs (YYYY-MM-DD.log)

Workspace Directory (configurable, default: ~/.blades/workspace/):
	├── AGENTS.md            behaviour rules (loaded at startup)
  ├── SOUL.md / USER.md / IDENTITY.md / MEMORY.md
  ├── memory/              daily session logs
  ├── knowledges/          domain reference files
  └── outputs/             agent-generated artifacts

Use --workspace to specify a custom workspace directory (e.g., ~/my-agent).`,
		SilenceUsage: true,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			configureRootLoggerForOptions(time.Now(), commandOptions(cmd))
		},
	}

	root.PersistentFlags().String("config", "", "path to config.yaml (default: ~/.blades/config.yaml)")
	root.PersistentFlags().String("workspace", "", "workspace directory (default: ~/.blades/workspace)")
	root.PersistentFlags().Bool("debug", false, "enable verbose debug logging")

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
	configureRootLoggerForOptions(now, appcore.Options{})
}

func configureRootLoggerForOptions(now time.Time, opts appcore.Options) {
	configureRootLoggerWithDeps(now, opts, rootLoggerDeps{
		stderr:   os.Stderr,
		setenv:   os.Setenv,
		unsetenv: os.Unsetenv,
	})
}

func configureRootLoggerWithDeps(now time.Time, opts appcore.Options, deps rootLoggerDeps) {
	stderr := deps.stderr
	if stderr == nil {
		stderr = io.Discard
	}
	if opts.Debug {
		log.SetFlags(log.LstdFlags | log.Lshortfile)
		if deps.setenv != nil {
			_ = deps.setenv("BLADES_DEBUG", "1")
		}
	} else {
		log.SetFlags(log.LstdFlags)
		if deps.unsetenv != nil {
			_ = deps.unsetenv("BLADES_DEBUG")
		}
	}

	f, path, err := openRootLogFileForOptions(now, opts)
	if err != nil {
		log.SetOutput(stderr)
		if path != "" {
			log.Printf("blades: use stderr logging (open %s failed): %v", path, err)
		} else {
			log.Printf("blades: use stderr logging: %v", err)
		}
		return
	}

	if opts.Debug {
		log.SetOutput(io.MultiWriter(stderr, f))
		return
	}
	log.SetOutput(f)
}

func openRootLogFile(now time.Time) (*os.File, string, error) {
	return openRootLogFileForOptions(now, appcore.Options{})
}

func openRootLogFileForOptions(now time.Time, opts appcore.Options) (*os.File, string, error) {
	root := resolveLogRootDirForOptions(opts)
	if root == "" {
		return nil, "", errors.New("workspace root is empty")
	}

	logDir := filepath.Join(root, "logs")
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

// resolveLogRootDir determines the home directory for log file placement.
// Logs are always stored in ~/.blades/logs (the home directory, not workspace).
func resolveLogRootDir() string {
	return resolveLogRootDirForOptions(appcore.Options{})
}

func resolveLogRootDirForOptions(opts appcore.Options) string {
	return bootstrapFromOptions(opts).LogRootDir()
}
