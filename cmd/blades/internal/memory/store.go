// Package memory provides multi-layer persistent storage for the blades CLI agent.
//
// Layers:
//
//	L1 MEMORY.md           — long-term consolidated facts; atomic write
//	L2 memories/YYYY-MM-DD.md — daily session logs; append-only
//	L3 knowledges/         — domain knowledge files; read-only
package memory

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const knowledgeSizeLimit = 4 * 1024 // 4 KB — BuildInstruction only injects files up to this size

const defaultMemoryContent = `# MEMORY.md — Long-Term Memory

## Hard Lessons

(none yet)

## User Preferences

(none yet)

## Recurring Patterns

(none yet)

## Key Decisions

(none yet)
`

// Store manages workspace memory files.
type Store struct {
	memoryFile    string // MEMORY.md
	memoriesDir   string // memories/
	knowledgesDir string // knowledges/

	mu sync.RWMutex
}

// New creates a Store from workspace root paths.
// Directories are created if they do not exist.
func New(memoryFile, memoriesDir, knowledgesDir string) (*Store, error) {
	for _, d := range []string{memoriesDir, knowledgesDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, fmt.Errorf("memory: mkdir %s: %w", d, err)
		}
	}
	// Ensure MEMORY.md exists.
	if _, err := os.Stat(memoryFile); os.IsNotExist(err) {
		if err2 := os.WriteFile(memoryFile, []byte(defaultMemoryContent), 0o644); err2 != nil {
			return nil, fmt.Errorf("memory: init MEMORY.md: %w", err2)
		}
	}
	return &Store{
		memoryFile:    memoryFile,
		memoriesDir:   memoriesDir,
		knowledgesDir: knowledgesDir,
	}, nil
}

// ReadMemory returns the current MEMORY.md content.
func (s *Store) ReadMemory() (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.readMemoryLocked()
}

func (s *Store) readMemoryLocked() (string, error) {
	b, err := os.ReadFile(s.memoryFile)
	if err != nil {
		return "", fmt.Errorf("memory: read MEMORY.md: %w", err)
	}
	return strings.TrimSpace(string(b)), nil
}

// WriteMemory atomically overwrites MEMORY.md.
func (s *Store) WriteMemory(content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := filepath.Dir(s.memoryFile)
	tmp, err := os.CreateTemp(dir, ".memory-*.tmp")
	if err != nil {
		return fmt.Errorf("memory: create temp: %w", err)
	}
	name := tmp.Name()
	if _, err = tmp.WriteString(content); err != nil {
		tmp.Close()
		os.Remove(name)
		return fmt.Errorf("memory: write temp: %w", err)
	}
	if err = tmp.Close(); err != nil {
		os.Remove(name)
		return fmt.Errorf("memory: close temp: %w", err)
	}
	if err = os.Rename(name, s.memoryFile); err != nil {
		os.Remove(name)
		return fmt.Errorf("memory: rename MEMORY.md: %w", err)
	}
	return nil
}

// AppendMemory appends text to MEMORY.md.
func (s *Store) AppendMemory(text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cur, err := s.readMemoryLocked()
	if err != nil {
		return err
	}
	if !strings.HasSuffix(cur, "\n") {
		cur += "\n"
	}
	return s.writeMemoryLocked(cur + text + "\n")
}

func (s *Store) writeMemoryLocked(content string) error {
	dir := filepath.Dir(s.memoryFile)
	tmp, err := os.CreateTemp(dir, ".memory-*.tmp")
	if err != nil {
		return fmt.Errorf("memory: create temp: %w", err)
	}
	name := tmp.Name()
	if _, werr := tmp.WriteString(content); werr != nil {
		tmp.Close()
		os.Remove(name)
		return fmt.Errorf("memory: write temp: %w", werr)
	}
	if cerr := tmp.Close(); cerr != nil {
		os.Remove(name)
		return fmt.Errorf("memory: close temp: %w", cerr)
	}
	if rerr := os.Rename(name, s.memoryFile); rerr != nil {
		os.Remove(name)
		return fmt.Errorf("memory: rename MEMORY.md: %w", rerr)
	}
	return nil
}

