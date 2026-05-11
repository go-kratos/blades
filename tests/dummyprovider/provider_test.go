package dummyprovider

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/model"
	"github.com/stretchr/testify/assert"
	"github.com/tidwall/gjson"
)

func TestProviderReturnsPredefinedUsageAndTracksCalls(t *testing.T) {
	provider := NewProvider(TextResponse(
		"hello world",
		WithResponseUsage(model.Usage{InputTokens: 3, OutputTokens: 4}),
	))

	resp, err := provider.Generate(context.Background(), &model.Request{
		System: "Be concise.",
		Messages: []*model.Message{{
			Role:  model.RoleUser,
			Parts: []content.Part{Text("hi there")},
		}},
	})

	assert.NoError(t, err)
	assert.Equal(t, "dummy", provider.Name())
	assert.Equal(t, "hello world", textParts(resp.Message.Parts))
	assert.Equal(t, model.Usage{InputTokens: 3, OutputTokens: 4}, resp.Usage)
	assert.Equal(t, 1, provider.CallCount())
}

func TestHelpersConstructTextThinkingAndToolUseParts(t *testing.T) {
	input := json.RawMessage(`{"text":"hi"}`)
	resp := AssistantResponse(
		[]content.Part{
			Thinking("think"),
			ToolUse("tool-1", "echo", input),
			Text("done"),
		},
		WithStopReason(model.StopToolUse),
	)

	assert.Equal(t, model.StopToolUse, resp.StopReason)
	if !assert.Len(t, resp.Message.Parts, 3) {
		return
	}

	thinking, ok := resp.Message.Parts[0].(content.Thinking)
	if assert.True(t, ok) {
		assert.Equal(t, "think", thinking.Text)
	}

	toolUse, ok := resp.Message.Parts[1].(content.ToolUse)
	if assert.True(t, ok) {
		assert.Equal(t, "tool-1", toolUse.ID)
		assert.Equal(t, "echo", toolUse.Name)
		assert.Equal(t, "hi", gjson.GetBytes(toolUse.Input, "text").String())
	}

	assert.Equal(t, "done", textParts([]content.Part{resp.Message.Parts[2]}))
}

func TestProviderConsumesQueuedResponsesInOrderAndErrorsWhenExhausted(t *testing.T) {
	provider := NewProvider(
		TextResponse("first"),
		TextResponse("second"),
	)

	first, err := provider.Generate(context.Background(), &model.Request{})
	assert.NoError(t, err)
	assert.Equal(t, "first", textParts(first.Message.Parts))

	second, err := provider.Generate(context.Background(), &model.Request{})
	assert.NoError(t, err)
	assert.Equal(t, "second", textParts(second.Message.Parts))

	_, err = provider.Generate(context.Background(), &model.Request{})
	assert.ErrorIs(t, err, ErrNoResponses)
	assert.Equal(t, 0, provider.PendingResponseCount())
	assert.Equal(t, 3, provider.CallCount())
}

func TestProviderCanReplaceAndAppendQueuedResponses(t *testing.T) {
	provider := NewProvider(TextResponse("first"))

	resp, err := provider.Generate(context.Background(), &model.Request{})
	assert.NoError(t, err)
	assert.Equal(t, "first", textParts(resp.Message.Parts))
	assert.Equal(t, 0, provider.PendingResponseCount())

	provider.SetResponses(TextResponse("second"))
	assert.Equal(t, 1, provider.PendingResponseCount())
	resp, err = provider.Generate(context.Background(), &model.Request{})
	assert.NoError(t, err)
	assert.Equal(t, "second", textParts(resp.Message.Parts))

	provider.AppendResponses(TextResponse("third"), TextResponse("fourth"))
	assert.Equal(t, 2, provider.PendingResponseCount())

	resp, err = provider.Generate(context.Background(), &model.Request{})
	assert.NoError(t, err)
	assert.Equal(t, "third", textParts(resp.Message.Parts))

	resp, err = provider.Generate(context.Background(), &model.Request{})
	assert.NoError(t, err)
	assert.Equal(t, "fourth", textParts(resp.Message.Parts))
	assert.Equal(t, 0, provider.PendingResponseCount())
}

