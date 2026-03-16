package tools_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/go-kratos/blades/tools"
)

// mockExiter records the most recent ExitLoop call.
type mockExiter struct {
	called   bool
	reason   string
	escalate bool
}

func (m *mockExiter) ExitLoop(reason string, escalate bool) {
	m.called = true
	m.reason = reason
	m.escalate = escalate
}

func ctxWithMock(m tools.LoopExiter) context.Context {
	return tools.WithLoopExiter(context.Background(), m)
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
	// No LoopExiter in ctx — should be a silent no-op, not an error.
	if _, err := et.Handle(context.Background(), string(input)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExitTool_HandleCallsLoopExiter(t *testing.T) {
	et := tools.NewExitTool()
	m := &mockExiter{}
	input, _ := json.Marshal(tools.ExitInput{Reason: "done"})
	if _, err := et.Handle(ctxWithMock(m), string(input)); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if !m.called {
		t.Fatal("ExitLoop not called")
	}
	if m.reason != "done" {
		t.Errorf("expected reason %q, got %q", "done", m.reason)
	}
	if m.escalate {
		t.Error("expected escalate=false")
	}
}

func TestExitTool_HandleEscalateCallsLoopExiter(t *testing.T) {
	et := tools.NewExitTool()
	m := &mockExiter{}
	input, _ := json.Marshal(tools.ExitInput{Reason: "needs review", Escalate: true})
	if _, err := et.Handle(ctxWithMock(m), string(input)); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	if !m.escalate {
		t.Error("expected escalate=true")
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
