package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/model"
)

func TestToolInputJSONRepairConfiguration(t *testing.T) {
	t.Parallel()

	defaultProvider := NewModel("claude-test", WithAPIKey("test-key")).(*Claude)
	if defaultProvider.config.ToolInputJSONRepairer == nil {
		t.Fatal("default tool input JSON repairer is nil")
	}

	strictProvider := NewModel(
		"claude-test",
		WithAPIKey("test-key"),
		WithToolInputJSONRepairer(nil),
	).(*Claude)
	if strictProvider.config.ToolInputJSONRepairer != nil {
		t.Fatal("tool input JSON repairer is enabled after WithToolInputJSONRepairer(nil)")
	}
}

func TestStreamCollectsToolUseFromInputJSONDeltas(t *testing.T) {
	t.Parallel()

	var sse strings.Builder
	writeSSEEvent(&sse, "message_start", `{"message":{"content":[],"id":"msg_1","model":"claude-test","role":"assistant","stop_reason":null,"stop_sequence":null,"type":"message","usage":{"input_tokens":1,"output_tokens":0}},"type":"message_start"}`)
	writeSSEEvent(&sse, "content_block_start", `{"content_block":{"id":"toolu_1","input":{},"name":"read","type":"tool_use"},"index":0,"type":"content_block_start"}`)
	writeSSEEvent(&sse, "content_block_delta", `{"delta":{"partial_json":"{\"path\": ","type":"input_json_delta"},"index":0,"type":"content_block_delta"}`)
	writeSSEEvent(&sse, "content_block_delta", `{"delta":{"partial_json":"\"/tmp/file\"}","type":"input_json_delta"},"index":0,"type":"content_block_delta"}`)
	writeSSEEvent(&sse, "content_block_stop", `{"index":0,"type":"content_block_stop"}`)
	writeSSEEvent(&sse, "message_delta", `{"delta":{"stop_reason":"tool_use","stop_sequence":null},"type":"message_delta","usage":{"input_tokens":1,"output_tokens":5}}`)
	writeSSEEvent(&sse, "message_stop", `{"type":"message_stop"}`)

	provider := newTestProvider(t, sse.String())
	var toolUse content.ToolUse
	for chunk, err := range provider.Stream(context.Background(), &model.Request{
		Messages: []*model.Message{{Role: model.RoleUser, Parts: []content.Part{content.Text{Text: "read"}}}},
	}) {
		if err != nil {
			t.Fatalf("Stream returned error: %v", err)
		}
		for _, part := range chunk.Parts {
			if got, ok := part.(content.ToolUse); ok {
				toolUse = got
			}
		}
	}

	if toolUse.ID != "toolu_1" {
		t.Fatalf("tool use id = %q, want toolu_1", toolUse.ID)
	}
	if toolUse.Name != "read" {
		t.Fatalf("tool use name = %q, want read", toolUse.Name)
	}
	var input map[string]string
	if err := json.Unmarshal(toolUse.Input, &input); err != nil {
		t.Fatalf("tool input = %s, unmarshal error = %v", string(toolUse.Input), err)
	}
	if got, want := input["path"], "/tmp/file"; got != want {
		t.Fatalf("tool input path = %q, want %q", got, want)
	}
}

