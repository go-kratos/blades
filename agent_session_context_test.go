package blades

import (
	"context"
	"testing"

	bladestools "github.com/go-kratos/blades/tools"
)

type toolLoopSessionModel struct {
	calls       int
	secondInput []*Message
}

func (m *toolLoopSessionModel) Name() string { return "tool-loop-session" }

func (m *toolLoopSessionModel) Generate(_ context.Context, req *ModelRequest) (*ModelResponse, error) {
	m.calls++
	if m.calls == 2 {
		m.secondInput = append(m.secondInput[:0], req.Messages...)
	}

	msg := NewAssistantMessage(StatusCompleted)
	if m.calls == 1 {
		msg.Role = RoleTool
		msg.Parts = append(msg.Parts, NewToolPart("call_1", "echo", `{"value":"hi"}`))
		return &ModelResponse{Message: msg}, nil
	}

	msg.Parts = append(msg.Parts, TextPart{Text: "done"})
	return &ModelResponse{Message: msg}, nil
}

func (m *toolLoopSessionModel) NewStreaming(context.Context, *ModelRequest) Generator[*ModelResponse, error] {
	return nil
}

func TestAgentRunWithSessionAndNilPreparedContextKeepsInvocationMessagesAcrossToolLoop(t *testing.T) {
	t.Parallel()

	model := &toolLoopSessionModel{}
	tool := bladestools.NewTool("echo", "echo", bladestools.HandleFunc(func(context.Context, string) (string, error) {
		return `{"ok":true}`, nil
	}))
	agent, err := NewAgent("tool-agent", WithModel(model), WithTools(tool))
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}

	runner := NewRunner(agent)
	session := NewSession()
	output, err := runner.Run(context.Background(), UserMessage("hello"), WithSession(session))
	if err != nil {
		t.Fatalf("runner run: %v", err)
	}
	if got, want := output.Text(), "done"; got != want {
		t.Fatalf("output text = %q, want %q", got, want)
	}
	if got, want := model.calls, 2; got != want {
		t.Fatalf("model calls = %d, want %d", got, want)
	}
	if got, want := len(model.secondInput), 2; got != want {
		t.Fatalf("second input len = %d, want %d", got, want)
	}
	if got, want := model.secondInput[0].Role, RoleUser; got != want {
		t.Fatalf("second input first role = %q, want %q", got, want)
	}
	if got, want := model.secondInput[0].Text(), "hello"; got != want {
		t.Fatalf("second input first text = %q, want %q", got, want)
	}
	if got, want := model.secondInput[1].Role, RoleTool; got != want {
		t.Fatalf("second input second role = %q, want %q", got, want)
	}
	part, ok := model.secondInput[1].Parts[0].(ToolPart)
	if !ok {
		t.Fatalf("second input second part type = %T, want ToolPart", model.secondInput[1].Parts[0])
	}
	if got, want := part.Completed, true; got != want {
		t.Fatalf("tool completed = %t, want %t", got, want)
	}
	if got, want := part.Response, `{"ok":true}`; got != want {
		t.Fatalf("tool response = %q, want %q", got, want)
	}
}
