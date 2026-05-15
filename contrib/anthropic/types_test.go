package anthropic

import (
	"encoding/json"
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

func decodeAnthropicMessage(t *testing.T, data string) *anthropicSDK.Message {
	t.Helper()

	var message anthropicSDK.Message
	if err := json.Unmarshal([]byte(data), &message); err != nil {
		t.Fatalf("unmarshal anthropic message: %v", err)
	}
	return &message
}