func TestStreamPreservesThinkingSignatureForToolUseReplay(t *testing.T) {
	t.Parallel()

	var sse strings.Builder
	writeSSEEvent(&sse, "message_start", `{"message":{"content":[],"id":"msg_1","model":"claude-test","role":"assistant","stop_reason":null,"stop_sequence":null,"type":"message","usage":{"input_tokens":1,"output_tokens":0}},"type":"message_start"}`)
	writeSSEEvent(&sse, "content_block_start", `{"content_block":{"thinking":"","type":"thinking"},"index":0,"type":"content_block_start"}`)
	writeSSEEvent(&sse, "content_block_delta", `{"delta":{"thinking":"Need ","type":"thinking_delta"},"index":0,"type":"content_block_delta"}`)
	writeSSEEvent(&sse, "content_block_delta", `{"delta":{"thinking":"a tool.","type":"thinking_delta"},"index":0,"type":"content_block_delta"}`)
	writeSSEEvent(&sse, "content_block_delta", `{"delta":{"signature":"opaque-provider-signature","type":"signature_delta"},"index":0,"type":"content_block_delta"}`)
	writeSSEEvent(&sse, "content_block_stop", `{"index":0,"type":"content_block_stop"}`)
	writeSSEEvent(&sse, "content_block_start", `{"content_block":{"id":"toolu_1","input":{},"name":"read","type":"tool_use"},"index":1,"type":"content_block_start"}`)
	writeSSEEvent(&sse, "content_block_delta", `{"delta":{"partial_json":"{\"path\":\"/tmp/file\"}","type":"input_json_delta"},"index":1,"type":"content_block_delta"}`)
	writeSSEEvent(&sse, "content_block_stop", `{"index":1,"type":"content_block_stop"}`)
	writeSSEEvent(&sse, "message_delta", `{"delta":{"stop_reason":"tool_use","stop_sequence":null},"type":"message_delta","usage":{"input_tokens":1,"output_tokens":5}}`)
	writeSSEEvent(&sse, "message_stop", `{"type":"message_stop"}`)

	provider := newTestProvider(t, sse.String())
	var streamedParts []content.Part
	for chunk, err := range provider.Stream(context.Background(), &model.Request{
		Messages: []*model.Message{{Role: model.RoleUser, Parts: []content.Part{content.Text{Text: "read"}}}},
	}) {
		if err != nil {
			t.Fatalf("Stream returned error: %v", err)
		}
		streamedParts = append(streamedParts, chunk.Parts...)
	}

	parts := content.Coalesce(streamedParts)
	if got, want := len(parts), 2; got != want {
		t.Fatalf("coalesced parts len = %d, want %d: %#v", got, want, parts)
	}
	thinking, ok := parts[0].(content.Thinking)
	if !ok {
		t.Fatalf("first part type = %T, want content.Thinking", parts[0])
	}
	if got, want := thinking.Text, "Need a tool."; got != want {
		t.Fatalf("thinking text = %q, want %q", got, want)
	}
	if got, want := string(thinking.Signature), "opaque-provider-signature"; got != want {
		t.Fatalf("thinking signature = %q, want %q", got, want)
	}
	if _, ok := parts[1].(content.ToolUse); !ok {
		t.Fatalf("second part type = %T, want content.ToolUse", parts[1])
	}

	params, err := provider.(*Claude).toClaudeParams(&model.Request{
		Messages: []*model.Message{{Role: model.RoleAssistant, Parts: parts}},
	})
	if err != nil {
		t.Fatalf("toClaudeParams returned error: %v", err)
	}
	payload, err := json.Marshal(params.Messages)
	if err != nil {
		t.Fatalf("marshal replay messages: %v", err)
	}
	if !bytes.Contains(payload, []byte(`"signature":"opaque-provider-signature"`)) {
		t.Fatalf("thinking signature missing from replay payload: %s", payload)
	}
}

func TestStreamRejectsInvalidToolInputJSONWhenRepairDisabled(t *testing.T) {
	t.Parallel()

	var sse strings.Builder
	writeSSEEvent(&sse, "message_start", `{"message":{"content":[],"id":"msg_1","model":"claude-test","role":"assistant","stop_reason":null,"stop_sequence":null,"type":"message","usage":{"input_tokens":1,"output_tokens":0}},"type":"message_start"}`)
	writeSSEEvent(&sse, "content_block_start", `{"content_block":{"id":"toolu_1","input":{},"name":"read","type":"tool_use"},"index":0,"type":"content_block_start"}`)
	writeSSEEvent(&sse, "content_block_delta", `{"delta":{"partial_json":"{\"path\": ","type":"input_json_delta"},"index":0,"type":"content_block_delta"}`)
	writeSSEEvent(&sse, "content_block_stop", `{"index":0,"type":"content_block_stop"}`)
	writeSSEEvent(&sse, "message_delta", `{"delta":{"stop_reason":"tool_use","stop_sequence":null},"type":"message_delta","usage":{"input_tokens":1,"output_tokens":5}}`)
	writeSSEEvent(&sse, "message_stop", `{"type":"message_stop"}`)

	provider := newTestProvider(t, sse.String(), WithToolInputJSONRepairer(nil))
	var streamErr error
	for _, err := range provider.Stream(context.Background(), &model.Request{
		Messages: []*model.Message{{Role: model.RoleUser, Parts: []content.Part{content.Text{Text: "read"}}}},
	}) {
		if err != nil {
			streamErr = err
			break
		}
	}
	if streamErr == nil {
		t.Fatal("Stream error = nil, want invalid tool input JSON error")
	}
	if got := streamErr.Error(); !strings.Contains(got, `invalid tool input JSON for tool "read" (toolu_1)`) ||
		!strings.Contains(got, "unexpected end of JSON input") {
		t.Fatalf("Stream error = %q, want invalid tool input JSON error", got)
	}
	if got := streamErr.Error(); strings.Contains(got, "error converting content block to JSON") ||
		strings.Contains(got, "json.RawMessage") {
		t.Fatalf("Stream error = %q, want Blades error without SDK marshal detail", got)
	}
}

