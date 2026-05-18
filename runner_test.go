package blades

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/event"
)

type runnerAgent struct {
	outputs []event.Output
	err     error
}

type outputAgent struct {
	output <-chan event.Output
}

func (a runnerAgent) Name() string        { return "runner-test" }
func (a runnerAgent) Description() string { return "" }

func (a runnerAgent) Run(context.Context, <-chan event.Input) (<-chan event.Output, error) {
	if a.err != nil {
		return nil, a.err
	}
	output := make(chan event.Output, len(a.outputs))
	go func() {
		defer close(output)
		for _, out := range a.outputs {
			output <- out
		}
	}()
	return output, nil
}

func (a outputAgent) Name() string        { return "output-test" }
func (a outputAgent) Description() string { return "" }

func (a outputAgent) Run(context.Context, <-chan event.Input) (<-chan event.Output, error) {
	return a.output, nil
}

func TestRunnerRunReturnsFinalResult(t *testing.T) {
	t.Parallel()

	usage := event.Usage{InputTokens: 3, OutputTokens: 5}
	runner := NewRunner(runnerAgent{outputs: []event.Output{
		event.TextDelta{Text: "intermediate"},
		event.TurnEnd{
			Parts: []content.Part{
				content.Text{Text: "hello"},
				content.Text{Text: " world"},
			},
			StopReason: event.StopEnd,
			Usage:      usage,
		},
		event.Done{},
	}})

	result, err := runner.Run(context.Background(), event.NewPrompt("hi"))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Text() != "hello world" {
		t.Fatalf("Run().Text() = %q, want hello world", result.Text())
	}
	if result.StopReason != event.StopEnd {
		t.Fatalf("Run().StopReason = %q, want %q", result.StopReason, event.StopEnd)
	}
	if result.Usage != usage {
		t.Fatalf("Run().Usage = %+v, want %+v", result.Usage, usage)
	}
}

func TestResultText(t *testing.T) {
	t.Parallel()

	parts := []content.Part{
		content.Text{Text: "hello"},
		content.DataPart{Bytes: []byte("ignored")},
		content.Text{Text: " world"},
	}
	result := Result{TurnEnd: event.TurnEnd{Parts: parts}}

	if got := result.Parts; len(got) != len(parts) {
		t.Fatalf("Result.Parts length = %d, want %d", len(got), len(parts))
	}
	if got := result.Text(); got != "hello world" {
		t.Fatalf("Result.Text() = %q, want hello world", got)
	}
}

func TestRunnerRunReturnsLastTurnEnd(t *testing.T) {
	t.Parallel()

	runner := NewRunner(runnerAgent{outputs: []event.Output{
		event.TurnEnd{Parts: []content.Part{content.Text{Text: "first"}}},
		event.TextDelta{Text: "ignored"},
		event.TurnEnd{Parts: []content.Part{content.Text{Text: "second"}}},
		event.Done{},
	}})

	result, err := runner.Run(context.Background(), event.NewPrompt("hi"))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Text() != "second" {
		t.Fatalf("Run().Text() = %q, want second", result.Text())
	}
}

func TestRunnerRunDrainsAfterRuntimeError(t *testing.T) {
	t.Parallel()

	first := errors.New("first failed")
	outputs := make(chan event.Output)
	sent := make(chan struct{})
	runner := NewRunner(outputAgent{output: outputs})
	go func() {
		defer close(sent)
		outputs <- event.Error{Err: first}
		outputs <- event.TurnEnd{Parts: []content.Part{content.Text{Text: "after error"}}}
		outputs <- event.Done{}
		close(outputs)
	}()

	result, err := runner.Run(context.Background(), event.NewPrompt("hi"))
	if !errors.Is(err, first) {
		t.Fatalf("Run() error = %v, want %v", err, first)
	}
	if result.Text() != "" {
		t.Fatalf("Run() result = %#v, want zero value", result)
	}
	select {
	case <-sent:
	case <-time.After(time.Second):
		t.Fatal("Run returned before draining output channel")
	}
}

func TestRunnerRunReturnsRuntimeError(t *testing.T) {
	t.Parallel()

	want := errors.New("runtime failed")
	runner := NewRunner(runnerAgent{outputs: []event.Output{
		event.Error{Err: want},
		event.Done{},
	}})

	result, err := runner.Run(context.Background(), event.NewPrompt("hi"))
	if !errors.Is(err, want) {
		t.Fatalf("Run() error = %v, want %v", err, want)
	}
	if result.Text() != "" {
		t.Fatalf("Run() result = %#v, want zero value", result)
	}
}

func TestRunnerRunReturnsStartError(t *testing.T) {
	t.Parallel()

	want := errors.New("start failed")
	runner := NewRunner(runnerAgent{err: want})

	result, err := runner.Run(context.Background(), event.NewPrompt("hi"))
	if !errors.Is(err, want) {
		t.Fatalf("Run() error = %v, want %v", err, want)
	}
	if result.Text() != "" {
		t.Fatalf("Run() result = %#v, want zero value", result)
	}
}

func TestRunnerRunRequiresTurnEnd(t *testing.T) {
	t.Parallel()

	runner := NewRunner(runnerAgent{outputs: []event.Output{event.Done{}}})

	result, err := runner.Run(context.Background(), event.NewPrompt("hi"))
	if !errors.Is(err, ErrNoResult) {
		t.Fatalf("Run() error = %v, want %v", err, ErrNoResult)
	}
	if result.Text() != "" {
		t.Fatalf("Run() result = %#v, want zero value", result)
	}
}

func TestRunnerRunPreservesTurnErrorOnResult(t *testing.T) {
	t.Parallel()

	want := errors.New("turn aborted")
	runner := NewRunner(runnerAgent{outputs: []event.Output{
		event.TurnEnd{
			Parts:      []content.Part{content.Text{Text: "partial"}},
			StopReason: event.StopAbort,
			Err:        want,
		},
		event.Done{},
	}})

	result, err := runner.Run(context.Background(), event.NewPrompt("hi"))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !errors.Is(result.Err, want) {
		t.Fatalf("Run().Err = %v, want %v", result.Err, want)
	}
	if result.Text() != "partial" {
		t.Fatalf("Run().Text() = %q, want partial", result.Text())
	}
}
