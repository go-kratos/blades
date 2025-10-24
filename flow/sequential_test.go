package flow

import (
	"context"
	"errors"
	"testing"

	"github.com/go-kratos/blades"
)

// mockRunnable is a mock implementation of blades.Runnable for testing
type mockRunnable struct {
	name      string
	output    *blades.Message
	err       error
	callCount int
}

func (m *mockRunnable) Name() string {
	return m.name
}

func (m *mockRunnable) Run(ctx context.Context, input *blades.Prompt, opts ...blades.ModelOption) (*blades.Message, error) {
	m.callCount++
	return m.output, m.err
}

func (m *mockRunnable) RunStream(ctx context.Context, input *blades.Prompt, opts ...blades.ModelOption) (blades.Streamable[*blades.Message], error) {
	pipe := blades.NewStreamPipe[*blades.Message]()
	pipe.Go(func() error {
		output, err := m.Run(ctx, input, opts...)
		if err != nil {
			return err
		}
		pipe.Send(output)
		return nil
	})
	return pipe, nil
}

func TestNewSequential(t *testing.T) {
	runners := []blades.Runnable{
		&mockRunnable{name: "runner1"},
		&mockRunnable{name: "runner2"},
	}

	seq := NewSequential("test-sequential", runners...)

	if seq.name != "test-sequential" {
		t.Errorf("Sequential.name = %v, want test-sequential", seq.name)
	}
	if len(seq.runners) != 2 {
		t.Errorf("Sequential.runners length = %v, want 2", len(seq.runners))
	}
}

func TestSequentialName(t *testing.T) {
	seq := NewSequential("test-name")
	if seq.Name() != "test-name" {
		t.Errorf("Sequential.Name() = %v, want test-name", seq.Name())
	}
}

func TestSequentialRun(t *testing.T) {
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

	seq := NewSequential("test-sequential", runners...)

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

	output, err := seq.Run(ctx, input)
	if err != nil {
		t.Errorf("Sequential.Run() returned error: %v", err)
	}

	if output != output2 {
		t.Errorf("Sequential.Run() output = %v, want %v", output, output2)
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

func TestSequentialRunWithError(t *testing.T) {
	expectedErr := errors.New("runner error")

	runners := []blades.Runnable{
		&mockRunnable{name: "runner1", err: expectedErr},
		&mockRunnable{name: "runner2"},
	}

	seq := NewSequential("test-sequential", runners...)

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

	_, err := seq.Run(ctx, input)
	if err == nil {
		t.Errorf("Sequential.Run() should return error")
	}
	if err != expectedErr {
		t.Errorf("Sequential.Run() error = %v, want %v", err, expectedErr)
	}

	// Check that only the first runner was called
	runner1 := runners[0].(*mockRunnable)
	runner2 := runners[1].(*mockRunnable)

	if runner1.callCount != 1 {
		t.Errorf("Runner1 call count = %v, want 1", runner1.callCount)
	}
	if runner2.callCount != 0 {
		t.Errorf("Runner2 call count = %v, want 0", runner2.callCount)
	}
}

func TestSequentialRunEmpty(t *testing.T) {
	seq := NewSequential("test-sequential")

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

	output, err := seq.Run(ctx, input)
	if err != nil {
		t.Errorf("Sequential.Run() returned error: %v", err)
	}
	if output != nil {
		t.Errorf("Sequential.Run() output = %v, want nil", output)
	}
}

func TestSequentialRunStream(t *testing.T) {
	output := &blades.Message{
		ID:    "msg-1",
		Role:  blades.RoleAssistant,
		Parts: []blades.Part{blades.TextPart{Text: "Output"}},
	}

	runners := []blades.Runnable{
		&mockRunnable{name: "runner1", output: output},
	}

	seq := NewSequential("test-sequential", runners...)

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

	stream, err := seq.RunStream(ctx, input)
	if err != nil {
		t.Errorf("Sequential.RunStream() returned error: %v", err)
	}

	if stream == nil {
		t.Errorf("Sequential.RunStream() returned nil stream")
	}

	// Test that stream implements Streamable interface
	var _ blades.Streamable[*blades.Message] = stream
}

func TestSequentialRunStreamWithError(t *testing.T) {
	expectedErr := errors.New("runner error")

	runners := []blades.Runnable{
		&mockRunnable{name: "runner1", err: expectedErr},
	}

	seq := NewSequential("test-sequential", runners...)

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

	stream, err := seq.RunStream(ctx, input)
	if err != nil {
		t.Errorf("Sequential.RunStream() returned error: %v", err)
	}

	if stream == nil {
		t.Errorf("Sequential.RunStream() returned nil stream")
	}

	// Test that stream implements Streamable interface
	var _ blades.Streamable[*blades.Message] = stream
}