func TestStreamRepairsInvalidToolInputJSONByDefault(t *testing.T) {
	t.Parallel()

	partialJSON := `{"question":{"prompt":"输入"已登录"后继续，或取消。"}}`
	delta, err := json.Marshal(map[string]any{
		"delta": map[string]any{
			"partial_json": partialJSON,
			"type":         "input_json_delta",
		},
		"index": 1,
		"type":  "content_block_delta",
	})
	if err != nil {
		t.Fatal(err)
	}

	var sse strings.Builder
	writeSSEEvent(&sse, "message_start", `{"message":{"content":[],"id":"msg_1","model":"claude-test","role":"assistant","stop_reason":null,"stop_sequence":null,"type":"message","usage":{"input_tokens":1,"output_tokens":0}},"type":"message_start"}`)
	writeSSEEvent(&sse, "content_block_start", `{"content_block":{"id":"toolu_1","input":{},"name":"ask_user_question","type":"tool_use"},"index":1,"type":"content_block_start"}`)
	writeSSEEvent(&sse, "content_block_delta", string(delta))
	writeSSEEvent(&sse, "content_block_stop", `{"index":1,"type":"content_block_stop"}`)
	writeSSEEvent(&sse, "message_delta", `{"delta":{"stop_reason":"tool_use","stop_sequence":null},"type":"message_delta","usage":{"input_tokens":1,"output_tokens":5}}`)
	writeSSEEvent(&sse, "message_stop", `{"type":"message_stop"}`)

	provider := newTestProvider(t, sse.String())
	var toolUse content.ToolUse
	for chunk, streamErr := range provider.Stream(context.Background(), &model.Request{
		Messages: []*model.Message{{Role: model.RoleUser, Parts: []content.Part{content.Text{Text: "ask"}}}},
	}) {
		if streamErr != nil {
			t.Fatalf("Stream returned error: %v", streamErr)
		}
		for _, part := range chunk.Parts {
			if got, ok := part.(content.ToolUse); ok {
				toolUse = got
			}
		}
	}

	var input struct {
		Question struct {
			Prompt string `json:"prompt"`
		} `json:"question"`
	}
	if err := json.Unmarshal(toolUse.Input, &input); err != nil {
		t.Fatalf("tool input = %s, unmarshal error = %v", toolUse.Input, err)
	}
	if got, want := input.Question.Prompt, `输入"已登录"后继续，或取消。`; got != want {
		t.Fatalf("prompt = %q, want %q", got, want)
	}
}

func TestStreamRepairsIncompleteToolInputJSONAtEOF(t *testing.T) {
	t.Parallel()

	partialJSON := `{"command":"printf '保留，全部内容'`
	delta, err := json.Marshal(map[string]any{
		"delta": map[string]any{
			"partial_json": partialJSON,
			"type":         "input_json_delta",
		},
		"index": 0,
		"type":  "content_block_delta",
	})
	if err != nil {
		t.Fatal(err)
	}

	var sse strings.Builder
	writeSSEEvent(&sse, "message_start", `{"message":{"content":[],"id":"msg_1","model":"claude-test","role":"assistant","stop_reason":null,"stop_sequence":null,"type":"message","usage":{"input_tokens":1,"output_tokens":0}},"type":"message_start"}`)
	writeSSEEvent(&sse, "content_block_start", `{"content_block":{"id":"toolu_1","input":{},"name":"bash","type":"tool_use"},"index":0,"type":"content_block_start"}`)
	writeSSEEvent(&sse, "content_block_delta", string(delta))

	provider := newTestProvider(t, sse.String())
	var toolUse content.ToolUse
	var stopReason model.StopReason
	for chunk, streamErr := range provider.Stream(context.Background(), &model.Request{
		Messages: []*model.Message{{Role: model.RoleUser, Parts: []content.Part{content.Text{Text: "run"}}}},
	}) {
		if streamErr != nil {
			t.Fatalf("Stream returned error: %v", streamErr)
		}
		if chunk.StopReason != "" {
			stopReason = chunk.StopReason
		}
		for _, part := range chunk.Parts {
			if got, ok := part.(content.ToolUse); ok {
				toolUse = got
			}
		}
	}

	var input struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(toolUse.Input, &input); err != nil {
		t.Fatalf("tool input = %s, unmarshal error = %v", toolUse.Input, err)
	}
	if got, want := input.Command, `printf '保留，全部内容'`; got != want {
		t.Fatalf("command = %q, want %q", got, want)
	}
	if got, want := stopReason, model.StopToolUse; got != want {
		t.Fatalf("stop reason = %q, want %q", got, want)
	}
}

