package lark

import (
	"strings"
	"testing"
)

func TestBuildToolCodeBlockIncludesCommandAndOutput(t *testing.T) {
	tool := toolInfo{
		name:   "exec",
		input:  `{"command":"ls -la","working_dir":"/tmp"}`,
		output: "file-a\nfile-b",
		status: "done",
	}

	block := buildToolCodeBlock(tool, 2000)
	if !strings.Contains(block, "```bash") {
		t.Fatalf("expected bash code block, got: %s", block)
	}
	if !strings.Contains(block, "# cwd: /tmp") {
		t.Fatalf("expected working directory in code block, got: %s", block)
	}
	if !strings.Contains(block, "$ ls -la") {
		t.Fatalf("expected command in code block, got: %s", block)
	}
	if !strings.Contains(block, "file-a\nfile-b") {
		t.Fatalf("expected tool output in code block, got: %s", block)
	}
}

func TestBuildToolCodeBlockWithNoOutput(t *testing.T) {
	tool := toolInfo{
		name:   "exec",
		input:  `{"command":"touch a.txt"}`,
		status: "done",
	}

	block := buildToolCodeBlock(tool, 2000)
	if !strings.Contains(block, "$ touch a.txt") {
		t.Fatalf("expected command in code block, got: %s", block)
	}
	if !strings.Contains(block, "[no output]") {
		t.Fatalf("expected no output marker, got: %s", block)
	}
}

func TestParseToolCommand(t *testing.T) {
	cmd, cwd := parseToolCommand(`{"command":"pwd","working_dir":"/work"}`)
	if cmd != "pwd" {
		t.Fatalf("command = %q, want %q", cmd, "pwd")
	}
	if cwd != "/work" {
		t.Fatalf("working dir = %q, want %q", cwd, "/work")
	}

	cmd, cwd = parseToolCommand("not-json")
	if cmd != "" || cwd != "" {
		t.Fatalf("invalid input should return empty command and cwd")
	}
}
