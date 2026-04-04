package tools

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestBashToolHelpersAndGuard(t *testing.T) {
	t.Parallel()

	cfg := DefaultExecConfig("/workspace")
	if cfg.Timeout != 60*time.Second {
		t.Fatalf("DefaultExecConfig timeout = %s", cfg.Timeout)
	}
	if cfg.WorkingDir != "/workspace" {
		t.Fatalf("DefaultExecConfig working dir = %q", cfg.WorkingDir)
	}

	if tool := NewBashTool(cfg); tool.Name() != "bash" || tool.Description() == "" {
		t.Fatalf("NewBashTool metadata = name:%q desc:%q", tool.Name(), tool.Description())
	}

	if _, err := compileRegexList([]string{"["}); err == nil {
		t.Fatal("expected invalid regex error")
	}

	tool := &bashTool{cfg: cfg}
	if deny, err := compileRegexList(cfg.DenyPatterns); err != nil {
		t.Fatalf("compile deny regex: %v", err)
	} else {
		tool.denyRegex = deny
	}

	if msg := tool.guard("rm -rf /tmp"); !strings.Contains(msg, "blocked") {
		t.Fatalf("guard deny message = %q", msg)
	}

	tool.allowRegex = nil
	tool.denyRegex = nil
	tool.regexErr = context.DeadlineExceeded
	if msg := tool.guard("echo ok"); !strings.Contains(msg, "misconfigured") {
		t.Fatalf("guard regexErr message = %q", msg)
	}

	allow, err := compileRegexList([]string{`^echo\b`})
	if err != nil {
		t.Fatalf("compile allow regex: %v", err)
	}
	tool.regexErr = nil
	tool.allowRegex = allow
	if msg := tool.guard("pwd"); !strings.Contains(msg, "allow list") {
		t.Fatalf("allow-list guard message = %q", msg)
	}
	if msg := tool.guard("echo ok"); msg != "" {
		t.Fatalf("allow-list guard should allow command, got %q", msg)
	}
}

func TestBashToolHandleBranches(t *testing.T) {
	t.Parallel()

	t.Run("invalid json and required command", func(t *testing.T) {
		tool := &bashTool{cfg: ExecConfig{Timeout: time.Second}}
		if _, err := tool.handle(context.Background(), `{"command":`); err == nil {
			t.Fatal("expected parse error")
		}
		if _, err := tool.handle(context.Background(), `{}`); err == nil {
			t.Fatal("expected missing command error")
		}
	})

	t.Run("path traversal and timeout", func(t *testing.T) {
		tool := &bashTool{cfg: ExecConfig{
			Timeout:             10 * time.Millisecond,
			WorkingDir:          ".",
			RestrictToWorkspace: true,
		}}
		if out, err := tool.handle(context.Background(), `{"command":"cat ../secret"}`); err != nil || !strings.Contains(out, "path traversal blocked") {
			t.Fatalf("path traversal output = %q err=%v", out, err)
		}
		if out, err := tool.handle(context.Background(), `{"command":"sleep 1"}`); err != nil || !strings.Contains(out, "timed out") {
			t.Fatalf("timeout output = %q err=%v", out, err)
		}
	})

	t.Run("command error and success", func(t *testing.T) {
		tool := &bashTool{cfg: ExecConfig{
			Timeout:    time.Second,
			WorkingDir: ".",
		}}
		if out, err := tool.handle(context.Background(), `{"command":"printf ok"}`); err != nil || !strings.Contains(out, "ok") {
			t.Fatalf("success output = %q err=%v", out, err)
		}
		if out, err := tool.handle(context.Background(), `{"command":"sh -c 'echo nope >&2; exit 2'"}`); err != nil || !strings.Contains(out, "Exit:") {
			t.Fatalf("error output = %q err=%v", out, err)
		}
	})
}
