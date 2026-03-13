package blades

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

type countingSessionModel struct {
	mu            sync.Mutex
	generateCalls int
	streamCalls   int
}

func (m *countingSessionModel) Name() string {
	return "counting-session"
}

func (m *countingSessionModel) Generate(context.Context, *ModelRequest) (*ModelResponse, error) {
	m.mu.Lock()
	m.generateCalls++
	call := m.generateCalls
	m.mu.Unlock()

	msg := NewAssistantMessage(StatusCompleted)
	msg.Parts = append(msg.Parts, TextPart{Text: fmt.Sprintf("reply-%d", call)})
	return &ModelResponse{Message: msg}, nil
}

func (m *countingSessionModel) NewStreaming(context.Context, *ModelRequest) Generator[*ModelResponse, error] {
	m.mu.Lock()
	m.streamCalls++
	call := m.streamCalls
	m.mu.Unlock()

	return func(yield func(*ModelResponse, error) bool) {
		msg := NewAssistantMessage(StatusCompleted)
		msg.ID = "shared-stream-message-id"
		msg.Parts = append(msg.Parts, TextPart{Text: fmt.Sprintf("stream-%d", call)})
		yield(&ModelResponse{Message: msg}, nil)
	}
}

func TestRunnerRun_RerunsWithSameSession(t *testing.T) {
	t.Parallel()

	model := &countingSessionModel{}
	agent, err := NewAgent("rerun-agent", WithModel(model))
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}
	runner := NewRunner(agent)
	session := NewSession()
	const invocationID = "shared-invocation-id"

	first, err := runner.Run(context.Background(), UserMessage("hi"), WithSession(session), WithInvocationID(invocationID))
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	second, err := runner.Run(context.Background(), UserMessage("hi"), WithSession(session), WithInvocationID(invocationID))
	if err != nil {
		t.Fatalf("second run: %v", err)
	}

	if got, want := first.Text(), "reply-1"; got != want {
		t.Fatalf("first reply = %q, want %q", got, want)
	}
	if got, want := second.Text(), "reply-2"; got != want {
		t.Fatalf("second reply = %q, want %q", got, want)
	}
	if got, want := first.InvocationID, invocationID; got != want {
		t.Fatalf("first invocation id = %q, want %q", got, want)
	}
	if got, want := second.InvocationID, invocationID; got != want {
		t.Fatalf("second invocation id = %q, want %q", got, want)
	}

	model.mu.Lock()
	generateCalls := model.generateCalls
	model.mu.Unlock()
	if got, want := generateCalls, 2; got != want {
		t.Fatalf("generate calls = %d, want %d", got, want)
	}

	history := session.History()
	if got, want := len(history), 4; got != want {
		t.Fatalf("session history len = %d, want %d", got, want)
	}
	if got, want := history[0].Text(), "hi"; got != want {
		t.Fatalf("first history text = %q, want %q", got, want)
	}
	if got, want := history[1].Text(), "reply-1"; got != want {
		t.Fatalf("second history text = %q, want %q", got, want)
	}
	if got, want := history[2].Text(), "hi"; got != want {
		t.Fatalf("third history text = %q, want %q", got, want)
	}
	if got, want := history[3].Text(), "reply-2"; got != want {
		t.Fatalf("fourth history text = %q, want %q", got, want)
	}
	for i, msg := range history {
		if got, want := msg.InvocationID, invocationID; got != want {
			t.Fatalf("history[%d] invocation id = %q, want %q", i, got, want)
		}
	}
}

