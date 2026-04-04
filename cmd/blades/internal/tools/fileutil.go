package tools

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const maxReadBytes = 256 * 1024

func workspaceRoot(cfg ExecConfig) (string, error) {
	root := strings.TrimSpace(cfg.WorkingDir)
	if root == "" {
		root = "."
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err == nil {
		abs = real
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func resolveWorkingDir(cfg ExecConfig, input string) (string, error) {
	if strings.TrimSpace(input) == "" {
		return workspaceRoot(cfg)
	}
	return resolvePath(cfg, input, true)
}

func resolvePath(cfg ExecConfig, path string, mustExist bool) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("path is required")
	}
	root, err := workspaceRoot(cfg)
	if err != nil {
		return "", err
	}

	candidate := path
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(root, candidate)
	}
	candidate, err = filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	if cfg.RestrictToWorkspace && !isWithinRoot(root, candidate) {
		return "", fmt.Errorf("path escapes workspace: %s", path)
	}

	if _, err := os.Lstat(candidate); err == nil {
		real, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			return "", err
		}
		real, err = filepath.Abs(real)
		if err != nil {
			return "", err
		}
		if cfg.RestrictToWorkspace && !isWithinRoot(root, real) {
			return "", fmt.Errorf("path escapes workspace via symlink: %s", path)
		}
		return filepath.Clean(real), nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}

	if mustExist {
		return "", fmt.Errorf("path does not exist: %s", path)
	}

	ancestor, err := deepestExistingAncestor(candidate)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(ancestor, candidate)
	if err != nil {
		return "", err
	}
	realAncestor, err := filepath.EvalSymlinks(ancestor)
	if err != nil {
		return "", err
	}
	realAncestor, err = filepath.Abs(realAncestor)
	if err != nil {
		return "", err
	}
	resolved := filepath.Join(realAncestor, rel)
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", err
	}
	if cfg.RestrictToWorkspace && !isWithinRoot(root, resolved) {
		return "", fmt.Errorf("path escapes workspace via symlink: %s", path)
	}
	return filepath.Clean(resolved), nil
}

func deepestExistingAncestor(path string) (string, error) {
	current := filepath.Clean(path)
	for {
		if _, err := os.Stat(current); err == nil {
			return current, nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("no existing ancestor for path: %s", path)
		}
		current = parent
	}
}

func isWithinRoot(root string, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}

func readTextFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(data) > maxReadBytes {
		data = data[:maxReadBytes]
	}
	if !utf8.Valid(data) || bytesContainNUL(data) {
		return "", fmt.Errorf("binary or non-UTF-8 file is not supported")
	}
	return string(data), nil
}

func bytesContainNUL(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}

func sliceLines(content string, startLine int, endLine int) (string, int, int, int, error) {
	lines := splitLines(content)
	total := len(lines)
	if total == 0 {
		if startLine > 1 || endLine > 0 {
			return "", 0, 0, 0, fmt.Errorf("line range out of bounds")
		}
		return "", 0, 0, 0, nil
	}

	if startLine <= 0 {
		startLine = 1
	}
	if endLine <= 0 {
		endLine = total
	}
	if startLine > endLine {
		return "", 0, 0, total, fmt.Errorf("start_line must be <= end_line")
	}
	if startLine > total || endLine > total {
		return "", 0, 0, total, fmt.Errorf("line range out of bounds")
	}

	return strings.Join(lines[startLine-1:endLine], ""), startLine, endLine, total, nil
}

func splitLines(content string) []string {
	if content == "" {
		return nil
	}
	lines := strings.SplitAfter(content, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func atomicWriteFile(path string, content string) error {
	parent := filepath.Dir(path)
	tmp, err := os.CreateTemp(parent, ".blades-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