func TestStreamRejectsInvalidToolInputAtEOFWhenRepairDisabled(t *testing.T) {
	t.Parallel()

	partialJSON := `{"command":"printf 'received content'`
	delta, err := json.Marshal(map[string]any{
		"delta": map[string]any{
			"partial_json": partialJSON,
			"type":         "input_json_delta",
		},
		"index": 0,
		"type":  "content_block_delta",
	})
	if err != nil {
		t.Fatal(err)
	}

	var sse strings.Builder
	writeSSEEvent(&sse, "message_start", `{"message":{"content":[],"id":"msg_1","model":"claude-test","role":"assistant","stop_reason":null,"stop_sequence":null,"type":"message","usage":{"input_tokens":1,"output_tokens":0}},"type":"message_start"}`)
	writeSSEEvent(&sse, "content_block_start", `{"content_block":{"id":"toolu_1","input":{},"name":"bash","type":"tool_use"},"index":0,"type":"content_block_start"}`)
	writeSSEEvent(&sse, "content_block_delta", string(delta))

	provider := newTestProvider(t, sse.String(), WithToolInputJSONRepairer(nil))
	var streamErr error
	for _, err := range provider.Stream(context.Background(), &model.Request{
		Messages: []*model.Message{{Role: model.RoleUser, Parts: []content.Part{content.Text{Text: "run"}}}},
	}) {
		if err != nil {
			streamErr = err
			break
		}
	}
	if streamErr == nil {
		t.Fatal("Stream error = nil, want invalid tool input JSON error")
	}
	if got := streamErr.Error(); !strings.Contains(got, `invalid tool input JSON for tool "bash" (toolu_1)`) ||
		!strings.Contains(got, "unexpected end of JSON input") {
		t.Fatalf("Stream error = %q, want invalid tool input JSON error", got)
	}
}

func TestToClaudeParamsAssistantRole(t *testing.T) {
	t.Parallel()

	provider := &Claude{model: "claude-test"}
	params, err := provider.toClaudeParams(&model.Request{
		Messages: []*model.Message{
			{Role: model.RoleUser, Parts: []content.Part{content.Text{Text: "hello"}}},
			{Role: model.RoleAssistant, Parts: []content.Part{content.Text{Text: "world"}}},
		},
	})
	if err != nil {
		t.Fatalf("toClaudeParams returned error: %v", err)
	}
	if got, want := len(params.Messages), 2; got != want {
		t.Fatalf("messages len = %d, want %d", got, want)
	}
	if got, want := string(params.Messages[0].Role), "user"; got != want {
		t.Fatalf("first role = %q, want %q", got, want)
	}
	if got, want := string(params.Messages[1].Role), "assistant"; got != want {
		t.Fatalf("second role = %q, want %q", got, want)
	}
}

func TestToClaudeParamsImageParts(t *testing.T) {
	t.Parallel()

	provider := &Claude{model: "claude-test"}
	params, err := provider.toClaudeParams(&model.Request{
		Messages: []*model.Message{{
			Role: model.RoleUser,
			Parts: []content.Part{
				content.Text{Text: "describe these images"},
				content.DataPart{Bytes: []byte("inline image"), MIME: "image/png", Filename: "inline.png"},
				content.FilePart{URI: "https://files.example/remote.webp", MIME: "image/webp", Filename: "remote.webp"},
			},
		}},
	})
	if err != nil {
		t.Fatalf("toClaudeParams returned error: %v", err)
	}
	payload, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	for _, want := range [][]byte{
		[]byte(`{"source":{"data":"aW5saW5lIGltYWdl","media_type":"image/png","type":"base64"},"type":"image"}`),
		[]byte(`{"source":{"url":"https://files.example/remote.webp","type":"url"},"type":"image"}`),
	} {
		if !bytes.Contains(payload, want) {
			t.Fatalf("image block %s missing from payload: %s", want, payload)
		}
	}
}

