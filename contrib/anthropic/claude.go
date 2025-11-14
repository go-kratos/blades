package anthropic

import (
	"context"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/go-kratos/blades"
)

// Option is a functional option for configuring the Claude client.
type Option func(*Options)

// WithThinking sets the thinking configuration.
func WithThinking(thinking *anthropic.ThinkingConfigParamUnion) Option {
	return func(o *Options) {
		o.Thinking = thinking
	}
}

// Options holds configuration for the Claude client.
type Options struct {
	Thinking    *anthropic.ThinkingConfigParamUnion
	RequestOpts []option.RequestOption
}

// claudeModel provides a unified interface for Claude API access.
type claudeModel struct {
	model  string
	opts   Options
	client anthropic.Client
}

// NewModel creates a new Claude client with the given options.
// Accepts official Anthropic SDK RequestOptions for maximum flexibility:
//   - Direct API: option.WithAPIKey("sk-...")
//   - AWS Bedrock: bedrock.WithLoadDefaultConfig(ctx)
//   - Google Vertex: vertex.WithGoogleAuth(ctx, region, projectID)
func NewModel(model string, opts ...Option) blades.ModelProvider {
	opt := Options{}
	for _, apply := range opts {
		apply(&opt)
	}
	return &claudeModel{
		model:  model,
		opts:   opt,
		client: anthropic.NewClient(opt.RequestOpts...),
	}
}

// Name returns the name of the Claude model.
func (m *claudeModel) Name() string {
	return m.model
}

// Generate generates content using the Claude API.
// Returns blades.ModelResponse instead of SDK-specific types.
func (m *claudeModel) Generate(ctx context.Context, req *blades.ModelRequest, opts ...blades.ModelOption) (*blades.ModelResponse, error) {
	opt := blades.ModelOptions{}
	for _, apply := range opts {
		apply(&opt)
	}
	params, err := m.toClaudeParams(req, opt)
	if err != nil {
		return nil, fmt.Errorf("converting request: %w", err)
	}
	message, err := m.client.Messages.New(ctx, *params)
	if err != nil {
		return nil, fmt.Errorf("generating content: %w", err)
	}
	return convertClaudeToBlades(message)
}

// NewStreaming executes the request and returns a stream of assistant responses.
func (m *claudeModel) NewStreaming(ctx context.Context, req *blades.ModelRequest, opts ...blades.ModelOption) blades.Generator[*blades.ModelResponse, error] {
	opt := blades.ModelOptions{}
	for _, apply := range opts {
		apply(&opt)
	}
	return func(yield func(*blades.ModelResponse, error) bool) {
		params, err := m.toClaudeParams(req, opt)
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
		finalResponse, err := convertClaudeToBlades(message)
		if err != nil {
			yield(nil, err)
			return
		}
		yield(finalResponse, nil)
	}
}

// toClaudeParams converts Blades ModelRequest and ModelOptions to Claude MessageNewParams.
func (m *claudeModel) toClaudeParams(req *blades.ModelRequest, opt blades.ModelOptions) (*anthropic.MessageNewParams, error) {
	params := &anthropic.MessageNewParams{
		Model: anthropic.Model(m.model),
	}
	if opt.MaxOutputTokens > 0 {
		params.MaxTokens = int64(opt.MaxOutputTokens)
	}
	if opt.Temperature > 0 {
		params.Temperature = anthropic.Float(opt.Temperature)
	}
	if opt.TopP > 0 {
		params.TopP = anthropic.Float(opt.TopP)
	}
	if m.opts.Thinking != nil {
		params.Thinking = *m.opts.Thinking
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
