package tools

import "github.com/google/jsonschema-go/jsonschema"

// ToolSpec describes a tool's metadata for model requests.
type ToolSpec struct {
	Name         string
	Description  string
	InputSchema  *jsonschema.Schema
	OutputSchema *jsonschema.Schema
}
