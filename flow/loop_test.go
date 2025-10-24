package flow

import (
	"context"
	"errors"
	"testing"

	"github.com/go-kratos/blades"
)

func TestNewLoop(t *testing.T) {
	condition := func(ctx context.Context, output *blades.Message) (bool, error) {
		return false, nil
	}

	runner := &mockRunnable{name: "runner1"}

	loop := NewLoop("test-loop", condition, runner)

	if loop.name != "test-loop" {
		t.Errorf("Loop.name = %v, want test-loop", loop.name)
	}
	if loop.condition == nil {
		t.Errorf("Loop.condition should not be nil")
	}
	if loop.runner != runner {
		t.Errorf("Loop.runner = %v, want %v", loop.runner, runner)
	}
	if loop.maxIterations != 3 {
		t.Errorf("Loop.maxIterations = %v, want 3", loop.maxIterations)
	}
}

func TestNewLoopWithOptions(t *testing.T) {
	condition := func(ctx context.Context, output *blades.Message) (bool, error) {
		return false, nil
	}

	runner := &mockRunnable{name: "runner1"}

	loop := NewLoop("test-loop", condition, runner, WithLoopMaxIterations(5))

	if loop.name != "test-loop" {
		t.Errorf("Loop.name = %v, want test-loop", loop.name)
	}
	if loop.maxIterations != 5 {
		t.Errorf("Loop.maxIterations = %v, want 5", loop.maxIterations)
	}
}

func TestLoopName(t *testing.T) {
	condition := func(ctx context.Context, output *blades.Message) (bool, error) {
		return false, nil
	}

	loop := NewLoop("test-name", condition, &mockRunnable{})
	if loop.Name() != "test-name" {
		t.Errorf("Loop.Name() = %v, want test-name", loop.Name())
	}
}

func TestLoopRun(t *testing.T) {
	output := &blades.Message{
		ID:    "msg-1",
		Role:  blades.RoleAssistant,
		Parts: []blades.Part{blades.TextPart{Text: "Output"}},
	}

	condition := func(ctx context.Context, output *blades.Message) (bool, error) {
		return false, nil // Stop after first iteration
	}

	runner := &mockRunnable{name: "runner1", output: output}
	loop := NewLoop("test-loop", condition, runner)

	ctx := context.Background()
	input := &blades.Prompt{
		Messages: []*blades.Message{
			{
				ID:    "msg-0",
				Role:  blades.RoleUser,
				Parts: []blades.Part{blades.TextPart{Text: "Input"}},
			},
		},
	}

	result, err := loop.Run(ctx, input)
	if err != nil {
		t.Errorf("Loop.Run() returned error: %v", err)
	}

	if result != output {
		t.Errorf("Loop.Run() result = %v, want %v", result, output)
	}

	// Check that runner was called once
	if runner.callCount != 1 {
		t.Errorf("Runner call count = %v, want 1", runner.callCount)
	}
}

func TestLoopRunMultipleIterations(t *testing.T) {
	output := &blades.Message{
		ID:    "msg-1",
		Role:  blades.RoleAssistant,
		Parts: []blades.Part{blades.TextPart{Text: "Output"}},
	}

	iterationCount := 0
	condition := func(ctx context.Context, output *blades.Message) (bool, error) {
		iterationCount++
		return iterationCount < 2, nil // Continue for 2 iterations
	}

	runner := &mockRunnable{name: "runner1", output: output}
	loop := NewLoop("test-loop", condition, runner)

	ctx := context.Background()
	input := &blades.Prompt{
		Messages: []*blades.Message{
			{
				ID:    "msg-0",
				Role:  blades.RoleUser,
				Parts: []blades.Part{blades.TextPart{Text: "Input"}},
			},
		},
	}

	result, err := loop.Run(ctx, input)
	if err != nil {
		t.Errorf("Loop.Run() returned error: %v", err)
	}

	if result != output {
		t.Errorf("Loop.Run() result = %v, want %v", result, output)
	}

	// Check that runner was called twice
	if runner.callCount != 2 {
		t.Errorf("Runner call count = %v, want 2", runner.callCount)
	}
}

func TestLoopRunMaxIterations(t *testing.T) {
	output := &blades.Message{
		ID:    "msg-1",
		Role:  blades.RoleAssistant,
		Parts: []blades.Part{blades.TextPart{Text: "Output"}},
	}

	condition := func(ctx context.Context, output *blades.Message) (bool, error) {
		return true, nil // Always continue
	}

	runner := &mockRunnable{name: "runner1", output: output}
	loop := NewLoop("test-loop", condition, runner, WithLoopMaxIterations(2))

	ctx := context.Background()
	input := &blades.Prompt{
		Messages: []*blades.Message{
			{
				ID:    "msg-0",
				Role:  blades.RoleUser,
				Parts: []blades.Part{blades.TextPart{Text: "Input"}},
			},
		},
	}

	result, err := loop.Run(ctx, input)
	if err != nil {
		t.Errorf("Loop.Run() returned error: %v", err)
	}

	if result != output {
		t.Errorf("Loop.Run() result = %v, want %v", result, output)
	}

	// Check that runner was called maxIterations times
	if runner.callCount != 2 {
		t.Errorf("Runner call count = %v, want 2", runner.callCount)
	}
}

