// Package tools provides built-in agent tools for the blades CLI.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	bladestools "github.com/go-kratos/blades/tools"
	"github.com/google/jsonschema-go/jsonschema"
)

// ExecConfig configures the exec tool behaviour.
type ExecConfig struct {
	// Timeout is the maximum time a command may run (default: 60s).
	Timeout time.Duration
	// WorkingDir is the default working directory.
	WorkingDir string
	// DenyPatterns are regex patterns for commands to block.
	DenyPatterns []string
	// AllowPatterns, when non-empty, restricts commands to those matching at least one pattern.
	AllowPatterns []string
	// RestrictToWorkspace blocks path-traversal sequences when true.
	RestrictToWorkspace bool
}

// DefaultExecConfig returns a safe default configuration.
func DefaultExecConfig(workingDir string) ExecConfig {
	return ExecConfig{
		Timeout:    60 * time.Second,
		WorkingDir: workingDir,
		DenyPatterns: []string{
			`\brm\s+-[rf]{1,2}\b`,
			`\brmdir\s+/s\b`,
			`(?:^|[;&|]\s*)format\b`,
			`\b(mkfs|diskpart)\b`,
			`\bdd\s+if=`,
			`>\s*/dev/sd`,
			`\b(shutdown|reboot|poweroff)\b`,
			`:\(\)\s*\{.*\};\s*:`,   // fork bomb
			`\b(export|set)\s+\w+=`, // env manipulation
			`(?:^|[;&|]\s*)\benv\b`, // env dump
			`\b(printenv)\b`,
			`\becho\s+["']?\$\{?\w`, // echo $VAR
			`/etc/(passwd|shadow|sudoers)`,
			`\~/?\.(ssh|aws|kube|gnupg)\b`,
			`\b(id_rsa|id_ecdsa|id_ed25519|authorized_keys)\b`,
		},
	}
}

type execInput struct {
	Command    string `json:"command"`
	WorkingDir string `json:"working_dir,omitempty"`
}

type execTool struct {
	cfg ExecConfig
}

// NewExecTool returns a tool that executes shell commands with safety guards.
func NewExecTool(cfg ExecConfig) bladestools.Tool {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 60 * time.Second
	}
	t := &execTool{cfg: cfg}
	inputSchema, _ := jsonschema.For[execInput](nil)
	outputSchema, _ := jsonschema.For[string](nil)
	return bladestools.NewTool(
		"exec",
		"Execute a shell command and return its combined stdout+stderr output. "+
			"Use for file operations, running scripts, checking system state, etc.",
		bladestools.HandleFunc(t.handle),
		bladestools.WithInputSchema(inputSchema),
		bladestools.WithOutputSchema(outputSchema),
	)
}

func (t *execTool) handle(ctx context.Context, raw string) (string, error) {
	var in execInput
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return "", fmt.Errorf("exec: parse input: %w", err)
	}
	if in.Command == "" {
		return "", fmt.Errorf("exec: command is required")
	}

	// Safety check.
	if msg := t.guard(in.Command); msg != "" {
		return msg, nil
	}

	cwd := in.WorkingDir
	if cwd == "" {
		cwd = t.cfg.WorkingDir
	}
	if cwd == "" {
		cwd = "."
	}

	if t.cfg.RestrictToWorkspace {
		if strings.Contains(in.Command, "../") || strings.Contains(in.Command, `..\ `) {
			return "Error: path traversal blocked by safety guard", nil
		}
	}

	tCtx, cancel := context.WithTimeout(ctx, t.cfg.Timeout)
	defer cancel()

	cmd := exec.CommandContext(tCtx, "sh", "-c", in.Command)
	cmd.Dir = cwd
	cmd.Env = os.Environ()

	out, err := cmd.CombinedOutput()
	if tCtx.Err() == context.DeadlineExceeded {
		return fmt.Sprintf("Error: command timed out after %s", t.cfg.Timeout), nil
	}
	result := string(out)
	if err != nil {
		result += "\nExit: " + err.Error()
	}
	const maxLen = 10000
	if len(result) > maxLen {
		result = result[:maxLen] + fmt.Sprintf("\n... (truncated, %d more bytes)", len(result)-maxLen)
	}
	return result, nil
}

func (t *execTool) guard(command string) string {
	lower := strings.ToLower(strings.TrimSpace(command))
	for _, pattern := range t.cfg.DenyPatterns {
		if regexp.MustCompile(pattern).MatchString(lower) {
			return "Error: command blocked by safety guard (matched deny pattern: " + pattern + ")"
		}
	}
	if len(t.cfg.AllowPatterns) > 0 {
		for _, pattern := range t.cfg.AllowPatterns {
			if regexp.MustCompile(pattern).MatchString(lower) {
				return ""
			}
		}
		return "Error: command blocked by safety guard (not in allow list)"
	}
	return ""
}
