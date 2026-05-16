package compact

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/model"
	"github.com/go-kratos/blades/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWindowDropsWholeToolPairAtBoundary(t *testing.T) {
	msgs := []*model.Message{
		textMessage(model.RoleUser, "old"),
		toolUseMessage("call-1"),
		toolResultMessage("call-1", "result"),
		textMessage(model.RoleAssistant, "final"),
	}

	view, err := NewWindow(WithMaxMessages(2)).Compact(context.Background(), Request{Messages: msgs})
	require.NoError(t, err)
	require.Len(t, view, 1)
	assert.Equal(t, model.RoleAssistant, view[0].Role)
	assert.Equal(t, "final", content.TextFromParts(view[0].Parts))

	view, err = NewWindow(WithMaxMessages(3)).Compact(context.Background(), Request{Messages: msgs})
	require.NoError(t, err)
	require.Len(t, view, 3)
	assert.Equal(t, model.RoleAssistant, view[0].Role)
	assert.Equal(t, model.RoleTool, view[1].Role)
	assert.Equal(t, model.RoleAssistant, view[2].Role)
}

func TestWindowRejectsDanglingToolResult(t *testing.T) {
	msgs := []*model.Message{toolResultMessage("call-1", "result")}

	_, err := NewWindow().Compact(context.Background(), Request{Messages: msgs})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dangling tool message")
}

func TestToolResultBudgetDoesNotMutateOriginal(t *testing.T) {
	originalText := "abcdefghijklmnopqrstuvwxyz"
	msgs := []*model.Message{
		toolUseMessage("call-1"),
		toolResultMessage("call-1", originalText),
	}

	view, err := NewToolResultBudget(20).Compact(context.Background(), Request{Messages: msgs})
	require.NoError(t, err)
	require.Len(t, view, 2)

	assert.Equal(t, originalText, toolResultText(msgs[1]))
	truncated := toolResultText(view[1])
	assert.Contains(t, truncated, "truncated")
	assert.NotEqual(t, originalText, truncated)
}

func TestCountMessagesTokensUsesMessageBreakdown(t *testing.T) {
	counter := model.TokenCounterFunc(func(_ context.Context, req *model.Request) (model.TokenCount, error) {
		assert.Empty(t, req.System)
		assert.Empty(t, req.Tools)
		return model.TokenCount{InputTokens: 99, MessagesTokens: 7, HasBreakdown: true}, nil
	})

	tokens, err := countMessagesTokens(context.Background(), counter, textMessage(model.RoleUser, "hello"))
	require.NoError(t, err)
	assert.Equal(t, int64(7), tokens)
}

func TestCountMessagesTokensFallsBackToInputTokens(t *testing.T) {
	counter := model.TokenCounterFunc(func(context.Context, *model.Request) (model.TokenCount, error) {
		return model.TokenCount{InputTokens: 9}, nil
	})

	tokens, err := countMessagesTokens(context.Background(), counter, textMessage(model.RoleUser, "hello"))
	require.NoError(t, err)
	assert.Equal(t, int64(9), tokens)
}

func TestWindowUsesRequestTokenCounter(t *testing.T) {
	called := false
	counter := model.TokenCounterFunc(func(_ context.Context, req *model.Request) (model.TokenCount, error) {
		called = true
		return model.TokenCount{InputTokens: int64(len(req.Messages)) * 10}, nil
	})
	msgs := []*model.Message{
		textMessage(model.RoleUser, "one"),
		textMessage(model.RoleAssistant, "two"),
		textMessage(model.RoleUser, "three"),
	}

	view, err := NewWindow(WithMaxTokens(15)).Compact(context.Background(), Request{
		Messages:     msgs,
		TokenCounter: counter,
	})
	require.NoError(t, err)

	require.True(t, called)
	require.Len(t, view, 1)
	assert.Equal(t, "three", content.TextFromParts(view[0].Parts))
}

