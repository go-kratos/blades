package blades

import (
	"context"
	"testing"

	bladestools "github.com/go-kratos/blades/tools"
)

type exitToolLoopModel struct {
	calls int
}

func (m *exitToolLoopModel) Name() string { return "exit-tool-loop" }

func (m *exitToolLoopModel) Generate(_ context.Context, _ *ModelRequest) (*ModelResponse, error) {
	m.calls++
	msg := NewAssistantMessage(StatusCompleted)
	msg.Role = RoleTool
	msg.Parts = append(msg.Parts, NewToolPart("call_exit", "exit", `{"reason":"done"}`))
	return &ModelResponse{Message: msg}, nil
}

func (m *exitToolLoopModel) NewStreaming(context.Context, *ModelRequest) Generator[*ModelResponse, error] {
	return nil
}

func TestAgentStopsImmediatelyAfterExitTool(t *testing.T) {
	t.Parallel()

	model := &exitToolLoopModel{}
	agent, err := NewAgent(
		"review",
		WithModel(model),
		WithTools(bladestools.NewExitTool()),
		WithMaxIterations(5),
	)
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}

	msg, err := NewRunner(agent).Run(context.Background(), UserMessage("review this"))
	if err != nil {
		t.Fatalf("runner run: %v", err)
	}
	if got, want := model.calls, 1; got != want {
		t.Fatalf("model calls = %d, want %d", got, want)
	}
	if got, want := msg.Role, RoleTool; got != want {
		t.Fatalf("message role = %q, want %q", got, want)
	}
	if _, ok := msg.Actions[bladestools.ActionLoopExit]; !ok {
		t.Fatalf("expected %q action on final tool message", bladestools.ActionLoopExit)
	}
}
