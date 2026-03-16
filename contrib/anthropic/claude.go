package anthropic

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/go-kratos/blades"
)

// maxCacheBreakpoints is the maximum number of ephemeral cache_control tags
// the Anthropic API accepts per request.
const maxCacheBreakpoints = 4

// Config holds configuration options for the Claude client.
type Config struct {
	BaseURL         string
	APIKey          string
	MaxOutputTokens int64
	Seed            int64
	TopK            int64
	TopP            float64
	Temperature     float64
	StopSequences   []string
	RequestOptions  []option.RequestOption
	Thinking        *anthropic.ThinkingConfigParamUnion
	// CacheControl enables prompt caching. When true, an ephemeral
	// cache_control breakpoint is added to the last content block of the last
	// message on every request. A sliding window of maxCacheBreakpoints tags
	// is maintained across the message list: if the limit is already reached,
	// the earliest tag is removed first. Disabled by default.
	CacheControl bool
}

// Claude provides a unified interface for Claude API access.
type Claude struct {
	model  string
	config Config
	client anthropic.Client
}

// NewModel creates a new Claude model provider with the given model name and configuration.
func NewModel(model string, config Config) blades.ModelProvider {
	// Apply BaseURL and APIKey if provided
	opts := config.RequestOptions
	if config.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(config.BaseURL))
	}
	if config.APIKey != "" {
		opts = append(opts, option.WithAPIKey(config.APIKey))
	}
	return &Claude{
		model:  model,
		config: config,
		client: anthropic.NewClient(opts...),
	}
}

// Name returns the name of the Claude model.
func (m *Claude) Name() string {
	return m.model
}

// Generate generates content using the Claude API.
// Returns blades.ModelResponse instead of SDK-specific types.
func (m *Claude) Generate(ctx context.Context, req *blades.ModelRequest) (*blades.ModelResponse, error) {
	params, err := m.toClaudeParams(req)
	if err != nil {
		return nil, fmt.Errorf("converting request: %w", err)
	}
	message, err := m.client.Messages.New(ctx, *params)
	if err != nil {
		return nil, fmt.Errorf("generating content: %w", err)
	}
	return convertClaudeToBlades(message, blades.StatusCompleted)
}

// NewStreaming executes the request and returns a stream of assistant responses.
func (m *Claude) NewStreaming(ctx context.Context, req *blades.ModelRequest) blades.Generator[*blades.ModelResponse, error] {
	return func(yield func(*blades.ModelResponse, error) bool) {
		params, err := m.toClaudeParams(req)
		if err != nil {
			yield(nil, err)
			return
		}
		streaming := m.client.Messages.NewStreaming(ctx, *params)
		defer streaming.Close()
		message := &anthropic.Message{}
		for streaming.Next() {
			event := streaming.Current()
			if err := message.Accumulate(event); err != nil {
				yield(nil, err)
				return
			}
			if ev, ok := event.AsAny().(anthropic.ContentBlockDeltaEvent); ok {
				response, err := convertStreamDeltaToBlades(ev)
				if err != nil {
					yield(nil, err)
					return
				}
				if !yield(response, nil) {
					return
				}
			}
		}
		if err := streaming.Err(); err != nil {
			yield(nil, err)
			return
		}
		finalResponse, err := convertClaudeToBlades(message, blades.StatusCompleted)
		if err != nil {
			yield(nil, err)
			return
		}
		yield(finalResponse, nil)
	}
}

