package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	bladestools "github.com/go-kratos/blades/tools"
	"github.com/google/jsonschema-go/jsonschema"
)

type writeInput struct {
	Path       string `json:"path" jsonschema:"Workspace-relative file path to write."`
	Content    string `json:"content" jsonschema:"Full text content to write to the file."`
	IfExists   string `json:"if_exists,omitempty" jsonschema:"Behavior when the file already exists. Use exact values overwrite or error. Omit to default to overwrite."`
	CreateDirs bool   `json:"create_dirs,omitempty" jsonschema:"Create missing parent directories before writing when true."`
}

type writeTool struct {
	cfg ExecConfig
}

// NewWriteTool returns a tool that creates or overwrites text files atomically.
func NewWriteTool(cfg ExecConfig) bladestools.Tool {
	inputSchema, _ := jsonschema.For[writeInput](nil)
	outputSchema, _ := jsonschema.For[string](nil)
	return bladestools.NewTool(
		"write",
		"Create a text file or overwrite an existing one atomically. Use this when replacing the whole file. if_exists only accepts overwrite or error. For partial changes or anchored replacements, use edit instead.",
		bladestools.HandleFunc((&writeTool{cfg: cfg}).handle),
		bladestools.WithInputSchema(inputSchema),
		bladestools.WithOutputSchema(outputSchema),
	)
}

func (t *writeTool) handle(ctx context.Context, raw string) (string, error) {
	_ = ctx

	var in writeInput
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return "", fmt.Errorf("write: parse input: %w", err)
	}

	ifExists := strings.ToLower(strings.TrimSpace(in.IfExists))
	if ifExists == "" {
		ifExists = "overwrite"
	}
	if ifExists != "overwrite" && ifExists != "error" {
		return "", fmt.Errorf("write: if_exists must be overwrite or error")
	}

	path, err := resolvePath(t.cfg, in.Path, false)
	if err != nil {
		return "", fmt.Errorf("write: %w", err)
	}
	parent := filepath.Dir(path)
	if in.CreateDirs {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return "", fmt.Errorf("write: create parent dirs: %w", err)
		}
	} else if _, err := os.Stat(parent); err != nil {
		return "", fmt.Errorf("write: parent directory does not exist: %s", parent)
	}

	if info, err := os.Stat(path); err == nil {
		if info.IsDir() {
			return "", fmt.Errorf("write: path is a directory: %s", in.Path)
		}
		if ifExists == "error" {
			return "", fmt.Errorf("write: file already exists: %s", in.Path)
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("write: %w", err)
	}

	if err := atomicWriteFile(path, in.Content); err != nil {
		return "", fmt.Errorf("write: %w", err)
	}
	return "Wrote file: " + path, nil
}
