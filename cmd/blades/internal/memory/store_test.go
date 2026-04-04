package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestStore(t *testing.T) (*Store, string, string, string) {
	t.Helper()

	root := t.TempDir()
	memoryFile := filepath.Join(root, "MEMORY.md")
	memoriesDir := filepath.Join(root, "memory")
	knowledgesDir := filepath.Join(root, "knowledges")

	store, err := New(memoryFile, memoriesDir, knowledgesDir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return store, memoryFile, memoriesDir, knowledgesDir
}

func TestStoreReadWriteAppendAndSearch(t *testing.T) {
	store, memoryFile, memoriesDir, knowledgesDir := newTestStore(t)

	initial, err := store.ReadMemory()
	if err != nil {
		t.Fatalf("ReadMemory: %v", err)
	}
	if !strings.Contains(initial, "Long-Term Memory") {
		t.Fatalf("unexpected initial memory: %s", initial)
	}

	if err := store.WriteMemory("alpha"); err != nil {
		t.Fatalf("WriteMemory: %v", err)
	}
	if err := store.AppendMemory("beta"); err != nil {
		t.Fatalf("AppendMemory: %v", err)
	}

	got, err := store.ReadMemory()
	if err != nil {
		t.Fatalf("ReadMemory after append: %v", err)
	}
	if !strings.Contains(got, "alpha") || !strings.Contains(got, "beta") {
		t.Fatalf("memory contents = %q", got)
	}

	if err := store.AppendDailyLog("user", "hello blades"); err != nil {
		t.Fatalf("AppendDailyLog: %v", err)
	}
	logPath := filepath.Join(memoriesDir, time.Now().Format("2006-01-02")+".md")
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read daily log: %v", err)
	}
	if !strings.Contains(string(logData), "hello blades") {
		t.Fatalf("daily log = %s", string(logData))
	}

	yesterday := time.Now().Add(-24 * time.Hour).Format("2006-01-02") + ".md"
	if err := os.WriteFile(filepath.Join(memoriesDir, yesterday), []byte("# old\n\nlegacy line\n"), 0o644); err != nil {
		t.Fatalf("write old log: %v", err)
	}

	if err := os.WriteFile(filepath.Join(knowledgesDir, "small.txt"), []byte("small knowledge"), 0o644); err != nil {
		t.Fatalf("write small knowledge: %v", err)
	}
	largeContent := strings.Repeat("x", knowledgeSizeLimit+10)
	if err := os.WriteFile(filepath.Join(knowledgesDir, "large.txt"), []byte(largeContent), 0o644); err != nil {
		t.Fatalf("write large knowledge: %v", err)
	}

	instruction, err := store.BuildInstruction("base instructions", 2)
	if err != nil {
		t.Fatalf("BuildInstruction: %v", err)
	}
	for _, want := range []string{"base instructions", "alpha", "hello blades", "legacy line", "small knowledge", "large.txt"} {
		if !strings.Contains(instruction, want) {
			t.Fatalf("instruction missing %q: %s", want, instruction)
		}
	}

	results, err := store.SearchLogs("hello")
	if err != nil {
		t.Fatalf("SearchLogs: %v", err)
	}
	if len(results) == 0 || !strings.Contains(results[0], "hello blades") {
		t.Fatalf("search results = %v", results)
	}

	if _, err := os.Stat(memoryFile); err != nil {
		t.Fatalf("expected memory file to exist: %v", err)
	}
}

func TestStoreHandlesMissingLogsDir(t *testing.T) {
	store, _, memoriesDir, _ := newTestStore(t)
	if err := os.RemoveAll(memoriesDir); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}

	logs, err := store.recentLogs(5)
	if err != nil {
		t.Fatalf("recentLogs: %v", err)
	}
	if logs != nil {
		t.Fatalf("expected nil recent logs, got %v", logs)
	}

	results, err := store.SearchLogs("anything")
	if err != nil {
		t.Fatalf("SearchLogs: %v", err)
	}
	if results != nil {
		t.Fatalf("expected nil search results, got %v", results)
	}
}

func TestStoreReadMemoryReturnsErrorWhenFileRemoved(t *testing.T) {
	store, memoryFile, _, _ := newTestStore(t)
	if err := os.Remove(memoryFile); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	if _, err := store.ReadMemory(); err == nil {
		t.Fatal("expected ReadMemory to fail when MEMORY.md is removed")
	}
}
