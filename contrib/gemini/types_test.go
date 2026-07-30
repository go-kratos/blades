package gemini

import (
	"encoding/json"
	"testing"

	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/model"
	"google.golang.org/genai"
)

func TestConvertMessageToGenAI_AssistantRole(t *testing.T) {
	t.Parallel()

	_, contents, err := convertMessageToGenAI(&model.Request{
		Messages: []*model.Message{
			{Role: model.RoleUser, Parts: []content.Part{content.Text{Text: "hello"}}},
			{Role: model.RoleAssistant, Parts: []content.Part{content.Text{Text: "world"}}},
		},
	})
	if err != nil {
		t.Fatalf("convertMessageToGenAI returned error: %v", err)
	}
	if got, want := len(contents), 2; got != want {
		t.Fatalf("contents len = %d, want %d", got, want)
	}
	if got, want := contents[0].Role, genai.RoleUser; got != want {
		t.Fatalf("first role = %q, want %q", got, want)
	}
	if got, want := contents[1].Role, genai.RoleModel; got != want {
		t.Fatalf("second role = %q, want %q", got, want)
	}
}

func TestConvertGenAIToBlades_FunctionCallMappedToToolPart(t *testing.T) {
	t.Parallel()

	resp := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{
				Content: &genai.Content{
					Parts: []*genai.Part{
						{
							FunctionCall: &genai.FunctionCall{
								ID:   "call_1",
								Name: "get_weather",
								Args: map[string]any{"city": "Paris", "unit": "C"},
							},
						},
					},
				},
			},
		},
	}

	converted, err := convertGenAIToBlades(resp)
	if err != nil {
		t.Fatalf("convertGenAIToBlades returned error: %v", err)
	}
	if got, want := len(converted.Message.Parts), 1; got != want {
		t.Fatalf("parts len = %d, want %d", got, want)
	}

	toolPart, ok := converted.Message.Parts[0].(content.ToolUse)
	if !ok {
		t.Fatalf("part type = %T, want content.ToolUse", converted.Message.Parts[0])
	}
	if got, want := toolPart.ID, "call_1"; got != want {
		t.Fatalf("tool id = %q, want %q", got, want)
	}
	if got, want := toolPart.Name, "get_weather"; got != want {
		t.Fatalf("tool name = %q, want %q", got, want)
	}
	var args map[string]any
	if err := json.Unmarshal(toolPart.Input, &args); err != nil {
		t.Fatalf("unmarshal tool request: %v", err)
	}
	if got, want := args["city"], "Paris"; got != want {
		t.Fatalf("tool args city = %v, want %v", got, want)
	}
	if got, want := args["unit"], "C"; got != want {
		t.Fatalf("tool args unit = %v, want %v", got, want)
	}
}

