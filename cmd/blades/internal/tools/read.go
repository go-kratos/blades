package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	bladestools "github.com/go-kratos/blades/tools"
	"github.com/google/jsonschema-go/jsonschema"
)

type readInput struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
}

type readTool struct {
	cfg ExecConfig
}

// NewReadTool returns a tool that reads text files from the workspace.
func NewReadTool(cfg ExecConfig) bladestools.Tool {
	inputSchema, _ := jsonschema.For[readInput](nil)
	outputSchema, _ := jsonschema.For[string](nil)
	return bladestools.NewTool(
		"read",
		"Read a UTF-8 text file from the workspace. Supports optional line ranges. Use this instead of bash for normal file reads.",
		bladestools.HandleFunc((&readTool{cfg: cfg}).handle),
		bladestools.WithInputSchema(inputSchema),
		bladestools.WithOutputSchema(outputSchema),
	)
}

func (t *readTool) handle(ctx context.Context, raw string) (string, error) {
	_ = ctx

	var in readInput
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return "", fmt.Errorf("read: parse input: %w", err)
	}

	path, err := resolvePath(t.cfg, in.Path, true)
	if err != nil {
		return "", fmt.Errorf("read: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("read: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("read: path is a directory: %s", in.Path)
	}

	content, err := readTextFile(path)
	if err != nil {
		return "", fmt.Errorf("read: %w", err)
	}
	sliced, start, end, total, err := sliceLines(content, in.StartLine, in.EndLine)
	if err != nil {
		return "", fmt.Errorf("read: %w", err)
	}

	var b strings.Builder
	b.WriteString("Path: ")
	b.WriteString(path)
	b.WriteString("\n")
	if total == 0 {
		b.WriteString("Lines: 0\n\n")
		return b.String(), nil
	}
	b.WriteString(fmt.Sprintf("Lines: %d-%d of %d\n\n", start, end, total))
	b.WriteString(sliced)
	return b.String(), nil
}