func TestProviderStreamsThinkingTextAndToolUseChunks(t *testing.T) {
	provider := NewProvider(AssistantResponse(
		[]content.Part{
			Thinking("think"),
			Text("answer"),
			ToolUse("tool-1", "echo", json.RawMessage(`{"text":"hi","count":12}`)),
		},
		WithStopReason(model.StopToolUse),
	))

	chunks, errs := collectStream(provider, context.Background(), nil)

	assert.Empty(t, errs)
	if !assert.Len(t, chunks, 12) {
		return
	}
	assert.Equal(t, []string{"t", "h", "i", "n", "k"}, thinkingChunks(chunks[:5]))
	assert.Equal(t, []string{"a", "n", "s", "w", "e", "r"}, textChunks(chunks[5:11]))

	toolUse, ok := chunks[11].Parts[0].(content.ToolUse)
	if assert.True(t, ok) {
		assert.Equal(t, "tool-1", toolUse.ID)
		assert.Equal(t, "echo", toolUse.Name)
		assert.Equal(t, "hi", gjson.GetBytes(toolUse.Input, "text").String())
		assert.Equal(t, int64(12), gjson.GetBytes(toolUse.Input, "count").Int())
	}
	assert.Equal(t, model.StopToolUse, chunks[11].StopReason)
}

func TestProviderStreamsExactChunkOrder(t *testing.T) {
	provider := NewProvider(AssistantResponse(
		[]content.Part{
			Thinking("go"),
			Text("ok"),
			ToolUse("tool-1", "echo", json.RawMessage(`{}`)),
		},
		WithStopReason(model.StopToolUse),
	))

	chunks, errs := collectStream(provider, context.Background(), nil)

	assert.Empty(t, errs)
	assert.Equal(t, []string{
		"thinking:g",
		"thinking:o",
		"text:o",
		"text:k",
		"tool:echo",
	}, chunkLabels(chunks))
}

func TestProviderStreamsMultipleToolUsesInOneMessage(t *testing.T) {
	provider := NewProvider(AssistantResponse(
		[]content.Part{
			ToolUse("tool-1", "echo", json.RawMessage(`{"text":"one"}`)),
			ToolUse("tool-2", "echo", json.RawMessage(`{"text":"two"}`)),
		},
		WithStopReason(model.StopToolUse),
	))

	chunks, errs := collectStream(provider, context.Background(), nil)

	assert.Empty(t, errs)
	if !assert.Len(t, chunks, 2) {
		return
	}
	assert.Equal(t, []string{"tool:echo", "tool:echo"}, chunkLabels(chunks))
	assert.Equal(t, model.StopToolUse, chunks[1].StopReason)
}

func TestProviderStreamsExplicitAssistantErrorStopReason(t *testing.T) {
	provider := NewProvider(TextResponse("partial", WithStopReason(model.StopReason("error"))))

	chunks, errs := collectStream(provider, context.Background(), nil)

	assert.Empty(t, errs)
	if assert.Len(t, chunks, 7) {
		assert.Equal(t, model.StopReason("error"), chunks[6].StopReason)
		assert.Equal(t, "partial", joinTextChunks(chunks))
	}
}

func TestProviderStreamsExplicitAssistantAbortedStopReason(t *testing.T) {
	provider := NewProvider(TextResponse("partial", WithStopReason(model.StopReason("aborted"))))

	chunks, errs := collectStream(provider, context.Background(), nil)

	assert.Empty(t, errs)
	if assert.Len(t, chunks, 7) {
		assert.Equal(t, model.StopReason("aborted"), chunks[6].StopReason)
		assert.Equal(t, "partial", joinTextChunks(chunks))
	}
}

