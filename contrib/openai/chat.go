package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"strings"

	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/jsonrepair"
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
	// ContentPartEncoders extend user-content encoding for OpenAI-compatible APIs.
	ContentPartEncoders []ChatContentPartEncoder
	// ToolInputJSONRepairer repairs malformed completed tool input before it is
	// emitted. Nil disables repair.
	ToolInputJSONRepairer jsonrepair.Repairer
}

// ChatOption configures an OpenAI chat model provider.
type ChatOption func(*ChatConfig)

// WithContentPartEncoders appends custom Chat Completions content encoders.
func WithContentPartEncoders(encoders ...ChatContentPartEncoder) ChatOption {
	return func(c *ChatConfig) {
		c.ContentPartEncoders = append(c.ContentPartEncoders, encoders...)
	}
}

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

// WithToolInputJSONRepairer configures receive-side repair for malformed tool
// input JSON. Nil disables repair.
func WithToolInputJSONRepairer(repairer jsonrepair.Repairer) ChatOption {
	return func(c *ChatConfig) {
		c.ToolInputJSONRepairer = repairer
	}
}

// NewChat constructs an OpenAI chat provider. The API key is read
// from the SDK default environment variables unless WithAPIKey is used.
func NewChat(modelName string, opts ...ChatOption) model.Provider {
	config := ChatConfig{ToolInputJSONRepairer: jsonrepair.New()}
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
	return choiceToResponse(chatResponse, m.config.ToolInputJSONRepairer)
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
		accumulator := newChatStreamAccumulator(m.config.ToolInputJSONRepairer)
		for streaming.Next() {
			chunk := streaming.Current()
			converted, err := accumulator.addChunk(chunk)
			if err != nil {
				yield(nil, err)
				return
			}
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
		messages, err := m.toMessageParams(msg)
		if err != nil {
			return openai.ChatCompletionNewParams{}, err
		}
		params.Messages = append(params.Messages, messages...)
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

func (m *chatModel) toMessageParams(msg *model.Message) ([]openai.ChatCompletionMessageParamUnion, error) {
	if msg == nil {
		return nil, nil
	}
	switch msg.Role {
	case model.RoleUser:
		parts, err := m.toContentParts(msg.Parts)
		if err != nil {
			return nil, err
		}
		return []openai.ChatCompletionMessageParamUnion{openai.UserMessage(parts)}, nil
	case model.RoleAssistant:
		return []openai.ChatCompletionMessageParamUnion{toAssistantMessage(msg.Parts)}, nil
	case model.RoleTool:
		return m.toToolMessages(msg.Parts)
	default:
		parts, err := m.toContentParts(msg.Parts)
		if err != nil {
			return nil, err
		}
		return []openai.ChatCompletionMessageParamUnion{openai.UserMessage(parts)}, nil
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

func (m *chatModel) toToolMessages(parts []content.Part) ([]openai.ChatCompletionMessageParamUnion, error) {
	var messages []openai.ChatCompletionMessageParamUnion
	var userParts []content.Part
	for _, part := range parts {
		if result, ok := part.(content.ToolResult); ok {
			messages = append(messages, openai.ToolMessage(textFromParts(result.Parts), result.ID))
			continue
		}
		userParts = append(userParts, part)
	}
	contentParts, err := m.toContentParts(userParts)
	if err != nil {
		return nil, err
	}
	if len(contentParts) > 0 {
		messages = append(messages, openai.UserMessage(contentParts))
	}
	return messages, nil
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
func (m *chatModel) toContentParts(parts []content.Part) ([]openai.ChatCompletionContentPartUnionParam, error) {
	out := make([]openai.ChatCompletionContentPartUnionParam, 0, len(parts))
	for _, part := range parts {
		encoded, err := m.encodeContentPart(part)
		if err != nil {
			return nil, err
		}
		out = append(out, encoded)
	}
	return out, nil
}

func (m *chatModel) encodeContentPart(part content.Part) (openai.ChatCompletionContentPartUnionParam, error) {
	for _, encoder := range m.config.ContentPartEncoders {
		if encoder == nil {
			continue
		}
		encoded, handled, err := encoder.EncodeChatContentPart(part)
		if err != nil {
			return openai.ChatCompletionContentPartUnionParam{}, fmt.Errorf("openai/chat: encode content part %q with %T: %w", contentPartKind(part), encoder, err)
		}
		if handled {
			return encoded, nil
		}
	}

	switch v := part.(type) {
	case content.Text:
		return openai.TextContentPart(v.Text), nil
	case content.FilePart:
		switch mimeKind(v.MIME) {
		case "image":
			return openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{URL: v.URI}), nil
		case "audio":
			return openai.InputAudioContentPart(openai.ChatCompletionContentPartInputAudioInputAudioParam{
				Data:   v.URI,
				Format: mimeFormat(v.MIME),
			}), nil
		}
	case content.DataPart:
		switch mimeKind(v.MIME) {
		case "image":
			base64Data := "data:" + v.MIME + ";base64," + base64.StdEncoding.EncodeToString(v.Bytes)
			return openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{URL: base64Data}), nil
		case "audio":
			return openai.InputAudioContentPart(openai.ChatCompletionContentPartInputAudioInputAudioParam{
				Data:   base64.StdEncoding.EncodeToString(v.Bytes),
				Format: mimeFormat(v.MIME),
			}), nil
		default:
			return openai.FileContentPart(openai.ChatCompletionContentPartFileFileParam{
				FileData: param.NewOpt(base64.StdEncoding.EncodeToString(v.Bytes)),
				Filename: param.NewOpt(v.Filename),
			}), nil
		}
	}
	return openai.ChatCompletionContentPartUnionParam{}, unsupportedContentPart("chat", part)
}

// choiceToResponse converts a non-streaming response to a model.Response.
func choiceToResponse(cc *openai.ChatCompletion, repairer jsonrepair.Repairer) (*model.Response, error) {
	resp := &model.Response{
		Message: &model.Message{Role: model.RoleAssistant},
		Usage:   completionUsageToModel(cc.Usage),
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
			input, err := normalizeToolInputJSON([]byte(call.Function.Arguments), repairer)
			if err != nil {
				return nil, fmt.Errorf("openai/chat: invalid tool input JSON for tool %q (%s): %w", call.Function.Name, call.ID, err)
			}
			resp.Message.Parts = append(resp.Message.Parts, content.ToolUse{
				ID:    call.ID,
				Name:  call.Function.Name,
				Input: json.RawMessage(input),
			})
		}
	}
	if resp.StopReason == "" {
		resp.StopReason = model.StopEnd
	}
	return resp, nil
}

type chatStreamAccumulator struct {
	sdk              openai.ChatCompletionAccumulator
	emittedToolCalls map[chatToolCallKey]struct{}
	repairer         jsonrepair.Repairer
}

type chatToolCallKey struct {
	choiceIndex int64
	toolIndex   int
}

func newChatStreamAccumulator(repairer jsonrepair.Repairer) *chatStreamAccumulator {
	return &chatStreamAccumulator{
		emittedToolCalls: make(map[chatToolCallKey]struct{}),
		repairer:         repairer,
	}
}

func (a *chatStreamAccumulator) addChunk(chunk openai.ChatCompletionChunk) (*model.Chunk, error) {
	if !a.sdk.AddChunk(chunk) {
		return nil, errors.New("openai/chat: could not accumulate stream chunk")
	}

	converted := chunkToModelChunk(chunk)
	for _, choice := range chunk.Choices {
		if mapOpenAIStopReason(choice.FinishReason) != model.StopToolUse {
			continue
		}
		parts, err := a.toolParts(choice.Index)
		if err != nil {
			return nil, err
		}
		converted.Parts = append(converted.Parts, parts...)
	}
	return converted, nil
}

func (a *chatStreamAccumulator) toolParts(choiceIndex int64) ([]content.Part, error) {
	if choiceIndex < 0 || int(choiceIndex) >= len(a.sdk.Choices) {
		return nil, nil
	}
	choice := a.sdk.Choices[choiceIndex]
	parts := make([]content.Part, 0, len(choice.Message.ToolCalls))
	for i, call := range choice.Message.ToolCalls {
		key := chatToolCallKey{choiceIndex: choiceIndex, toolIndex: i}
		if _, ok := a.emittedToolCalls[key]; ok {
			continue
		}
		if call.Function.Name == "" {
			continue
		}
		input, err := normalizeToolInputJSON([]byte(call.Function.Arguments), a.repairer)
		if err != nil {
			return nil, fmt.Errorf("openai/chat: invalid tool input JSON for tool %q (%s): %w", call.Function.Name, call.ID, err)
		}
		parts = append(parts, content.ToolUse{
			ID:    call.ID,
			Name:  call.Function.Name,
			Input: json.RawMessage(input),
		})
		a.emittedToolCalls[key] = struct{}{}
	}
	return parts, nil
}

func chunkToModelChunk(chunk openai.ChatCompletionChunk) *model.Chunk {
	converted := &model.Chunk{}
	usage := completionUsageToModel(chunk.Usage)
	if usage.TotalInputTokens != 0 || usage.TotalOutputTokens != 0 || usage.TotalTokens != 0 || len(usage.Raw) > 0 {
		converted.Usage = &usage
	}
	for _, choice := range chunk.Choices {
		if reasoningContent := reasoningContentFromDelta(choice.Delta); reasoningContent != "" {
			converted.Parts = append(converted.Parts, content.Thinking{Text: reasoningContent})
		}
		if choice.Delta.Content != "" {
			converted.Parts = append(converted.Parts, content.Text{Text: choice.Delta.Content})
		}
		if choice.FinishReason != "" {
			converted.StopReason = mapOpenAIStopReason(choice.FinishReason)
		}
	}
	return converted
}

func reasoningContentFromDelta(delta openai.ChatCompletionChunkChoiceDelta) string {
	for _, name := range []string{"reasoning_content", "reasoning"} {
		field, ok := delta.JSON.ExtraFields[name]
		if !ok || field.Raw() == "" {
			continue
		}
		var text string
		if err := json.Unmarshal([]byte(field.Raw()), &text); err == nil && text != "" {
			return text
		}
	}
	return ""
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
