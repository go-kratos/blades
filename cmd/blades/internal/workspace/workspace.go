// Package workspace manages the blades workspace directory structure.
package workspace

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

//go:embed templates
var templateFS embed.FS

// Workspace manages both the global home directory and the active workspace directory.
//
// Home directory (e.g., ~/.blades/):
//
//	├── agent.yaml           LLM provider, model, API key
//	├── mcp.json             global MCP server connections
//	├── cron.json            scheduled tasks
//	├── skills/              global skills (shared across workspaces)
//	├── sessions/            conversation session files
//	└── logs/                runtime logs
//
// Workspace directory (e.g., ~/my-agent/ or ~/.blades/workspace/):
//
//	├── AGENTS.md            behaviour rules
//	├── SOUL.md / USER.md / IDENTITY.md / MEMORY.md / HEARTBEAT.md / TOOLS.md
//	├── memory/              daily session logs
//	├── knowledges/          domain knowledge files
//	└── outputs/             agent-generated artifacts
type Workspace struct {
	home      string // global config directory (~/.blades)
	workspace string // agent operating directory (can be separate from home)
}

// New returns a Workspace with home directory at homeDir.
// The workspace directory defaults to homeDir/workspace.
// Use WithWorkspace to set a custom workspace directory.
func New(homeDir string) *Workspace {
	return &Workspace{
		home:      homeDir,
		workspace: filepath.Join(homeDir, "workspace"),
	}
}

// NewWithWorkspace returns a Workspace with separate home and workspace directories.
// homeDir: global config directory (e.g., ~/.blades)
// workspaceDir: agent operating directory (e.g., ~/my-agent)
func NewWithWorkspace(homeDir, workspaceDir string) *Workspace {
	if workspaceDir == "" {
		workspaceDir = filepath.Join(homeDir, "workspace")
	}
	return &Workspace{
		home:      homeDir,
		workspace: workspaceDir,
	}
}

// Home returns the global config directory (~/.blades).
func (w *Workspace) Home() string { return w.home }

// Root returns the workspace directory (for backward compatibility).
// Deprecated: Use WorkspaceDir() instead.
func (w *Workspace) Root() string { return w.workspace }

// WorkspaceDir returns the agent operating directory.
func (w *Workspace) WorkspaceDir() string { return w.workspace }

// IsCustomWorkspace returns true if the workspace is separate from home.
func (w *Workspace) IsCustomWorkspace() bool {
	defaultWs := filepath.Join(w.home, "workspace")
	return w.workspace != defaultWs
}

// --- Home-level paths (global config) ---

// ConfigPath returns the path to agent.yaml.
func (w *Workspace) ConfigPath() string { return filepath.Join(w.home, "agent.yaml") }

// MCPPath returns the path to the global mcp.json.
func (w *Workspace) MCPPath() string { return filepath.Join(w.home, "mcp.json") }

// CronStorePath returns the path to the cron jobs store file.
func (w *Workspace) CronStorePath() string { return filepath.Join(w.home, "cron.json") }

// SkillsDir returns the global skills directory.
func (w *Workspace) SkillsDir() string { return filepath.Join(w.home, "skills") }

// SessionsDir returns the sessions directory.
func (w *Workspace) SessionsDir() string { return filepath.Join(w.home, "sessions") }

// LogDir returns the log directory.
func (w *Workspace) LogDir() string { return filepath.Join(w.home, "logs") }

// --- Workspace-level paths ---

// AgentsPath returns the path to AGENTS.md.
func (w *Workspace) AgentsPath() string { return filepath.Join(w.workspace, "AGENTS.md") }

// SoulPath returns the path to SOUL.md.
func (w *Workspace) SoulPath() string { return filepath.Join(w.workspace, "SOUL.md") }

// IdentityPath returns the path to IDENTITY.md.
func (w *Workspace) IdentityPath() string { return filepath.Join(w.workspace, "IDENTITY.md") }

// UserPath returns the path to USER.md.
func (w *Workspace) UserPath() string { return filepath.Join(w.workspace, "USER.md") }

// MemoryPath returns the path to MEMORY.md.
func (w *Workspace) MemoryPath() string { return filepath.Join(w.workspace, "MEMORY.md") }

// HeartbeatPath returns the path to HEARTBEAT.md.
func (w *Workspace) HeartbeatPath() string { return filepath.Join(w.workspace, "HEARTBEAT.md") }

