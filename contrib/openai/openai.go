package openai

import (
	"context"
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

// Provider implements blades.ModelProvider for OpenAI-compatible chat models.
type Provider struct {
	client openai.Client
}

// NewProvider constructs an OpenAI provider. The API key is read from
// the OPENAI_API_KEY environment variable. If OPENAI_BASE_URL is set,
// it is used as the API base URL; otherwise the library default is used.
func NewProvider(opts ...option.RequestOption) blades.ModelProvider {
	return &Provider{client: openai.NewClient(opts...)}
}

// Generate executes a non-streaming chat completion request.
func (p *Provider) Generate(ctx context.Context, req *blades.ModelRequest, opts ...blades.ModelOption) (*blades.ModelResponse, error) {
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
	res, err := p.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, err
	}
	if len(res.Choices) == 0 {
		return nil, ErrEmptyResponse
	}
	m, err := choiceToResponse(ctx, req.Tools, res.Choices)
	if err != nil {
		return nil, err
	}
	if m.Message.Role == blades.RoleTool {
		req.Messages = append(req.Messages, m.Message)
		return p.Generate(ctx, req, opts...)
	}
	return m, nil
}

// NewStream streams chat completion chunks and converts each choice delta
// into a ModelResponse for incremental consumption.
func (p *Provider) NewStream(ctx context.Context, req *blades.ModelRequest, opts ...blades.ModelOption) (blades.Streamer[*blades.ModelResponse], error) {
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
	stream := p.client.Chat.Completions.NewStreaming(ctx, params)
	pipe := blades.NewStreamPipe[*blades.ModelResponse]()
	go func() {
		defer pipe.Close()
		var (
			err          error
			acc          = openai.ChatCompletionAccumulator{}
			lastResposne *blades.ModelResponse
		)
		for stream.Next() {
			chunk := stream.Current()
			if len(chunk.Choices) == 0 {
				continue
			}
			acc.AddChunk(chunk)
			lastResposne, err = chunkChoiceToResponse(ctx, req.Tools, chunk.Choices)
			if err != nil {
				pipe.SetError(err)
				return
			}
			pipe.Send(lastResposne)
		}
		if err := stream.Err(); err != nil {
			pipe.SetError(err)
			return
		}
		// If the final accumulated response includes tool calls, invoke them
		toolResponse, err := choiceToToolCalls(ctx, req.Tools, acc.ChatCompletion.Choices)
		if err != nil {
			pipe.SetError(err)
			return
		}
		if toolResponse.Message.Role == blades.RoleTool && len(toolResponse.Message.ToolCalls) > 0 {
			req.Messages = append(req.Messages, toolResponse.Message)
			toolStream, err := p.NewStream(ctx, req, opts...)
			if err != nil {
				pipe.SetError(err)
				return
			}
			defer toolStream.Close()
			for toolStream.Next() {
				res, err := toolStream.Current()
				if err != nil {
					pipe.SetError(err)
					return
				}
				pipe.Send(res)
			}
		}
	}()
	return pipe, nil
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
		case blades.RoleTool:
			assistantMessage := openai.AssistantMessage(msg.AsText())
			params.Messages = append(params.Messages, assistantMessage)
			for _, call := range msg.ToolCalls {
				assistantMessage.OfAssistant.ToolCalls = append(assistantMessage.OfAssistant.ToolCalls, openai.ChatCompletionMessageToolCallUnionParam{
					OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
						ID: call.ID,
						Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
							Name:      call.Name,
							Arguments: call.Arguments,
						},
					},
				})
				params.Messages = append(params.Messages, openai.ToolMessage(call.Result, call.ID))
			}
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
		param := openai.ChatCompletionToolUnionParam{
			OfFunction: &openai.ChatCompletionFunctionToolParam{
				Function: fn,
			},
		}
		params = append(params, param)
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
		}
	}
	return parts
}

// toolCall invokes a tool by name with the given arguments.
func toolCall(ctx context.Context, tools []*blades.Tool, name, arguments string) (string, error) {
	for _, tool := range tools {
		if tool.Name == name {
			return tool.Call(ctx, arguments)
		}
	}
	return "", ErrToolNotFound
}

func choiceToToolCalls(ctx context.Context, tools []*blades.Tool, choices []openai.ChatCompletionChoice) (*blades.ModelResponse, error) {
	msg := &blades.Message{
		Role:   blades.RoleTool,
		Status: blades.StatusCompleted,
	}
	for _, choice := range choices {
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
	}
	return &blades.ModelResponse{msg}, nil
}

// choiceToResponse converts a non-streaming choice to a ModelResponse.
func choiceToResponse(ctx context.Context, tools []*blades.Tool, choices []openai.ChatCompletionChoice) (*blades.ModelResponse, error) {
	msg := &blades.Message{
		Role:   blades.RoleAssistant,
		Status: blades.StatusCompleted,
	}
	for _, choice := range choices {
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
			return &blades.ModelResponse{Message: msg}, nil
		}
		if choice.Message.Content != "" {
			msg.Parts = append(msg.Parts, blades.TextPart{choice.Message.Content})
		}
		// Attach metadata when available.
		if choice.FinishReason != "" || choice.Message.Refusal != "" {
			msg.Metadata = map[string]string{}
			if choice.FinishReason != "" {
				msg.Metadata["finish_reason"] = choice.FinishReason
			}
			if choice.Message.Refusal != "" {
				msg.Metadata["refusal"] = choice.Message.Refusal
			}
		}
	}
	return &blades.ModelResponse{Message: msg}, nil
}

// chunkChoiceToResponse converts a streaming chunk choice to a ModelResponse.
func chunkChoiceToResponse(ctx context.Context, tools []*blades.Tool, choices []openai.ChatCompletionChunkChoice) (*blades.ModelResponse, error) {
	var (
		msg = &blades.Message{
			Role:   blades.RoleAssistant,
			Status: blades.StatusInProgress,
		}
	)
	for _, choice := range choices {
		if len(choice.Delta.ToolCalls) > 0 {
			for _, call := range choice.Delta.ToolCalls {
				msg.Role = blades.RoleTool
				msg.ToolCalls = append(msg.ToolCalls, &blades.ToolCall{
					ID:        call.ID,
					Name:      call.Function.Name,
					Arguments: call.Function.Arguments,
				})
			}
		}
		if choice.Delta.Content != "" {
			msg.Parts = append(msg.Parts, blades.TextPart{choice.Delta.Content})
		}
		if choice.FinishReason != "" {
			msg.Status = blades.StatusCompleted
			msg.Metadata = map[string]string{"finish_reason": choice.FinishReason}
		}
	}
	return &blades.ModelResponse{Message: msg}, nil
}
