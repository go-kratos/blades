package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"iter"
	"strings"

	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/model"
	"github.com/go-kratos/blades/tools"
	openai "github.com/openai/openai-go/v3"
	sdkoption "github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/shared"
)

var ErrChatRequestNil = errors.New("openai/chat: request is nil")

type ChatConfig struct {
	BaseURL          string
	APIKey           string
	Seed             int64
	MaxOutputTokens  int64
	FrequencyPenalty float64
	PresencePenalty  float64
	Temperature      float64
	TopP             float64
	StopSequences    []string
	ExtraFields      map[string]any
	RequestOptions   []sdkoption.RequestOption
	ModelOptions     []model.Option
	ReasoningEffort  shared.ReasoningEffort
}

// ChatOption configures an OpenAI chat model provider.
type ChatOption func(*ChatConfig)

// WithConfig applies a full ChatConfig value.
func WithConfig(config ChatConfig) ChatOption {
	return func(c *ChatConfig) {
		*c = config
	}
}

// WithBaseURL sets a custom API base URL.
func WithBaseURL(baseURL string) ChatOption {
	return func(c *ChatConfig) {
		c.BaseURL = baseURL
	}
}

// WithAPIKey sets the API key.
func WithAPIKey(apiKey string) ChatOption {
	return func(c *ChatConfig) {
		c.APIKey = apiKey
	}
}

// WithRequestOptions appends SDK request options.
func WithRequestOptions(opts ...sdkoption.RequestOption) ChatOption {
	return func(c *ChatConfig) {
		c.RequestOptions = append(c.RequestOptions, opts...)
	}
}

// WithParallelToolCalls configures whether the model may emit multiple tool calls in one response.
func WithParallelToolCalls(enabled bool) ChatOption {
	return func(c *ChatConfig) {
		c.ModelOptions = model.MergeOptions(c.ModelOptions, []model.Option{
			model.ParallelToolCalls{Enabled: enabled},
		})
	}
}

// NewChat constructs an OpenAI chat provider. The API key is read
// from the SDK default environment variables unless WithAPIKey is used.
func NewChat(modelName string, opts ...ChatOption) model.Provider {
	var config ChatConfig
	for _, opt := range opts {
		opt(&config)
	}
	sdkOpts := append([]sdkoption.RequestOption(nil), config.RequestOptions...)
	if config.BaseURL != "" {
		sdkOpts = append(sdkOpts, sdkoption.WithBaseURL(config.BaseURL))
	}
	if config.APIKey != "" {
		sdkOpts = append(sdkOpts, sdkoption.WithAPIKey(config.APIKey))
	}
	return &chatModel{
		model:  modelName,
		config: config,
		client: openai.NewClient(sdkOpts...),
	}
}

// chatModel implements model.Provider for OpenAI-compatible chat models.
type chatModel struct {
	model  string
	config ChatConfig
	client openai.Client
}

var _ model.Provider = (*chatModel)(nil)

// Name returns the model name.
func (m *chatModel) Name() string {
	return m.model
}

// Generate executes a non-streaming chat completion request.
func (m *chatModel) Generate(ctx context.Context, req *model.Request) (*model.Response, error) {
	params, err := m.toChatCompletionParams(false, req)
	if err != nil {
		return nil, err
	}
	chatResponse, err := m.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, err
	}
	return choiceToResponse(chatResponse)
}

// Stream streams chat completion chunks.
func (m *chatModel) Stream(ctx context.Context, req *model.Request) iter.Seq2[*model.Chunk, error] {
	return func(yield func(*model.Chunk, error) bool) {
		params, err := m.toChatCompletionParams(true, req)
		if err != nil {
			yield(nil, err)
			return
		}
		streaming := m.client.Chat.Completions.NewStreaming(ctx, params)
		defer streaming.Close()
		for streaming.Next() {
			chunk := streaming.Current()
			converted := chunkToModelChunk(chunk)
			if len(converted.Parts) == 0 && converted.StopReason == "" && converted.Usage == nil {
				continue
			}
			if !yield(converted, nil) {
				return
			}
		}
		if err := streaming.Err(); err != nil {
			yield(nil, err)
			return
		}
	}
}

