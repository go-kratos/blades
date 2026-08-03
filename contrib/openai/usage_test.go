package openai

import (
	"encoding/json"
	"testing"

	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

func TestChatConversionsPreserveRawUsage(t *testing.T) {
	t.Parallel()

	const usageJSON = `{
		"prompt_tokens": 5,
		"completion_tokens": 4,
		"total_tokens": 9,
		"prompt_tokens_details": {"cached_tokens": 3, "audio_tokens": 1},
		"completion_tokens_details": {
			"reasoning_tokens": 2,
			"audio_tokens": 1,
			"accepted_prediction_tokens": 1,
			"rejected_prediction_tokens": 1
		},
		"provider_extension": {"billable_tokens": 6}
	}`

	var completion openaisdk.ChatCompletion
	if err := json.Unmarshal([]byte(`{"choices":[],"usage":`+usageJSON+`}`), &completion); err != nil {
		t.Fatalf("unmarshal chat completion: %v", err)
	}
	response, err := choiceToResponse(&completion, nil)
	if err != nil {
		t.Fatalf("choiceToResponse returned error: %v", err)
	}
	if got, want := response.Usage.TotalInputTokens, int64(5); got != want {
		t.Fatalf("total input tokens = %d, want %d", got, want)
	}
	if got, want := response.Usage.TotalOutputTokens, int64(4); got != want {
		t.Fatalf("total output tokens = %d, want %d", got, want)
	}
	assertModelUsageField(t, "input cached tokens", response.Usage.InputCachedTokens, 3)
	assertModelUsageField(t, "input cache miss tokens", response.Usage.InputCacheMissTokens, 2)
	assertModelUsageField(t, "input audio tokens", response.Usage.InputAudioTokens, 1)
	assertModelUsageField(t, "output audio tokens", response.Usage.OutputAudioTokens, 1)
	assertModelUsageField(t, "output reasoning tokens", response.Usage.OutputReasoningTokens, 2)
	assertModelUsageField(t, "accepted prediction tokens", response.Usage.AcceptedPredictionTokens, 1)
	assertModelUsageField(t, "rejected prediction tokens", response.Usage.RejectedPredictionTokens, 1)
	assertModelUsageField(t, "total tokens", response.Usage.TotalTokens, 9)
	assertRawUsageNumber(t, response.Usage.Raw, []string{"prompt_tokens_details", "cached_tokens"}, 3)
	assertRawUsageNumber(t, response.Usage.Raw, []string{"completion_tokens_details", "reasoning_tokens"}, 2)
	assertRawUsageNumber(t, response.Usage.Raw, []string{"provider_extension", "billable_tokens"}, 6)

	var streamChunk openaisdk.ChatCompletionChunk
	if err := json.Unmarshal([]byte(`{"choices":[],"usage":`+usageJSON+`}`), &streamChunk); err != nil {
		t.Fatalf("unmarshal chat completion chunk: %v", err)
	}
	chunk := chunkToModelChunk(streamChunk)
	if chunk.Usage == nil {
		t.Fatal("chunk usage is nil")
	}
	assertModelUsageField(t, "stream input cached tokens", chunk.Usage.InputCachedTokens, 3)
	assertModelUsageField(t, "stream output reasoning tokens", chunk.Usage.OutputReasoningTokens, 2)
	assertRawUsageNumber(t, chunk.Usage.Raw, []string{"prompt_tokens_details", "cached_tokens"}, 3)
	assertRawUsageNumber(t, chunk.Usage.Raw, []string{"provider_extension", "billable_tokens"}, 6)
}

func TestResponsesConversionPreservesRawUsage(t *testing.T) {
	t.Parallel()

	var usage responses.ResponseUsage
	if err := json.Unmarshal([]byte(`{
		"input_tokens": 8,
		"output_tokens": 5,
		"total_tokens": 13,
		"input_tokens_details": {"cached_tokens": 6},
		"output_tokens_details": {"reasoning_tokens": 4},
		"provider_extension": {"billable_tokens": 7}
	}`), &usage); err != nil {
		t.Fatalf("unmarshal responses usage: %v", err)
	}

	converted := responseUsageToModel(usage)
	assertModelUsageField(t, "input cached tokens", converted.InputCachedTokens, 6)
	assertModelUsageField(t, "input cache miss tokens", converted.InputCacheMissTokens, 2)
	assertModelUsageField(t, "output reasoning tokens", converted.OutputReasoningTokens, 4)
	assertModelUsageField(t, "total input tokens", converted.TotalInputTokens, 8)
	assertModelUsageField(t, "total output tokens", converted.TotalOutputTokens, 5)
	assertModelUsageField(t, "total tokens", converted.TotalTokens, 13)
	assertRawUsageNumber(t, converted.Raw, []string{"input_tokens_details", "cached_tokens"}, 6)
	assertRawUsageNumber(t, converted.Raw, []string{"output_tokens_details", "reasoning_tokens"}, 4)
	assertRawUsageNumber(t, converted.Raw, []string{"provider_extension", "billable_tokens"}, 7)
}

func TestImageConversionPreservesRawUsage(t *testing.T) {
	t.Parallel()

	var imageResponse openaisdk.ImagesResponse
	if err := json.Unmarshal([]byte(`{
		"data": [],
		"output_format": "png",
		"usage": {
			"input_tokens": 10,
			"output_tokens": 12,
			"total_tokens": 22,
			"input_tokens_details": {"image_tokens": 8, "text_tokens": 2},
			"provider_extension": {"billable_tokens": 20}
		}
	}`), &imageResponse); err != nil {
		t.Fatalf("unmarshal image response: %v", err)
	}

	response, err := toImageResponse(&imageResponse)
	if err != nil {
		t.Fatalf("toImageResponse returned error: %v", err)
	}
	assertModelUsageField(t, "input cache miss tokens", response.Usage.InputCacheMissTokens, 10)
	assertModelUsageField(t, "input text tokens", response.Usage.InputTextTokens, 2)
	assertModelUsageField(t, "input image tokens", response.Usage.InputImageTokens, 8)
	assertModelUsageField(t, "output image tokens", response.Usage.OutputImageTokens, 12)
	assertModelUsageField(t, "total input tokens", response.Usage.TotalInputTokens, 10)
	assertModelUsageField(t, "total output tokens", response.Usage.TotalOutputTokens, 12)
	assertModelUsageField(t, "total tokens", response.Usage.TotalTokens, 22)
	assertRawUsageNumber(t, response.Usage.Raw, []string{"input_tokens_details", "image_tokens"}, 8)
	assertRawUsageNumber(t, response.Usage.Raw, []string{"provider_extension", "billable_tokens"}, 20)
}

func assertModelUsageField(t *testing.T, name string, got, want int64) {
	t.Helper()
	if got != want {
		t.Fatalf("%s = %d, want %d", name, got, want)
	}
}

func assertRawUsageNumber(t *testing.T, raw json.RawMessage, path []string, want float64) {
	t.Helper()

	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("unmarshal raw usage %s: %v", raw, err)
	}
	for _, name := range path {
		fields, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("raw usage path %v does not contain an object at %q: %s", path, name, raw)
		}
		value, ok = fields[name]
		if !ok {
			t.Fatalf("raw usage path %v is missing %q: %s", path, name, raw)
		}
	}
	if got, ok := value.(float64); !ok || got != want {
		t.Fatalf("raw usage path %v = %v, want %v: %s", path, value, want, raw)
	}
}
