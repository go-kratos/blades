package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/stream"
	"github.com/go-kratos/blades/tools"
	"github.com/openai/openai-go/v2"
	"github.com/openai/openai-go/v2/option"
	"github.com/openai/openai-go/v2/packages/param"
	"github.com/openai/openai-go/v2/shared"
)

var (
	// ErrEmptyResponse indicates the provider returned no choices.
	ErrEmptyResponse = errors.New("empty completion response")
)

// ChatOption defines options for chat providers.
type ChatOption func(*ChatOptions)

// WithReasoningEffort sets the reasoning effort for chat completions.
func WithReasoningEffort(effort shared.ReasoningEffort) ChatOption {
	return func(o *ChatOptions) {
		o.ReasoningEffort = effort
	}
}

// WithChatOptions sets request options for chat completions.
func WithChatOptions(opts ...option.RequestOption) ChatOption {
	return func(o *ChatOptions) {
		o.RequestOpts = opts
	}
}

type ChatOptions struct {
	ReasoningEffort shared.ReasoningEffort
	RequestOpts     []option.RequestOption
}

// ChatProvider implements blades.ModelProvider for OpenAI-compatible chat models.
type ChatProvider struct {
	opts   ChatOptions
	client openai.Client
}

// NewChatProvider constructs an OpenAI provider. The API key is read from
// the OPENAI_API_KEY environment variable. If OPENAI_BASE_URL is set,
// it is used as the API base URL; otherwise the library default is used.
func NewChatProvider(opts ...ChatOption) blades.ModelProvider {
	chatOpts := ChatOptions{}
	for _, opt := range opts {
		opt(&chatOpts)
	}
	return &ChatProvider{
		opts:   chatOpts,
		client: openai.NewClient(chatOpts.RequestOpts...),
	}
}

// Generate executes a non-streaming chat completion request.
func (p *ChatProvider) Generate(ctx context.Context, req *blades.ModelRequest, opts ...blades.ModelOption) (*blades.ModelResponse, error) {
	opt := blades.ModelOptions{}
	for _, apply := range opts {
		apply(&opt)
	}
	params, err := p.toChatCompletionParams(req, opt)
	if err != nil {
		return nil, err
	}
	chatResponse, err := p.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, err
	}
	res, err := choiceToResponse(ctx, params, chatResponse)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// NewStream streams chat completion chunks and converts each choice delta
// into a ModelResponse for incremental consumption.
func (p *ChatProvider) NewStream(ctx context.Context, req *blades.ModelRequest, opts ...blades.ModelOption) (stream.Streamable[*blades.ModelResponse], error) {
	opt := blades.ModelOptions{}
	for _, apply := range opts {
		apply(&opt)
	}
	params, err := p.toChatCompletionParams(req, opt)
	if err != nil {
		return nil, err
	}
	return stream.Go(func(yield func(*blades.ModelResponse, error) bool) {
		streaming := p.client.Chat.Completions.NewStreaming(ctx, params)
		defer streaming.Close()
		acc := openai.ChatCompletionAccumulator{}
		for streaming.Next() {
			chunk := streaming.Current()
			acc.AddChunk(chunk)
			message, err := chunkChoiceToResponse(ctx, chunk.Choices)
			if err != nil {
				yield(nil, err)
				return
			}
			if !yield(message, nil) {
				return
			}
		}
		if err := streaming.Err(); err != nil {
			yield(nil, err)
			return
		}
		finalResponse, err := choiceToResponse(ctx, params, &acc.ChatCompletion)
		if err != nil {
			yield(nil, err)
			return
		}
		yield(finalResponse, nil)
	}), nil
}

