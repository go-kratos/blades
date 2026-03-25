package blades

import (
	"context"
	"testing"

	bladestools "github.com/go-kratos/blades/tools"
)

// captureMessagesModel records req.Messages from every Generate call.
type captureMessagesModel struct {
	calls    int
	captured [][]*Message
}

func (m *captureMessagesModel) Name() string { return "capture-messages" }

func (m *captureMessagesModel) Generate(_ context.Context, req *ModelRequest) (*ModelResponse, error) {
	m.calls++
	snapshot := make([]*Message, len(req.Messages))
	copy(snapshot, req.Messages)
	m.captured = append(m.captured, snapshot)

	msg := NewAssistantMessage(StatusCompleted)
	msg.Parts = append(msg.Parts, TextPart{Text: "reply"})
	return &ModelResponse{Message: msg}, nil
}

func (m *captureMessagesModel) NewStreaming(context.Context, *ModelRequest) Generator[*ModelResponse, error] {
	return nil
}

// TestWithContextFalse_DefaultStateless verifies that with the default
// (useContext=false) the model receives only the current invocation message
// and no prior history, even when a session with existing history is supplied.
func TestWithContextFalse_DefaultStateless(t *testing.T) {
	t.Parallel()

	model := &captureMessagesModel{}
	a, err := NewAgent("ctx-false-agent", WithModel(model))
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}
	runner := NewRunner(a)
	session := NewSession()

	// First run – "turn 1"
	if _, err := runner.Run(context.Background(), UserMessage("turn1"), WithSession(session)); err != nil {
		t.Fatalf("first run: %v", err)
	}

	// Second run – "turn 2". Prior history must NOT be included.
	if _, err := runner.Run(context.Background(), UserMessage("turn2"), WithSession(session)); err != nil {
		t.Fatalf("second run: %v", err)
	}

	if got, want := model.calls, 2; got != want {
		t.Fatalf("model calls = %d, want %d", got, want)
	}
	// Each run should see exactly one message (the current user message).
	if got, want := len(model.captured[0]), 1; got != want {
		t.Fatalf("first call messages len = %d, want %d", got, want)
	}
	if got, want := model.captured[0][0].Text(), "turn1"; got != want {
		t.Fatalf("first call message = %q, want %q", got, want)
	}
	if got, want := len(model.captured[1]), 1; got != want {
		t.Fatalf("second call messages len = %d, want %d", got, want)
	}
	if got, want := model.captured[1][0].Text(), "turn2"; got != want {
		t.Fatalf("second call message = %q, want %q", got, want)
	}
}

// TestWithContextTrue_LoadsSessionHistory verifies that WithContext(true)
// causes the model to receive the full session history on each call.
func TestWithContextTrue_LoadsSessionHistory(t *testing.T) {
	t.Parallel()

	model := &captureMessagesModel{}
	a, err := NewAgent("ctx-true-agent", WithModel(model), WithContext(true))
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}
	runner := NewRunner(a)
	session := NewSession()

	// First run – "turn 1"
	if _, err := runner.Run(context.Background(), UserMessage("turn1"), WithSession(session)); err != nil {
		t.Fatalf("first run: %v", err)
	}

	// Second run – "turn 2". Session history (user1 + assistant1) must be included.
	if _, err := runner.Run(context.Background(), UserMessage("turn2"), WithSession(session)); err != nil {
		t.Fatalf("second run: %v", err)
	}

	if got, want := model.calls, 2; got != want {
		t.Fatalf("model calls = %d, want %d", got, want)
	}
	// First call: only the first user message.
	if got, want := len(model.captured[0]), 1; got != want {
		t.Fatalf("first call messages len = %d, want %d", got, want)
	}
	// Second call: user1 + assistant1 + user2 = 3 messages.
	if got, want := len(model.captured[1]), 3; got != want {
		t.Fatalf("second call messages len = %d, want %d", got, want)
	}
	if got, want := model.captured[1][0].Text(), "turn1"; got != want {
		t.Fatalf("second call message[0] = %q, want %q", got, want)
	}
	if got, want := model.captured[1][1].Text(), "reply"; got != want {
		t.Fatalf("second call message[1] = %q, want %q", got, want)
	}
	if got, want := model.captured[1][2].Text(), "turn2"; got != want {
		t.Fatalf("second call message[2] = %q, want %q", got, want)
	}
}

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
