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
	budget := ContextBudget{SystemTokens: 100, MessagesTokens: 100, ResponseReserveTokens: 10}
	var compactInfo ContextInfo
	var compactCounterOK bool
	var promptInfo ContextInfo
	agent := &llmAgent{
		provider: testProvider{name: "test-model"},
		compactor: compact.CompactorFunc(func(ctx context.Context, req compact.Request) ([]*model.Message, error) {
			compactInfo, _ = ContextInfoFromContext(ctx)
			compactCounterOK = req.TokenCounter != nil
			return req.Messages[1:], nil
		}),
		promptBuilders: []prompt.Builder{
			prompt.Section(func(ctx context.Context) ([]content.Part, error) {
				promptInfo, _ = ContextInfoFromContext(ctx)
				return []content.Part{content.Text{Text: "system"}}, nil
			}),
		},
		contextBudget: budget,
		tokenCounter: model.TokenCounterFunc(func(_ context.Context, req *model.Request) (model.TokenCount, error) {
			return model.TokenCount{
				SystemTokens:   int64(len(req.System)),
				MessagesTokens: int64(len(content.TextFromParts(req.Messages[0].Parts))),
				HasBreakdown:   true,
			}, nil
		}),
	}

	req, stats, err := contextBuilder{agent: agent, sess: sess}.Build(context.Background())
	require.NoError(t, err)

	assert.Equal(t, "test-model", req.Model)
	assert.Equal(t, "system", req.System)
	require.Len(t, req.Messages, 1)
	assert.Equal(t, "recent", content.TextFromParts(req.Messages[0].Parts))
	assert.Equal(t, ContextPurposeMain, compactInfo.Purpose)
	assert.Equal(t, budget, compactInfo.Budget)
	assert.Equal(t, compactInfo, promptInfo)
	assert.True(t, compactCounterOK)
	assert.Equal(t, ContextPurposeMain, stats.Purpose)
	assert.Equal(t, int64(len("system")), stats.Count.SystemTokens)
	assert.Equal(t, int64(len("recent")), stats.Count.MessagesTokens)
	assert.Equal(t, int64(len("system")+len("recent")), stats.Count.InputTokens)
	assert.Equal(t, 2, stats.MessagesBefore)
	assert.Equal(t, 1, stats.MessagesAfter)
}

func TestEnforceContextBudgetReturnsBudgetError(t *testing.T) {
	err := enforceContextBudget(
		ContextBudget{SystemTokens: 3},
		ContextStats{Count: model.TokenCount{SystemTokens: int64(len("too long")), HasBreakdown: true}},
	)
	require.Error(t, err)

	var budgetErr *ContextBudgetError
	require.True(t, errors.As(err, &budgetErr))
	assert.Equal(t, ContextSegmentSystem, budgetErr.Segment)
	assert.Equal(t, int64(3), budgetErr.Limit)
	assert.Equal(t, int64(len("too long")), budgetErr.Actual)
}

func TestEnforceContextBudgetRequiresSegmentBreakdown(t *testing.T) {
	err := enforceContextBudget(
		ContextBudget{SystemTokens: 3},
		ContextStats{Count: model.TokenCount{InputTokens: 2}},
	)
	require.Error(t, err)

	var budgetErr *ContextBudgetError
	require.True(t, errors.As(err, &budgetErr))
	assert.Equal(t, ContextSegmentSystem, budgetErr.Segment)
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