// toChatCompletionParams converts a generic model request into OpenAI params.
func (p *ChatProvider) toChatCompletionParams(req *blades.ModelRequest, opt blades.ModelOptions) (openai.ChatCompletionNewParams, error) {
	tools, err := toTools(req.Tools)
	if err != nil {
		return openai.ChatCompletionNewParams{}, err
	}
	params := openai.ChatCompletionNewParams{
		Tools:           tools,
		Model:           req.Model,
		ReasoningEffort: p.opts.ReasoningEffort,
		Messages:        make([]openai.ChatCompletionMessageParamUnion, 0, len(req.Messages)),
	}
	if opt.Seed > 0 {
		params.Seed = param.NewOpt(opt.Seed)
	}
	if opt.MaxOutputTokens > 0 {
		params.MaxCompletionTokens = param.NewOpt(opt.MaxOutputTokens)
	}
	if opt.FrequencyPenalty > 0 {
		params.FrequencyPenalty = param.NewOpt(opt.FrequencyPenalty)
	}
	if opt.PresencePenalty > 0 {
		params.PresencePenalty = param.NewOpt(opt.PresencePenalty)
	}
	if opt.Temperature > 0 {
		params.Temperature = param.NewOpt(opt.Temperature)
	}
	if opt.TopP > 0 {
		params.TopP = param.NewOpt(opt.TopP)
	}
	if len(opt.StopSequences) > 0 {
		params.Stop = openai.ChatCompletionNewParamsStopUnion{OfStringArray: opt.StopSequences}
	}
	if req.OutputSchema != nil {
		schemaParam := openai.ResponseFormatJSONSchemaJSONSchemaParam{
			Name:   "structured_outputs",
			Schema: req.OutputSchema,
			Strict: openai.Bool(true),
		}
		if req.OutputSchema.Title != "" {
			schemaParam.Name = req.OutputSchema.Title
		}
		if req.OutputSchema.Description != "" {
			schemaParam.Description = openai.String(req.OutputSchema.Description)
		}
		params.ResponseFormat = openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{JSONSchema: schemaParam},
		}
	}
	for _, msg := range req.Messages {
		switch msg.Role {
		case blades.RoleUser:
			params.Messages = append(params.Messages, openai.UserMessage(toContentParts(msg)))
		case blades.RoleAssistant:
			params.Messages = append(params.Messages, openai.UserMessage(toContentParts(msg)))
		case blades.RoleSystem:
			params.Messages = append(params.Messages, openai.SystemMessage(toTextParts(msg)))
		case blades.RoleTool:
			params.Messages = append(params.Messages, toToolCallMessage(msg))
			// Also include any tool responses in the messages.
			for _, part := range msg.Parts {
				switch v := any(part).(type) {
				case blades.ToolPart:
					params.Messages = append(params.Messages, openai.ToolMessage(v.Response, v.ID))
				}
			}
		}
	}
	return params, nil
}

func toToolCallMessage(msg *blades.Message) openai.ChatCompletionMessageParamUnion {
	toolCalls := make([]openai.ChatCompletionMessageToolCallUnionParam, 0, len(msg.Parts))
	for _, part := range msg.Parts {
		switch v := any(part).(type) {
		case blades.ToolPart:
			toolCalls = append(toolCalls, openai.ChatCompletionMessageToolCallUnionParam{
				OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
					ID: v.ID,
					Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
						Name:      v.Name,
						Arguments: v.Request,
					},
				},
			})
		}
	}
	return openai.ChatCompletionMessageParamUnion{
		OfAssistant: &openai.ChatCompletionAssistantMessageParam{
			ToolCalls: toolCalls,
		},
	}
}

func toTools(tools []*tools.Tool) ([]openai.ChatCompletionToolUnionParam, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	params := make([]openai.ChatCompletionToolUnionParam, 0, len(tools))
	for _, tool := range tools {
		fn := openai.FunctionDefinitionParam{
			Name: tool.Name,
		}
		if tool.Description != "" {
			fn.Description = openai.String(tool.Description)
		}
		if tool.InputSchema != nil {
			b, err := json.Marshal(tool.InputSchema)
			if err != nil {
				return nil, err
			}
			if err := json.Unmarshal(b, &fn.Parameters); err != nil {
				return nil, err
			}
		}
		unionParam := openai.ChatCompletionToolUnionParam{
			OfFunction: &openai.ChatCompletionFunctionToolParam{
				Function: fn,
			},
		}
		params = append(params, unionParam)
	}
	return params, nil
}

// toTextParts converts message parts to text-only parts (system/assistant messages).
func toTextParts(message *blades.Message) []openai.ChatCompletionContentPartTextParam {
	parts := make([]openai.ChatCompletionContentPartTextParam, 0, len(message.Parts))
	for _, part := range message.Parts {
		switch v := part.(type) {
		case blades.TextPart:
			parts = append(parts, openai.ChatCompletionContentPartTextParam{Text: v.Text})
		}
	}
	return parts
}