func TestToClaudeParamsParallelToolCalls(t *testing.T) {
	t.Parallel()

	provider := NewModel("claude-test", WithParallelToolCalls(false)).(*Claude)
	params, err := provider.toClaudeParams(&model.Request{})
	if err != nil {
		t.Fatalf("toClaudeParams returned error: %v", err)
	}
	payload, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	if !bytes.Contains(payload, []byte(`"disable_parallel_tool_use":true`)) {
		t.Fatalf("disable_parallel_tool_use missing from payload: %s", payload)
	}
}

func TestToClaudeParamsParallelToolCallsRequestOverridesDefault(t *testing.T) {
	t.Parallel()

	provider := NewModel("claude-test", WithParallelToolCalls(false)).(*Claude)
	params, err := provider.toClaudeParams(&model.Request{
		Options: []model.Option{model.ParallelToolCalls{Enabled: true}},
	})
	if err != nil {
		t.Fatalf("toClaudeParams returned error: %v", err)
	}
	payload, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	if !bytes.Contains(payload, []byte(`"disable_parallel_tool_use":false`)) {
		t.Fatalf("disable_parallel_tool_use override missing from payload: %s", payload)
	}
}

// countCacheControlTags returns the number of content blocks across all
// messages that have a non-zero cache_control stamp.
func countCacheControlTags(messages []anthropic.MessageParam) int {
	n := 0
	for i := range messages {
		for j := range messages[i].Content {
			if cc := messages[i].Content[j].GetCacheControl(); cc != nil && cc.Type != "" {
				n++
			}
		}
	}
	return n
}

func TestCacheControlDisabledByDefault(t *testing.T) {
	t.Parallel()

	provider := &Claude{model: "claude-test"}
	params, err := provider.toClaudeParams(&model.Request{
		Messages: []*model.Message{
			{Role: model.RoleUser, Parts: []content.Part{content.Text{Text: "hello"}}},
		},
	})
	if err != nil {
		t.Fatalf("toClaudeParams returned error: %v", err)
	}
	if got := countCacheControlTags(params.Messages); got != 0 {
		t.Fatalf("cache_control tags = %d, want 0 when CacheControl is disabled", got)
	}
}

func TestCacheControlStampsLastMessageBlock(t *testing.T) {
	t.Parallel()

	provider := &Claude{model: "claude-test", config: Config{CacheControl: true}}
	params, err := provider.toClaudeParams(&model.Request{
		Messages: []*model.Message{
			{Role: model.RoleUser, Parts: []content.Part{content.Text{Text: "turn 1"}}},
			{Role: model.RoleAssistant, Parts: []content.Part{content.Text{Text: "reply 1"}}},
			{Role: model.RoleUser, Parts: []content.Part{content.Text{Text: "turn 2"}}},
		},
	})
	if err != nil {
		t.Fatalf("toClaudeParams returned error: %v", err)
	}

	if got := countCacheControlTags(params.Messages); got != 1 {
		t.Fatalf("cache_control tags = %d, want 1", got)
	}
	last := params.Messages[len(params.Messages)-1]
	cc := last.Content[len(last.Content)-1].GetCacheControl()
	if cc == nil || cc.Type != "ephemeral" {
		t.Fatalf("last message block cache_control = %v, want ephemeral", cc)
	}
}

func TestCacheControlStampsLastSystemBlock(t *testing.T) {
	t.Parallel()

	provider := &Claude{model: "claude-test", config: Config{CacheControl: true}}
	params, err := provider.toClaudeParams(&model.Request{
		System:   "You are helpful.",
		Messages: []*model.Message{{Role: model.RoleUser, Parts: []content.Part{content.Text{Text: "hi"}}}},
	})
	if err != nil {
		t.Fatalf("toClaudeParams returned error: %v", err)
	}

	last := params.System[len(params.System)-1]
	if last.CacheControl.Type != "ephemeral" {
		t.Fatalf("last system block cache_control = %v, want ephemeral", last.CacheControl)
	}
}

