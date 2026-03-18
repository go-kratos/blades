package logger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRuntimeWriteAndConversation(t *testing.T) {
	home := t.TempDir()
	rt := NewRuntime(home)

	rt.Write("hello %s", "world")
	rt.WriteConversation("session-1", "assistant", "line one\nline two")

	logPath := filepath.Join(home, "logs", time.Now().Format("2006-01-02")+".log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	text := string(data)
	for _, want := range []string{"hello world", "session=session-1 role=assistant", "line one", "line two"} {
		if !strings.Contains(text, want) {
			t.Fatalf("log output missing %q: %s", want, text)
		}
	}
}