// toChatCompletionParams converts a generic model request into OpenAI params.
func (m *chatModel) toChatCompletionParams(isStreaming bool, req *model.Request) (openai.ChatCompletionNewParams, error) {
	if req == nil {
		return openai.ChatCompletionNewParams{}, ErrChatRequestNil
	}
	toolParams, err := toTools(req.Tools)
	if err != nil {
		return openai.ChatCompletionNewParams{}, err
	}
	params := openai.ChatCompletionNewParams{
		Tools:           toolParams,
		Model:           shared.ChatModel(m.model),
		ReasoningEffort: m.config.ReasoningEffort,
		Messages:        make([]openai.ChatCompletionMessageParamUnion, 0, len(req.Messages)+1),
	}
	if m.config.Seed > 0 {
		params.Seed = param.NewOpt(m.config.Seed)
	}
	if m.config.MaxOutputTokens > 0 {
		params.MaxCompletionTokens = param.NewOpt(m.config.MaxOutputTokens)
	}
	if m.config.FrequencyPenalty > 0 {
		params.FrequencyPenalty = param.NewOpt(m.config.FrequencyPenalty)
	}
	if m.config.PresencePenalty > 0 {
		params.PresencePenalty = param.NewOpt(m.config.PresencePenalty)
	}
	if m.config.Temperature > 0 {
		params.Temperature = param.NewOpt(m.config.Temperature)
	}
	if m.config.TopP > 0 {
		params.TopP = param.NewOpt(m.config.TopP)
	}
	if len(m.config.StopSequences) > 0 {
		params.Stop = openai.ChatCompletionNewParamsStopUnion{OfStringArray: m.config.StopSequences}
	}
	if len(m.config.ExtraFields) > 0 {
		params.SetExtraFields(m.config.ExtraFields)
	}
	applyModelOptions(&params, model.MergeOptions(m.config.ModelOptions, req.Options))
	if isStreaming {
		params.StreamOptions = openai.ChatCompletionStreamOptionsParam{
			IncludeUsage: openai.Bool(true),
		}
	}
	if req.System != "" {
		params.Messages = append(params.Messages, openai.SystemMessage([]openai.ChatCompletionContentPartTextParam{{Text: req.System}}))
	}
	for _, msg := range req.Messages {
		params.Messages = append(params.Messages, toMessageParams(msg)...)
	}
	return params, nil
}

func applyModelOptions(params *openai.ChatCompletionNewParams, opts []model.Option) {
	for _, opt := range opts {
		switch o := opt.(type) {
		case model.ParallelToolCalls:
			params.ParallelToolCalls = openai.Bool(o.Enabled)
		case model.ReasoningEffort:
			if o.Level != "" {
				params.ReasoningEffort = shared.ReasoningEffort(o.Level)
			}
		case model.ResponseFormat:
			if o.Schema != nil {
				schemaParam := openai.ResponseFormatJSONSchemaJSONSchemaParam{
					Name:   "structured_outputs",
					Schema: o.Schema,
					Strict: openai.Bool(o.Strict),
				}
				if o.Schema.Title != "" {
					schemaParam.Name = o.Schema.Title
				}
				if o.Schema.Description != "" {
					schemaParam.Description = openai.String(o.Schema.Description)
				}
				params.ResponseFormat = openai.ChatCompletionNewParamsResponseFormatUnion{
					OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{JSONSchema: schemaParam},
				}
			}
		case model.Sampling:
			if o.Temperature != nil {
				params.Temperature = param.NewOpt(*o.Temperature)
			}
			if o.TopP != nil {
				params.TopP = param.NewOpt(*o.TopP)
			}
			if o.MaxTokens != nil {
				params.MaxCompletionTokens = param.NewOpt(int64(*o.MaxTokens))
			}
			if len(o.Stop) > 0 {
				params.Stop = openai.ChatCompletionNewParamsStopUnion{OfStringArray: o.Stop}
			}
		}
	}
}

