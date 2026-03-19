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

type editInput struct {
	Path  string       `json:"path"`
	Edits []stringEdit `json:"edits"`
}

type stringEdit struct {
	OldString            string `json:"old_string"`
	NewString            string `json:"new_string"`
	ReplaceAll           bool   `json:"replace_all,omitempty"`
	ExpectedReplacements int    `json:"expected_replacements,omitempty"`
}

type editTool struct {
	cfg ExecConfig
}

// NewEditTool returns a tool that applies exact string replacements to a file.
func NewEditTool(cfg ExecConfig) bladestools.Tool {
	inputSchema, _ := jsonschema.For[editInput](nil)
	outputSchema, _ := jsonschema.For[string](nil)
	return bladestools.NewTool(
		"edit",
		"Precisely edit an existing text file by replacing exact text snippets. Fails when the target text is missing or ambiguous unless expected_replacements or replace_all is set.",
		bladestools.HandleFunc((&editTool{cfg: cfg}).handle),
		bladestools.WithInputSchema(inputSchema),
		bladestools.WithOutputSchema(outputSchema),
	)
}

func (t *editTool) handle(ctx context.Context, raw string) (string, error) {
	_ = ctx

	var in editInput
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return "", fmt.Errorf("edit: parse input: %w", err)
	}
	if len(in.Edits) == 0 {
		return "", fmt.Errorf("edit: edits is required")
	}

	path, err := resolvePath(t.cfg, in.Path, true)
	if err != nil {
		return "", fmt.Errorf("edit: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("edit: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("edit: path is a directory: %s", in.Path)
	}

	content, err := readTextFile(path)
	if err != nil {
		return "", fmt.Errorf("edit: %w", err)
	}

	for i, change := range in.Edits {
		if change.OldString == "" {
			return "", fmt.Errorf("edit: edits[%d].old_string is required", i)
		}
		count := strings.Count(content, change.OldString)
		if count == 0 {
			return "", fmt.Errorf("edit: edits[%d] target not found", i)
		}

		expected := change.ExpectedReplacements
		if change.ReplaceAll {
			if expected > 0 && expected != count {
				return "", fmt.Errorf("edit: edits[%d] expected %d matches, found %d", i, expected, count)
			}
			content = strings.ReplaceAll(content, change.OldString, change.NewString)
			continue
		}

		if expected <= 0 {
			expected = 1
		}
		if count != expected {
			return "", fmt.Errorf("edit: edits[%d] expected %d matches, found %d", i, expected, count)
		}
		content = strings.Replace(content, change.OldString, change.NewString, expected)
	}

	if err := atomicWriteFile(path, content); err != nil {
		return "", fmt.Errorf("edit: %w", err)
	}
	return fmt.Sprintf("Updated file: %s (%d edits)", path, len(in.Edits)), nil
}
