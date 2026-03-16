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
	target int
	calls  int
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
				tools.ActionLoopExit: tools.ExitInput{Reason: "done"},
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

func TestLoopState_Reason(t *testing.T) {
	t.Parallel()
	// Reason is populated when the loop reads ActionLoopExit from a message.
	var capturedReason string
	exiter := &exitOnNthAgent{target: 1}
	loop := NewLoopAgent(LoopConfig{
		Name:          "test",
		MaxIterations: 5,
		SubAgents:     []blades.Agent{exiter},
		Condition: func(_ context.Context, state LoopState) (LoopPhase, error) {
			capturedReason = state.Reason()
			return PhaseComplete, nil
		},
	})
	drainLoop(context.Background(), loop, blades.UserMessage("go")) //nolint:errcheck
	if capturedReason != "done" {
		t.Errorf("expected reason %q, got %q", "done", capturedReason)
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
	if len(msgs) != 3 {
		t.Errorf("expected 3 messages (one per iteration), got %d", len(msgs))
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
	// ActionLoopExit set on first call → only 1 iteration → 1 message
	if len(msgs) != 1 {
		t.Errorf("expected 1 message (stopped after first iteration), got %d", len(msgs))
	}
}

func TestLoopAgent_ExitSignal_Escalate(t *testing.T) {
	t.Parallel()
	// Use a Condition that escalates on the first iteration.
	a := newEchoAgent(t, "worker", "output")
	loop := NewLoopAgent(LoopConfig{
		Name:          "test",
		MaxIterations: 5,
		SubAgents:     []blades.Agent{a},
		Condition: func(_ context.Context, state LoopState) (LoopPhase, error) {
			if state.Iteration == 0 {
				return PhaseEscalate, nil
			}
			return PhaseContinue, nil
		},
	})
	_, err := drainLoop(context.Background(), loop, blades.UserMessage("go"))
	if !errors.Is(err, blades.ErrLoopEscalated) {
		t.Errorf("expected ErrLoopEscalated, got %v", err)
	}
}

func TestLoopAgent_Condition_Overrides_ExitTool(t *testing.T) {
	t.Parallel()
	// ExitTool signals PhaseComplete on iteration 0, but Condition always returns Continue.
	exiter := &exitOnNthAgent{target: 1}
	const maxIter = 3
	loop := NewLoopAgent(LoopConfig{
		Name:          "test",
		MaxIterations: maxIter,
		SubAgents:     []blades.Agent{exiter},
		Condition: func(_ context.Context, _ LoopState) (LoopPhase, error) {
			return PhaseContinue, nil // always continue, overriding ExitTool
		},
	})
	msgs, err := drainLoop(context.Background(), loop, blades.UserMessage("go"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(msgs) != maxIter {
		t.Errorf("expected %d messages (condition forced all iterations), got %d", maxIter, len(msgs))
	}
}
