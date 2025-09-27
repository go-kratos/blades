package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"

	"github.com/go-kratos/blades"
	"github.com/openai/openai-go/v2"
	"github.com/openai/openai-go/v2/option"
	"github.com/openai/openai-go/v2/packages/param"
	"github.com/openai/openai-go/v2/shared"
)

var (
	// ErrEmptyResponse indicates the provider returned no choices.
	ErrEmptyResponse = errors.New("empty completion response")
	// ErrToolNotFound indicates a tool call was made to an unknown tool.
	ErrToolNotFound = errors.New("tool not found")
	// ErrTooManyIterations indicates the max iterations option is less than 1.
	ErrTooManyIterations = errors.New("too many iterations requested")
)

// ChatProvider implements blades.ModelProvider for OpenAI-compatible chat models.
type ChatProvider struct {
	client openai.Client
}

// NewChatProvider constructs an OpenAI provider. The API key is read from
// the OPENAI_API_KEY environment variable. If OPENAI_BASE_URL is set,
// it is used as the API base URL; otherwise the library default is used.
func NewChatProvider(opts ...option.RequestOption) blades.ModelProvider {
	return &ChatProvider{client: openai.NewClient(opts...)}
}

// New executes a non-streaming chat completion request.
func (p *ChatProvider) New(ctx context.Context,
	params openai.ChatCompletionNewParams, tools []*blades.Tool, opts blades.ModelOptions) (*blades.ModelResponse, error) {
	// Ensure we have at least one iteration left.
	if opts.MaxIterations < 1 {
		return nil, ErrTooManyIterations
	}
	chatResponse, err := p.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, err
	}
	res, err := choiceToResponse(ctx, &params, tools, chatResponse.Choices)
	if err != nil {
		return nil, err
	}
	for _, msg := range res.Messages {
		switch msg.Role {
		case blades.RoleTool:
			if len(msg.ToolCalls) == 0 {
				continue
			}
			// Recursively call Execute to handle multiple tool calls.
			opts.MaxIterations--
			return p.New(ctx, params, tools, opts)
		}
	}
	return res, nil
}

// Generate executes a non-streaming chat completion request.
func (p *ChatProvider) Generate(ctx context.Context, req *blades.ModelRequest, opts ...blades.ModelOption) (*blades.ModelResponse, error) {
	opt := blades.ModelOptions{MaxIterations: 3}
	for _, apply := range opts {
		apply(&opt)
	}
	params, err := toChatCompletionParams(req, opt)
	if err != nil {
		return nil, err
	}
	return p.New(ctx, params, req.Tools, opt)
}

// NewStreaming executes a streaming chat completion request.
func (p *ChatProvider) NewStreaming(ctx context.Context,
	params openai.ChatCompletionNewParams, tools []*blades.Tool, opts blades.ModelOptions) (blades.Streamer[*blades.ModelResponse], error) {
	// Ensure we have at least one iteration left.
	if opts.MaxIterations < 1 {
		return nil, ErrTooManyIterations
	}
	stream := p.client.Chat.Completions.NewStreaming(ctx, params)
	pipe := blades.NewStreamPipe[*blades.ModelResponse]()
	pipe.Go(func() error {
		defer stream.Close()
		acc := openai.ChatCompletionAccumulator{}
		for stream.Next() {
			chunk := stream.Current()
			acc.AddChunk(chunk)
			res, err := chunkChoiceToResponse(ctx, tools, chunk.Choices)
			if err != nil {
				return err
			}
			pipe.Send(res)
		}
		lastResponse, err := choiceToResponse(ctx, &params, tools, acc.ChatCompletion.Choices)
		if err != nil {
			return err
		}
		pipe.Send(lastResponse)
		for _, msg := range lastResponse.Messages {
			switch msg.Role {
			case blades.RoleTool:
				if len(msg.ToolCalls) == 0 {
					continue
				}
				// Recursively call Execute to handle multiple tool calls.
				opts.MaxIterations--
				toolStream, err := p.NewStreaming(ctx, params, tools, opts)
				if err != nil {
					return err
				}
				for toolStream.Next() {
					res, err := toolStream.Current()
					if err != nil {
						return err
					}
					pipe.Send(res)
				}
			}
		}
		return nil
	})
	return pipe, nil
}

