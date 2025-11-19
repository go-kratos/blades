package anthropic

import (
	"context"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/go-kratos/blades"
)

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
			params.Messages = append(params.Messages, anthropic.NewUserMessage(convertPartsToContent(msg.Parts)...))
		case blades.RoleTool:
			var content []anthropic.ContentBlockParamUnion
			for _, part := range msg.Parts {
				switch v := any(part).(type) {
				case blades.ToolPart:
					content = append(content, anthropic.NewToolResultBlock(v.ID, v.Response, false))
				}
			}
			params.Messages = append(params.Messages, anthropic.NewUserMessage(content...))
		}
	}
	if len(req.Tools) > 0 {
		tools, err := convertBladesToolsToClaude(req.Tools)
		if err != nil {
			return params, fmt.Errorf("converting tools: %w", err)
		}
		params.Tools = tools
	}
	return params, nil
}
