package anthropic

import (
	"bytes"
	"encoding/json"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/model"
)

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