// NewStream streams chat completion chunks and converts each choice delta
// into a ModelResponse for incremental consumption.
func (p *ChatProvider) NewStream(ctx context.Context, req *blades.ModelRequest, opts ...blades.ModelOption) (blades.Streamer[*blades.ModelResponse], error) {
	opt := blades.ModelOptions{MaxIterations: 3}
	for _, apply := range opts {
		apply(&opt)
	}
	if opt.MaxIterations > 0 {
		opts = append(opts, blades.MaxIterations(opt.MaxIterations-1))
	} else {
		return nil, ErrTooManyIterations
	}
	params, err := toChatCompletionParams(req, opt)
	if err != nil {
		return nil, err
	}
	return p.NewStreaming(ctx, params, req.Tools, opt)
}

// toChatCompletionParams converts a generic model request into OpenAI params.
func toChatCompletionParams(req *blades.ModelRequest, opt blades.ModelOptions) (openai.ChatCompletionNewParams, error) {
	tools, err := toTools(req.Tools)
	if err != nil {
		return openai.ChatCompletionNewParams{}, err
	}
	params := openai.ChatCompletionNewParams{
		Tools:    tools,
		Model:    req.Model,
		Messages: make([]openai.ChatCompletionMessageParamUnion, 0, len(req.Messages)),
	}
	if opt.TopP > 0 {
		params.TopP = param.NewOpt(opt.TopP)
	}
	if opt.Temperature > 0 {
		params.Temperature = param.NewOpt(opt.Temperature)
	}
	if opt.MaxOutputTokens > 0 {
		params.MaxCompletionTokens = param.NewOpt(opt.MaxOutputTokens)
	}
	if opt.ReasoningEffort != "" {
		params.ReasoningEffort = shared.ReasoningEffort(opt.ReasoningEffort)
	}
	for _, msg := range req.Messages {
		log.Println("Processing message:", msg.Role, msg.Parts)
		switch msg.Role {
		case blades.RoleUser:
			params.Messages = append(params.Messages, openai.UserMessage(toContentParts(msg)))
		case blades.RoleAssistant:
			params.Messages = append(params.Messages, openai.UserMessage(toContentParts(msg)))
		case blades.RoleSystem:
			params.Messages = append(params.Messages, openai.SystemMessage(toTextParts(msg)))
		}
	}
	return params, nil
}

