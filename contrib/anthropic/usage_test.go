package anthropic

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/model"
)

func TestStreamAccumulatorUsesMessageDeltaUsage(t *testing.T) {
	t.Parallel()

	accumulator := newStreamAccumulator(nil)
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

// TestStreamKeepsInputUsageFromMessageStart pins that input usage reported only in message_start
// survives to the final chunk: some gateways put just output_tokens in message_delta.
func TestStreamKeepsInputUsageFromMessageStart(t *testing.T) {
	t.Parallel()

	var sse strings.Builder
	writeSSEEvent(&sse, "message_start", `{"message":{"content":[],"id":"msg_1","model":"claude-test","role":"assistant","stop_reason":null,"stop_sequence":null,"type":"message","usage":{"input_tokens":11,"cache_read_input_tokens":6,"output_tokens":1}},"type":"message_start"}`)
	writeSSEEvent(&sse, "content_block_start", `{"content_block":{"text":"","type":"text"},"index":0,"type":"content_block_start"}`)
	writeSSEEvent(&sse, "content_block_delta", `{"delta":{"text":"hi","type":"text_delta"},"index":0,"type":"content_block_delta"}`)
	writeSSEEvent(&sse, "content_block_stop", `{"index":0,"type":"content_block_stop"}`)
	start := sse.String()
	// A gateway may omit the input counts from message_delta or send them as zero.
	for _, delta := range []string{`{"output_tokens":4}`, `{"input_tokens":0,"cache_read_input_tokens":0,"output_tokens":4}`} {
		var sse strings.Builder
		sse.WriteString(start)
		writeSSEEvent(&sse, "message_delta", `{"delta":{"stop_reason":"end_turn","stop_sequence":null},"type":"message_delta","usage":`+delta+`}`)
		writeSSEEvent(&sse, "message_stop", `{"type":"message_stop"}`)
		checkStreamUsage(t, delta, sse.String())
	}
}

func checkStreamUsage(t *testing.T, delta, stream string) {
	t.Helper()
	provider := newTestProvider(t, stream)
	var usage *model.Usage
	for chunk, err := range provider.Stream(context.Background(), &model.Request{
		Messages: []*model.Message{{Role: model.RoleUser, Parts: []content.Part{content.Text{Text: "hi"}}}},
	}) {
		if err != nil {
			t.Fatalf("Stream returned error: %v", err)
		}
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
	}
	if usage == nil {
		t.Fatalf("delta %s: stream yielded no usage", delta)
	}
	for name, values := range map[string][2]int64{
		"input cached tokens":     {usage.InputCachedTokens, 6},
		"input cache miss tokens": {usage.InputCacheMissTokens, 11},
		"total input tokens":      {usage.TotalInputTokens, 17},
		"total output tokens":     {usage.TotalOutputTokens, 4},
		"total tokens":            {usage.TotalTokens, 21},
	} {
		if got, want := values[0], values[1]; got != want {
			t.Fatalf("delta %s: %s = %d, want %d", delta, name, got, want)
		}
	}
}
