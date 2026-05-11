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

func TestProviderGeneratesPredefinedResponses(t *testing.T) {
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
}

func TestProviderStreamsPredefinedResponse(t *testing.T) {
	provider := NewProvider(TextResponse(
		"hello",
		WithResponseUsage(model.Usage{InputTokens: 1, OutputTokens: 2}),
	))

	var chunks []*model.Chunk
	for chunk, err := range provider.Stream(context.Background(), &model.Request{}) {
		assert.NoError(t, err)
		chunks = append(chunks, chunk)
	}

	if assert.Len(t, chunks, 5) {
		assert.Equal(t, []string{"h", "e", "l", "l", "o"}, textChunks(chunks))
		assert.Nil(t, chunks[0].Usage)
		assert.Equal(t, int64(1), chunks[4].Usage.InputTokens)
		assert.Equal(t, int64(2), chunks[4].Usage.OutputTokens)
	}
}

func TestTextResponse(t *testing.T) {
	resp := TextResponse(
		"hello",
		WithStopReason(model.StopEnd),
		WithResponseUsage(model.Usage{InputTokens: 3, OutputTokens: 4}),
	)

	assert.Equal(t, model.StopEnd, resp.StopReason)
	assert.Equal(t, model.Usage{InputTokens: 3, OutputTokens: 4}, resp.Usage)
	assert.Equal(t, "hello", textParts(resp.Message.Parts))
}

func TestToolUseResponse(t *testing.T) {
	resp := ToolUseResponse("call-1", "echo", json.RawMessage(`{"text":"hi"}`))

	assert.Equal(t, model.StopToolUse, resp.StopReason)
	if !assert.Len(t, resp.Message.Parts, 1) {
		return
	}

	toolUse, ok := resp.Message.Parts[0].(content.ToolUse)
	if assert.True(t, ok) {
		assert.Equal(t, "call-1", toolUse.ID)
		assert.Equal(t, "echo", toolUse.Name)
		assert.Equal(t, "hi", gjson.GetBytes(toolUse.Input, "text").String())
	}
}

func TestChunksFromResponse(t *testing.T) {
	resp := AssistantResponse(
		[]content.Part{
			Text("hello"),
			Thinking("thinking"),
		},
		WithStopReason(model.StopEnd),
		WithResponseUsage(model.Usage{InputTokens: 1, OutputTokens: 2}),
	)

	chunks := ChunksFromResponse(resp)
	if !assert.Len(t, chunks, 13) {
		return
	}
	assert.Equal(t, []string{"h", "e", "l", "l", "o"}, textChunks(chunks[:5]))
	assert.Equal(t, []string{"t", "h", "i", "n", "k", "i", "n", "g"}, thinkingChunks(chunks[5:]))

	assert.Equal(t, model.StopEnd, chunks[12].StopReason)
	assert.Nil(t, chunks[11].Usage)
	assert.Equal(t, int64(1), chunks[12].Usage.InputTokens)
	assert.Equal(t, int64(2), chunks[12].Usage.OutputTokens)
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