func TestCacheControlStampsLastTool(t *testing.T) {
	t.Parallel()

	tool1 := anthropic.ToolParam{Name: "ping", InputSchema: anthropic.ToolInputSchemaParam{}}
	tool2 := anthropic.ToolParam{Name: "pong", InputSchema: anthropic.ToolInputSchemaParam{}}
	params := &anthropic.MessageNewParams{
		Tools: []anthropic.ToolUnionParam{
			{OfTool: &tool1},
			{OfTool: &tool2},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("hi")),
		},
	}
	applyEphemeralCache(params)

	if cc := params.Tools[len(params.Tools)-1].GetCacheControl(); cc == nil || cc.Type != "ephemeral" {
		t.Fatalf("last tool cache_control = %v, want ephemeral", cc)
	}
	if cc := params.Tools[0].GetCacheControl(); cc != nil && cc.Type != "" {
		t.Fatalf("first tool cache_control = %v, want empty", cc)
	}
}

func TestToClaudeParamsToolMessages(t *testing.T) {
	t.Parallel()

	provider := &Claude{model: "claude-test"}
	params, err := provider.toClaudeParams(&model.Request{
		Messages: []*model.Message{
			{
				Role: model.RoleAssistant,
				Parts: []content.Part{
					content.Text{Text: "Let me check that."},
					content.ToolUse{ID: "toolu_123", Name: "get_weather", Input: json.RawMessage(`{"city":"Paris","unit":"C"}`)},
				},
			},
			{
				Role: model.RoleTool,
				Parts: []content.Part{
					content.ToolResult{ID: "toolu_123", Name: "get_weather", Parts: []content.Part{content.Text{Text: `{"temperature":21}`}}},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("toClaudeParams returned error: %v", err)
	}

	payload, err := json.Marshal(params.Messages)
	if err != nil {
		t.Fatalf("marshal params messages: %v", err)
	}
	var messages []map[string]any
	if err := json.Unmarshal(payload, &messages); err != nil {
		t.Fatalf("unmarshal params messages payload: %v", err)
	}

	if got, want := len(messages), 2; got != want {
		t.Fatalf("messages len = %d, want %d", got, want)
	}
	if got, want := messages[0]["role"], "assistant"; got != want {
		t.Fatalf("first role = %v, want %v", got, want)
	}
	if got, want := messages[1]["role"], "user"; got != want {
		t.Fatalf("second role = %v, want %v", got, want)
	}

	assistantContent, ok := messages[0]["content"].([]any)
	if !ok || len(assistantContent) != 2 {
		t.Fatalf("first message content malformed: %v", messages[0]["content"])
	}
	toolUseBlock, ok := assistantContent[1].(map[string]any)
	if !ok {
		t.Fatalf("assistant tool_use block malformed: %v", assistantContent[1])
	}
	if got, want := toolUseBlock["type"], "tool_use"; got != want {
		t.Fatalf("tool_use block type = %v, want %v", got, want)
	}
	if got, want := toolUseBlock["id"], "toolu_123"; got != want {
		t.Fatalf("tool_use id = %v, want %v", got, want)
	}

	userContent, ok := messages[1]["content"].([]any)
	if !ok || len(userContent) != 1 {
		t.Fatalf("second message content malformed: %v", messages[1]["content"])
	}
	toolResultBlock, ok := userContent[0].(map[string]any)
	if !ok {
		t.Fatalf("tool_result block malformed: %v", userContent[0])
	}
	if got, want := toolResultBlock["type"], "tool_result"; got != want {
		t.Fatalf("tool_result block type = %v, want %v", got, want)
	}
	if got, want := toolResultBlock["tool_use_id"], "toolu_123"; got != want {
		t.Fatalf("tool_result tool_use_id = %v, want %v", got, want)
	}
}

func newTestProvider(t *testing.T, sse string, opts ...ModelOption) model.Provider {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/messages" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("content-type", "text/event-stream")
		_, _ = w.Write([]byte(sse))
	}))
	t.Cleanup(server.Close)

	opts = append(opts, WithBaseURL(server.URL), WithAPIKey("test-key"))
	return NewModel("claude-test", opts...)
}

func writeSSEEvent(sb *strings.Builder, event string, data string) {
	sb.WriteString("event: ")
	sb.WriteString(event)
	sb.WriteByte('\n')
	sb.WriteString("data: ")
	sb.WriteString(data)
	sb.WriteString("\n\n")
}
