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
	var compactWindow ContextWindow
	var compactCounterOK bool
	var promptWindow ContextWindow
	agent := &llmAgent{
		provider: testProvider{name: "test-model"},
		compactor: compact.CompactorFunc(func(ctx context.Context, req compact.Request) ([]*model.Message, error) {
			compactWindow, _ = ContextWindowFrom(ctx)
			compactCounterOK = req.TokenCounter != nil
			return req.Messages[1:], nil
		}),
		promptBuilders: []prompt.Builder{
			prompt.Section(func(ctx context.Context) ([]content.Part, error) {
				promptWindow, _ = ContextWindowFrom(ctx)
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

	req, w, err := contextBuilder{agent: agent, sess: sess}.Build(context.Background())
	require.NoError(t, err)

	assert.Equal(t, "test-model", req.Model)
	assert.Equal(t, "system", req.System)
	require.Len(t, req.Messages, 1)
	assert.Equal(t, "recent", content.TextFromParts(req.Messages[0].Parts))
	assert.Equal(t, budget, compactWindow.Budget)
	assert.Equal(t, compactWindow.Budget, promptWindow.Budget)
	assert.True(t, compactCounterOK)
	assert.Equal(t, int64(len("system")), w.Usage.System)
	assert.Equal(t, int64(len("recent")), w.Usage.Messages)
	assert.Equal(t, int64(len("system")+len("recent")), w.Usage.Input)
	assert.Equal(t, 2, w.MessagesBefore)
	assert.Equal(t, 1, w.MessagesAfter)
}

func TestEnforceContextBudgetReturnsBudgetError(t *testing.T) {
	w := ContextWindow{
		Budget: model.TokenCount{System: 3},
		Usage:  model.TokenCount{System: int64(len("too long"))},
	}
	err := w.Enforce()
	require.Error(t, err)

	var budgetErr *BudgetError
	require.True(t, errors.As(err, &budgetErr))
	assert.Equal(t, "system", budgetErr.Segment)
	assert.Equal(t, int64(3), budgetErr.Limit)
	assert.Equal(t, int64(len("too long")), budgetErr.Actual)
}

func TestEnforceContextBudgetRequiresSegmentBreakdown(t *testing.T) {
	w := ContextWindow{
		Budget: model.TokenCount{System: 3},
		Usage:  model.TokenCount{Input: 2},
	}
	err := w.Enforce()
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
