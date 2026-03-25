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
	Path  string       `json:"path" jsonschema:"Workspace-relative file path to modify."`
	Edits []stringEdit `json:"edits" jsonschema:"One or more exact string replacements to apply in order."`
}

type stringEdit struct {
	OldString            string `json:"old_string" jsonschema:"Exact existing text to replace. Required and must be non-empty. edit cannot insert text by using an empty old_string."`
	NewString            string `json:"new_string" jsonschema:"Replacement text. May be empty to delete the matched text."`
	ReplaceAll           bool   `json:"replace_all,omitempty" jsonschema:"Replace every occurrence of old_string when true."`
	ExpectedReplacements int    `json:"expected_replacements,omitempty" jsonschema:"Expected number of matches. Use this to guard against ambiguous edits."`
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
		"Precisely edit an existing text file by replacing exact non-empty text snippets. old_string must already exist in the file and cannot be empty. Use replace_all or expected_replacements when a snippet appears multiple times. If you have not read the file yet, or need to add a new section, use read first or replace the whole file with write instead.",
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
			return "", fmt.Errorf("edit: edits[%d] target not found; old_string must match existing text exactly. Read the file for the exact text, or use write to replace the whole file", i)
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
