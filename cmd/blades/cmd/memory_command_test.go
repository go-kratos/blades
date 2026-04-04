package cmd

import (
	"os"
	"strings"
	"testing"

	"github.com/go-kratos/blades/cmd/blades/internal/memory"
)

func TestMemoryCommandsAddShowAndSearch(t *testing.T) {
	ws := setupCommandWorkspace(t)

	addOut := captureStdout(t, func() {
		cmd := newMemoryAddCmd()
		withCommandOptions(cmd, workspaceOptions(ws))
		cmd.SetArgs([]string{"remember coffee"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("memory add: %v", err)
		}
	})
	if !strings.Contains(addOut, "appended to MEMORY.md") {
		t.Fatalf("memory add output = %q", addOut)
	}

	data, err := os.ReadFile(ws.MemoryPath())
	if err != nil {
		t.Fatalf("read memory file: %v", err)
	}
	if !strings.Contains(string(data), "remember coffee") {
		t.Fatalf("MEMORY.md missing appended text: %s", string(data))
	}

	mem, err := memory.New(ws.MemoryPath(), ws.MemoriesDir(), ws.KnowledgesDir())
	if err != nil {
		t.Fatalf("memory.New: %v", err)
	}
	if err := mem.AppendDailyLog("user", "remember coffee in the morning"); err != nil {
		t.Fatalf("AppendDailyLog: %v", err)
	}

	showOut := captureStdout(t, func() {
		cmd := newMemoryShowCmd()
		withCommandOptions(cmd, workspaceOptions(ws))
		if err := cmd.Execute(); err != nil {
			t.Fatalf("memory show: %v", err)
		}
	})
	if !strings.Contains(showOut, "remember coffee") {
		t.Fatalf("memory show output = %q", showOut)
	}

	searchOut := captureStdout(t, func() {
		cmd := newMemorySearchCmd()
		withCommandOptions(cmd, workspaceOptions(ws))
		cmd.SetArgs([]string{"coffee"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("memory search: %v", err)
		}
	})
	if !strings.Contains(searchOut, "remember coffee in the morning") {
		t.Fatalf("memory search output = %q", searchOut)
	}

	emptyOut := captureStdout(t, func() {
		cmd := newMemorySearchCmd()
		withCommandOptions(cmd, workspaceOptions(ws))
		cmd.SetArgs([]string{"does-not-exist"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("memory search no results: %v", err)
		}
	})
	if !strings.Contains(emptyOut, "(no results)") {
		t.Fatalf("memory search empty output = %q", emptyOut)
	}
}
