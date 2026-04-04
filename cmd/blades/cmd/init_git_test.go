package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitGitCreatesRepositoryAndGitignore(t *testing.T) {
	dir := t.TempDir()

	if err := initGit(dir); err != nil {
		t.Fatalf("initGit: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		t.Fatalf("expected .git directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".gitignore")); err != nil {
		t.Fatalf("expected .gitignore file: %v", err)
	}
}

func TestEnsureGitRepoUsesRunner(t *testing.T) {
	dir := t.TempDir()
	called := false
	err := ensureGitRepo(dir, func(gotDir string) ([]byte, error) {
		called = true
		if gotDir != dir {
			t.Fatalf("runner dir = %q, want %q", gotDir, dir)
		}
		return nil, nil
	})
	if err != nil {
		t.Fatalf("ensureGitRepo: %v", err)
	}
	if !called {
		t.Fatal("expected custom git runner to be called")
	}
}

func TestEnsureGitignoreAndBackupHint(t *testing.T) {
	dir := t.TempDir()
	if err := ensureGitignore(dir); err != nil {
		t.Fatalf("ensureGitignore: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	if string(data) != "*.tmp\n" {
		t.Fatalf(".gitignore = %q", string(data))
	}

	var buf bytes.Buffer
	buf.WriteString(renderGitBackupHint(dir))
	if !strings.Contains(buf.String(), "git-backup") || !strings.Contains(buf.String(), dir) {
		t.Fatalf("backup hint = %q", buf.String())
	}
}