// toContentParts converts message parts to OpenAI content parts (multi-modal user input).
func toContentParts(message *blades.Message) []openai.ChatCompletionContentPartUnionParam {
	parts := make([]openai.ChatCompletionContentPartUnionParam, 0, len(message.Parts))
	for _, part := range message.Parts {
		switch v := part.(type) {
		case blades.TextPart:
			parts = append(parts, openai.TextContentPart(v.Text))
		case blades.FilePart:
			// Handle different content types based on MIME type
			switch v.MIMEType.Type() {
			case "image":
				parts = append(parts, openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{
					URL: v.URI,
				}))
			case "audio":
				parts = append(parts, openai.InputAudioContentPart(openai.ChatCompletionContentPartInputAudioInputAudioParam{
					Data:   v.URI,
					Format: v.MIMEType.Format(),
				}))
			default:
				log.Println("failed to process file part with MIME type:", v.MIMEType)
			}
		case blades.DataPart:
			// Handle different content types based on MIME type
			switch v.MIMEType.Type() {
			case "image":
				mimeType := string(v.MIMEType)
				base64Data := "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(v.Bytes)
				parts = append(parts, openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{
					URL: base64Data,
				}))
			case "audio":
				parts = append(parts, openai.InputAudioContentPart(openai.ChatCompletionContentPartInputAudioInputAudioParam{
					Data:   "data:;base64," + base64.StdEncoding.EncodeToString(v.Bytes),
					Format: v.MIMEType.Format(),
				}))
			default:
				fileParam := openai.ChatCompletionContentPartFileFileParam{
					FileData: param.NewOpt(base64.StdEncoding.EncodeToString(v.Bytes)),
					Filename: param.NewOpt(v.Name),
				}
				parts = append(parts, openai.FileContentPart(fileParam))
			}
		}
	}
	return parts
}

func choiceToToolCalls(ctx context.Context, tools []*tools.Tool, choices []openai.ChatCompletionChoice) (*blades.ModelResponse, error) {
	msg := &blades.Message{
		Role:   blades.RoleTool,
		Status: blades.StatusCompleted,
	}
	for _, choice := range choices {
		if choice.Message.Content != "" {
			msg.Parts = append(msg.Parts, blades.TextPart{Text: choice.Message.Content})
		}
		if len(choice.Message.ToolCalls) > 0 {
			for _, call := range choice.Message.ToolCalls {
				msg.Role = blades.RoleTool
				msg.Parts = append(msg.Parts, blades.ToolPart{
					ID:      call.ID,
					Name:    call.Function.Name,
					Request: call.Function.Arguments,
				})
			}
		}
	}
	return &blades.ModelResponse{
		Message: msg,
	}, nil
}

// choiceToResponse converts a non-streaming choice to a ModelResponse.
func choiceToResponse(ctx context.Context, params openai.ChatCompletionNewParams, cc *openai.ChatCompletion) (*blades.ModelResponse, error) {
	msg := &blades.Message{
		Role:   blades.RoleAssistant,
		Status: blades.StatusCompleted,
		TokenUsage: blades.TokenUsage{
			PromptTokens:     cc.Usage.PromptTokens,
			CompletionTokens: cc.Usage.CompletionTokens,
			TotalTokens:      cc.Usage.TotalTokens,
		},
		Metadata: map[string]any{},
	}
	for _, choice := range cc.Choices {
		if choice.Message.Content != "" {
			msg.Parts = append(msg.Parts, blades.TextPart{Text: choice.Message.Content})
		}
		if choice.Message.Audio.Data != "" {
			bytes, err := base64.StdEncoding.DecodeString(choice.Message.Audio.Data)
			if err != nil {
				return nil, err
			}
			msg.Parts = append(msg.Parts, blades.DataPart{Bytes: bytes})
		}
		if choice.Message.Refusal != "" {
			// TODO: map refusal codes to specific error types
		}
		if choice.FinishReason != "" {
			msg.FinishReason = choice.FinishReason
		}
		for _, call := range choice.Message.ToolCalls {
			msg.Role = blades.RoleTool
			msg.Parts = append(msg.Parts, blades.ToolPart{
				ID:      call.ID,
				Name:    call.Function.Name,
				Request: call.Function.Arguments,
			})
		}

	}
	return &blades.ModelResponse{Message: msg}, nil
}

// chunkChoiceToResponse converts a streaming chunk choice to a ModelResponse.
func chunkChoiceToResponse(ctx context.Context, choices []openai.ChatCompletionChunkChoice) (*blades.ModelResponse, error) {
	msg := &blades.Message{
		Role:     blades.RoleAssistant,
		Status:   blades.StatusIncomplete,
		Metadata: map[string]any{},
	}
	for _, choice := range choices {
		if choice.Delta.Content != "" {
			msg.Parts = append(msg.Parts, blades.TextPart{Text: choice.Delta.Content})
		}
		if choice.Delta.Refusal != "" {
			// TODO: map refusal codes to specific error types
		}
		if choice.FinishReason != "" {
			msg.FinishReason = choice.FinishReason
		}
		for _, call := range choice.Delta.ToolCalls {
			msg.Role = blades.RoleTool
			msg.Parts = append(msg.Parts, blades.ToolPart{
				ID:      call.ID,
				Name:    call.Function.Name,
				Request: call.Function.Arguments,
			})
		}
	}
	return &blades.ModelResponse{Message: msg}, nil
}
