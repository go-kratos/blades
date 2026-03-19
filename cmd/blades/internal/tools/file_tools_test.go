package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadToolHandle(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "notes.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	tool := &readTool{cfg: ExecConfig{WorkingDir: root, RestrictToWorkspace: true}}
	out, err := tool.handle(context.Background(), `{"path":"notes.txt","start_line":2,"end_line":3}`)
	if err != nil {
		t.Fatalf("read handle: %v", err)
	}
	if !strings.Contains(out, "Lines: 2-3 of 3") || !strings.Contains(out, "two\nthree\n") {
		t.Fatalf("read output = %q", out)
	}
}

func TestWriteToolHandle(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	tool := &writeTool{cfg: ExecConfig{WorkingDir: root, RestrictToWorkspace: true}}

	out, err := tool.handle(context.Background(), `{"path":"dir/out.txt","content":"hello","create_dirs":true}`)
	if err != nil {
		t.Fatalf("write handle: %v", err)
	}
	if !strings.Contains(out, "Wrote file:") {
		t.Fatalf("write output = %q", out)
	}
	data, err := os.ReadFile(filepath.Join(root, "dir", "out.txt"))
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("written content = %q", string(data))
	}

	if _, err := tool.handle(context.Background(), `{"path":"dir/out.txt","content":"again","if_exists":"error"}`); err == nil {
		t.Fatal("expected exists error")
	}
}

func TestEditToolHandle(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "edit.txt")
	if err := os.WriteFile(path, []byte("alpha\nbeta\nbeta\n"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	tool := &editTool{cfg: ExecConfig{WorkingDir: root, RestrictToWorkspace: true}}
	if _, err := tool.handle(context.Background(), `{"path":"edit.txt","edits":[{"old_string":"beta","new_string":"gamma"}]}`); err == nil {
		t.Fatal("expected ambiguous match error")
	}

	out, err := tool.handle(context.Background(), `{"path":"edit.txt","edits":[{"old_string":"beta","new_string":"gamma","replace_all":true,"expected_replacements":2}]}`)
	if err != nil {
		t.Fatalf("edit handle: %v", err)
	}
	if !strings.Contains(out, "Updated file:") {
		t.Fatalf("edit output = %q", out)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read edited file: %v", err)
	}
	if string(data) != "alpha\ngamma\ngamma\n" {
		t.Fatalf("edited content = %q", string(data))
	}
}

func TestResolvePathRestrictsWorkspace(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	_, err := resolvePath(ExecConfig{WorkingDir: root, RestrictToWorkspace: true}, "../nope.txt", false)
	if err == nil || !strings.Contains(err.Error(), "escapes workspace") {
		t.Fatalf("resolvePath err = %v", err)
	}
}
