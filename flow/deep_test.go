package flow

import (
	"context"
	"testing"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/event"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeepAgentInvokesHandoffTarget(t *testing.T) {
	t.Parallel()

	first := &recordingAgent{
		name: "first",
		respond: func(string) event.TurnEnd {
			return event.TurnEnd{
				Parts:  []content.Part{content.Text{Text: "delegated task"}},
				Action: event.Handoff{Agent: "second"},
			}
		},
	}
	second := &recordingAgent{
		name: "second",
		respond: func(string) event.TurnEnd {
			return event.TurnEnd{Parts: []content.Part{content.Text{Text: "completed"}}}
		},
	}
	agent, err := NewDeepAgent(DeepConfig{
		Name:          "deep",
		SubAgents:     []blades.Agent{first, second},
		MaxIterations: 2,
	})
	require.NoError(t, err)

	outputs, err := agent.Run(context.Background(), promptInput([]content.Part{content.Text{Text: "original task"}}))
	require.NoError(t, err)
	collectOutputs(outputs)

	assert.Equal(t, []string{"original task"}, first.Inputs())
	assert.Equal(t, []string{"delegated task"}, second.Inputs())
}
