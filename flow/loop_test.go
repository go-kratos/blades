package flow

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/event"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoopAgentBridgesEachSubAgentOutput(t *testing.T) {
	t.Parallel()

	first := &recordingAgent{
		name: "first",
		respond: func(input string) event.TurnEnd {
			return event.TurnEnd{Parts: []content.Part{content.Text{Text: input + " -> first"}}}
		},
	}
	second := &recordingAgent{
		name: "second",
		respond: func(input string) event.TurnEnd {
			return event.TurnEnd{Parts: []content.Part{content.Text{Text: input + " -> second"}}}
		},
	}
	agent := NewLoopAgent(LoopConfig{
		Name:          "loop",
		SubAgents:     []blades.Agent{first, second},
		MaxIterations: 1,
	})

	outputs, err := agent.Run(context.Background(), promptInput([]content.Part{content.Text{Text: "start"}}))
	require.NoError(t, err)
	collectOutputs(outputs)

	assert.Equal(t, []string{"start"}, first.Inputs())
	assert.Equal(t, []string{"start -> first"}, second.Inputs())
}

func TestLoopAgentEvaluatesConditionAfterFinalEmptyOutput(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("stop empty turn")
	conditionCalled := false
	agent := NewLoopAgent(LoopConfig{
		Name: "loop",
		SubAgents: []blades.Agent{&recordingAgent{
			name: "empty",
			respond: func(string) event.TurnEnd {
				return event.TurnEnd{}
			},
		}},
		MaxIterations: 1,
		Condition: func(_ context.Context, state LoopState) (bool, error) {
			conditionCalled = true
			assert.Empty(t, state.LastOutput.Parts)
			return false, wantErr
		},
	})

	outputs, err := agent.Run(context.Background(), promptInput([]content.Part{content.Text{Text: "start"}}))
	require.NoError(t, err)
	items := collectOutputs(outputs)

	require.True(t, conditionCalled)
	require.Len(t, items, 3)
	assert.IsType(t, event.TurnEnd{}, items[0])
	assert.ErrorIs(t, items[1].(event.Error).Err, wantErr)
	assert.IsType(t, event.Done{}, items[2])
}

type recordingAgent struct {
	name    string
	respond func(string) event.TurnEnd
	mu      sync.Mutex
	inputs  []string
}

func (a *recordingAgent) Name() string        { return a.name }
func (a *recordingAgent) Description() string { return "" }

func (a *recordingAgent) Run(_ context.Context, input <-chan event.Input) (<-chan event.Output, error) {
	var text string
	for item := range input {
		if prompt, ok := item.(event.Prompt); ok {
			text += content.TextFromParts(prompt.Parts)
		}
	}
	a.mu.Lock()
	a.inputs = append(a.inputs, text)
	a.mu.Unlock()

	output := make(chan event.Output, 2)
	output <- a.respond(text)
	output <- event.Done{}
	close(output)
	return output, nil
}

func (a *recordingAgent) Inputs() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.inputs...)
}

func collectOutputs(output <-chan event.Output) []event.Output {
	var items []event.Output
	for item := range output {
		items = append(items, item)
	}
	return items
}
