package blades

import (
	"context"
	"testing"
)

type resumeCaptureModel struct {
	req   *ModelRequest
	calls int
}

func (m *resumeCaptureModel) Name() string { return "resume-capture" }

func (m *resumeCaptureModel) Generate(_ context.Context, req *ModelRequest) (*ModelResponse, error) {
	m.calls++
	m.req = req
	msg := NewAssistantMessage(StatusCompleted)
	msg.Parts = append(msg.Parts, TextPart{Text: "done"})
	return &ModelResponse{Message: msg}, nil
}

func (m *resumeCaptureModel) NewStreaming(context.Context, *ModelRequest) Generator[*ModelResponse, error] {
	return nil
}

func TestAgentResumeUsesStoredIntermediateMessages(t *testing.T) {
	t.Parallel()

	model := &resumeCaptureModel{}
	agent, err := NewAgent("resume-agent", WithModel(model))
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}

	session := NewSession()
	const invocationID = "resume-invocation-id"
	resumeMessage := NewAssistantMessage(StatusCompleted)
	resumeMessage.Role = RoleTool
	resumeMessage.Author = "resume-agent"
	resumeMessage.InvocationID = invocationID
	resumeMessage.Parts = append(resumeMessage.Parts, ToolPart{
		ID:        "call_1",
		Name:      "lookup",
		Request:   `{"value":"hi"}`,
		Response:  `{"ok":true}`,
		Completed: true,
	})
	if err := session.Append(context.Background(), resumeMessage); err != nil {
		t.Fatalf("append resume message: %v", err)
	}

	runner := NewRunner(agent)
	if _, err := runner.Run(
		context.Background(),
		UserMessage("fresh input should be ignored on resume"),
		WithSession(session),
		WithInvocationID(invocationID),
		WithResume(true),
	); err != nil {
		t.Fatalf("runner run: %v", err)
	}

	if got, want := model.calls, 1; got != want {
		t.Fatalf("model calls = %d, want %d", got, want)
	}
	if model.req == nil {
		t.Fatalf("model request not captured")
	}
	if got, want := len(model.req.Messages), 1; got != want {
		t.Fatalf("messages len = %d, want %d", got, want)
	}

	got := model.req.Messages[0]
	if got.Role != RoleTool {
		t.Fatalf("message role = %q, want %q", got.Role, RoleTool)
	}
	if got.Author != "resume-agent" {
		t.Fatalf("message author = %q, want %q", got.Author, "resume-agent")
	}
	toolPart, ok := got.Parts[0].(ToolPart)
	if !ok {
		t.Fatalf("part type = %T, want ToolPart", got.Parts[0])
	}
	if got, want := toolPart.Completed, true; got != want {
		t.Fatalf("tool completed = %t, want %t", got, want)
	}
	if got, want := toolPart.Response, `{"ok":true}`; got != want {
		t.Fatalf("tool response = %q, want %q", got, want)
	}
}
