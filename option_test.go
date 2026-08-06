package blades

import (
	"testing"

	"github.com/go-kratos/blades/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithTokenCounterNilUsesDefault(t *testing.T) {
	t.Parallel()

	agent, err := NewAgent(
		"agent",
		WithModel(testProvider{name: "test"}),
		WithTokenCounter(nil),
	)
	require.NoError(t, err)

	configured := agent.(*llmAgent)
	assert.IsType(t, model.ApproxTokenCounter{}, configured.tokenCounter)
}
