package blades

import (
	"context"
	"errors"
	"testing"

	"github.com/go-kratos/blades/compact"
	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/model"
	"github.com/go-kratos/blades/prompt"
	"github.com/go-kratos/blades/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContextBuilderCompactsThenBuildsPromptAndStats(t *testing.T) {
	sess := session.NewSession(session.WithMessages(
		testTextMessage(model.RoleUser, "old"),
		testTextMessage(model.RoleAssistant, "recent"),
	))
	budget := model.TokenCount{System: 100, Messages: 100}
	var compactCounterOK bool
	agent := &llmAgent{
		provider: testProvider{name: "test-model"},
		compactor: compact.CompactorFunc(func(ctx context.Context, req compact.Request) ([]*model.Message, error) {
			compactCounterOK = req.TokenCounter != nil
			return req.Messages[1:], nil
		}),
		promptBuilders: []prompt.Builder{
			prompt.Section(func(ctx context.Context) ([]content.Part, error) {
				return []content.Part{content.Text{Text: "system"}}, nil
			}),
		},
		contextBudget: budget,
		tokenCounter: model.TokenCounterFunc(func(_ context.Context, req *model.Request) (model.TokenCount, error) {
			return model.TokenCount{
				System:   int64(len(req.System)),
				Messages: int64(len(content.TextFromParts(req.Messages[0].Parts))),
			}, nil
		}),
	}

	req, err := contextBuilder{agent: agent, sess: sess}.Build(context.Background())
	require.NoError(t, err)

	assert.Equal(t, "test-model", req.Model)
	assert.Equal(t, "system", req.System)
	require.Len(t, req.Messages, 1)
	assert.Equal(t, "recent", content.TextFromParts(req.Messages[0].Parts))
	assert.True(t, compactCounterOK)
}

func TestEnforceContextBudgetReturnsBudgetError(t *testing.T) {
	budget := model.TokenCount{System: 3}
	usage := model.TokenCount{System: int64(len("too long"))}
	err := checkBudget(budget, usage)
	require.Error(t, err)

	var budgetErr *BudgetError
	require.True(t, errors.As(err, &budgetErr))
	assert.Equal(t, "system", budgetErr.Segment)
	assert.Equal(t, int64(3), budgetErr.Limit)
	assert.Equal(t, int64(len("too long")), budgetErr.Actual)
}

func TestEnforceContextBudgetRequiresSegmentBreakdown(t *testing.T) {
	budget := model.TokenCount{System: 3}
	usage := model.TokenCount{Input: 2}
	err := checkBudget(budget, usage)
	require.Error(t, err)

	var budgetErr *BudgetError
	require.True(t, errors.As(err, &budgetErr))
	assert.Equal(t, "system", budgetErr.Segment)
	assert.True(t, budgetErr.Unavailable)
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