func toTools(tools []*blades.Tool) ([]openai.ChatCompletionToolUnionParam, error) {
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
			switch v.MimeType.Type() {
			case "image":
				parts = append(parts, openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{
					URL: v.URI,
				}))
			case "audio":
				parts = append(parts, openai.InputAudioContentPart(openai.ChatCompletionContentPartInputAudioInputAudioParam{
					Data:   v.URI,
					Format: v.MimeType.Format(),
				}))
			default:
				log.Println("failed to process file part with MIME type:", v.MimeType)
			}
		case blades.DataPart:
			// Handle different content types based on MIME type
			switch v.MimeType.Type() {
			case "image":
				mimeType := string(v.MimeType)
				base64Data := "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(v.Bytes)
				parts = append(parts, openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{
					URL: base64Data,
				}))
			case "audio":
				parts = append(parts, openai.InputAudioContentPart(openai.ChatCompletionContentPartInputAudioInputAudioParam{
					Data:   "data:;base64," + base64.StdEncoding.EncodeToString(v.Bytes),
					Format: v.MimeType.Format(),
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

// toolCall invokes a tool by name with the given arguments.
func toolCall(ctx context.Context, tools []*blades.Tool, name, arguments string) (string, error) {
	for _, tool := range tools {
		if tool.Name == name {
			return tool.Handle(ctx, arguments)
		}
	}
	return "", ErrToolNotFound
}

func choiceToToolCalls(ctx context.Context, tools []*blades.Tool, choices []openai.ChatCompletionChoice) (*blades.ModelResponse, error) {
	res := &blades.ModelResponse{}
	for _, choice := range choices {
		msg := &blades.Message{
			Role:   blades.RoleTool,
			Status: blades.StatusCompleted,
		}
		if len(choice.Message.ToolCalls) > 0 {
			for _, call := range choice.Message.ToolCalls {
				result, err := toolCall(ctx, tools, call.Function.Name, call.Function.Arguments)
				if err != nil {
					return nil, err
				}
				msg.Role = blades.RoleTool
				msg.ToolCalls = append(msg.ToolCalls, &blades.ToolCall{
					ID:        call.ID,
					Name:      call.Function.Name,
					Arguments: call.Function.Arguments,
					Result:    result,
				})
			}
		}
		res.Messages = append(res.Messages, msg)
	}
	return res, nil
}

// choiceToResponse converts a non-streaming choice to a ModelResponse.
func choiceToResponse(ctx context.Context, params *openai.ChatCompletionNewParams, tools []*blades.Tool, choices []openai.ChatCompletionChoice) (*blades.ModelResponse, error) {
	res := &blades.ModelResponse{}
	for _, choice := range choices {
		msg := &blades.Message{
			Role:     blades.RoleAssistant,
			Status:   blades.StatusCompleted,
			Metadata: map[string]string{},
		}
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
			msg.Metadata["refusal"] = choice.Message.Refusal
		}
		if choice.FinishReason != "" {
			msg.Metadata["finish_reason"] = choice.FinishReason
		}
		if len(choice.Message.ToolCalls) > 0 {
			// If there is a was a function call, continue the conversation
			params.Messages = append(params.Messages, choice.Message.ToParam())
		}
		for _, call := range choice.Message.ToolCalls {
			result, err := toolCall(ctx, tools, call.Function.Name, call.Function.Arguments)
			if err != nil {
				return nil, err
			}
			msg.Role = blades.RoleTool
			msg.ToolCalls = append(msg.ToolCalls, &blades.ToolCall{
				ID:        call.ID,
				Name:      call.Function.Name,
				Arguments: call.Function.Arguments,
				Result:    result,
			})
			params.Messages = append(params.Messages, openai.ToolMessage(result, call.ID))
		}
		res.Messages = append(res.Messages, msg)
	}
	return res, nil
}

// chunkChoiceToResponse converts a streaming chunk choice to a ModelResponse.
func chunkChoiceToResponse(ctx context.Context, tools []*blades.Tool, choices []openai.ChatCompletionChunkChoice) (*blades.ModelResponse, error) {
	res := &blades.ModelResponse{}
	for _, choice := range choices {
		msg := &blades.Message{
			Role:     blades.RoleAssistant,
			Status:   blades.StatusIncomplete,
			Metadata: map[string]string{},
		}
		if choice.Delta.Content != "" {
			msg.Parts = append(msg.Parts, blades.TextPart{Text: choice.Delta.Content})
		}
		if choice.Delta.Refusal != "" {
			msg.Metadata["refusal"] = choice.Delta.Refusal
		}
		if choice.FinishReason != "" {
			msg.Metadata["finish_reason"] = choice.FinishReason
		}
		for _, call := range choice.Delta.ToolCalls {
			msg.Role = blades.RoleTool
			msg.ToolCalls = append(msg.ToolCalls, &blades.ToolCall{
				ID:        call.ID,
				Name:      call.Function.Name,
				Arguments: call.Function.Arguments,
			})
		}
		res.Messages = append(res.Messages, msg)
	}
	return res, nil
}
