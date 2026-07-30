package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"iter"

	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/model"
	"github.com/go-kratos/blades/tools"
	openai "github.com/openai/openai-go/v3"
	sdkoption "github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
)

var ErrResponseRequestNil = errors.New("openai/response: request is nil")

type ResponsesConfig struct {
	BaseURL            string
	APIKey             string
	MaxOutputTokens    int64
	Temperature        float64
	TopP               float64
	Store              *bool
	PreviousResponseID string
	ExtraFields        map[string]any
	RequestOptions     []sdkoption.RequestOption
	ModelOptions       []model.Option
	ReasoningEffort    shared.ReasoningEffort
}

// ResponsesOption configures an OpenAI Responses API model provider.
type ResponsesOption func(*ResponsesConfig)

// WithResponsesConfig applies a full ResponsesConfig value.
func WithResponsesConfig(config ResponsesConfig) ResponsesOption {
	return func(c *ResponsesConfig) {
		*c = config
	}
}

// WithResponsesBaseURL sets a custom API base URL.
func WithResponsesBaseURL(baseURL string) ResponsesOption {
	return func(c *ResponsesConfig) {
		c.BaseURL = baseURL
	}
}

// WithResponsesAPIKey sets the API key.
func WithResponsesAPIKey(apiKey string) ResponsesOption {
	return func(c *ResponsesConfig) {
		c.APIKey = apiKey
	}
}

// WithResponsesRequestOptions appends SDK request options.
func WithResponsesRequestOptions(opts ...sdkoption.RequestOption) ResponsesOption {
	return func(c *ResponsesConfig) {
		c.RequestOptions = append(c.RequestOptions, opts...)
	}
}

// WithResponsesParallelToolCalls configures whether the model may emit multiple tool calls in one response.
func WithResponsesParallelToolCalls(enabled bool) ResponsesOption {
	return func(c *ResponsesConfig) {
		c.ModelOptions = model.MergeOptions(c.ModelOptions, []model.Option{
			model.ParallelToolCalls{Enabled: enabled},
		})
	}
}

// WithResponsesStore configures whether OpenAI stores generated responses for later retrieval.
func WithResponsesStore(store bool) ResponsesOption {
	return func(c *ResponsesConfig) {
		c.Store = &store
	}
}

// WithResponsesPreviousResponseID uses OpenAI server-side response chaining.
// Leave unset to send Blades-managed full-history input.
func WithResponsesPreviousResponseID(id string) ResponsesOption {
	return func(c *ResponsesConfig) {
		c.PreviousResponseID = id
	}
}

// NewResponses constructs an OpenAI Responses API provider. The API key is read
// from the SDK default environment variables unless WithResponsesAPIKey is used.
func NewResponses(modelName string, opts ...ResponsesOption) model.Provider {
	var config ResponsesConfig
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
	return &responseModel{
		model:  modelName,
		config: config,
		client: openai.NewClient(sdkOpts...),
	}
}

// responseModel implements model.Provider for OpenAI Responses API models.
type responseModel struct {
	model  string
	config ResponsesConfig
	client openai.Client
}

var _ model.Provider = (*responseModel)(nil)

// Name returns the model name.
func (m *responseModel) Name() string {
	return m.model
}

// Generate executes a non-streaming Responses API request.
func (m *responseModel) Generate(ctx context.Context, req *model.Request) (*model.Response, error) {
	params, err := m.toResponseParams(false, req)
	if err != nil {
		return nil, err
	}
	apiResponse, err := m.client.Responses.New(ctx, params)
	if err != nil {
		return nil, err
	}
	return responseToModelResponse(apiResponse)
}

