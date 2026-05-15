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