func TestRunnerRunStream_RerunsWithSameSession(t *testing.T) {
	t.Parallel()

	model := &countingSessionModel{}
	agent, err := NewAgent("stream-agent", WithModel(model))
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}
	runner := NewRunner(agent)
	session := NewSession()
	const invocationID = "shared-stream-invocation-id"

	var firstTexts []string
	for output, err := range runner.RunStream(context.Background(), UserMessage("hi"), WithSession(session), WithInvocationID(invocationID)) {
		if err != nil {
			t.Fatalf("first stream: %v", err)
		}
		firstTexts = append(firstTexts, output.Text())
	}

	var secondTexts []string
	for output, err := range runner.RunStream(context.Background(), UserMessage("hi"), WithSession(session), WithInvocationID(invocationID)) {
		if err != nil {
			t.Fatalf("second stream: %v", err)
		}
		secondTexts = append(secondTexts, output.Text())
	}

	if got, want := len(firstTexts), 1; got != want {
		t.Fatalf("first stream output len = %d, want %d", got, want)
	}
	if got, want := len(secondTexts), 1; got != want {
		t.Fatalf("second stream output len = %d, want %d", got, want)
	}
	if got, want := firstTexts[0], "stream-1"; got != want {
		t.Fatalf("first stream text = %q, want %q", got, want)
	}
	if got, want := secondTexts[0], "stream-2"; got != want {
		t.Fatalf("second stream text = %q, want %q", got, want)
	}

	model.mu.Lock()
	streamCalls := model.streamCalls
	model.mu.Unlock()
	if got, want := streamCalls, 2; got != want {
		t.Fatalf("stream calls = %d, want %d", got, want)
	}

	history := session.History()
	if got, want := len(history), 4; got != want {
		t.Fatalf("session history len = %d, want %d", got, want)
	}
	if got, want := history[1].Text(), "stream-1"; got != want {
		t.Fatalf("first assistant history text = %q, want %q", got, want)
	}
	if got, want := history[3].Text(), "stream-2"; got != want {
		t.Fatalf("second assistant history text = %q, want %q", got, want)
	}
	for i, msg := range history {
		if got, want := msg.InvocationID, invocationID; got != want {
			t.Fatalf("history[%d] invocation id = %q, want %q", i, got, want)
		}
	}
}

type captureStreamHistoryModel struct {
	requests [][]*Message
	calls    int
}

func (m *captureStreamHistoryModel) Name() string {
	return "capture-stream-history"
}

func (m *captureStreamHistoryModel) Generate(context.Context, *ModelRequest) (*ModelResponse, error) {
	return nil, fmt.Errorf("unexpected Generate call")
}

func (m *captureStreamHistoryModel) NewStreaming(_ context.Context, req *ModelRequest) Generator[*ModelResponse, error] {
	m.calls++
	m.requests = append(m.requests, append([]*Message(nil), req.Messages...))
	text := fmt.Sprintf("reply-%d", m.calls)

	return func(yield func(*ModelResponse, error) bool) {
		msg := NewAssistantMessage(StatusCompleted)
		msg.Parts = append(msg.Parts, TextPart{Text: text})
		yield(&ModelResponse{Message: msg}, nil)
	}
}

func TestRunnerRunStream_UsesSessionHistoryOnSubsequentTurn(t *testing.T) {
	t.Parallel()

	model := &captureStreamHistoryModel{}
	agent, err := NewAgent("history-agent", WithModel(model))
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}
	runner := NewRunner(agent)
	session := NewSession()

	for _, input := range []string{"first", "second"} {
		for _, err := range runner.RunStream(context.Background(), UserMessage(input), WithSession(session)) {
			if err != nil {
				t.Fatalf("run stream %q: %v", input, err)
			}
		}
	}

	if got, want := len(model.requests), 2; got != want {
		t.Fatalf("request count = %d, want %d", got, want)
	}
	if got, want := len(model.requests[0]), 1; got != want {
		t.Fatalf("first request messages = %d, want %d", got, want)
	}
	if got, want := model.requests[0][0].Text(), "first"; got != want {
		t.Fatalf("first request text = %q, want %q", got, want)
	}

	second := model.requests[1]
	if got, want := len(second), 3; got != want {
		t.Fatalf("second request messages = %d, want %d", got, want)
	}
	if got, want := second[0].Text(), "first"; got != want {
		t.Fatalf("second request first text = %q, want %q", got, want)
	}
	if got, want := second[1].Text(), "reply-1"; got != want {
		t.Fatalf("second request second text = %q, want %q", got, want)
	}
	if got, want := second[2].Text(), "second"; got != want {
		t.Fatalf("second request third text = %q, want %q", got, want)
	}
}