// Stream streams Responses API events as model chunks.
func (m *responseModel) Stream(ctx context.Context, req *model.Request) iter.Seq2[*model.Chunk, error] {
	return func(yield func(*model.Chunk, error) bool) {
		params, err := m.toResponseParams(true, req)
		if err != nil {
			yield(nil, err)
			return
		}
		streaming := m.client.Responses.NewStreaming(ctx, params)
		defer streaming.Close()

		seenToolCall := make(map[string]struct{})
		for streaming.Next() {
			event := streaming.Current()
			chunk, err := responseStreamEventToChunk(event, seenToolCall)
			if err != nil {
				yield(nil, err)
				return
			}
			if chunk == nil || (len(chunk.Parts) == 0 && chunk.StopReason == "" && chunk.Usage == nil) {
				continue
			}
			if !yield(chunk, nil) {
				return
			}
		}
		if err := streaming.Err(); err != nil {
			yield(nil, err)
			return
		}
	}
}

// toResponseParams converts a generic model request into Responses API params.
func (m *responseModel) toResponseParams(_ bool, req *model.Request) (responses.ResponseNewParams, error) {
	if req == nil {
		return responses.ResponseNewParams{}, ErrResponseRequestNil
	}
	toolParams, err := toResponseTools(req.Tools)
	if err != nil {
		return responses.ResponseNewParams{}, err
	}
	input, err := toResponseInput(req.Messages)
	if err != nil {
		return responses.ResponseNewParams{}, err
	}
	params := responses.ResponseNewParams{
		Model:     shared.ResponsesModel(m.model),
		Input:     responses.ResponseNewParamsInputUnion{OfInputItemList: input},
		Tools:     toolParams,
		Reasoning: shared.ReasoningParam{Effort: m.config.ReasoningEffort},
	}
	if req.System != "" {
		params.Instructions = param.NewOpt(req.System)
	}
	if m.config.MaxOutputTokens > 0 {
		params.MaxOutputTokens = param.NewOpt(m.config.MaxOutputTokens)
	}
	if m.config.Temperature > 0 {
		params.Temperature = param.NewOpt(m.config.Temperature)
	}
	if m.config.TopP > 0 {
		params.TopP = param.NewOpt(m.config.TopP)
	}
	if m.config.Store != nil {
		params.Store = param.NewOpt(*m.config.Store)
	}
	if m.config.PreviousResponseID != "" {
		params.PreviousResponseID = param.NewOpt(m.config.PreviousResponseID)
	}
	if len(m.config.ExtraFields) > 0 {
		params.SetExtraFields(m.config.ExtraFields)
	}
	applyResponseOptions(&params, model.MergeOptions(m.config.ModelOptions, req.Options))
	return params, nil
}

func applyResponseOptions(params *responses.ResponseNewParams, opts []model.Option) {
	for _, opt := range opts {
		switch o := opt.(type) {
		case model.ParallelToolCalls:
			params.ParallelToolCalls = param.NewOpt(o.Enabled)
		case model.ReasoningEffort:
			if o.Level != "" {
				params.Reasoning.Effort = shared.ReasoningEffort(o.Level)
			}
		case model.ResponseFormat:
			if o.Schema != nil {
				schemaParam := responses.ResponseFormatTextJSONSchemaConfigParam{
					Name:   "structured_outputs",
					Schema: schemaToMap(o.Schema),
					Strict: param.NewOpt(o.Strict),
				}
				if o.Schema.Title != "" {
					schemaParam.Name = o.Schema.Title
				}
				if o.Schema.Description != "" {
					schemaParam.Description = param.NewOpt(o.Schema.Description)
				}
				params.Text.Format = responses.ResponseFormatTextConfigUnionParam{OfJSONSchema: &schemaParam}
			}
		case model.Sampling:
			if o.Temperature != nil {
				params.Temperature = param.NewOpt(*o.Temperature)
			}
			if o.TopP != nil {
				params.TopP = param.NewOpt(*o.TopP)
			}
			if o.MaxTokens != nil {
				params.MaxOutputTokens = param.NewOpt(int64(*o.MaxTokens))
			}
		}
	}
}

