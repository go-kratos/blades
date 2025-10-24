package flow

import (
	"context"
	"errors"
	"testing"

	"github.com/go-kratos/blades"
)

func TestNewParallel(t *testing.T) {
	runners := []blades.Runnable{
		&mockRunnable{name: "runner1"},
		&mockRunnable{name: "runner2"},
	}

	parallel := NewParallel("test-parallel", runners)

	if parallel.name != "test-parallel" {
		t.Errorf("Parallel.name = %v, want test-parallel", parallel.name)
	}
	if len(parallel.runners) != 2 {
		t.Errorf("Parallel.runners length = %v, want 2", len(parallel.runners))
	}
	if parallel.merger == nil {
		t.Errorf("Parallel.merger should not be nil")
	}
}

func TestNewParallelWithOptions(t *testing.T) {
	runners := []blades.Runnable{
		&mockRunnable{name: "runner1"},
	}

	customMerger := func(ctx context.Context, outputs []*blades.Message) (*blades.Message, error) {
		return outputs[0], nil
	}

	parallel := NewParallel("test-parallel", runners, WithParallelMerger(customMerger))

	if parallel.name != "test-parallel" {
		t.Errorf("Parallel.name = %v, want test-parallel", parallel.name)
	}
	if parallel.merger == nil {
		t.Errorf("Parallel.merger should not be nil")
	}
}

func TestParallelName(t *testing.T) {
	parallel := NewParallel("test-name", []blades.Runnable{})
	if parallel.Name() != "test-name" {
		t.Errorf("Parallel.Name() = %v, want test-name", parallel.Name())
	}
}

func TestParallelRun(t *testing.T) {
	output1 := &blades.Message{
		ID:    "msg-1",
		Role:  blades.RoleAssistant,
		Parts: []blades.Part{blades.TextPart{Text: "Output 1"}},
	}
	output2 := &blades.Message{
		ID:    "msg-2",
		Role:  blades.RoleAssistant,
		Parts: []blades.Part{blades.TextPart{Text: "Output 2"}},
	}

	runners := []blades.Runnable{
		&mockRunnable{name: "runner1", output: output1},
		&mockRunnable{name: "runner2", output: output2},
	}

	parallel := NewParallel("test-parallel", runners)

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

	output, err := parallel.Run(ctx, input)
	if err != nil {
		t.Errorf("Parallel.Run() returned error: %v", err)
	}

	if output == nil {
		t.Errorf("Parallel.Run() output should not be nil")
	}

	// Check that runners were called
	runner1 := runners[0].(*mockRunnable)
	runner2 := runners[1].(*mockRunnable)

	if runner1.callCount != 1 {
		t.Errorf("Runner1 call count = %v, want 1", runner1.callCount)
	}
	if runner2.callCount != 1 {
		t.Errorf("Runner2 call count = %v, want 1", runner2.callCount)
	}
}

func TestParallelRunWithError(t *testing.T) {
	expectedErr := errors.New("runner error")

	runners := []blades.Runnable{
		&mockRunnable{name: "runner1", err: expectedErr},
		&mockRunnable{name: "runner2"},
	}

	parallel := NewParallel("test-parallel", runners)

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

	_, err := parallel.Run(ctx, input)
	if err == nil {
		t.Errorf("Parallel.Run() should return error")
	}
	if err != expectedErr {
		t.Errorf("Parallel.Run() error = %v, want %v", err, expectedErr)
	}
}

func TestParallelRunEmpty(t *testing.T) {
	parallel := NewParallel("test-parallel", []blades.Runnable{})

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

	output, err := parallel.Run(ctx, input)
	if err != nil {
		t.Errorf("Parallel.Run() returned error: %v", err)
	}
	if output == nil {
		t.Errorf("Parallel.Run() output should not be nil")
	}
}

func TestParallelRunWithCustomMerger(t *testing.T) {
	output1 := &blades.Message{
		ID:    "msg-1",
		Role:  blades.RoleAssistant,
		Parts: []blades.Part{blades.TextPart{Text: "Output 1"}},
	}
	output2 := &blades.Message{
		ID:    "msg-2",
		Role:  blades.RoleAssistant,
		Parts: []blades.Part{blades.TextPart{Text: "Output 2"}},
	}

	runners := []blades.Runnable{
		&mockRunnable{name: "runner1", output: output1},
		&mockRunnable{name: "runner2", output: output2},
	}

	customMerger := func(ctx context.Context, outputs []*blades.Message) (*blades.Message, error) {
		return outputs[0], nil // Return only the first output
	}

	parallel := NewParallel("test-parallel", runners, WithParallelMerger(customMerger))

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

	output, err := parallel.Run(ctx, input)
	if err != nil {
		t.Errorf("Parallel.Run() returned error: %v", err)
	}

	if output != output1 {
		t.Errorf("Parallel.Run() output = %v, want %v", output, output1)
	}
}

func TestParallelRunWithMergerError(t *testing.T) {
	output1 := &blades.Message{
		ID:    "msg-1",
		Role:  blades.RoleAssistant,
		Parts: []blades.Part{blades.TextPart{Text: "Output 1"}},
	}

	runners := []blades.Runnable{
		&mockRunnable{name: "runner1", output: output1},
	}

	expectedErr := errors.New("merger error")
	customMerger := func(ctx context.Context, outputs []*blades.Message) (*blades.Message, error) {
		return nil, expectedErr
	}

	parallel := NewParallel("test-parallel", runners, WithParallelMerger(customMerger))

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

	_, err := parallel.Run(ctx, input)
	if err == nil {
		t.Errorf("Parallel.Run() should return error")
	}
	if err != expectedErr {
		t.Errorf("Parallel.Run() error = %v, want %v", err, expectedErr)
	}
}

func TestParallelRunStream(t *testing.T) {
	output := &blades.Message{
		ID:    "msg-1",
		Role:  blades.RoleAssistant,
		Parts: []blades.Part{blades.TextPart{Text: "Output"}},
	}

	runners := []blades.Runnable{
		&mockRunnable{name: "runner1", output: output},
	}

	parallel := NewParallel("test-parallel", runners)

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

	stream, err := parallel.RunStream(ctx, input)
	if err != nil {
		t.Errorf("Parallel.RunStream() returned error: %v", err)
	}

	if stream == nil {
		t.Errorf("Parallel.RunStream() returned nil stream")
	}

	// Test that stream implements Streamable interface
	var _ blades.Streamable[*blades.Message] = stream
}

func TestParallelRunStreamWithError(t *testing.T) {
	expectedErr := errors.New("runner error")

	runners := []blades.Runnable{
		&mockRunnable{name: "runner1", err: expectedErr},
	}

	parallel := NewParallel("test-parallel", runners)

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

	stream, err := parallel.RunStream(ctx, input)
	if err != nil {
		t.Errorf("Parallel.RunStream() returned error: %v", err)
	}

	if stream == nil {
		t.Errorf("Parallel.RunStream() returned nil stream")
	}

	// Test that stream implements Streamable interface
	var _ blades.Streamable[*blades.Message] = stream
}
