package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/model"
	bladessession "github.com/go-kratos/blades/session"
	"github.com/go-kratos/blades/tools"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/openai/openai-go/v3/responses"
)

func TestToResponseParamsMapsRequest(t *testing.T) {
	t.Parallel()

	maxTokens := 64
	temperature := 0.2
	provider := NewResponses("gpt-test", WithResponsesParallelToolCalls(false)).(*responseModel)
	params, err := provider.toResponseParams(context.Background(), false, &model.Request{
		System: "system",
		Messages: []*model.Message{
			{
				Role: model.RoleUser,
				Parts: []content.Part{
					content.Text{Text: "hello"},
					content.DataPart{Bytes: []byte{1, 2, 3}, MIME: "image/png"},
				},
			},
			{
				Role: model.RoleAssistant,
				Parts: []content.Part{
					content.Text{Text: "previous answer"},
					content.ToolUse{ID: "call_1", Name: "lookup", Input: json.RawMessage(`{"q":"blades"}`)},
				},
			},
			{
				Role: model.RoleTool,
				Parts: []content.Part{
					content.ToolResult{ID: "call_1", Name: "lookup", Parts: []content.Part{content.Text{Text: "found"}}},
				},
			},
		},
		Tools: []tools.ToolSpec{
			{
				Name:        "lookup",
				Description: "Search docs",
				InputSchema: &jsonschema.Schema{
					Title:       "lookup_input",
					Description: "Lookup input",
					Type:        "object",
				},
			},
		},
		Options: []model.Option{
			model.Sampling{Temperature: &temperature, MaxTokens: &maxTokens},
			model.ReasoningEffort{Level: "low"},
			model.ResponseFormat{
				Schema: &jsonschema.Schema{
					Title:       "answer",
					Description: "Answer schema",
					Type:        "object",
				},
				Strict: true,
			},
		},
	})
	if err != nil {
		t.Fatalf("toResponseParams returned error: %v", err)
	}

	payload, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal response params: %v", err)
	}
	for _, want := range [][]byte{
		[]byte(`"model":"gpt-test"`),
		[]byte(`"instructions":"system"`),
		[]byte(`"type":"input_text"`),
		[]byte(`"type":"input_image"`),
		[]byte(`"role":"assistant"`),
		[]byte(`"type":"function_call"`),
		[]byte(`"type":"function_call_output"`),
		[]byte(`"call_id":"call_1"`),
		[]byte(`"parallel_tool_calls":false`),
		[]byte(`"max_output_tokens":64`),
		[]byte(`"temperature":0.2`),
		[]byte(`"effort":"low"`),
		[]byte(`"type":"json_schema"`),
		[]byte(`"name":"answer"`),
		[]byte(`"strict":true`),
		[]byte(`"description":"Search docs"`),
	} {
		if !bytes.Contains(payload, want) {
			t.Fatalf("payload missing %s: %s", want, payload)
		}
	}
}

func TestToResponseParamsUsesPreviousResponseIDFromSessionState(t *testing.T) {
	t.Parallel()

	sess := bladessession.NewSession(bladessession.WithState(map[string]any{
		PreviousResponseIDStateKey: "resp_from_state",
	}))
	ctx := bladessession.NewContext(context.Background(), sess)
	provider := NewResponses("gpt-test", WithResponsesPreviousResponseID("resp_from_config")).(*responseModel)

	params, err := provider.toResponseParams(ctx, false, &model.Request{
		Messages: []*model.Message{
			{Role: model.RoleUser, Parts: []content.Part{content.Text{Text: "hello"}}},
		},
	})
	if err != nil {
		t.Fatalf("toResponseParams returned error: %v", err)
	}
	if got := params.PreviousResponseID.Value; got != "resp_from_state" {
		t.Fatalf("previous response id = %q, want resp_from_state", got)
	}
}