// ToolsPath returns the path to TOOLS.md.
func (w *Workspace) ToolsPath() string { return filepath.Join(w.workspace, "TOOLS.md") }

// MemoriesDir returns the daily session logs directory.
func (w *Workspace) MemoriesDir() string { return filepath.Join(w.workspace, "memory") }

// KnowledgesDir returns the domain knowledge directory.
func (w *Workspace) KnowledgesDir() string { return filepath.Join(w.workspace, "knowledges") }

// OutputsDir returns the agent outputs directory.
func (w *Workspace) OutputsDir() string { return filepath.Join(w.workspace, "outputs") }

// DailyLogPath returns the path to today's session log.
func (w *Workspace) DailyLogPath() string {
	return filepath.Join(w.MemoriesDir(), time.Now().Format("2006-01-02")+".md")
}

// Init creates both the home and workspace directory structures.
// If a file already exists it is left untouched.
func (w *Workspace) Init() error {
	// Create home directories
	homeDirs := []string{
		w.home,
		w.SkillsDir(),
		w.SessionsDir(),
		w.LogDir(),
	}
	for _, d := range homeDirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("workspace: mkdir %s: %w", d, err)
		}
	}

	// Create workspace directories
	workspaceDirs := []string{
		w.workspace,
		w.MemoriesDir(),
		w.KnowledgesDir(),
		w.OutputsDir(),
	}
	for _, d := range workspaceDirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("workspace: mkdir %s: %w", d, err)
		}
	}

	// Copy home-level templates (agent.yaml, mcp.json, skills/)
	if err := w.copyTemplates("templates", w.home, true); err != nil {
		return err
	}

	// Copy workspace-level templates (workspace/* -> workspace dir)
	if err := w.copyTemplates("templates/workspace", w.workspace, false); err != nil {
		return err
	}

	return nil
}

// copyTemplates copies files from embedded templates to target directory.
// If homeLevel is true, it skips the "workspace" subdirectory (handled separately).
func (w *Workspace) copyTemplates(srcRoot, dstRoot string, homeLevel bool) error {
	return fs.WalkDir(templateFS, srcRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(srcRoot, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		// Skip workspace subdirectory when copying home-level templates
		if homeLevel && (rel == "workspace" || strings.HasPrefix(rel, "workspace/") || strings.HasPrefix(rel, "workspace\\")) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		dst := filepath.Join(dstRoot, rel)

		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}

		// Never overwrite user customisations
		if _, err := os.Stat(dst); err == nil {
			return nil
		}

		data, err := templateFS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("workspace: read template %s: %w", path, err)
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return fmt.Errorf("workspace: write %s: %w", dst, err)
		}
		return nil
	})
}

// InitHome creates only the home directory structure (no workspace).
// Useful when initializing a new workspace that references existing home.
func (w *Workspace) InitHome() error {
	dirs := []string{
		w.home,
		w.SkillsDir(),
		w.SessionsDir(),
		w.LogDir(),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("workspace: mkdir %s: %w", d, err)
		}
	}
	return w.copyTemplates("templates", w.home, true)
}

// InitWorkspace creates only the workspace directory structure.
func (w *Workspace) InitWorkspace() error {
	dirs := []string{
		w.workspace,
		w.MemoriesDir(),
		w.KnowledgesDir(),
		w.OutputsDir(),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("workspace: mkdir %s: %w", d, err)
		}
	}
	return w.copyTemplates("templates/workspace", w.workspace, false)
}

// Load validates that the workspace exists.
func (w *Workspace) Load() error {
	if _, err := os.Stat(w.workspace); err != nil {
		return fmt.Errorf("workspace %q does not exist; run 'blades init' first", w.workspace)
	}
	return nil
}

// LoadHome validates that the home directory exists.
func (w *Workspace) LoadHome() error {
	if _, err := os.Stat(w.home); err != nil {
		return fmt.Errorf("home %q does not exist; run 'blades init' first", w.home)
	}
	return nil
}

// ReadFile reads a file within the workspace directory by relative name.
// Returns "" if the file does not exist (not an error).
func (w *Workspace) ReadFile(name string) (string, error) {
	p := filepath.Join(w.workspace, name)
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("workspace: read %s: %w", name, err)
	}
	return string(b), nil
}
