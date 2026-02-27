package mcp

import (
	"context"
	"testing"

	"github.com/go-kratos/blades/tools"
	"github.com/google/jsonschema-go/jsonschema"
)

type mockTool struct {
	name string
}

func (t mockTool) Name() string                                   { return t.name }
func (t mockTool) Description() string                            { return "mock" }
func (t mockTool) InputSchema() *jsonschema.Schema                { return nil }
func (t mockTool) OutputSchema() *jsonschema.Schema               { return nil }
func (t mockTool) Handle(context.Context, string) (string, error) { return "", nil }

var _ tools.Tool = mockTool{}

func TestToolsResolverGetToolsReturnsCopy(t *testing.T) {
	t.Parallel()

	r := &ToolsResolver{}
	r.setTools([]tools.Tool{mockTool{name: "origin"}})

	got := r.getTools()
	if len(got) != 1 {
		t.Fatalf("expected one tool, got %d", len(got))
	}
	got[0] = mockTool{name: "mutated"}
	got = append(got, mockTool{name: "extra"})

	again := r.getTools()
	if got, want := len(again), 1; got != want {
		t.Fatalf("resolver tools len = %d, want %d", got, want)
	}
	if got, want := again[0].Name(), "origin"; got != want {
		t.Fatalf("resolver first tool = %q, want %q", got, want)
	}
}