func TestResponseToModelResponseReturnsTextAndToolUses(t *testing.T) {
	t.Parallel()

	resp, err := responseToModelResponse(&responses.Response{
		ID:     "resp_non_stream",
		Status: responses.ResponseStatusCompleted,
		Usage:  responses.ResponseUsage{InputTokens: 3, OutputTokens: 4},
		Output: []responses.ResponseOutputItemUnion{
			{
				Type: "message",
				Content: []responses.ResponseOutputMessageContentUnion{
					{Type: "output_text", Text: "hello"},
				},
			},
			{
				Type:      "function_call",
				CallID:    "call_1",
				Name:      "lookup",
				Arguments: `{"q":"blades"}`,
			},
		},
	})
	if err != nil {
		t.Fatalf("responseToModelResponse returned error: %v", err)
	}
	if got, want := resp.StopReason, model.StopToolUse; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	if got, want := resp.ResponseID, "resp_non_stream"; got != want {
		t.Fatalf("response id = %q, want %q", got, want)
	}
	if got, want := resp.Usage.InputTokens, int64(3); got != want {
		t.Fatalf("input tokens = %d, want %d", got, want)
	}
	if got, want := resp.Usage.OutputTokens, int64(4); got != want {
		t.Fatalf("output tokens = %d, want %d", got, want)
	}
	text, ok := resp.Message.Parts[0].(content.Text)
	if !ok {
		t.Fatalf("part type = %T, want content.Text", resp.Message.Parts[0])
	}
	if got, want := text.Text, "hello"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	toolUse, ok := resp.Message.Parts[1].(content.ToolUse)
	if !ok {
		t.Fatalf("part type = %T, want content.ToolUse", resp.Message.Parts[1])
	}
	if got, want := toolUse.ID, "call_1"; got != want {
		t.Fatalf("tool id = %q, want %q", got, want)
	}
	if got, want := string(toolUse.Input), `{"q":"blades"}`; got != want {
		t.Fatalf("tool input = %q, want %q", got, want)
	}
}