// AppendDailyLog appends a timestamped entry to today's session log.
func (s *Store) AppendDailyLog(role, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.memoriesDir, time.Now().Format("2006-01-02")+".md")

	// Create with header if it doesn't exist.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		header := fmt.Sprintf("# Session Log %s\n\n", time.Now().Format("2006-01-02"))
		if err2 := os.WriteFile(path, []byte(header), 0o644); err2 != nil {
			return fmt.Errorf("memory: init daily log: %w", err2)
		}
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("memory: open daily log: %w", err)
	}
	defer f.Close()

	_, err = fmt.Fprintf(f, "\n## %s | %s\n%s\n",
		time.Now().Format("15:04:05"), role, text)
	return err
}

// BuildInstruction assembles the instruction string from all memory sources.
// It is kept for optional callers that want eager memory injection. The default
// blades chat/run path now relies on AGENTS.md to direct runtime file reads.
func (s *Store) BuildInstruction(base string, window int) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var b strings.Builder
	if base != "" {
		b.WriteString(base)
		b.WriteString("\n\n")
	}

	// L1: MEMORY.md
	mem, err := s.readMemoryLocked()
	if err == nil && mem != "" {
		b.WriteString("---\n## Long-term Memory\n")
		b.WriteString(mem)
		b.WriteString("\n\n")
	}

	// L2: recent daily logs
	if window > 0 {
		logs, err := s.recentLogs(window)
		if err == nil && len(logs) > 0 {
			b.WriteString("---\n## Recent Session Logs\n")
			for _, log := range logs {
				b.WriteString(log)
				b.WriteString("\n")
			}
			b.WriteString("\n")
		}
	}

	// L3: small knowledges/ files
	knowledge, err := s.injectKnowledges()
	if err == nil && knowledge != "" {
		b.WriteString("---\n## Domain Knowledge\n")
		b.WriteString(knowledge)
		b.WriteString("\n")
	}

	return strings.TrimSpace(b.String()), nil
}

// recentLogs returns the content of the N most-recent daily log files.
func (s *Store) recentLogs(n int) ([]string, error) {
	entries, err := os.ReadDir(s.memoriesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	// Collect .md files sorted descending.
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			files = append(files, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(files)))

	var results []string
	for i, name := range files {
		if i >= n {
			break
		}
		b, err := os.ReadFile(filepath.Join(s.memoriesDir, name))
		if err != nil {
			continue
		}
		results = append(results, string(b))
	}
	// Return chronological order (oldest first).
	for i, j := 0, len(results)-1; i < j; i, j = i+1, j-1 {
		results[i], results[j] = results[j], results[i]
	}
	return results, nil
}

// injectKnowledges reads all files in knowledges/ that are ≤ knowledgeSizeLimit bytes.
// Files larger than the limit contribute only their filename to a "Files available" list.
func (s *Store) injectKnowledges() (string, error) {
	entries, err := os.ReadDir(s.knowledgesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}

	var small, large []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		path := filepath.Join(s.knowledgesDir, e.Name())
		if info.Size() <= knowledgeSizeLimit {
			b, err := fs.ReadFile(os.DirFS(s.knowledgesDir), e.Name())
			if err == nil {
				small = append(small, fmt.Sprintf("### %s\n%s", e.Name(), string(b)))
			}
		} else {
			large = append(large, path)
		}
	}

	var b strings.Builder
	for _, s := range small {
		b.WriteString(s)
		b.WriteString("\n\n")
	}
	if len(large) > 0 {
		b.WriteString("#### Large knowledge files (use read_file tool to access):\n")
		for _, f := range large {
			b.WriteString("- ")
			b.WriteString(f)
			b.WriteString("\n")
		}
	}
	return strings.TrimSpace(b.String()), nil
}

// SearchLogs performs a simple case-insensitive substring search across all daily logs.
// Returns matching lines with their source file.
func (s *Store) SearchLogs(query string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.memoriesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	lq := strings.ToLower(query)
	var results []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.memoriesDir, e.Name()))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			if strings.Contains(strings.ToLower(line), lq) {
				results = append(results, fmt.Sprintf("[%s] %s", e.Name(), line))
			}
		}
	}
	return results, nil
}