func toResponseInput(messages []*model.Message) (responses.ResponseInputParam, error) {
	input := make(responses.ResponseInputParam, 0, len(messages))
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		items, err := toResponseInputItems(msg)
		if err != nil {
			return nil, err
		}
		input = append(input, items...)
	}
	return input, nil
}

func toResponseInputItems(msg *model.Message) ([]responses.ResponseInputItemUnionParam, error) {
	var items []responses.ResponseInputItemUnionParam
	switch msg.Role {
	case model.RoleAssistant:
		if text := textFromParts(msg.Parts); text != "" {
			items = append(items, responses.ResponseInputItemUnionParam{
				OfMessage: &responses.EasyInputMessageParam{
					Role:    responses.EasyInputMessageRoleAssistant,
					Content: responses.EasyInputMessageContentUnionParam{OfString: param.NewOpt(text)},
				},
			})
		}
		for _, part := range msg.Parts {
			if toolUse, ok := part.(content.ToolUse); ok {
				items = append(items, responses.ResponseInputItemParamOfFunctionCall(string(toolUse.Input), toolUse.ID, toolUse.Name))
			}
		}
	case model.RoleTool:
		var userParts []content.Part
		for _, part := range msg.Parts {
			if result, ok := part.(content.ToolResult); ok {
				items = append(items, responses.ResponseInputItemParamOfFunctionCallOutput(result.ID, textFromParts(result.Parts)))
				continue
			}
			userParts = append(userParts, part)
		}
		contentParts, err := toResponseInputContent(userParts)
		if err != nil {
			return nil, err
		}
		if len(contentParts) > 0 {
			items = append(items, responses.ResponseInputItemParamOfMessage(contentParts, responses.EasyInputMessageRoleUser))
		}
	case model.RoleUser:
		fallthrough
	default:
		contentParts, err := toResponseInputContent(msg.Parts)
		if err != nil {
			return nil, err
		}
		if len(contentParts) > 0 {
			items = append(items, responses.ResponseInputItemParamOfMessage(contentParts, responses.EasyInputMessageRoleUser))
		}
	}
	return items, nil
}

func toResponseInputContent(parts []content.Part) (responses.ResponseInputMessageContentListParam, error) {
	out := make(responses.ResponseInputMessageContentListParam, 0, len(parts))
	for _, part := range parts {
		switch v := part.(type) {
		case content.Text:
			out = append(out, responses.ResponseInputContentParamOfInputText(v.Text))
		case content.FileRefPart:
			switch mimeKind(v.MIME) {
			case "image":
				out = append(out, responses.ResponseInputContentUnionParam{
					OfInputImage: &responses.ResponseInputImageParam{
						FileID: param.NewOpt(v.ID),
						Detail: responses.ResponseInputImageDetailAuto,
					},
				})
			default:
				out = append(out, responses.ResponseInputContentUnionParam{
					OfInputFile: &responses.ResponseInputFileParam{FileID: param.NewOpt(v.ID)},
				})
			}
		case content.FilePart:
			switch mimeKind(v.MIME) {
			case "image":
				out = append(out, responses.ResponseInputContentUnionParam{
					OfInputImage: &responses.ResponseInputImageParam{
						ImageURL: param.NewOpt(v.URI),
						Detail:   responses.ResponseInputImageDetailAuto,
					},
				})
			default:
				out = append(out, responses.ResponseInputContentUnionParam{
					OfInputFile: &responses.ResponseInputFileParam{
						FileURL:  param.NewOpt(v.URI),
						Filename: param.NewOpt(v.Filename),
					},
				})
			}
		case content.DataPart:
			switch mimeKind(v.MIME) {
			case "image":
				imageURL := "data:" + v.MIME + ";base64," + base64.StdEncoding.EncodeToString(v.Bytes)
				out = append(out, responses.ResponseInputContentUnionParam{
					OfInputImage: &responses.ResponseInputImageParam{
						ImageURL: param.NewOpt(imageURL),
						Detail:   responses.ResponseInputImageDetailAuto,
					},
				})
			default:
				fileData := base64.StdEncoding.EncodeToString(v.Bytes)
				if v.MIME != "" {
					fileData = "data:" + v.MIME + ";base64," + fileData
				}
				out = append(out, responses.ResponseInputContentUnionParam{
					OfInputFile: &responses.ResponseInputFileParam{
						FileData: param.NewOpt(fileData),
						Filename: param.NewOpt(v.Filename),
					},
				})
			}
		}
	}
	return out, nil
}

