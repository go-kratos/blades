// Package workspace manages the blades workspace directory structure.
package workspace

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

//go:embed templates
var templateFS embed.FS

// Workspace represents a blades root directory (~/.blades).
//
// Directory layout:
//
//	~/.blades/
//	├── config.yaml
//	├── mcp.json          global MCP server config
//	├── cron.json
//	├── skills/           global skills (shared across workspaces)
//	├── sessions/         conversation session files
//	└── workspace/        agent operating directory
//	    ├── AGENTS.md
//	    ├── SOUL.md
//	    ├── IDENTITY.md
//	    ├── USER.md
//	    ├── MEMORY.md
//	    ├── HEARTBEAT.md
//	    ├── TOOLS.md
//	    ├── mcp.json      workspace-level MCP server config
//	    ├── skills/       workspace-local skills
//	    ├── memory/       daily session logs
//	    ├── knowledges/   domain knowledge files
//	    └── outputs/      agent-generated artifacts
type Workspace struct {
	root string
}

// New returns a Workspace rooted at dir. It does NOT create any files;
// call Init() for first-time setup or Load() to validate an existing workspace.
func New(dir string) *Workspace {
	return &Workspace{root: dir}
}

// Root returns the blades root directory (~/.blades).
func (w *Workspace) Root() string { return w.root }

// WorkspaceDir returns the agent operating directory (<root>/workspace).
func (w *Workspace) WorkspaceDir() string { return filepath.Join(w.root, "workspace") }

// --- Root-level paths ---

// ConfigPath returns the path to config.yaml.
func (w *Workspace) ConfigPath() string { return filepath.Join(w.root, "config.yaml") }

// MCPPath returns the path to the global mcp.json.
func (w *Workspace) MCPPath() string { return filepath.Join(w.root, "mcp.json") }

// CronStorePath returns the path to the cron jobs store file.
func (w *Workspace) CronStorePath() string { return filepath.Join(w.root, "cron.json") }

// SkillsDir returns the global skills directory (<root>/skills).
func (w *Workspace) SkillsDir() string { return filepath.Join(w.root, "skills") }

// SessionsDir returns the sessions directory (<root>/sessions).
func (w *Workspace) SessionsDir() string { return filepath.Join(w.root, "sessions") }

// --- Workspace-level paths ---

// AgentsPath returns the path to workspace/AGENTS.md.
func (w *Workspace) AgentsPath() string { return filepath.Join(w.WorkspaceDir(), "AGENTS.md") }

// SoulPath returns the path to workspace/SOUL.md.
func (w *Workspace) SoulPath() string { return filepath.Join(w.WorkspaceDir(), "SOUL.md") }

// IdentityPath returns the path to workspace/IDENTITY.md.
func (w *Workspace) IdentityPath() string { return filepath.Join(w.WorkspaceDir(), "IDENTITY.md") }

// UserPath returns the path to workspace/USER.md.
func (w *Workspace) UserPath() string { return filepath.Join(w.WorkspaceDir(), "USER.md") }

// MemoryPath returns the path to workspace/MEMORY.md.
func (w *Workspace) MemoryPath() string { return filepath.Join(w.WorkspaceDir(), "MEMORY.md") }

// HeartbeatPath returns the path to workspace/HEARTBEAT.md.
func (w *Workspace) HeartbeatPath() string { return filepath.Join(w.WorkspaceDir(), "HEARTBEAT.md") }

// ToolsPath returns the path to workspace/TOOLS.md.
func (w *Workspace) ToolsPath() string { return filepath.Join(w.WorkspaceDir(), "TOOLS.md") }

// WorkspaceMCPPath returns the path to workspace/mcp.json.
func (w *Workspace) WorkspaceMCPPath() string { return filepath.Join(w.WorkspaceDir(), "mcp.json") }

// WorkspaceSkillsDir returns the workspace-local skills directory (workspace/skills).
func (w *Workspace) WorkspaceSkillsDir() string { return filepath.Join(w.WorkspaceDir(), "skills") }

// MemoriesDir returns the daily session logs directory (workspace/memory).
func (w *Workspace) MemoriesDir() string { return filepath.Join(w.WorkspaceDir(), "memory") }

// KnowledgesDir returns the domain knowledge directory (workspace/knowledges).
func (w *Workspace) KnowledgesDir() string { return filepath.Join(w.WorkspaceDir(), "knowledges") }

// OutputsDir returns the agent outputs directory (workspace/outputs).
func (w *Workspace) OutputsDir() string { return filepath.Join(w.WorkspaceDir(), "outputs") }

// DailyLogPath returns the path to today's session log (workspace/memory/YYYY-MM-DD.md).
func (w *Workspace) DailyLogPath() string {
	return filepath.Join(w.MemoriesDir(), time.Now().Format("2006-01-02")+".md")
}

// Init creates the workspace directory structure and template files.
// If a file already exists it is left untouched.
func (w *Workspace) Init() error {
	dirs := []string{
		w.root,
		w.WorkspaceDir(),
		w.SkillsDir(),
		w.SessionsDir(),
		w.WorkspaceSkillsDir(),
		w.MemoriesDir(),
		w.KnowledgesDir(),
		w.OutputsDir(),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("workspace: mkdir %s: %w", d, err)
		}
	}

	// Recursively copy template files (skip existing files).
	// Templates mirror the target layout: templates/<rel> → <root>/<rel>.
	err := fs.WalkDir(templateFS, "templates", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel("templates", path)
		if err != nil {
			return err
		}
		dst := filepath.Join(w.root, rel)

		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}

		// Never overwrite user customisations.
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
	return err
}

// Load validates that the workspace exists and has required files.
func (w *Workspace) Load() error {
	if _, err := os.Stat(w.root); err != nil {
		return fmt.Errorf("workspace %q does not exist; run 'blades init' first", w.root)
	}
	return nil
}

// ReadFile reads a file within the workspace operating directory by relative name.
// Returns "" if the file does not exist (not an error).
func (w *Workspace) ReadFile(name string) (string, error) {
	p := filepath.Join(w.WorkspaceDir(), name)
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("workspace: read %s: %w", name, err)
	}
	return string(b), nil
}