func toMessageParams(msg *model.Message) []openai.ChatCompletionMessageParamUnion {
	if msg == nil {
		return nil
	}
	switch msg.Role {
	case model.RoleUser:
		return []openai.ChatCompletionMessageParamUnion{openai.UserMessage(toContentParts(msg.Parts))}
	case model.RoleAssistant:
		return []openai.ChatCompletionMessageParamUnion{toAssistantMessage(msg.Parts)}
	case model.RoleTool:
		return toToolMessages(msg.Parts)
	default:
		return []openai.ChatCompletionMessageParamUnion{openai.UserMessage(toContentParts(msg.Parts))}
	}
}

func toAssistantMessage(parts []content.Part) openai.ChatCompletionMessageParamUnion {
	text := textFromParts(parts)
	toolCalls := make([]openai.ChatCompletionMessageToolCallUnionParam, 0)
	for _, part := range parts {
		if toolUse, ok := part.(content.ToolUse); ok {
			toolCalls = append(toolCalls, openai.ChatCompletionMessageToolCallUnionParam{
				OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
					ID: toolUse.ID,
					Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
						Name:      toolUse.Name,
						Arguments: string(toolUse.Input),
					},
				},
			})
		}
	}
	if len(toolCalls) == 0 {
		return openai.AssistantMessage(text)
	}
	msg := openai.ChatCompletionAssistantMessageParam{ToolCalls: toolCalls}
	if text != "" {
		msg.Content = openai.ChatCompletionAssistantMessageParamContentUnion{OfString: openai.String(text)}
	}
	return openai.ChatCompletionMessageParamUnion{OfAssistant: &msg}
}

func toToolMessages(parts []content.Part) []openai.ChatCompletionMessageParamUnion {
	var messages []openai.ChatCompletionMessageParamUnion
	for _, part := range parts {
		result, ok := part.(content.ToolResult)
		if !ok {
			continue
		}
		messages = append(messages, openai.ToolMessage(textFromParts(result.Parts), result.ID))
	}
	return messages
}

func toTools(toolSpecs []tools.ToolSpec) ([]openai.ChatCompletionToolUnionParam, error) {
	if len(toolSpecs) == 0 {
		return nil, nil
	}
	params := make([]openai.ChatCompletionToolUnionParam, 0, len(toolSpecs))
	for _, spec := range toolSpecs {
		fn := shared.FunctionDefinitionParam{Name: spec.Name}
		if spec.Description != "" {
			fn.Description = openai.String(spec.Description)
		}
		if spec.InputSchema != nil {
			b, err := json.Marshal(spec.InputSchema)
			if err != nil {
				return nil, err
			}
			if err := json.Unmarshal(b, &fn.Parameters); err != nil {
				return nil, err
			}
		}
		params = append(params, openai.ChatCompletionFunctionTool(fn))
	}
	return params, nil
}

// toContentParts converts message parts to OpenAI content parts.
func toContentParts(parts []content.Part) []openai.ChatCompletionContentPartUnionParam {
	out := make([]openai.ChatCompletionContentPartUnionParam, 0, len(parts))
	for _, part := range parts {
		switch v := part.(type) {
		case content.Text:
			out = append(out, openai.TextContentPart(v.Text))
		case content.FilePart:
			switch mimeKind(v.MIME) {
			case "image":
				out = append(out, openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{URL: v.URI}))
			case "audio":
				out = append(out, openai.InputAudioContentPart(openai.ChatCompletionContentPartInputAudioInputAudioParam{
					Data:   v.URI,
					Format: mimeFormat(v.MIME),
				}))
			default:
			}
		case content.DataPart:
			switch mimeKind(v.MIME) {
			case "image":
				base64Data := "data:" + v.MIME + ";base64," + base64.StdEncoding.EncodeToString(v.Bytes)
				out = append(out, openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{URL: base64Data}))
			case "audio":
				out = append(out, openai.InputAudioContentPart(openai.ChatCompletionContentPartInputAudioInputAudioParam{
					Data:   base64.StdEncoding.EncodeToString(v.Bytes),
					Format: mimeFormat(v.MIME),
				}))
			default:
				out = append(out, openai.FileContentPart(openai.ChatCompletionContentPartFileFileParam{
					FileData: param.NewOpt(base64.StdEncoding.EncodeToString(v.Bytes)),
					Filename: param.NewOpt(v.Filename),
				}))
			}
		}
	}
	return out
}