func TestResponseToModelResponseReturnsReasoningAsThinking(t *testing.T) {
	t.Parallel()

	resp, err := responseToModelResponse(&responses.Response{
		ID:     "resp_reasoning",
		Status: responses.ResponseStatusCompleted,
		Output: []responses.ResponseOutputItemUnion{
			{
				Type: "reasoning",
				Content: []responses.ResponseOutputMessageContentUnion{
					{Type: "reasoning_text", Text: "先看本轮目标"},
				},
				Summary: []responses.ResponseReasoningItemSummary{
					{Text: "目标是自然推进群聊"},
				},
			},
			{
				Type: "message",
				Content: []responses.ResponseOutputMessageContentUnion{
					{Type: "output_text", Text: "hello"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("responseToModelResponse returned error: %v", err)
	}
	if len(resp.Message.Parts) != 3 {
		t.Fatalf("parts = %#v, want reasoning, summary, text", resp.Message.Parts)
	}
	thinking, ok := resp.Message.Parts[0].(content.Thinking)
	if !ok {
		t.Fatalf("part type = %T, want content.Thinking", resp.Message.Parts[0])
	}
	if got, want := thinking.Text, "先看本轮目标"; got != want {
		t.Fatalf("thinking text = %q, want %q", got, want)
	}
	summary, ok := resp.Message.Parts[1].(content.Thinking)
	if !ok {
		t.Fatalf("summary part type = %T, want content.Thinking", resp.Message.Parts[1])
	}
	if got, want := summary.Text, "目标是自然推进群聊"; got != want {
		t.Fatalf("summary thinking text = %q, want %q", got, want)
	}
}

func TestResponseToModelResponseMapsIncompleteReasons(t *testing.T) {
	t.Parallel()

	resp, err := responseToModelResponse(&responses.Response{
		Status:            responses.ResponseStatusIncomplete,
		IncompleteDetails: responses.ResponseIncompleteDetails{Reason: "max_output_tokens"},
	})
	if err != nil {
		t.Fatalf("responseToModelResponse returned error: %v", err)
	}
	if got, want := resp.StopReason, model.StopMaxTokens; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
}

func TestResponseToModelResponseReturnsFailedStatusError(t *testing.T) {
	t.Parallel()

	_, err := responseToModelResponse(&responses.Response{
		Status: responses.ResponseStatusFailed,
		Error:  responses.ResponseError{Message: "model failed"},
	})
	if err == nil {
		t.Fatal("responseToModelResponse returned nil error")
	}
	if !strings.Contains(err.Error(), "model failed") {
		t.Fatalf("error = %v, want model failure message", err)
	}
}

func TestResponseStreamEventToChunk(t *testing.T) {
	t.Parallel()

	seen := make(map[string]struct{})
	chunk, err := responseStreamEventToChunk(responses.ResponseStreamEventUnion{
		Type:  "response.output_text.delta",
		Delta: "hi",
	}, seen)
	if err != nil {
		t.Fatalf("responseStreamEventToChunk text delta returned error: %v", err)
	}
	text, ok := chunk.Parts[0].(content.Text)
	if !ok {
		t.Fatalf("part type = %T, want content.Text", chunk.Parts[0])
	}
	if got, want := text.Text, "hi"; got != want {
		t.Fatalf("text delta = %q, want %q", got, want)
	}

	chunk, err = responseStreamEventToChunk(responses.ResponseStreamEventUnion{
		Type:  "response.reasoning_text.delta",
		Delta: "先判断",
	}, seen)
	if err != nil {
		t.Fatalf("responseStreamEventToChunk reasoning delta returned error: %v", err)
	}
	thinking, ok := chunk.Parts[0].(content.Thinking)
	if !ok {
		t.Fatalf("part type = %T, want content.Thinking", chunk.Parts[0])
	}
	if got, want := thinking.Text, "先判断"; got != want {
		t.Fatalf("thinking delta = %q, want %q", got, want)
	}

	chunk, err = responseStreamEventToChunk(responses.ResponseStreamEventUnion{
		Type:  "response.reasoning_summary_text.delta",
		Delta: "目标检查",
	}, seen)
	if err != nil {
		t.Fatalf("responseStreamEventToChunk reasoning summary delta returned error: %v", err)
	}
	thinking, ok = chunk.Parts[0].(content.Thinking)
	if !ok {
		t.Fatalf("summary part type = %T, want content.Thinking", chunk.Parts[0])
	}
	if got, want := thinking.Text, "目标检查"; got != want {
		t.Fatalf("summary thinking delta = %q, want %q", got, want)
	}

	chunk, err = responseStreamEventToChunk(responses.ResponseStreamEventUnion{
		Type:      "response.function_call_arguments.done",
		ItemID:    "item_1",
		Name:      "lookup",
		Arguments: `{"q":"blades"}`,
	}, seen)
	if err != nil {
		t.Fatalf("responseStreamEventToChunk arguments done returned error: %v", err)
	}
	if chunk != nil {
		t.Fatalf("arguments done chunk = %#v, want nil until call_id is available", chunk)
	}

	chunk, err = responseStreamEventToChunk(responses.ResponseStreamEventUnion{
		Type: "response.output_item.done",
		Item: responses.ResponseOutputItemUnion{
			ID:        "item_1",
			Type:      "function_call",
			CallID:    "call_1",
			Name:      "lookup",
			Arguments: `{"q":"blades"}`,
		},
	}, seen)
	if err != nil {
		t.Fatalf("responseStreamEventToChunk tool item returned error: %v", err)
	}
	if got, want := chunk.StopReason, model.StopToolUse; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
	toolUse, ok := chunk.Parts[0].(content.ToolUse)
	if !ok {
		t.Fatalf("part type = %T, want content.ToolUse", chunk.Parts[0])
	}
	if got, want := toolUse.ID, "call_1"; got != want {
		t.Fatalf("tool id = %q, want %q", got, want)
	}

	chunk, err = responseStreamEventToChunk(responses.ResponseStreamEventUnion{
		Type: "response.output_item.done",
		Item: responses.ResponseOutputItemUnion{
			ID:        "item_1",
			Type:      "function_call",
			CallID:    "call_1",
			Name:      "lookup",
			Arguments: `{"q":"blades"}`,
		},
	}, seen)
	if err != nil {
		t.Fatalf("responseStreamEventToChunk duplicate tool item returned error: %v", err)
	}
	if chunk != nil {
		t.Fatalf("duplicate tool chunk = %#v, want nil", chunk)
	}

	chunk, err = responseStreamEventToChunk(responses.ResponseStreamEventUnion{
		Type: "response.completed",
		Response: responses.Response{
			ID:     "resp_stream",
			Status: responses.ResponseStatusCompleted,
			Usage:  responses.ResponseUsage{InputTokens: 5, OutputTokens: 6},
		},
	}, seen)
	if err != nil {
		t.Fatalf("responseStreamEventToChunk completed returned error: %v", err)
	}
	if got, want := chunk.StopReason, model.StopToolUse; got != want {
		t.Fatalf("completed stop reason = %q, want %q", got, want)
	}
	if got, want := chunk.ResponseID, "resp_stream"; got != want {
		t.Fatalf("completed response id = %q, want %q", got, want)
	}
	if chunk.Usage == nil {
		t.Fatal("usage is nil, want token usage")
	}
	if got, want := chunk.Usage.InputTokens, int64(5); got != want {
		t.Fatalf("input tokens = %d, want %d", got, want)
	}
	if got, want := chunk.Usage.OutputTokens, int64(6); got != want {
		t.Fatalf("output tokens = %d, want %d", got, want)
	}
}

func TestResponseGenerateRejectsNilRequest(t *testing.T) {
	t.Parallel()

	provider := &responseModel{model: "gpt-test"}
	_, err := provider.Generate(context.Background(), nil)
	if !errors.Is(err, ErrResponseRequestNil) {
		t.Fatalf("Generate error = %v, want %v", err, ErrResponseRequestNil)
	}
}