func toResponseTools(toolSpecs []tools.ToolSpec) ([]responses.ToolUnionParam, error) {
	if len(toolSpecs) == 0 {
		return nil, nil
	}
	params := make([]responses.ToolUnionParam, 0, len(toolSpecs))
	for _, spec := range toolSpecs {
		schema := map[string]any{}
		if spec.InputSchema != nil {
			schema = schemaToMap(spec.InputSchema)
		}
		fn := responses.ToolParamOfFunction(spec.Name, schema, false)
		if spec.Description != "" && fn.OfFunction != nil {
			fn.OfFunction.Description = param.NewOpt(spec.Description)
		}
		params = append(params, fn)
	}
	return params, nil
}

func responseToModelResponse(resp *responses.Response) (*model.Response, error) {
	if resp == nil {
		return &model.Response{
			Message:    &model.Message{Role: model.RoleAssistant},
			StopReason: model.StopEnd,
		}, nil
	}
	if resp.Status == responses.ResponseStatusFailed {
		return nil, responseFailedError(*resp)
	}
	parts, err := responseOutputToParts(resp.Output)
	if err != nil {
		return nil, err
	}
	return &model.Response{
		Message:    &model.Message{Role: model.RoleAssistant, Parts: parts},
		StopReason: responseStopReason(*resp),
		Usage:      responseUsageToModel(resp.Usage),
	}, nil
}

func responseOutputToParts(items []responses.ResponseOutputItemUnion) ([]content.Part, error) {
	parts := make([]content.Part, 0, len(items))
	for _, item := range items {
		switch item.Type {
		case "message":
			for _, c := range item.Content {
				switch c.Type {
				case "output_text":
					if c.Text != "" {
						parts = append(parts, content.Text{Text: c.Text})
					}
				case "refusal":
					if c.Refusal != "" {
						parts = append(parts, content.Text{Text: c.Refusal})
					}
				}
			}
		case "function_call":
			if item.Name == "" {
				continue
			}
			parts = append(parts, content.ToolUse{
				ID:    item.CallID,
				Name:  item.Name,
				Input: json.RawMessage(item.Arguments),
			})
		case "reasoning":
			reasoning := item.AsReasoning()
			for _, c := range reasoning.Content {
				if c.Text != "" {
					parts = append(parts, content.Thinking{Text: c.Text})
				}
			}
			summary := reasoning.Summary
			if len(summary) == 0 {
				summary = item.Summary
			}
			for _, s := range summary {
				if s.Text != "" {
					parts = append(parts, content.Thinking{Text: s.Text})
				}
			}
		}
	}
	return parts, nil
}