// choiceToResponse converts a non-streaming response to a model.Response.
func choiceToResponse(cc *openai.ChatCompletion) (*model.Response, error) {
	resp := &model.Response{
		Message: &model.Message{Role: model.RoleAssistant},
		Usage: model.Usage{
			InputTokens:  cc.Usage.PromptTokens,
			OutputTokens: cc.Usage.CompletionTokens,
		},
	}
	for _, choice := range cc.Choices {
		resp.StopReason = mapOpenAIStopReason(choice.FinishReason)
		if choice.Message.Content != "" {
			resp.Message.Parts = append(resp.Message.Parts, content.Text{Text: choice.Message.Content})
		}
		if choice.Message.Audio.Data != "" {
			bytes, err := base64.StdEncoding.DecodeString(choice.Message.Audio.Data)
			if err != nil {
				return nil, err
			}
			resp.Message.Parts = append(resp.Message.Parts, content.DataPart{Bytes: bytes})
		}
		for _, call := range choice.Message.ToolCalls {
			if call.Function.Name == "" {
				continue
			}
			resp.Message.Parts = append(resp.Message.Parts, content.ToolUse{
				ID:    call.ID,
				Name:  call.Function.Name,
				Input: json.RawMessage(call.Function.Arguments),
			})
		}
	}
	if resp.StopReason == "" {
		resp.StopReason = model.StopEnd
	}
	return resp, nil
}

func chunkToModelChunk(chunk openai.ChatCompletionChunk) *model.Chunk {
	converted := &model.Chunk{}
	if chunk.Usage.PromptTokens != 0 || chunk.Usage.CompletionTokens != 0 {
		converted.Usage = &model.Usage{
			InputTokens:  chunk.Usage.PromptTokens,
			OutputTokens: chunk.Usage.CompletionTokens,
		}
	}
	for _, choice := range chunk.Choices {
		if choice.Delta.Content != "" {
			converted.Parts = append(converted.Parts, content.Text{Text: choice.Delta.Content})
		}
		if choice.FinishReason != "" {
			converted.StopReason = mapOpenAIStopReason(choice.FinishReason)
		}
		for _, call := range choice.Delta.ToolCalls {
			if call.ID == "" && call.Function.Name == "" && call.Function.Arguments == "" {
				continue
			}
			converted.Parts = append(converted.Parts, content.ToolUse{
				ID:    call.ID,
				Name:  call.Function.Name,
				Input: json.RawMessage(call.Function.Arguments),
			})
		}
	}
	return converted
}

func mapOpenAIStopReason(reason string) model.StopReason {
	switch reason {
	case "tool_calls", "function_call":
		return model.StopToolUse
	case "length":
		return model.StopMaxTokens
	case "content_filter":
		return model.StopSafety
	default:
		return model.StopEnd
	}
}

func textFromParts(parts []content.Part) string {
	var b strings.Builder
	for _, part := range parts {
		switch v := part.(type) {
		case content.Text:
			b.WriteString(v.Text)
		}
	}
	return b.String()
}

func mimeKind(mime string) string {
	if i := strings.IndexByte(mime, '/'); i > 0 {
		return mime[:i]
	}
	return mime
}

func mimeFormat(mime string) string {
	if i := strings.IndexByte(mime, '/'); i >= 0 && i+1 < len(mime) {
		return mime[i+1:]
	}
	return mime
}
