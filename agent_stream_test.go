package blades

import (
	"context"
	"errors"
	"testing"
)

type scriptedStreamingModel struct {
	generateSet      bool
	generateResponse *ModelResponse
	generateErr      error
	streamResponses  []*ModelResponse
	streamErr        error
}

func (m *scriptedStreamingModel) Name() string {
	return "scripted-stream"
}

func (m *scriptedStreamingModel) Generate(context.Context, *ModelRequest) (*ModelResponse, error) {
	if m.generateSet {
		if m.generateErr != nil {
			return nil, m.generateErr
		}
		return m.generateResponse, nil
	}
	msg := NewAssistantMessage(StatusCompleted)
	msg.Parts = append(msg.Parts, TextPart{Text: "ok"})
	return &ModelResponse{Message: msg}, nil
}

func (m *scriptedStreamingModel) NewStreaming(context.Context, *ModelRequest) Generator[*ModelResponse, error] {
	return func(yield func(*ModelResponse, error) bool) {
		for _, response := range m.streamResponses {
			if !yield(response, nil) {
				return
			}
		}
		if m.streamErr != nil {
			yield(nil, m.streamErr)
		}
	}
}

func streamingResponse(status Status, text string) *ModelResponse {
	msg := NewAssistantMessage(status)
	if text != "" {
		msg.Parts = append(msg.Parts, TextPart{Text: text})
	}
	return &ModelResponse{Message: msg}
}

func TestRunnerRunStream_RequiresCompletedFinalMessage(t *testing.T) {
	t.Parallel()

	model := &scriptedStreamingModel{
		streamResponses: []*ModelResponse{
			streamingResponse(StatusIncomplete, "hello"),
		},
	}
	agent, err := NewAgent("stream-agent", WithModel(model))
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}
	runner := NewRunner(agent)

	var (
		gotErr   error
		statuses []Status
	)
	for output, err := range runner.RunStream(context.Background(), UserMessage("hi")) {
		if err != nil {
			gotErr = err
			break
		}
		statuses = append(statuses, output.Status)
	}

	if !errors.Is(gotErr, ErrNoFinalResponse) {
		t.Fatalf("expected ErrNoFinalResponse, got %v", gotErr)
	}
	if got, want := len(statuses), 1; got != want {
		t.Fatalf("statuses len = %d, want %d", got, want)
	}
	if got, want := statuses[0], StatusIncomplete; got != want {
		t.Fatalf("first status = %q, want %q", got, want)
	}
}

func TestRunnerRun_ReturnsNoFinalResponseWhenGenerateMessageNil(t *testing.T) {
	t.Parallel()

	model := &scriptedStreamingModel{
		generateSet:      true,
		generateResponse: &ModelResponse{},
	}
	agent, err := NewAgent("stream-agent", WithModel(model))
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}
	runner := NewRunner(agent)

	_, err = runner.Run(context.Background(), UserMessage("hi"))
	if !errors.Is(err, ErrNoFinalResponse) {
		t.Fatalf("expected ErrNoFinalResponse, got %v", err)
	}
}

func TestRunnerRun_ReturnsNoFinalResponseWhenGenerateResponseNil(t *testing.T) {
	t.Parallel()

	model := &scriptedStreamingModel{
		generateSet:      true,
		generateResponse: nil,
	}
	agent, err := NewAgent("stream-agent", WithModel(model))
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}
	runner := NewRunner(agent)

	_, err = runner.Run(context.Background(), UserMessage("hi"))
	if !errors.Is(err, ErrNoFinalResponse) {
		t.Fatalf("expected ErrNoFinalResponse, got %v", err)
	}
}

func TestRunnerRunStream_AllowsIncompleteThenCompleted(t *testing.T) {
	t.Parallel()

	model := &scriptedStreamingModel{
		streamResponses: []*ModelResponse{
			streamingResponse(StatusIncomplete, "hel"),
			streamingResponse(StatusCompleted, "hello"),
		},
	}
	agent, err := NewAgent("stream-agent", WithModel(model))
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}
	runner := NewRunner(agent)

	var (
		gotErr   error
		statuses []Status
	)
	for output, err := range runner.RunStream(context.Background(), UserMessage("hi")) {
		if err != nil {
			gotErr = err
			break
		}
		statuses = append(statuses, output.Status)
	}

	if gotErr != nil {
		t.Fatalf("expected no error, got %v", gotErr)
	}
	if got, want := len(statuses), 2; got != want {
		t.Fatalf("statuses len = %d, want %d", got, want)
	}
	if got, want := statuses[0], StatusIncomplete; got != want {
		t.Fatalf("first status = %q, want %q", got, want)
	}
	if got, want := statuses[1], StatusCompleted; got != want {
		t.Fatalf("second status = %q, want %q", got, want)
	}
}

func TestRunnerRunStream_ReturnsNoFinalResponseWhenChunkMessageNil(t *testing.T) {
	t.Parallel()

	model := &scriptedStreamingModel{
		streamResponses: []*ModelResponse{
			{},
		},
	}
	agent, err := NewAgent("stream-agent", WithModel(model))
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}
	runner := NewRunner(agent)

	var (
		gotErr   error
		statuses []Status
	)
	for output, err := range runner.RunStream(context.Background(), UserMessage("hi")) {
		if err != nil {
			gotErr = err
			break
		}
		statuses = append(statuses, output.Status)
	}

	if !errors.Is(gotErr, ErrNoFinalResponse) {
		t.Fatalf("expected ErrNoFinalResponse, got %v", gotErr)
	}
	if got, want := len(statuses), 0; got != want {
		t.Fatalf("statuses len = %d, want %d", got, want)
	}
}

func TestRunnerRunStream_ReturnsNoFinalResponseWhenChunkResponseNil(t *testing.T) {
	t.Parallel()

	model := &scriptedStreamingModel{
		streamResponses: []*ModelResponse{
			nil,
		},
	}
	agent, err := NewAgent("stream-agent", WithModel(model))
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}
	runner := NewRunner(agent)

	var (
		gotErr   error
		statuses []Status
	)
	for output, err := range runner.RunStream(context.Background(), UserMessage("hi")) {
		if err != nil {
			gotErr = err
			break
		}
		statuses = append(statuses, output.Status)
	}

	if !errors.Is(gotErr, ErrNoFinalResponse) {
		t.Fatalf("expected ErrNoFinalResponse, got %v", gotErr)
	}
	if got, want := len(statuses), 0; got != want {
		t.Fatalf("statuses len = %d, want %d", got, want)
	}
}
