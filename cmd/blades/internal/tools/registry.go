package tools

import (
	"fmt"

	bladestools "github.com/go-kratos/blades/tools"
)

// Registry implements recipe.ToolResolver backed by a named tool map.
type Registry struct {
	tools map[string]bladestools.Tool
}

// NewRegistry creates a Registry from a name→Tool map.
func NewRegistry(tools map[string]bladestools.Tool) *Registry {
	return &Registry{tools: tools}
}

// Resolve implements recipe.ToolResolver.
func (r *Registry) Resolve(name string) (bladestools.Tool, error) {
	if t, ok := r.tools[name]; ok {
		return t, nil
	}
	return nil, fmt.Errorf("tool %q not registered", name)
}
