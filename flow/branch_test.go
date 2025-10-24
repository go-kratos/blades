package flow

import (
	"context"
	"errors"
	"testing"

	"github.com/go-kratos/blades"
)

func TestNewBranch(t *testing.T) {
	condition := func(ctx context.Context, input *blades.Prompt) (string, error) {
		return "branch1", nil
	}

	runners := map[string]blades.Runnable{
		"branch1": &mockRunnable{name: "runner1"},
		"branch2": &mockRunnable{name: "runner2"},
	}

	branch := NewBranch(condition, runners)

	if branch.condition == nil {
		t.Errorf("Branch.condition should not be nil")
	}
	if len(branch.runners) != 2 {
		t.Errorf("Branch.runners length = %v, want 2", len(branch.runners))
	}
}

func TestBranchRun(t *testing.T) {
	condition := func(ctx context.Context, input *blades.Prompt) (string, error) {
		return "branch1", nil
	}

	output1 := &blades.Message{
		ID:    "msg-1",
		Role:  blades.RoleAssistant,
		Parts: []blades.Part{blades.TextPart{Text: "Output 1"}},
	}

	runners := map[string]blades.Runnable{
		"branch1": &mockRunnable{name: "runner1", output: output1},
		"branch2": &mockRunnable{name: "runner2"},
	}

	branch := NewBranch(condition, runners)

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

	output, err := branch.Run(ctx, input)
	if err != nil {
		t.Errorf("Branch.Run() returned error: %v", err)
	}

	if output != output1 {
		t.Errorf("Branch.Run() output = %v, want %v", output, output1)
	}

	// Check that only the selected runner was called
	runner1 := runners["branch1"].(*mockRunnable)
	runner2 := runners["branch2"].(*mockRunnable)

	if runner1.callCount != 1 {
		t.Errorf("Runner1 call count = %v, want 1", runner1.callCount)
	}
	if runner2.callCount != 0 {
		t.Errorf("Runner2 call count = %v, want 0", runner2.callCount)
	}
}

func TestBranchRunWithConditionError(t *testing.T) {
	expectedErr := errors.New("condition error")
	condition := func(ctx context.Context, input *blades.Prompt) (string, error) {
		return "", expectedErr
	}

	runners := map[string]blades.Runnable{
		"branch1": &mockRunnable{name: "runner1"},
	}

	branch := NewBranch(condition, runners)

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

	_, err := branch.Run(ctx, input)
	if err == nil {
		t.Errorf("Branch.Run() should return error")
	}
	if err != expectedErr {
		t.Errorf("Branch.Run() error = %v, want %v", err, expectedErr)
	}
}

func TestBranchRunWithUnknownBranch(t *testing.T) {
	condition := func(ctx context.Context, input *blades.Prompt) (string, error) {
		return "unknown", nil
	}

	runners := map[string]blades.Runnable{
		"branch1": &mockRunnable{name: "runner1"},
	}

	branch := NewBranch(condition, runners)

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

	_, err := branch.Run(ctx, input)
	if err == nil {
		t.Errorf("Branch.Run() should return error")
	}
	if err.Error() != "branch: runner not found: unknown" {
		t.Errorf("Branch.Run() error = %v, want branch: runner not found: unknown", err)
	}
}

func TestBranchRunWithRunnerError(t *testing.T) {
	condition := func(ctx context.Context, input *blades.Prompt) (string, error) {
		return "branch1", nil
	}

	expectedErr := errors.New("runner error")
	runners := map[string]blades.Runnable{
		"branch1": &mockRunnable{name: "runner1", err: expectedErr},
	}

	branch := NewBranch(condition, runners)

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

	_, err := branch.Run(ctx, input)
	if err == nil {
		t.Errorf("Branch.Run() should return error")
	}
	if err != expectedErr {
		t.Errorf("Branch.Run() error = %v, want %v", err, expectedErr)
	}
}

func TestBranchRunStream(t *testing.T) {
	condition := func(ctx context.Context, input *blades.Prompt) (string, error) {
		return "branch1", nil
	}

	output := &blades.Message{
		ID:    "msg-1",
		Role:  blades.RoleAssistant,
		Parts: []blades.Part{blades.TextPart{Text: "Output"}},
	}

	runners := map[string]blades.Runnable{
		"branch1": &mockRunnable{name: "runner1", output: output},
	}

	branch := NewBranch(condition, runners)

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

	stream, err := branch.RunStream(ctx, input)
	if err != nil {
		t.Errorf("Branch.RunStream() returned error: %v", err)
	}

	if stream == nil {
		t.Errorf("Branch.RunStream() returned nil stream")
	}

	// Test that stream implements Streamable interface
	var _ blades.Streamable[*blades.Message] = stream
}

func TestBranchRunStreamWithError(t *testing.T) {
	condition := func(ctx context.Context, input *blades.Prompt) (string, error) {
		return "branch1", nil
	}

	expectedErr := errors.New("runner error")
	runners := map[string]blades.Runnable{
		"branch1": &mockRunnable{name: "runner1", err: expectedErr},
	}

	branch := NewBranch(condition, runners)

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

	stream, err := branch.RunStream(ctx, input)
	if err != nil {
		t.Errorf("Branch.RunStream() returned error: %v", err)
	}

	if stream == nil {
		t.Errorf("Branch.RunStream() returned nil stream")
	}

	// Test that stream implements Streamable interface
	var _ blades.Streamable[*blades.Message] = stream
}

func TestBranchCondition(t *testing.T) {
	// Test condition that returns different branches based on input
	condition := func(ctx context.Context, input *blades.Prompt) (string, error) {
		if len(input.Messages) > 0 {
			text := input.Messages[0].Text()
			if text == "branch1" {
				return "branch1", nil
			}
			return "branch2", nil
		}
		return "branch1", nil
	}

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

	runners := map[string]blades.Runnable{
		"branch1": &mockRunnable{name: "runner1", output: output1},
		"branch2": &mockRunnable{name: "runner2", output: output2},
	}

	branch := NewBranch(condition, runners)

	ctx := context.Background()

	// Test branch1
	input1 := &blades.Prompt{
		Messages: []*blades.Message{
			{
				ID:    "msg-0",
				Role:  blades.RoleUser,
				Parts: []blades.Part{blades.TextPart{Text: "branch1"}},
			},
		},
	}

	output, err := branch.Run(ctx, input1)
	if err != nil {
		t.Errorf("Branch.Run() returned error: %v", err)
	}
	if output != output1 {
		t.Errorf("Branch.Run() output = %v, want %v", output, output1)
	}

	// Test branch2
	input2 := &blades.Prompt{
		Messages: []*blades.Message{
			{
				ID:    "msg-0",
				Role:  blades.RoleUser,
				Parts: []blades.Part{blades.TextPart{Text: "branch2"}},
			},
		},
	}

	output, err = branch.Run(ctx, input2)
	if err != nil {
		t.Errorf("Branch.Run() returned error: %v", err)
	}
	if output != output2 {
		t.Errorf("Branch.Run() output = %v, want %v", output, output2)
	}
}
