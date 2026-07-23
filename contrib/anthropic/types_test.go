package anthropic

import (
	"encoding/json"
	"strings"
	"testing"

	anthropicSDK "github.com/anthropics/anthropic-sdk-go"
	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/model"
)

func TestConvertClaudeToBladesToolUse(t *testing.T) {
	t.Parallel()

	message := decodeAnthropicMessage(t, `{
		"id": "msg_1",
		"content": [
			{"type":"tool_use","id":"toolu_1","name":"get_weather","input":{"city":"Paris"}}
		],
		"model": "claude-sonnet-4-20250514",
		"role": "assistant",
		"stop_reason": "tool_use",
		"stop_sequence": "",
		"type": "message",
		"usage": {
			"cache_creation": {"ephemeral_1h_input_tokens":0,"ephemeral_5m_input_tokens":0},
			"cache_creation_input_tokens": 0,
			"cache_read_input_tokens": 0,
			"input_tokens": 1,
			"output_tokens": 1,
			"server_tool_use": {"web_search_requests":0},
			"service_tier": "standard"
		}
	}`)

	response, err := convertClaudeToBlades(message)
	if err != nil {
		t.Fatalf("convertClaudeToBlades returned error: %v", err)
	}
	if got, want := response.Message.Role, model.RoleAssistant; got != want {
		t.Fatalf("message role = %q, want %q", got, want)
	}
	if got, want := response.StopReason, model.StopToolUse; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	if got, want := len(response.Message.Parts), 1; got != want {
		t.Fatalf("parts len = %d, want %d", got, want)
	}

	toolUse, ok := response.Message.Parts[0].(content.ToolUse)
	if !ok {
		t.Fatalf("part type = %T, want content.ToolUse", response.Message.Parts[0])
	}
	if got, want := toolUse.ID, "toolu_1"; got != want {
		t.Fatalf("tool id = %q, want %q", got, want)
	}
	if got, want := toolUse.Name, "get_weather"; got != want {
		t.Fatalf("tool name = %q, want %q", got, want)
	}
	var request map[string]any
	if err := json.Unmarshal(toolUse.Input, &request); err != nil {
		t.Fatalf("unmarshal tool request: %v", err)
	}
	if got, want := request["city"], "Paris"; got != want {
		t.Fatalf("tool request city = %v, want %v", got, want)
	}
}

func TestConvertClaudeToBladesTextAndToolUse(t *testing.T) {
	t.Parallel()

	message := decodeAnthropicMessage(t, `{
		"id": "msg_2",
		"content": [
			{"type":"text","text":"Checking weather"},
			{"type":"tool_use","id":"toolu_2","name":"get_weather","input":{"city":"Tokyo"}}
		],
		"model": "claude-sonnet-4-20250514",
		"role": "assistant",
		"stop_reason": "tool_use",
		"stop_sequence": "",
		"type": "message",
		"usage": {
			"cache_creation": {"ephemeral_1h_input_tokens":0,"ephemeral_5m_input_tokens":0},
			"cache_creation_input_tokens": 0,
			"cache_read_input_tokens": 0,
			"input_tokens": 1,
			"output_tokens": 1,
			"server_tool_use": {"web_search_requests":0},
			"service_tier": "standard"
		}
	}`)

	response, err := convertClaudeToBlades(message)
	if err != nil {
		t.Fatalf("convertClaudeToBlades returned error: %v", err)
	}
	if got, want := response.Message.Role, model.RoleAssistant; got != want {
		t.Fatalf("message role = %q, want %q", got, want)
	}
	if got, want := len(response.Message.Parts), 2; got != want {
		t.Fatalf("parts len = %d, want %d", got, want)
	}

	textPart, ok := response.Message.Parts[0].(content.Text)
	if !ok {
		t.Fatalf("first part type = %T, want content.Text", response.Message.Parts[0])
	}
	if got, want := textPart.Text, "Checking weather"; got != want {
		t.Fatalf("first part text = %q, want %q", got, want)
	}
	toolUse, ok := response.Message.Parts[1].(content.ToolUse)
	if !ok {
		t.Fatalf("second part type = %T, want content.ToolUse", response.Message.Parts[1])
	}
	if got, want := toolUse.ID, "toolu_2"; got != want {
		t.Fatalf("tool id = %q, want %q", got, want)
	}
}

func TestConvertStreamDeltaToChunkPreservesThinkingSignature(t *testing.T) {
	t.Parallel()

	event := decodeContentBlockDeltaEvent(t, `{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"opaque-provider-signature"}}`)
	chunk := convertStreamDeltaToChunk(event)
	if got, want := len(chunk.Parts), 1; got != want {
		t.Fatalf("parts len = %d, want %d", got, want)
	}
	thinking, ok := chunk.Parts[0].(content.Thinking)
	if !ok {
		t.Fatalf("part type = %T, want content.Thinking", chunk.Parts[0])
	}
	if got, want := thinking.Text, ""; got != want {
		t.Fatalf("thinking text = %q, want %q", got, want)
	}
	if got, want := string(thinking.Signature), "opaque-provider-signature"; got != want {
		t.Fatalf("thinking signature = %q, want %q", got, want)
	}
}