func responseStreamEventToChunk(event responses.ResponseStreamEventUnion, seenToolCall map[string]struct{}) (*model.Chunk, error) {
	switch event.Type {
	case "response.output_text.delta":
		if event.Delta == "" {
			return nil, nil
		}
		return &model.Chunk{Parts: []content.Part{content.Text{Text: event.Delta}}}, nil
	case "response.reasoning_text.delta", "response.reasoning_summary_text.delta":
		if event.Delta == "" {
			return nil, nil
		}
		return &model.Chunk{Parts: []content.Part{content.Thinking{Text: event.Delta}}}, nil
	case "response.function_call_arguments.done":
		return nil, nil
	case "response.output_item.done":
		if event.Item.Type != "function_call" {
			return nil, nil
		}
		if event.Item.Name == "" {
			return nil, nil
		}
		id := event.Item.CallID
		if id == "" {
			id = event.Item.ID
		}
		if id == "" {
			return nil, nil
		}
		if _, ok := seenToolCall[id]; ok {
			return nil, nil
		}
		seenToolCall[id] = struct{}{}
		return &model.Chunk{
			Parts: []content.Part{
				content.ToolUse{ID: id, Name: event.Item.Name, Input: json.RawMessage(event.Item.Arguments)},
			},
			StopReason: model.StopToolUse,
		}, nil
	case "response.completed":
		resp := event.Response
		stopReason := responseStopReason(resp)
		if stopReason == model.StopEnd && len(seenToolCall) > 0 {
			stopReason = model.StopToolUse
		}
		return &model.Chunk{
			StopReason: stopReason,
			Usage:      responseUsagePtrToModel(resp.Usage),
		}, nil
	case "response.incomplete":
		resp := event.Response
		stopReason := responseStopReason(resp)
		if stopReason == model.StopEnd && len(seenToolCall) > 0 {
			stopReason = model.StopToolUse
		}
		return &model.Chunk{
			StopReason: stopReason,
			Usage:      responseUsagePtrToModel(resp.Usage),
		}, nil
	case "response.failed":
		resp := event.Response
		return nil, responseFailedError(resp)
	case "error":
		if event.Param != "" {
			return nil, fmt.Errorf("openai/response: stream error: %s: %s (%s)", event.Code, event.Message, event.Param)
		}
		return nil, fmt.Errorf("openai/response: stream error: %s: %s", event.Code, event.Message)
	default:
		return nil, nil
	}
}

func responseFailedError(resp responses.Response) error {
	if resp.Error.Message == "" {
		return errors.New("openai/response: response failed")
	}
	return fmt.Errorf("openai/response: response failed: %s", resp.Error.Message)
}

func responseStopReason(resp responses.Response) model.StopReason {
	reason := mapResponseStopReason(resp.Status, resp.IncompleteDetails)
	if reason == model.StopEnd && responseOutputHasToolUse(resp.Output) {
		return model.StopToolUse
	}
	return reason
}

func mapResponseStopReason(status responses.ResponseStatus, details responses.ResponseIncompleteDetails) model.StopReason {
	switch status {
	case responses.ResponseStatusIncomplete:
		switch details.Reason {
		case "max_output_tokens":
			return model.StopMaxTokens
		case "content_filter":
			return model.StopSafety
		default:
			return model.StopEnd
		}
	case responses.ResponseStatusFailed, responses.ResponseStatusCancelled:
		return model.StopReason(status)
	default:
		return model.StopEnd
	}
}

func responseOutputHasToolUse(items []responses.ResponseOutputItemUnion) bool {
	for _, item := range items {
		if item.Type == "function_call" {
			return true
		}
	}
	return false
}

func responseUsageToModel(usage responses.ResponseUsage) model.Usage {
	return model.Usage{
		InputCachedTokens:     usage.InputTokensDetails.CachedTokens,
		InputCacheMissTokens:  usage.InputTokens - usage.InputTokensDetails.CachedTokens,
		OutputReasoningTokens: usage.OutputTokensDetails.ReasoningTokens,
		TotalInputTokens:      usage.InputTokens,
		TotalOutputTokens:     usage.OutputTokens,
		TotalTokens:           usage.TotalTokens,
		Raw:                   rawUsageJSON(usage.RawJSON()),
	}
}

func responseUsagePtrToModel(usage responses.ResponseUsage) *model.Usage {
	m := responseUsageToModel(usage)
	if m.TotalInputTokens == 0 && m.TotalOutputTokens == 0 && m.TotalTokens == 0 && len(m.Raw) == 0 {
		return nil
	}
	return &m
}

func schemaToMap(schema any) map[string]any {
	if schema == nil {
		return nil
	}
	b, err := json.Marshal(schema)
	if err != nil {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil
	}
	return m
}