func TestProviderSupportsCancelBeforeFirstChunk(t *testing.T) {
	provider := NewProvider(TextResponse("abcdefghijklmnopqrstuvwxyz"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	chunks, errs := collectStream(provider, ctx, nil)

	assert.Empty(t, chunks)
	if assert.Len(t, errs, 1) {
		assert.ErrorIs(t, errs[0], context.Canceled)
	}
}

func TestProviderSupportsCancelMidTextStream(t *testing.T) {
	provider := NewProvider(TextResponse("abcdefghijklmnopqrstuvwxyz"))
	ctx, cancel := context.WithCancel(context.Background())

	chunks, errs := collectStream(provider, ctx, func(chunk *model.Chunk) {
		if len(chunk.Parts) > 0 {
			cancel()
		}
	})

	if assert.Len(t, chunks, 1) {
		assert.Equal(t, "a", textParts(chunks[0].Parts))
	}
	if assert.Len(t, errs, 1) {
		assert.ErrorIs(t, errs[0], context.Canceled)
	}
}

func TestProviderSupportsCancelMidThinkingStream(t *testing.T) {
	provider := NewProvider(AssistantResponse([]content.Part{Thinking("abcdefghijklmnopqrstuvwxyz")}))
	ctx, cancel := context.WithCancel(context.Background())

	chunks, errs := collectStream(provider, ctx, func(chunk *model.Chunk) {
		if len(chunk.Parts) > 0 {
			cancel()
		}
	})

	if assert.Len(t, chunks, 1) {
		assert.Equal(t, []string{"a"}, thinkingChunks(chunks))
	}
	if assert.Len(t, errs, 1) {
		assert.ErrorIs(t, errs[0], context.Canceled)
	}
}

func TestProviderSupportsCancelBetweenToolUseChunks(t *testing.T) {
	provider := NewProvider(AssistantResponse(
		[]content.Part{
			ToolUse("tool-1", "echo", json.RawMessage(`{"text":"one"}`)),
			ToolUse("tool-2", "echo", json.RawMessage(`{"text":"two"}`)),
		},
		WithStopReason(model.StopToolUse),
	))
	ctx, cancel := context.WithCancel(context.Background())

	chunks, errs := collectStream(provider, ctx, func(chunk *model.Chunk) {
		if len(chunk.Parts) > 0 {
			cancel()
		}
	})

	if assert.Len(t, chunks, 1) {
		assert.Equal(t, []string{"tool:echo"}, chunkLabels(chunks))
	}
	if assert.Len(t, errs, 1) {
		assert.ErrorIs(t, errs[0], context.Canceled)
	}
}

func collectStream(provider *Provider, ctx context.Context, onChunk func(*model.Chunk)) ([]*model.Chunk, []error) {
	var chunks []*model.Chunk
	var errs []error
	for chunk, err := range provider.Stream(ctx, &model.Request{}) {
		if err != nil {
			errs = append(errs, err)
			continue
		}
		chunks = append(chunks, chunk)
		if onChunk != nil {
			onChunk(chunk)
		}
	}
	return chunks, errs
}

func chunkLabels(chunks []*model.Chunk) []string {
	var out []string
	for _, chunk := range chunks {
		if len(chunk.Parts) == 0 {
			out = append(out, "empty")
			continue
		}
		switch part := chunk.Parts[0].(type) {
		case content.Text:
			out = append(out, "text:"+part.Text)
		case content.Thinking:
			out = append(out, "thinking:"+part.Text)
		case content.ToolUse:
			out = append(out, "tool:"+part.Name)
		default:
			out = append(out, "unknown")
		}
	}
	return out
}

func joinTextChunks(chunks []*model.Chunk) string {
	var out string
	for _, chunk := range chunks {
		out += textParts(chunk.Parts)
	}
	return out
}

func textParts(parts []content.Part) string {
	var out string
	for _, part := range parts {
		if text, ok := part.(content.Text); ok {
			out += text.Text
		}
	}
	return out
}

func textChunks(chunks []*model.Chunk) []string {
	var out []string
	for _, chunk := range chunks {
		out = append(out, textParts(chunk.Parts))
	}
	return out
}

func thinkingChunks(chunks []*model.Chunk) []string {
	var out []string
	for _, chunk := range chunks {
		for _, part := range chunk.Parts {
			if thinking, ok := part.(content.Thinking); ok {
				out = append(out, thinking.Text)
			}
		}
	}
	return out
}
