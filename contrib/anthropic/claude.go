package anthropic

import (
	"context"
	"fmt"
	"iter"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	sdkoption "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/model"
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
	RequestOptions  []sdkoption.RequestOption
	ModelOptions    []model.Option
	Thinking        *anthropic.ThinkingConfigParamUnion
	// CacheControl enables prompt caching. When true, an ephemeral
	// cache_control breakpoint is added to the last content block of the last
	// message, as well as the final system block and the last tool, on every
	// request. Disabled by default.
	CacheControl bool
}

// ModelOption configures a Claude model provider.
type ModelOption func(*Config)

// WithConfig applies a full Config value.
func WithConfig(config Config) ModelOption {
	return func(c *Config) {
		*c = config
	}
}

// WithBaseURL sets a custom API base URL.
func WithBaseURL(baseURL string) ModelOption {
	return func(c *Config) {
		c.BaseURL = baseURL
	}
}

// WithAPIKey sets the API key.
func WithAPIKey(apiKey string) ModelOption {
	return func(c *Config) {
		c.APIKey = apiKey
	}
}

// WithRequestOptions appends SDK request options.
func WithRequestOptions(opts ...sdkoption.RequestOption) ModelOption {
	return func(c *Config) {
		c.RequestOptions = append(c.RequestOptions, opts...)
	}
}

// WithParallelToolCalls configures whether the model may emit multiple tool calls in one response.
func WithParallelToolCalls(enabled bool) ModelOption {
	return func(c *Config) {
		c.ModelOptions = model.MergeOptions(c.ModelOptions, []model.Option{
			model.ParallelToolCalls{Enabled: enabled},
		})
	}
}

// NewModel creates a Claude provider from model options.
func NewModel(modelName string, opts ...ModelOption) model.Provider {
	var config Config
	for _, opt := range opts {
		opt(&config)
	}
	return newModel(modelName, config)
}

// Claude provides a unified interface for Claude API access.
type Claude struct {
	model  string
	config Config
	client anthropic.Client
}

var _ model.Provider = (*Claude)(nil)

func newModel(modelName string, config Config) model.Provider {
	opts := append([]sdkoption.RequestOption(nil), config.RequestOptions...)
	if config.BaseURL != "" {
		opts = append(opts, sdkoption.WithBaseURL(config.BaseURL))
	}
	if config.APIKey != "" {
		opts = append(opts, sdkoption.WithAPIKey(config.APIKey))
	}
	return &Claude{
		model:  modelName,
		config: config,
		client: anthropic.NewClient(opts...),
	}
}

// Name returns the name of the Claude model.
func (m *Claude) Name() string {
	return m.model
}

// Generate generates content using the Claude API.
func (m *Claude) Generate(ctx context.Context, req *model.Request) (*model.Response, error) {
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

// Stream executes the request and returns model chunks.
func (m *Claude) Stream(ctx context.Context, req *model.Request) iter.Seq2[*model.Chunk, error] {
	return func(yield func(*model.Chunk, error) bool) {
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
			switch ev := event.AsAny().(type) {
			case anthropic.ContentBlockDeltaEvent:
				chunk := convertStreamDeltaToChunk(ev)
				if len(chunk.Parts) > 0 && !yield(chunk, nil) {
					return
				}
			case anthropic.MessageDeltaEvent:
				chunk := &model.Chunk{
					StopReason: mapClaudeStopReason(ev.Delta.StopReason),
					Usage: &model.Usage{
						InputTokens:  ev.Usage.InputTokens,
						OutputTokens: ev.Usage.OutputTokens,
					},
				}
				if !yield(chunk, nil) {
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
		toolParts := toolUseParts(finalResponse.Message)
		if len(toolParts) > 0 {
			yield(&model.Chunk{Parts: toolParts, StopReason: finalResponse.StopReason}, nil)
		}
	}
}

// toClaudeParams converts Blades model requests and options to Claude MessageNewParams.
func (m *Claude) toClaudeParams(req *model.Request) (*anthropic.MessageNewParams, error) {
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
	if req != nil && req.System != "" {
		params.System = []anthropic.TextBlockParam{{Text: req.System}}
	}
	if req != nil {
		for _, msg := range req.Messages {
			switch msg.Role {
			case model.RoleUser:
				params.Messages = append(params.Messages, anthropic.NewUserMessage(convertPartsToContent(msg.Parts)...))
			case model.RoleAssistant:
				params.Messages = append(params.Messages, anthropic.NewAssistantMessage(convertPartsToContent(msg.Parts)...))
			case model.RoleTool:
				params.Messages = append(params.Messages, anthropic.NewUserMessage(convertPartsToContent(msg.Parts)...))
			}
		}
		if len(req.Tools) > 0 {
			tools, err := convertBladesToolsToClaude(req.Tools)
			if err != nil {
				return params, fmt.Errorf("converting tools: %w", err)
			}
			params.Tools = tools
		}
		applyModelOptions(params, model.MergeOptions(m.config.ModelOptions, req.Options))
	} else {
		applyModelOptions(params, m.config.ModelOptions)
	}
	if m.config.CacheControl {
		applyEphemeralCache(params)
	}
	return params, nil
}

func applyModelOptions(params *anthropic.MessageNewParams, opts []model.Option) {
	for _, opt := range opts {
		switch o := opt.(type) {
		case model.ParallelToolCalls:
			params.ToolChoice = anthropic.ToolChoiceUnionParam{
				OfAuto: &anthropic.ToolChoiceAutoParam{
					DisableParallelToolUse: anthropic.Bool(!o.Enabled),
				},
			}
		case model.Sampling:
			if o.Temperature != nil {
				params.Temperature = anthropic.Float(*o.Temperature)
			}
			if o.TopP != nil {
				params.TopP = anthropic.Float(*o.TopP)
			}
			if o.MaxTokens != nil {
				params.MaxTokens = int64(*o.MaxTokens)
			}
			if len(o.Stop) > 0 {
				params.StopSequences = o.Stop
			}
		}
	}
}

// applyEphemeralCache stamps an ephemeral cache_control breakpoint on the last
// block of each cacheable section: system, tools, and messages.
func applyEphemeralCache(params *anthropic.MessageNewParams) {
	if len(params.System) > 0 {
		params.System[len(params.System)-1].CacheControl = anthropic.NewCacheControlEphemeralParam()
	}
	if len(params.Tools) > 0 {
		if cc := params.Tools[len(params.Tools)-1].GetCacheControl(); cc != nil {
			*cc = anthropic.NewCacheControlEphemeralParam()
		}
	}
	if len(params.Messages) > 0 {
		last := &params.Messages[len(params.Messages)-1]
		if len(last.Content) > 0 {
			if cc := last.Content[len(last.Content)-1].GetCacheControl(); cc != nil {
				*cc = anthropic.NewCacheControlEphemeralParam()
			}
		}
	}
}

func toolUseParts(msg *model.Message) []content.Part {
	if msg == nil {
		return nil
	}
	var parts []content.Part
	for _, part := range msg.Parts {
		if _, ok := part.(content.ToolUse); ok {
			parts = append(parts, part)
		}
	}
	return parts
}
