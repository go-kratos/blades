package anthropic

import (
	"context"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/go-kratos/blades"
)

// Option is a functional option for configuring the Claude client.
type Option func(*options)

// WithSeed sets the seed for the Claude client.
func WithSeed(seed int64) Option {
	return func(o *options) {
		o.Seed = &seed
	}
}

// WithMaxTokens sets the maximum tokens for the Claude client.
func WithMaxTokens(maxTokens int64) Option {
	return func(o *options) {
		o.MaxTokens = maxTokens
	}
}

// WithTopK sets the top-k value for the Claude client.
func WithTopK(topK int64) Option {
	return func(o *options) {
		o.TopK = &topK
	}
}

// WithTopP sets the top-p value for the Claude client.
func WithTopP(topP float64) Option {
	return func(o *options) {
		o.TopP = &topP
	}
}

// WithTemperature sets the temperature for the Claude client.
func WithTemperature(temperature float64) Option {
	return func(o *options) {
		o.Temperature = &temperature
	}
}

// WithStopSequences sets the stop sequences for the Claude client.
func WithStopSequences(stopSequences []string) Option {
	return func(o *options) {
		o.StopSequences = stopSequences
	}
}

// WithThinking sets the thinking configuration.
func WithThinking(config *anthropic.ThinkingConfigParamUnion) Option {
	return func(o *options) {
		o.Thinking = config
	}
}

// WithRequestOption sets the request options for the Anthropic client.
func WithRequestOption(opts ...option.RequestOption) Option {
	return func(o *options) {
		o.RequestOpts = opts
	}
}

// options holds configuration for the Claude client.
type options struct {
	MaxTokens     int64
	Seed          *int64
	TopK          *int64
	TopP          *float64
	Temperature   *float64
	StopSequences []string
	Thinking      *anthropic.ThinkingConfigParamUnion
	RequestOpts   []option.RequestOption
}

// claudeModel provides a unified interface for Claude API access.
type claudeModel struct {
	model  string
	opts   options
	client anthropic.Client
}

// NewModel creates a new Claude client with the given options.
// Accepts official Anthropic SDK RequestOptions for maximum flexibility:
//   - Direct API: option.WithAPIKey("sk-...")
//   - AWS Bedrock: bedrock.WithLoadDefaultConfig(ctx)
//   - Google Vertex: vertex.WithGoogleAuth(ctx, region, projectID)
func NewModel(model string, opts ...Option) blades.ModelProvider {
	opt := options{}
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
func (m *claudeModel) Generate(ctx context.Context, req *blades.ModelRequest) (*blades.ModelResponse, error) {
	params, err := m.toClaudeParams(req)
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
func (m *claudeModel) NewStreaming(ctx context.Context, req *blades.ModelRequest) blades.Generator[*blades.ModelResponse, error] {
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
		finalResponse, err := convertClaudeToBlades(message)
		if err != nil {
			yield(nil, err)
			return
		}
		yield(finalResponse, nil)
	}
}

// toClaudeParams converts Blades ModelRequest and ModelOptions to Claude MessageNewParams.
func (m *claudeModel) toClaudeParams(req *blades.ModelRequest) (*anthropic.MessageNewParams, error) {
	params := &anthropic.MessageNewParams{
		Model: anthropic.Model(m.model),
	}
	if m.opts.MaxTokens > 0 {
		params.MaxTokens = m.opts.MaxTokens
	}
	if m.opts.Temperature != nil {
		params.Temperature = anthropic.Float(*m.opts.Temperature)
	}
	if m.opts.TopK != nil {
		params.TopK = anthropic.Int(*m.opts.TopK)
	}
	if m.opts.TopP != nil {
		params.TopP = anthropic.Float(*m.opts.TopP)
	}
	if len(m.opts.StopSequences) > 0 {
		params.StopSequences = m.opts.StopSequences
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