func TestStreamAccumulatorCollectsToolUseInputJSONDeltas(t *testing.T) {
	t.Parallel()

	accumulator := newStreamAccumulator()
	accumulator.startContentBlock(decodeContentBlockStartEvent(t, `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"read","input":{}}}`))
	if err := accumulator.deltaContentBlock(decodeContentBlockDeltaEvent(t, `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\": "}}`)); err != nil {
		t.Fatalf("deltaContentBlock returned error: %v", err)
	}
	if err := accumulator.deltaContentBlock(decodeContentBlockDeltaEvent(t, `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"/tmp/file\"}"}}`)); err != nil {
		t.Fatalf("deltaContentBlock returned error: %v", err)
	}
	if err := accumulator.stopContentBlock(decodeContentBlockStopEvent(t, `{"type":"content_block_stop","index":0}`)); err != nil {
		t.Fatalf("stopContentBlock returned error: %v", err)
	}
	accumulator.messageDelta(decodeMessageDeltaEvent(t, `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":1,"output_tokens":5}}`))

	if got, want := accumulator.stopReason, model.StopToolUse; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	parts := accumulator.toolParts()
	if got, want := len(parts), 1; got != want {
		t.Fatalf("tool parts len = %d, want %d", got, want)
	}
	toolUse, ok := parts[0].(content.ToolUse)
	if !ok {
		t.Fatalf("part type = %T, want content.ToolUse", parts[0])
	}
	if got, want := toolUse.ID, "toolu_1"; got != want {
		t.Fatalf("tool id = %q, want %q", got, want)
	}
	if got, want := toolUse.Name, "read"; got != want {
		t.Fatalf("tool name = %q, want %q", got, want)
	}
	var input map[string]string
	if err := json.Unmarshal(toolUse.Input, &input); err != nil {
		t.Fatalf("tool input = %s, unmarshal error = %v", string(toolUse.Input), err)
	}
	if got, want := input["path"], "/tmp/file"; got != want {
		t.Fatalf("tool input path = %q, want %q", got, want)
	}
}

func TestStreamAccumulatorReportsInvalidToolInputJSON(t *testing.T) {
	t.Parallel()

	accumulator := newStreamAccumulator()
	accumulator.startContentBlock(decodeContentBlockStartEvent(t, `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"read","input":{}}}`))
	if err := accumulator.deltaContentBlock(decodeContentBlockDeltaEvent(t, `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\": "}}`)); err != nil {
		t.Fatalf("deltaContentBlock returned error: %v", err)
	}

	err := accumulator.stopContentBlock(decodeContentBlockStopEvent(t, `{"type":"content_block_stop","index":0}`))
	if err == nil {
		t.Fatal("stopContentBlock error = nil, want invalid tool input JSON error")
	}
	if got := err.Error(); !strings.Contains(got, `invalid tool input JSON for tool "read" (toolu_1)`) ||
		!strings.Contains(got, "unexpected end of JSON input") {
		t.Fatalf("stopContentBlock error = %q, want invalid tool input JSON error", got)
	}
	if got := err.Error(); strings.Contains(got, "error converting content block to JSON") ||
		strings.Contains(got, "json.RawMessage") {
		t.Fatalf("stopContentBlock error = %q, want Blades error without SDK marshal detail", got)
	}
	if got := len(accumulator.toolParts()); got != 0 {
		t.Fatalf("tool parts len = %d, want 0 after invalid JSON", got)
	}
}

func decodeAnthropicMessage(t *testing.T, data string) *anthropicSDK.Message {
	t.Helper()

	var message anthropicSDK.Message
	if err := json.Unmarshal([]byte(data), &message); err != nil {
		t.Fatalf("unmarshal anthropic message: %v", err)
	}
	return &message
}

func decodeStreamEvent(t *testing.T, data string) anthropicSDK.MessageStreamEventUnion {
	t.Helper()

	var event anthropicSDK.MessageStreamEventUnion
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		t.Fatalf("unmarshal anthropic stream event: %v", err)
	}
	return event
}

func decodeContentBlockStartEvent(t *testing.T, data string) anthropicSDK.ContentBlockStartEvent {
	t.Helper()

	event := decodeStreamEvent(t, data)
	startEvent, ok := event.AsAny().(anthropicSDK.ContentBlockStartEvent)
	if !ok {
		t.Fatalf("event type = %T, want anthropic.ContentBlockStartEvent", event.AsAny())
	}
	return startEvent
}

func decodeContentBlockDeltaEvent(t *testing.T, data string) anthropicSDK.ContentBlockDeltaEvent {
	t.Helper()

	event := decodeStreamEvent(t, data)
	deltaEvent, ok := event.AsAny().(anthropicSDK.ContentBlockDeltaEvent)
	if !ok {
		t.Fatalf("event type = %T, want anthropic.ContentBlockDeltaEvent", event.AsAny())
	}
	return deltaEvent
}

func decodeContentBlockStopEvent(t *testing.T, data string) anthropicSDK.ContentBlockStopEvent {
	t.Helper()

	event := decodeStreamEvent(t, data)
	stopEvent, ok := event.AsAny().(anthropicSDK.ContentBlockStopEvent)
	if !ok {
		t.Fatalf("event type = %T, want anthropic.ContentBlockStopEvent", event.AsAny())
	}
	return stopEvent
}

func decodeMessageDeltaEvent(t *testing.T, data string) anthropicSDK.MessageDeltaEvent {
	t.Helper()

	event := decodeStreamEvent(t, data)
	deltaEvent, ok := event.AsAny().(anthropicSDK.MessageDeltaEvent)
	if !ok {
		t.Fatalf("event type = %T, want anthropic.MessageDeltaEvent", event.AsAny())
	}
	return deltaEvent
}
