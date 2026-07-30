package anthropic

import (
	"encoding/json"
	"testing"
)

func TestStreamAccumulatorUsesMessageDeltaUsage(t *testing.T) {
	t.Parallel()

	accumulator := newStreamAccumulator()
	usage := accumulator.messageDelta(decodeMessageDeltaEvent(t, `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"cache_creation_input_tokens":3,"cache_read_input_tokens":4,"input_tokens":5,"output_tokens":6,"server_tool_use":{"web_search_requests":1},"provider_extension":{"billable_tokens":12}}}`))

	for name, values := range map[string][2]int64{
		"input cached tokens":      {usage.InputCachedTokens, 4},
		"input cache miss tokens":  {usage.InputCacheMissTokens, 5},
		"input write cache tokens": {usage.InputWriteCacheTokens, 3},
		"total input tokens":       {usage.TotalInputTokens, 12},
		"total output tokens":      {usage.TotalOutputTokens, 6},
		"total tokens":             {usage.TotalTokens, 18},
	} {
		if got, want := values[0], values[1]; got != want {
			t.Fatalf("%s = %d, want %d", name, got, want)
		}
	}
	assertAnthropicRawUsageNumber(t, usage.Raw, []string{"cache_creation_input_tokens"}, 3)
	assertAnthropicRawUsageNumber(t, usage.Raw, []string{"cache_read_input_tokens"}, 4)
	assertAnthropicRawUsageNumber(t, usage.Raw, []string{"output_tokens"}, 6)
	assertAnthropicRawUsageNumber(t, usage.Raw, []string{"provider_extension", "billable_tokens"}, 12)
}

func assertAnthropicRawUsageNumber(t *testing.T, raw json.RawMessage, path []string, want float64) {
	t.Helper()
	value := anthropicRawUsageValue(t, raw, path)
	if got, ok := value.(float64); !ok || got != want {
		t.Fatalf("raw usage path %v = %v, want %v: %s", path, value, want, raw)
	}
}

func anthropicRawUsageValue(t *testing.T, raw json.RawMessage, path []string) any {
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
	return value
}
