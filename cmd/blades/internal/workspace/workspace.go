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

// Workspace represents a blades workspace directory.
type Workspace struct {
	root string
}

// New returns a Workspace rooted at dir. It does NOT create any files;
// call Init() for first-time setup or Load() to validate an existing workspace.
func New(dir string) *Workspace {
	return &Workspace{root: dir}
}

// Root returns the workspace root directory.
func (w *Workspace) Root() string { return w.root }

// ConfigPath returns the path to config.yaml.
func (w *Workspace) ConfigPath() string { return filepath.Join(w.root, "config.yaml") }

// SoulPath returns the path to SOUL.md.
func (w *Workspace) SoulPath() string { return filepath.Join(w.root, "SOUL.md") }

// IdentityPath returns the path to IDENTITY.md.
func (w *Workspace) IdentityPath() string { return filepath.Join(w.root, "IDENTITY.md") }

// AgentsPath returns the path to AGENTS.md.
func (w *Workspace) AgentsPath() string { return filepath.Join(w.root, "AGENTS.md") }

// UserPath returns the path to USER.md.
func (w *Workspace) UserPath() string { return filepath.Join(w.root, "USER.md") }

// MemoryPath returns the path to MEMORY.md.
func (w *Workspace) MemoryPath() string { return filepath.Join(w.root, "MEMORY.md") }

// HeartbeatPath returns the path to HEARTBEAT.md.
func (w *Workspace) HeartbeatPath() string { return filepath.Join(w.root, "HEARTBEAT.md") }

// ToolsPath returns the path to TOOLS.md.
func (w *Workspace) ToolsPath() string { return filepath.Join(w.root, "TOOLS.md") }

// MemoriesDir returns the path to the memories/ directory (daily session logs).
func (w *Workspace) MemoriesDir() string { return filepath.Join(w.root, "memories") }

// SessionsDir returns the path to the sessions/ directory.
func (w *Workspace) SessionsDir() string { return filepath.Join(w.root, "sessions") }

// SkillsDir returns the path to the skills/ directory.
func (w *Workspace) SkillsDir() string { return filepath.Join(w.root, "skills") }

// KnowledgesDir returns the path to the knowledges/ directory.
func (w *Workspace) KnowledgesDir() string { return filepath.Join(w.root, "knowledges") }

// OutputsDir returns the path to the outputs/ directory.
func (w *Workspace) OutputsDir() string { return filepath.Join(w.root, "outputs") }

// CronStorePath returns the path to the cron jobs store file.
func (w *Workspace) CronStorePath() string { return filepath.Join(w.root, "cron.json") }

// DailyLogPath returns the path to today's session log.
func (w *Workspace) DailyLogPath() string {
	return filepath.Join(w.MemoriesDir(), time.Now().Format("2006-01-02")+".md")
}

// Init creates the workspace directory structure and template files.
// If a file already exists it is left untouched.
func (w *Workspace) Init() error {
	dirs := []string{
		w.root,
		w.MemoriesDir(),
		w.SessionsDir(),
		w.SkillsDir(),
		w.KnowledgesDir(),
		w.OutputsDir(),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("workspace: mkdir %s: %w", d, err)
		}
	}

	// Recursively copy template files (skip existing files).
	err := fs.WalkDir(templateFS, "templates", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Compute destination path by stripping the "templates/" prefix.
		rel, err := filepath.Rel("templates", path)
		if err != nil {
			return err
		}
		dst := filepath.Join(w.root, rel)

		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}

		// Skip the file if it already exists — never overwrite user customisations.
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
// It returns an error with a helpful message when critical files are missing.
func (w *Workspace) Load() error {
	if _, err := os.Stat(w.root); err != nil {
		return fmt.Errorf("workspace %q does not exist; run 'blades init' first", w.root)
	}
	return nil
}

// ReadFile reads a file within the workspace root by relative name.
// Returns "" if the file does not exist (not an error).
func (w *Workspace) ReadFile(name string) (string, error) {
	p := filepath.Join(w.root, name)
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("workspace: read %s: %w", name, err)
	}
	return string(b), nil
}
