package compact

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/model"
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

func TestCountMessagesTokensUsesMessageBreakdown(t *testing.T) {
	counter := model.TokenCounterFunc(func(_ context.Context, req *model.Request) (model.TokenCount, error) {
		assert.Empty(t, req.System)
		assert.Empty(t, req.Tools)
		return model.TokenCount{Input: 99, Messages: 7}, nil
	})

	tokens, err := countMessagesTokens(context.Background(), counter, []*model.Message{textMessage(model.RoleUser, "hello")})
	require.NoError(t, err)
	assert.Equal(t, int64(7), tokens)
}

func TestCountMessagesTokensFallsBackToInputTokens(t *testing.T) {
	counter := model.TokenCounterFunc(func(context.Context, *model.Request) (model.TokenCount, error) {
		return model.TokenCount{Input: 9}, nil
	})

	tokens, err := countMessagesTokens(context.Background(), counter, []*model.Message{textMessage(model.RoleUser, "hello")})
	require.NoError(t, err)
	assert.Equal(t, int64(9), tokens)
}

func TestWindowUsesRequestTokenCounter(t *testing.T) {
	called := false
	counter := model.TokenCounterFunc(func(_ context.Context, req *model.Request) (model.TokenCount, error) {
		called = true
		return model.TokenCount{Input: int64(len(req.Messages)) * 10}, nil
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

func TestBlockSummarizeBuildsSummaryAndKeepsRecentMessages(t *testing.T) {
	msgs := []*model.Message{
		textMessage(model.RoleUser, "u1"),
		textMessage(model.RoleAssistant, "a1"),
		textMessage(model.RoleUser, "u2"),
		textMessage(model.RoleAssistant, "a2"),
		textMessage(model.RoleUser, "u3"),
		textMessage(model.RoleAssistant, "a3"),
	}
	summarizer := &recordingSummarizer{}

	view, err := NewSummarize(
		WithKeepRecentMessages(2),
		WithSummarizer(summarizer),
	).Compact(context.Background(), Request{Messages: msgs})
	require.NoError(t, err)

	require.Len(t, view, 3)
	assert.Contains(t, content.TextFromParts(view[0].Parts), "summary-1")
	assert.Equal(t, "u3", content.TextFromParts(view[1].Parts))
	assert.Equal(t, "a3", content.TextFromParts(view[2].Parts))
	assert.Len(t, summarizer.calls, 1)
	assert.Len(t, summarizer.calls[0].Messages, 4)
}

func TestBlockSummarizeWithNoSummarizerDropsOldMessages(t *testing.T) {
	msgs := []*model.Message{
		textMessage(model.RoleUser, "u1"),
		textMessage(model.RoleAssistant, "a1"),
		textMessage(model.RoleUser, "u2"),
		textMessage(model.RoleAssistant, "a2"),
	}

	view, err := NewSummarize(
		WithKeepRecentMessages(1),
	).Compact(context.Background(), Request{Messages: msgs})
	require.NoError(t, err)

	require.Len(t, view, 1)
	assert.Equal(t, "a2", content.TextFromParts(view[0].Parts))
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
