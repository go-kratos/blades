package flow

import (
	"context"
	"errors"
	"testing"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/tools"
)

// echoModel is a minimal ModelProvider that returns a fixed text message.
type echoModel struct {
	name string
	text string
}

func (m *echoModel) Name() string { return m.name }

func (m *echoModel) Generate(_ context.Context, _ *blades.ModelRequest) (*blades.ModelResponse, error) {
	msg := blades.NewAssistantMessage(blades.StatusCompleted)
	msg.Parts = append(msg.Parts, blades.TextPart{Text: m.text})
	return &blades.ModelResponse{Message: msg}, nil
}

func (m *echoModel) NewStreaming(context.Context, *blades.ModelRequest) blades.Generator[*blades.ModelResponse, error] {
	return nil
}

// exitOnNthAgent is a sub-agent that yields one message per Run call, setting
// ActionLoopExit on the Nth call to simulate ExitTool being invoked by the LLM.
type exitOnNthAgent struct {
	target   int
	calls    int
	escalate bool
}

func (a *exitOnNthAgent) Name() string        { return "exit-on-nth" }
func (a *exitOnNthAgent) Description() string { return "" }

func (a *exitOnNthAgent) Run(_ context.Context, _ *blades.Invocation) blades.Generator[*blades.Message, error] {
	return func(yield func(*blades.Message, error) bool) {
		a.calls++
		msg := blades.NewAssistantMessage(blades.StatusCompleted)
		msg.Parts = append(msg.Parts, blades.TextPart{Text: "output"})
		if a.calls >= a.target {
			msg.Actions = map[string]any{
				tools.ActionLoopExit: a.escalate,
			}
		}
		yield(msg, nil)
	}
}

func newEchoAgent(t *testing.T, name, text string) blades.Agent {
	t.Helper()
	a, err := blades.NewAgent(name, blades.WithModel(&echoModel{name: name, text: text}))
	if err != nil {
		t.Fatalf("create agent %q: %v", name, err)
	}
	return a
}

// drainLoop collects all yielded messages and the final error from a loopAgent.
func drainLoop(ctx context.Context, loop blades.Agent, msg *blades.Message) ([]*blades.Message, error) {
	inv := &blades.Invocation{Message: msg}
	var msgs []*blades.Message
	var finalErr error
	for m, err := range loop.Run(ctx, inv) {
		if err != nil {
			finalErr = err
			break
		}
		if m != nil {
			msgs = append(msgs, m)
		}
	}
	return msgs, finalErr
}

// --- LoopState unit tests ---

