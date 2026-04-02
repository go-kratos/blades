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
	// Both the incomplete and completed messages are yielded; the completed
	// message has its text stripped to avoid duplicating the incremental chunks.
	if got, want := len(statuses), 2; got != want {
		t.Fatalf("statuses len = %d, want %d; got %v", got, want, statuses)
	}
	if got, want := statuses[0], StatusIncomplete; got != want {
		t.Fatalf("first status = %q, want %q", got, want)
	}
	if got, want := statuses[1], StatusCompleted; got != want {
		t.Fatalf("second status = %q, want %q", got, want)
	}
}

func TestRunnerRunStream_NoDuplicateOutput(t *testing.T) {
	t.Parallel()

	model := &scriptedStreamingModel{
		streamResponses: []*ModelResponse{
			streamingResponse(StatusIncomplete, "chunk1"),
			streamingResponse(StatusIncomplete, "chunk2"),
			streamingResponse(StatusCompleted, "chunk1chunk2"),
		},
	}
	agent, err := NewAgent("stream-agent", WithModel(model))
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}
	runner := NewRunner(agent)

	var (
		texts    []string
		statuses []Status
	)
	for output, err := range runner.RunStream(context.Background(), UserMessage("hi")) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		statuses = append(statuses, output.Status)
		for _, p := range output.Parts {
			if tp, ok := p.(TextPart); ok {
				texts = append(texts, tp.Text)
			}
		}
	}

	// All three messages are yielded, but the completed one has text stripped.
	if got, want := len(statuses), 3; got != want {
		t.Fatalf("statuses len = %d, want %d; got %v", got, want, statuses)
	}
	if got, want := statuses[2], StatusCompleted; got != want {
		t.Fatalf("last status = %q, want %q", got, want)
	}
	// Only the two incomplete chunks contribute text — no duplicate.
	if got, want := len(texts), 2; got != want {
		t.Fatalf("texts len = %d, want %d; got %v", got, want, texts)
	}
	if texts[0] != "chunk1" || texts[1] != "chunk2" {
		t.Fatalf("texts = %v, want [chunk1, chunk2]", texts)
	}
}

func TestRunnerRunStream_CompletedSignalRetainsNonTextParts(t *testing.T) {
	t.Parallel()

	// Build a completed response that carries both text and a tool part.
	completedResp := streamingResponse(StatusCompleted, "chunk1")
	completedResp.Message.Parts = append(completedResp.Message.Parts,
		NewToolPart("t1", "myTool", `{"arg":"val"}`),
	)

	model := &scriptedStreamingModel{
		streamResponses: []*ModelResponse{
			streamingResponse(StatusIncomplete, "chunk1"),
			completedResp,
		},
	}
	agent, err := NewAgent("stream-agent", WithModel(model))
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}
	runner := NewRunner(agent)

	var (
		texts     []string
		toolParts []ToolPart
		statuses  []Status
	)
	for output, err := range runner.RunStream(context.Background(), UserMessage("hi")) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		statuses = append(statuses, output.Status)
		for _, p := range output.Parts {
			switch v := p.(type) {
			case TextPart:
				texts = append(texts, v.Text)
			case ToolPart:
				toolParts = append(toolParts, v)
			}
		}
	}

	if got, want := len(statuses), 2; got != want {
		t.Fatalf("statuses len = %d, want %d; got %v", got, want, statuses)
	}
	if got, want := statuses[1], StatusCompleted; got != want {
		t.Fatalf("last status = %q, want %q", got, want)
	}
	// Text is only from the incomplete chunk — no duplicate.
	if got, want := len(texts), 1; got != want {
		t.Fatalf("texts len = %d, want %d; got %v", got, want, texts)
	}
	// ToolPart from the completed message must be preserved.
	if got, want := len(toolParts), 1; got != want {
		t.Fatalf("toolParts len = %d, want %d", got, want)
	}
	if got, want := toolParts[0].Name, "myTool"; got != want {
		t.Fatalf("toolPart name = %q, want %q", got, want)
	}
}

func TestRunnerRunStream_CompletedOnlyYieldsOutput(t *testing.T) {
	t.Parallel()

	model := &scriptedStreamingModel{
		streamResponses: []*ModelResponse{
			streamingResponse(StatusCompleted, "hello"),
		},
	}
	agent, err := NewAgent("stream-agent", WithModel(model))
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}
	runner := NewRunner(agent)

	var statuses []Status
	var texts []string
	for output, err := range runner.RunStream(context.Background(), UserMessage("hi")) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		statuses = append(statuses, output.Status)
		texts = append(texts, output.Text())
	}

	// A provider that only emits a single completed message must still produce output.
	if got, want := len(statuses), 1; got != want {
		t.Fatalf("statuses len = %d, want %d; got %v", got, want, statuses)
	}
	if got, want := statuses[0], StatusCompleted; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
	if got, want := texts[0], "hello"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
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
