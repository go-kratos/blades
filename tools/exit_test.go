package tools_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/go-kratos/blades/tools"
)

// mockToolContext is a minimal ToolContext for testing.
type mockToolContext struct {
	id      string
	name    string
	actions map[string]any
}

func (m *mockToolContext) ID() string              { return m.id }
func (m *mockToolContext) Name() string            { return m.name }
func (m *mockToolContext) Actions() map[string]any { return m.actions }
func (m *mockToolContext) SetAction(key string, value any) {
	m.actions[key] = value
}

func ctxWithToolContext(tc tools.ToolContext) context.Context {
	return tools.NewContext(context.Background(), tc)
}

func requireExitInput(t *testing.T, got any) tools.ExitInput {
	t.Helper()

	switch v := got.(type) {
	case tools.ExitInput:
		return v
	case *tools.ExitInput:
		if v == nil {
			t.Fatal("expected non-nil *tools.ExitInput")
		}
		return *v
	default:
		t.Fatalf("expected ExitInput payload, got %T", got)
		return tools.ExitInput{}
	}
}

func TestExitTool_Name(t *testing.T) {
	et := tools.NewExitTool()
	if et.Name() != "exit" {
		t.Errorf("expected name %q, got %q", "exit", et.Name())
	}
}

func TestExitTool_HandleNoContext(t *testing.T) {
	et := tools.NewExitTool()
	input, _ := json.Marshal(tools.ExitInput{Reason: "done"})
	// No ToolContext in ctx — should be a silent no-op, not an error.
	if _, err := et.Handle(context.Background(), string(input)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExitTool_HandleSetsAction(t *testing.T) {
	et := tools.NewExitTool()
	tc := &mockToolContext{id: "1", name: "exit", actions: make(map[string]any)}
	input, _ := json.Marshal(tools.ExitInput{Reason: "done"})
	if _, err := et.Handle(ctxWithToolContext(tc), string(input)); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	got, ok := tc.actions[tools.ActionLoopExit]
	if !ok {
		t.Fatalf("expected %q action to be set", tools.ActionLoopExit)
	}
	ei := requireExitInput(t, got)
	if ei.Reason != "done" {
		t.Errorf("expected reason %q, got %q", "done", ei.Reason)
	}
	if ei.Escalate {
		t.Error("expected escalate=false")
	}
}

func TestExitTool_HandleSetsActionEscalate(t *testing.T) {
	et := tools.NewExitTool()
	tc := &mockToolContext{id: "1", name: "exit", actions: make(map[string]any)}
	input, _ := json.Marshal(tools.ExitInput{Reason: "needs review", Escalate: true})
	if _, err := et.Handle(ctxWithToolContext(tc), string(input)); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	got, ok := tc.actions[tools.ActionLoopExit]
	if !ok {
		t.Fatalf("expected %q action to be set", tools.ActionLoopExit)
	}
	ei := requireExitInput(t, got)
	if !ei.Escalate {
		t.Error("expected escalate=true")
	}
	if ei.Reason != "needs review" {
		t.Errorf("expected reason %q, got %q", "needs review", ei.Reason)
	}
}

func TestExitTool_HandleInvalidJSON(t *testing.T) {
	et := tools.NewExitTool()
	_, err := et.Handle(context.Background(), "not-json")
	if err == nil {
		t.Error("expected error for invalid JSON input")
	}
}

func TestExitTool_InputSchema(t *testing.T) {
	et := tools.NewExitTool()
	if et.InputSchema() == nil {
		t.Error("InputSchema should not be nil")
	}
}

func TestExitTool_OutputSchema(t *testing.T) {
	et := tools.NewExitTool()
	if et.OutputSchema() != nil {
		t.Error("OutputSchema should be nil")
	}
}