func TestLoopRunWithRunnerError(t *testing.T) {
	expectedErr := errors.New("runner error")
	condition := func(ctx context.Context, output *blades.Message) (bool, error) {
		return false, nil
	}

	runner := &mockRunnable{name: "runner1", err: expectedErr}
	loop := NewLoop("test-loop", condition, runner)

	ctx := context.Background()
	input := &blades.Prompt{
		Messages: []*blades.Message{
			{
				ID:    "msg-0",
				Role:  blades.RoleUser,
				Parts: []blades.Part{blades.TextPart{Text: "Input"}},
			},
		},
	}

	_, err := loop.Run(ctx, input)
	if err == nil {
		t.Errorf("Loop.Run() should return error")
	}
	if err != expectedErr {
		t.Errorf("Loop.Run() error = %v, want %v", err, expectedErr)
	}
}

func TestLoopRunWithConditionError(t *testing.T) {
	output := &blades.Message{
		ID:    "msg-1",
		Role:  blades.RoleAssistant,
		Parts: []blades.Part{blades.TextPart{Text: "Output"}},
	}

	expectedErr := errors.New("condition error")
	condition := func(ctx context.Context, output *blades.Message) (bool, error) {
		return false, expectedErr
	}

	runner := &mockRunnable{name: "runner1", output: output}
	loop := NewLoop("test-loop", condition, runner)

	ctx := context.Background()
	input := &blades.Prompt{
		Messages: []*blades.Message{
			{
				ID:    "msg-0",
				Role:  blades.RoleUser,
				Parts: []blades.Part{blades.TextPart{Text: "Input"}},
			},
		},
	}

	_, err := loop.Run(ctx, input)
	if err == nil {
		t.Errorf("Loop.Run() should return error")
	}
	if err != expectedErr {
		t.Errorf("Loop.Run() error = %v, want %v", err, expectedErr)
	}
}

func TestLoopRunStream(t *testing.T) {
	output := &blades.Message{
		ID:    "msg-1",
		Role:  blades.RoleAssistant,
		Parts: []blades.Part{blades.TextPart{Text: "Output"}},
	}

	condition := func(ctx context.Context, output *blades.Message) (bool, error) {
		return false, nil // Stop after first iteration
	}

	runner := &mockRunnable{name: "runner1", output: output}
	loop := NewLoop("test-loop", condition, runner)

	ctx := context.Background()
	input := &blades.Prompt{
		Messages: []*blades.Message{
			{
				ID:    "msg-0",
				Role:  blades.RoleUser,
				Parts: []blades.Part{blades.TextPart{Text: "Input"}},
			},
		},
	}

	stream, err := loop.RunStream(ctx, input)
	if err != nil {
		t.Errorf("Loop.RunStream() returned error: %v", err)
	}

	if stream == nil {
		t.Errorf("Loop.RunStream() returned nil stream")
	}

	// Test that stream implements Streamable interface
	var _ blades.Streamable[*blades.Message] = stream
}

func TestLoopRunStreamWithError(t *testing.T) {
	expectedErr := errors.New("runner error")
	condition := func(ctx context.Context, output *blades.Message) (bool, error) {
		return false, nil
	}

	runner := &mockRunnable{name: "runner1", err: expectedErr}
	loop := NewLoop("test-loop", condition, runner)

	ctx := context.Background()
	input := &blades.Prompt{
		Messages: []*blades.Message{
			{
				ID:    "msg-0",
				Role:  blades.RoleUser,
				Parts: []blades.Part{blades.TextPart{Text: "Input"}},
			},
		},
	}

	stream, err := loop.RunStream(ctx, input)
	if err != nil {
		t.Errorf("Loop.RunStream() returned error: %v", err)
	}

	if stream == nil {
		t.Errorf("Loop.RunStream() returned nil stream")
	}

	// Test that stream implements Streamable interface
	var _ blades.Streamable[*blades.Message] = stream
}

func TestLoopCondition(t *testing.T) {
	output := &blades.Message{
		ID:    "msg-1",
		Role:  blades.RoleAssistant,
		Parts: []blades.Part{blades.TextPart{Text: "Output"}},
	}

	// Test condition that checks message content
	condition := func(ctx context.Context, output *blades.Message) (bool, error) {
		text := output.Text()
		return text == "continue", nil
	}

	runner := &mockRunnable{name: "runner1", output: output}
	loop := NewLoop("test-loop", condition, runner)

	ctx := context.Background()
	input := &blades.Prompt{
		Messages: []*blades.Message{
			{
				ID:    "msg-0",
				Role:  blades.RoleUser,
				Parts: []blades.Part{blades.TextPart{Text: "Input"}},
			},
		},
	}

	result, err := loop.Run(ctx, input)
	if err != nil {
		t.Errorf("Loop.Run() returned error: %v", err)
	}

	if result != output {
		t.Errorf("Loop.Run() result = %v, want %v", result, output)
	}

	// Check that runner was called once (condition returned false)
	if runner.callCount != 1 {
		t.Errorf("Runner call count = %v, want 1", runner.callCount)
	}
}
