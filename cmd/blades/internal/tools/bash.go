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

// ExecConfig configures the local tool behaviour.
type ExecConfig struct {
	// Timeout is the maximum time a command may run (default: 60s).
	Timeout time.Duration
	// WorkingDir is the workspace root used by read/write/edit/bash.
	WorkingDir string
	// DenyPatterns are regex patterns for bash commands to block.
	DenyPatterns []string
	// AllowPatterns, when non-empty, restrict bash commands to those matching at least one pattern.
	AllowPatterns []string
	// RestrictToWorkspace blocks workspace escapes when true.
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
			`:\(\)\s*\{.*\};\s*:`,
			`\b(export|set)\s+\w+=`,
			`(?:^|[;&|]\s*)\benv\b`,
			`\b(printenv)\b`,
			`\becho\s+["']?\$\{?\w`,
			`/etc/(passwd|shadow|sudoers)`,
			`\~/?\.(ssh|aws|kube|gnupg)\b`,
			`\b(id_rsa|id_ecdsa|id_ed25519|authorized_keys)\b`,
		},
	}
}

type bashInput struct {
	Command    string `json:"command"`
	WorkingDir string `json:"working_dir,omitempty"`
	TimeoutMs  int    `json:"timeout_ms,omitempty"`
}

type bashTool struct {
	cfg        ExecConfig
	denyRegex  []*regexp.Regexp
	allowRegex []*regexp.Regexp
	regexErr   error
}

func compileRegexList(patterns []string) ([]*regexp.Regexp, error) {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid regex %q: %w", pattern, err)
		}
		compiled = append(compiled, re)
	}
	return compiled, nil
}

// NewBashTool returns a tool that executes shell commands with safety guards.
func NewBashTool(cfg ExecConfig) bladestools.Tool {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 60 * time.Second
	}
	t := &bashTool{cfg: cfg}
	if denyRegex, err := compileRegexList(cfg.DenyPatterns); err != nil {
		t.regexErr = err
	} else {
		t.denyRegex = denyRegex
	}
	if allowRegex, err := compileRegexList(cfg.AllowPatterns); err != nil {
		t.regexErr = err
	} else {
		t.allowRegex = allowRegex
	}
	inputSchema, _ := jsonschema.For[bashInput](nil)
	outputSchema, _ := jsonschema.For[string](nil)
	return bladestools.NewTool(
		"bash",
		"Execute a shell command and return combined stdout+stderr output. "+
			"Use for commands and scripts. Do not use it for normal file reads or edits when read/write/edit are enough.",
		bladestools.HandleFunc(t.handle),
		bladestools.WithInputSchema(inputSchema),
		bladestools.WithOutputSchema(outputSchema),
	)
}

func (t *bashTool) handle(ctx context.Context, raw string) (string, error) {
	var in bashInput
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return "", fmt.Errorf("bash: parse input: %w", err)
	}
	if strings.TrimSpace(in.Command) == "" {
		return "", fmt.Errorf("bash: command is required")
	}

	if msg := t.guard(in.Command); msg != "" {
		return msg, nil
	}
	if t.cfg.RestrictToWorkspace && (strings.Contains(in.Command, "../") || strings.Contains(in.Command, `..\`)) {
		return "Error: path traversal blocked by safety guard", nil
	}

	cwd, err := resolveWorkingDir(t.cfg, in.WorkingDir)
	if err != nil {
		return "", fmt.Errorf("bash: %w", err)
	}

	timeout := t.cfg.Timeout
	if in.TimeoutMs > 0 {
		override := time.Duration(in.TimeoutMs) * time.Millisecond
		if timeout <= 0 || override < timeout {
			timeout = override
		}
	}
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	tCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(tCtx, "sh", "-c", in.Command)
	cmd.Dir = cwd
	cmd.Env = os.Environ()

	out, err := cmd.CombinedOutput()
	if tCtx.Err() == context.DeadlineExceeded {
		return fmt.Sprintf("Error: command timed out after %s", timeout), nil
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

func (t *bashTool) guard(command string) string {
	if t.regexErr != nil {
		return "Error: bash tool misconfigured: " + t.regexErr.Error()
	}

	lower := strings.ToLower(strings.TrimSpace(command))
	for _, re := range t.denyRegex {
		if re.MatchString(lower) {
			return "Error: command blocked by safety guard (matched deny pattern: " + re.String() + ")"
		}
	}
	if len(t.allowRegex) > 0 {
		for _, re := range t.allowRegex {
			if re.MatchString(lower) {
				return ""
			}
		}
		return "Error: command blocked by safety guard (not in allow list)"
	}
	return ""
}