func TestConvertGenAIToChunkPreservesRawUsage(t *testing.T) {
	t.Parallel()

	metadata := &genai.GenerateContentResponseUsageMetadata{
		PromptTokenCount:        10,
		CandidatesTokenCount:    4,
		CachedContentTokenCount: 7,
		ToolUsePromptTokenCount: 2,
		ThoughtsTokenCount:      3,
		TotalTokenCount:         19,
		PromptTokensDetails: []*genai.ModalityTokenCount{
			{Modality: genai.MediaModalityText, TokenCount: 4},
			{Modality: genai.MediaModalityImage, TokenCount: 3},
			{Modality: genai.MediaModalityAudio, TokenCount: 2},
			{Modality: genai.MediaModalityVideo, TokenCount: 1},
		},
		CacheTokensDetails: []*genai.ModalityTokenCount{
			{Modality: genai.MediaModalityText, TokenCount: 2},
			{Modality: genai.MediaModalityImage, TokenCount: 2},
			{Modality: genai.MediaModalityAudio, TokenCount: 2},
			{Modality: genai.MediaModalityVideo, TokenCount: 1},
		},
		CandidatesTokensDetails: []*genai.ModalityTokenCount{
			{Modality: genai.MediaModalityText, TokenCount: 1},
			{Modality: genai.MediaModalityImage, TokenCount: 2},
			{Modality: genai.MediaModalityAudio, TokenCount: 1},
		},
	}
	chunk, err := convertGenAIToChunk(&genai.GenerateContentResponse{UsageMetadata: metadata})
	if err != nil {
		t.Fatalf("convertGenAIToChunk returned error: %v", err)
	}
	if chunk.Usage == nil {
		t.Fatal("chunk usage is nil")
	}
	for name, values := range map[string][2]int64{
		"input cached tokens":       {chunk.Usage.InputCachedTokens, 7},
		"input cache miss tokens":   {chunk.Usage.InputCacheMissTokens, 3},
		"input cached text tokens":  {chunk.Usage.InputCachedTextTokens, 2},
		"input cached image tokens": {chunk.Usage.InputCachedImageTokens, 2},
		"input cached audio tokens": {chunk.Usage.InputCachedAudioTokens, 2},
		"input cached video tokens": {chunk.Usage.InputCachedVideoTokens, 1},
		"input text tokens":         {chunk.Usage.InputTextTokens, 4},
		"input image tokens":        {chunk.Usage.InputImageTokens, 3},
		"input audio tokens":        {chunk.Usage.InputAudioTokens, 2},
		"input video tokens":        {chunk.Usage.InputVideoTokens, 1},
		"input tool tokens":         {chunk.Usage.InputToolTokens, 2},
		"output text tokens":        {chunk.Usage.OutputTextTokens, 1},
		"output image tokens":       {chunk.Usage.OutputImageTokens, 2},
		"output audio tokens":       {chunk.Usage.OutputAudioTokens, 1},
		"output reasoning tokens":   {chunk.Usage.OutputReasoningTokens, 3},
		"total input tokens":        {chunk.Usage.TotalInputTokens, 12},
		"total output tokens":       {chunk.Usage.TotalOutputTokens, 7},
		"total tokens":              {chunk.Usage.TotalTokens, 19},
	} {
		if got, want := values[0], values[1]; got != want {
			t.Fatalf("%s = %d, want %d", name, got, want)
		}
	}

	var raw map[string]any
	if err := json.Unmarshal(chunk.Usage.Raw, &raw); err != nil {
		t.Fatalf("unmarshal raw usage %s: %v", chunk.Usage.Raw, err)
	}
	for name, want := range map[string]float64{
		"cachedContentTokenCount": 7,
		"thoughtsTokenCount":      3,
		"totalTokenCount":         19,
	} {
		if got := raw[name]; got != want {
			t.Fatalf("raw usage %s = %v, want %v: %s", name, got, want, chunk.Usage.Raw)
		}
	}
}

func TestConvertGenAIToBlades_MixedTextAndFunctionCallUsesToolRole(t *testing.T) {
	t.Parallel()

	resp := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{
				Content: &genai.Content{
					Parts: []*genai.Part{
						{Text: "Let me check that."},
						{
							FunctionCall: &genai.FunctionCall{
								ID:   "call_2",
								Name: "get_time",
								Args: map[string]any{"timezone": "UTC"},
							},
						},
					},
				},
			},
		},
	}

	converted, err := convertGenAIToBlades(resp)
	if err != nil {
		t.Fatalf("convertGenAIToBlades returned error: %v", err)
	}
	if got, want := len(converted.Message.Parts), 2; got != want {
		t.Fatalf("parts len = %d, want %d", got, want)
	}

	textPart, ok := converted.Message.Parts[0].(content.Text)
	if !ok {
		t.Fatalf("first part type = %T, want content.Text", converted.Message.Parts[0])
	}
	if got, want := textPart.Text, "Let me check that."; got != want {
		t.Fatalf("text part = %q, want %q", got, want)
	}
	_, ok = converted.Message.Parts[1].(content.ToolUse)
	if !ok {
		t.Fatalf("second part type = %T, want content.ToolUse", converted.Message.Parts[1])
	}
}