func TestBlockSummarizeBuildsSummaryBlocksAndKeepsSessionMessages(t *testing.T) {
	msgs := []*model.Message{
		textMessage(model.RoleUser, "u1"),
		textMessage(model.RoleAssistant, "a1"),
		textMessage(model.RoleUser, "u2"),
		textMessage(model.RoleAssistant, "a2"),
		textMessage(model.RoleUser, "u3"),
		textMessage(model.RoleAssistant, "a3"),
	}
	sess := session.NewSession(session.WithMessages(msgs...))
	ctx := session.NewContext(context.Background(), sess)
	summarizer := &recordingSummarizer{}

	snapshot, err := sess.Messages(ctx)
	require.NoError(t, err)
	view, err := NewBlockSummarize(
		WithKeepRecentMessages(2),
		WithSummaryBatchMessages(2),
		WithSummarizer(summarizer),
	).Compact(ctx, Request{Messages: snapshot})
	require.NoError(t, err)

	require.Len(t, view, 4)
	assert.Contains(t, content.TextFromParts(view[0].Parts), "summary-1")
	assert.Contains(t, content.TextFromParts(view[1].Parts), "summary-2")
	assert.Equal(t, "u3", content.TextFromParts(view[2].Parts))
	assert.Equal(t, "a3", content.TextFromParts(view[3].Parts))
	assert.Len(t, summarizer.calls, 2)

	full, err := sess.Messages(ctx)
	require.NoError(t, err)
	require.Len(t, full, 6)
	assert.Equal(t, "u1", content.TextFromParts(full[0].Parts))

	state, ok := sess.State()[blockSummaryStateKey].(blockSummaryState)
	require.True(t, ok)
	require.Len(t, state.Blocks, 2)
	assert.Equal(t, 0, state.Blocks[0].Start)
	assert.Equal(t, 2, state.Blocks[0].End)
	assert.Equal(t, 2, state.Blocks[1].Start)
	assert.Equal(t, 4, state.Blocks[1].End)
}

func TestBlockSummarizeMergesOldSummaryBlocks(t *testing.T) {
	msgs := []*model.Message{
		textMessage(model.RoleUser, "u1"),
		textMessage(model.RoleAssistant, "a1"),
		textMessage(model.RoleUser, "u2"),
		textMessage(model.RoleAssistant, "a2"),
	}
	ctx := session.NewContext(context.Background(), session.NewSession(session.WithMessages(msgs...)))
	summarizer := &recordingSummarizer{}

	view, err := NewBlockSummarize(
		WithKeepRecentMessages(1),
		WithSummaryBatchMessages(1),
		WithMaxSummaryBlocks(1),
		WithSummarizer(summarizer),
	).Compact(ctx, Request{Messages: msgs})
	require.NoError(t, err)

	require.Len(t, view, 2)
	assert.Contains(t, content.TextFromParts(view[0].Parts), "summary")
	assert.Equal(t, "a2", content.TextFromParts(view[1].Parts))

	state := ctxSessionState(ctx)
	require.Len(t, state.Blocks, 1)
	assert.Equal(t, 0, state.Blocks[0].Start)
	assert.Equal(t, 3, state.Blocks[0].End)
}

type recordingSummarizer struct {
	calls []SummaryRequest
}

func (s *recordingSummarizer) Summarize(_ context.Context, req SummaryRequest) (string, error) {
	s.calls = append(s.calls, req)
	return fmt.Sprintf("summary-%d", len(s.calls)), nil
}

func textMessage(role model.Role, text string) *model.Message {
	return &model.Message{Role: role, Parts: []content.Part{content.Text{Text: text}}}
}

func toolUseMessage(id string) *model.Message {
	return &model.Message{
		Role: model.RoleAssistant,
		Parts: []content.Part{content.ToolUse{
			ID:    id,
			Name:  "lookup",
			Input: json.RawMessage(`{"query":"x"}`),
		}},
	}
}

func toolResultMessage(id, text string) *model.Message {
	return &model.Message{
		Role: model.RoleTool,
		Parts: []content.Part{content.ToolResult{
			ID:    id,
			Name:  "lookup",
			Parts: []content.Part{content.Text{Text: text}},
		}},
	}
}

func toolResultText(msg *model.Message) string {
	for _, part := range msg.Parts {
		if result, ok := part.(content.ToolResult); ok {
			return content.TextFromParts(result.Parts)
		}
	}
	return ""
}

func ctxSessionState(ctx context.Context) blockSummaryState {
	sess, _ := session.FromContext(ctx)
	state, _ := sess.State()[blockSummaryStateKey].(blockSummaryState)
	return state
}
