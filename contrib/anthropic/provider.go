package anthropic

import (
	"context"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/go-kratos/blades"
)

var _ blades.ModelProvider = (*Provider)(nil)

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

// Provider provides a unified interface for Claude API access.
type Provider struct {
	opts   Options
	client anthropic.Client
}

// NewProvider creates a new Claude client with the given options
// Accepts official Anthropic SDK RequestOptions for maximum flexibility:
//   - Direct API: option.WithAPIKey("sk-...")
//   - AWS Bedrock: bedrock.WithLoadDefaultConfig(ctx)
//   - Google Vertex: vertex.WithGoogleAuth(ctx, region, projectID)
func NewProvider(opts ...Option) *Provider {
	opt := Options{}
	for _, apply := range opts {
		apply(&opt)
	}
	return &Provider{
		opts:   opt,
		client: anthropic.NewClient(opt.RequestOpts...),
	}
}

// Generate generates content using the Claude API
// Returns blades.ModelResponse instead of SDK-specific types
func (c *Provider) Generate(ctx context.Context, req *blades.ModelRequest, opts ...blades.ModelOption) (*blades.ModelResponse, error) {
	opt := blades.ModelOptions{}
	for _, apply := range opts {
		apply(&opt)
	}
	params, err := c.toClaudeParams(req, opt)
	if err != nil {
		return nil, fmt.Errorf("converting request: %w", err)
	}
	message, err := c.client.Messages.New(ctx, *params)
	if err != nil {
		return nil, fmt.Errorf("generating content: %w", err)
	}
	return convertClaudeToBlades(message)
}

// NewStreaming executes the request and returns a stream of assistant responses
func (c *Provider) NewStreaming(ctx context.Context, req *blades.ModelRequest, opts ...blades.ModelOption) blades.Generator[*blades.ModelResponse, error] {
	opt := blades.ModelOptions{}
	for _, apply := range opts {
		apply(&opt)
	}
	return func(yield func(*blades.ModelResponse, error) bool) {
		params, err := c.toClaudeParams(req, opt)
		if err != nil {
			yield(nil, err)
			return
		}
		streaming := c.client.Messages.NewStreaming(ctx, *params)
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
func (c *Provider) toClaudeParams(req *blades.ModelRequest, opt blades.ModelOptions) (*anthropic.MessageNewParams, error) {
	params := &anthropic.MessageNewParams{
		Model: anthropic.Model(req.Model),
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
	if c.opts.Thinking != nil {
		params.Thinking = *c.opts.Thinking
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
