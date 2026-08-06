package blades

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/go-kratos/blades/compact"
	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/model"
	"github.com/go-kratos/blades/prompt"
	"github.com/go-kratos/blades/session"
	"github.com/go-kratos/blades/tools"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContextBuilderCompactsThenBuildsPromptAndStats(t *testing.T) {
	sess := session.NewSession(session.WithMessages(
		testTextMessage(model.RoleUser, "old"),
		testTextMessage(model.RoleAssistant, "recent"),
	))
	var compactCounterOK bool
	agent := &llmAgent{
		provider: testProvider{name: "test-model"},
		compactor: compact.CompactorFunc(func(ctx context.Context, req compact.Request) ([]*model.Message, error) {
			compactCounterOK = req.TokenCounter != nil
			return req.Messages[1:], nil
		}),
		contextWindow: model.ContextWindow{MaxTokens: 15_000, OutputTokens: 1_000},
		promptBuilders: []prompt.Builder{
			prompt.Section(func(ctx context.Context) ([]content.Part, error) {
				return []content.Part{content.Text{Text: "system"}}, nil
			}),
		},
		outputSchema: &jsonschema.Schema{Type: "object"},
		tokenCounter: model.TokenCounterFunc(func(_ context.Context, req *model.Request) (model.TokenCount, error) {
			assert.Equal(t, "system", req.System)
			require.Len(t, req.Tools, 1)
			assert.Equal(t, "lookup", req.Tools[0].Name)
			assert.NotEmpty(t, req.Options)
			assert.Len(t, req.Messages, 2)
			return model.TokenCount{Input: 1_001}, nil
		}),
	}

	req, err := contextBuilder{
		agent:    agent,
		sess:     sess,
		allTools: []tools.Tool{contextBuilderTool{}},
	}.Build(context.Background())
	require.NoError(t, err)

	assert.Equal(t, "test-model", req.Model)
	assert.Equal(t, "system", req.System)
	require.Len(t, req.Messages, 1)
	assert.Equal(t, "recent", content.TextFromParts(req.Messages[0].Parts))
	assert.True(t, compactCounterOK)
}

type contextBuilderTool struct{}

func (contextBuilderTool) Spec() tools.ToolSpec {
	return tools.ToolSpec{Name: "lookup", Description: "Look up data"}
}

func (contextBuilderTool) Handle(context.Context, json.RawMessage) (*tools.Result, error) {
	return tools.TextResult("ok"), nil
}

type testProvider struct {
	model.Provider
	name string
}

func (p testProvider) Name() string {
	return p.name
}

func testTextMessage(role model.Role, text string) *model.Message {
	return &model.Message{Role: role, Parts: []content.Part{content.Text{Text: text}}}
}
