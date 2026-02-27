package openai

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/go-kratos/blades"
)

func TestToChatCompletionParamsAssistantRole(t *testing.T) {
	t.Parallel()

	model := &chatModel{model: "gpt-test"}
	req := &blades.ModelRequest{
		Messages: []*blades.Message{
			blades.UserMessage("hello"),
			blades.AssistantMessage("world"),
		},
	}
	params, err := model.toChatCompletionParams(false, req)
	if err != nil {
		t.Fatalf("toChatCompletionParams returned error: %v", err)
	}

	payload, err := json.Marshal(params.Messages)
	if err != nil {
		t.Fatalf("marshal params messages: %v", err)
	}
	if got, want := bytes.Count(payload, []byte(`"role":"assistant"`)), 1; got != want {
		t.Fatalf("assistant role count = %d, want %d; payload=%s", got, want, payload)
	}
	if got, want := bytes.Count(payload, []byte(`"role":"user"`)), 1; got != want {
		t.Fatalf("user role count = %d, want %d; payload=%s", got, want, payload)
	}
}