// toClaudeParams converts Blades ModelRequest and ModelOptions to Claude MessageNewParams.
func (m *Claude) toClaudeParams(req *blades.ModelRequest) (*anthropic.MessageNewParams, error) {
	params := &anthropic.MessageNewParams{
		Model: anthropic.Model(m.model),
	}
	if m.config.MaxOutputTokens > 0 {
		params.MaxTokens = m.config.MaxOutputTokens
	}
	if m.config.Temperature > 0 {
		params.Temperature = anthropic.Float(m.config.Temperature)
	}
	if m.config.TopK > 0 {
		params.TopK = anthropic.Int(m.config.TopK)
	}
	if m.config.TopP > 0 {
		params.TopP = anthropic.Float(m.config.TopP)
	}
	if len(m.config.StopSequences) > 0 {
		params.StopSequences = m.config.StopSequences
	}
	if m.config.Thinking != nil {
		params.Thinking = *m.config.Thinking
	}
	if req.Instruction != nil {
		params.System = []anthropic.TextBlockParam{{Text: req.Instruction.Text()}}
	}
	for _, msg := range req.Messages {
		switch msg.Role {
		case blades.RoleSystem:
			params.System = []anthropic.TextBlockParam{{Text: msg.Text()}}
		case blades.RoleUser:
			params.Messages = append(params.Messages, anthropic.NewUserMessage(convertPartsToContent(msg.Parts)...))
		case blades.RoleAssistant:
			params.Messages = append(params.Messages, anthropic.NewAssistantMessage(convertPartsToContent(msg.Parts)...))
		case blades.RoleTool:
			var (
				toolResults      []anthropic.ContentBlockParamUnion
				assistantContent []anthropic.ContentBlockParamUnion
			)
			for _, part := range msg.Parts {
				switch v := any(part).(type) {
				case blades.TextPart:
					assistantContent = append(assistantContent, anthropic.NewTextBlock(v.Text))
				case blades.ToolPart:
					toolResults = append(toolResults, anthropic.NewToolResultBlock(v.ID, v.Response, false))
					assistantContent = append(assistantContent, anthropic.NewToolUseBlock(v.ID, decodeToolRequest(v.Request), v.Name))
				}
			}
			if len(assistantContent) > 0 {
				params.Messages = append(params.Messages, anthropic.NewAssistantMessage(assistantContent...))
			}
			if len(toolResults) > 0 {
				params.Messages = append(params.Messages, anthropic.NewUserMessage(toolResults...))
			}
		}
	}
	if len(req.Tools) > 0 {
		tools, err := convertBladesToolsToClaude(req.Tools)
		if err != nil {
			return params, fmt.Errorf("converting tools: %w", err)
		}
		params.Tools = tools
	}
	if m.config.CacheControl {
		applyCacheControlSliding(params.Messages)
	}
	return params, nil
}

// applyCacheControlSliding stamps the last content block of the last message
// with an ephemeral cache_control breakpoint, maintaining a sliding window of
// at most maxCacheBreakpoints tags across all messages.
//
// If the tag count is already at the limit the earliest tag is removed first
// (only the cache_control field is cleared; the message and content block are
// left intact). The new tag is then placed on the last block regardless of its
// type (text, tool_use, tool_result, …).
func applyCacheControlSliding(messages []anthropic.MessageParam) {
	if len(messages) == 0 {
		return
	}

	// Locate all existing cache_control tags in message order.
	type location struct{ msg, block int }
	var tagged []location
	for i := range messages {
		for j := range messages[i].Content {
			if cc := messages[i].Content[j].GetCacheControl(); cc != nil && cc.Type != "" {
				tagged = append(tagged, location{i, j})
			}
		}
	}

	// Determine the location of the last content block of the last message.
	lastMsgIdx := len(messages) - 1
	if len(messages[lastMsgIdx].Content) == 0 {
		// Nothing to tag on the last message.
		return
	}
	lastBlockIdx := len(messages[lastMsgIdx].Content) - 1
	target := location{msg: lastMsgIdx, block: lastBlockIdx}

	// Check if the target block is already tagged (stamping would be idempotent).
	alreadyTagged := false
	for _, loc := range tagged {
		if loc == target {
			alreadyTagged = true
			break
		}
	}

	// Calculate how many existing tags we are allowed to keep before stamping
	// the target block, and evict the earliest ones (sliding window) if needed.
	requiredNewTags := 1
	if alreadyTagged {
		requiredNewTags = 0
	}
	allowedExisting := maxCacheBreakpoints - requiredNewTags
	if allowedExisting < 0 {
		allowedExisting = 0
	}
	if len(tagged) > allowedExisting {
		neededEvictions := len(tagged) - allowedExisting
		evicted := 0
		for _, loc := range tagged {
			if evicted >= neededEvictions {
				break
			}
			// Never evict the target block if it is already tagged.
			if loc == target {
				continue
			}
			if cc := messages[loc.msg].Content[loc.block].GetCacheControl(); cc != nil {
				*cc = anthropic.CacheControlEphemeralParam{}
				evicted++
			}
		}
	}

	// Stamp the last content block of the last message.
	if cc := messages[lastMsgIdx].Content[lastBlockIdx].GetCacheControl(); cc != nil {
		*cc = anthropic.NewCacheControlEphemeralParam()
	}
}

func decodeToolRequest(request string) any {
	var decoded any
	if err := json.Unmarshal([]byte(request), &decoded); err == nil {
		return decoded
	}
	return request
}