func TestLoopState_ExitSignal(t *testing.T) {
	t.Parallel()
	var capturedOutput *blades.Message
	exiter := &exitOnNthAgent{target: 1}
	loop := NewLoopAgent(LoopConfig{
		Name:          "test",
		MaxIterations: 5,
		SubAgents:     []blades.Agent{exiter},
		Condition: func(_ context.Context, state LoopState) (bool, error) {
			capturedOutput = state.Output
			return false, nil
		},
	})
	if _, err := drainLoop(context.Background(), loop, blades.UserMessage("go")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedOutput == nil {
		t.Fatal("expected loop state to capture the last output message")
	}
	got, ok := capturedOutput.Actions[tools.ActionLoopExit]
	if !ok {
		t.Fatalf("expected output actions to include %q", tools.ActionLoopExit)
	}
	escalated, ok := got.(bool)
	if !ok {
		t.Fatalf("expected exit action to be bool, got %T", got)
	}
	if escalated {
		t.Error("expected exit action to be false")
	}
}

// --- loopAgent integration tests ---

func TestLoopAgent_MaxIterations(t *testing.T) {
	t.Parallel()
	a := newEchoAgent(t, "worker", "hello")
	loop := NewLoopAgent(LoopConfig{
		Name:          "test",
		MaxIterations: 3,
		SubAgents:     []blades.Agent{a},
	})
	msgs, err := drainLoop(context.Background(), loop, blades.UserMessage("go"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("expected 1 final message, got %d", len(msgs))
	}
}

func TestLoopAgent_ExitSignal(t *testing.T) {
	t.Parallel()
	exiter := &exitOnNthAgent{target: 1}
	loop := NewLoopAgent(LoopConfig{
		Name:          "test",
		MaxIterations: 10,
		SubAgents:     []blades.Agent{exiter},
	})
	msgs, err := drainLoop(context.Background(), loop, blades.UserMessage("go"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("expected 1 message (stopped after first iteration), got %d", len(msgs))
	}
}

func TestLoopAgent_ExitSignal_FromEarlierSubAgent(t *testing.T) {
	t.Parallel()
	exiter := &exitOnNthAgent{target: 1}
	a := newEchoAgent(t, "worker", "hello")
	loop := NewLoopAgent(LoopConfig{
		Name:          "test",
		MaxIterations: 10,
		SubAgents:     []blades.Agent{exiter, a},
	})
	msgs, err := drainLoop(context.Background(), loop, blades.UserMessage("go"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("expected 1 final message from one iteration, got %d", len(msgs))
	}
	if got := msgs[0].Text(); got != "hello" {
		t.Errorf("final message = %q, want %q", got, "hello")
	}
}

func TestLoopAgent_ExitSignal_Escalate_NoCondition(t *testing.T) {
	t.Parallel()
	// Without a Condition, ExitTool with Escalate=true should yield ErrLoopEscalated.
	exiter := &exitOnNthAgent{target: 1, escalate: true}
	loop := NewLoopAgent(LoopConfig{
		Name:          "test",
		MaxIterations: 5,
		SubAgents:     []blades.Agent{exiter},
	})
	_, err := drainLoop(context.Background(), loop, blades.UserMessage("go"))
	if !errors.Is(err, blades.ErrLoopEscalated) {
		t.Errorf("expected ErrLoopEscalated, got %v", err)
	}
}

func TestLoopAgent_ExitSignal_Escalate_FromEarlierSubAgent(t *testing.T) {
	t.Parallel()
	exiter := &exitOnNthAgent{target: 1, escalate: true}
	a := newEchoAgent(t, "worker", "hello")
	loop := NewLoopAgent(LoopConfig{
		Name:          "test",
		MaxIterations: 5,
		SubAgents:     []blades.Agent{exiter, a},
	})
	msgs, err := drainLoop(context.Background(), loop, blades.UserMessage("go"))
	if !errors.Is(err, blades.ErrLoopEscalated) {
		t.Fatalf("expected ErrLoopEscalated, got %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("expected 1 final message before escalation, got %d", len(msgs))
	}
	if got := msgs[0].Text(); got != "hello" {
		t.Errorf("final message = %q, want %q", got, "hello")
	}
}

func TestLoopAgent_ExitSignal_Escalate(t *testing.T) {
	t.Parallel()
	// With a Condition, escalation is expressed by returning ErrLoopEscalated directly.
	a := newEchoAgent(t, "worker", "output")
	loop := NewLoopAgent(LoopConfig{
		Name:          "test",
		MaxIterations: 5,
		SubAgents:     []blades.Agent{a},
		Condition: func(_ context.Context, state LoopState) (bool, error) {
			if state.Iteration == 0 {
				return false, blades.ErrLoopEscalated
			}
			return true, nil
		},
	})
	_, err := drainLoop(context.Background(), loop, blades.UserMessage("go"))
	if !errors.Is(err, blades.ErrLoopEscalated) {
		t.Errorf("expected ErrLoopEscalated, got %v", err)
	}
}

func TestLoopAgent_Condition_Overrides_ExitTool(t *testing.T) {
	t.Parallel()
	// ExitTool fires on iteration 0, but Condition always returns true (continue).
	exiter := &exitOnNthAgent{target: 1}
	const maxIter = 3
	loop := NewLoopAgent(LoopConfig{
		Name:          "test",
		MaxIterations: maxIter,
		SubAgents:     []blades.Agent{exiter},
		Condition: func(_ context.Context, _ LoopState) (bool, error) {
			return true, nil // always continue, overriding ExitTool
		},
	})
	msgs, err := drainLoop(context.Background(), loop, blades.UserMessage("go"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("expected 1 final message after forced iterations, got %d", len(msgs))
	}
}
