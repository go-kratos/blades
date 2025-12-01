package ollama

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"time"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/tools"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/ollama/ollama/api"
)

type Config struct {
	BaseURL          string
	Model            string
	Seed             int64
	MaxOutputTokens  int
	FrequencyPenalty float64
	PresencePenalty  float64
	Temperature      float64
	TopP             float64
	StopSequences    []string
	KeepAlive        string // e.g., "5m", "1h"
	Think            string // e.g., "true", "false", "high", "medium", "low"
	Options          map[string]any
}

// chatModel implements blades.chatModel for Ollama-compatible chat models.
type chatModel struct {
	model  string
	config Config
	client *api.Client
}

// NewModel constructs an Ollama provider. The base URL defaults to
// http://localhost:11434 if not specified. If OLLAMA_HOST is set,
// it is used as the API base URL.
func NewModel(model string, config Config) blades.ModelProvider {
	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}

	baseURLParsed, err := url.Parse(baseURL)
	if err != nil {
		log.Fatalf("Invalid Ollama base URL: %v", err)
	}

	client := api.NewClient(baseURLParsed, nil)

	return &chatModel{
		model:  model,
		config: config,
		client: client,
	}
}

// Name returns the model name.
func (m *chatModel) Name() string {
	return m.model
}

// Generate executes a non-streaming chat completion request.
func (m *chatModel) Generate(ctx context.Context, req *blades.ModelRequest) (*blades.ModelResponse, error) {
	ollamaReq, err := m.toChatRequest(req, false)
	if err != nil {
		return nil, err
	}

	var response *api.ChatResponse
	err = m.client.Chat(ctx, ollamaReq, func(resp api.ChatResponse) error {
		response = &resp
		return nil
	})
	if err != nil {
		return nil, err
	}

	if response == nil {
		return nil, fmt.Errorf("no response received from Ollama")
	}

	return m.chatResponseToModelResponse(response)
}

// NewStreaming streams chat completion chunks and converts each choice delta
// into a ModelResponse for incremental consumption.
func (m *chatModel) NewStreaming(ctx context.Context, req *blades.ModelRequest) blades.Generator[*blades.ModelResponse, error] {
	return func(yield func(*blades.ModelResponse, error) bool) {
		ollamaReq, err := m.toChatRequest(req, true)
		if err != nil {
			yield(nil, err)
			return
		}

		err = m.client.Chat(ctx, ollamaReq, func(resp api.ChatResponse) error {
			if resp.Done {
				// Final response
				modelResp, err := m.chatResponseToModelResponse(&resp)
				if err != nil {
					return err
				}
				if !yield(modelResp, nil) {
					return fmt.Errorf("streaming stopped by consumer")
				}
				return nil
			}

			// Streaming response - handle partial content
			modelResp, err := m.chatChunkToModelResponse(&resp)
			if err != nil {
				return err
			}

			if !yield(modelResp, nil) {
				return fmt.Errorf("streaming stopped by consumer")
			}
			return nil
		})

		if err != nil {
			yield(nil, err)
		}
	}
}

// toChatRequest converts a generic model request into Ollama ChatRequest.
func (m *chatModel) toChatRequest(req *blades.ModelRequest, stream bool) (*api.ChatRequest, error) {
	options := make(map[string]any)

	// Copy user-provided options first
	for k, v := range m.config.Options {
		options[k] = v
	}

	// Apply standard parameters
	if m.config.Seed > 0 {
		options["seed"] = m.config.Seed
	}
	if m.config.MaxOutputTokens > 0 {
		options["num_predict"] = m.config.MaxOutputTokens
	}
	if m.config.FrequencyPenalty > 0 {
		options["repeat_penalty"] = m.config.FrequencyPenalty
	}
	if m.config.Temperature > 0 {
		options["temperature"] = m.config.Temperature
	}
	if m.config.TopP > 0 {
		options["top_p"] = m.config.TopP
	}
	if len(m.config.StopSequences) > 0 {
		options["stop"] = m.config.StopSequences
	}

	ollamaReq := &api.ChatRequest{
		Model:    m.model,
		Messages: make([]api.Message, 0, len(req.Messages)),
		Options:  options,
		Stream:   &stream,
	}

	// Set keep_alive if specified
	if m.config.KeepAlive != "" {
		duration, err := time.ParseDuration(m.config.KeepAlive)
		if err == nil {
			ollamaReq.KeepAlive = &api.Duration{Duration: duration}
		} else {
			log.Printf("Invalid keep_alive duration: %v", err)
		}
	}

	// Set think if specified
	if m.config.Think != "" {
		thinkVal := api.ThinkValue{Value: m.config.Think}
		ollamaReq.Think = &thinkVal
	}

	// Set format if output schema is specified
	if req.OutputSchema != nil {
		formatBytes, err := json.Marshal(req.OutputSchema)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal output schema: %w", err)
		}
		ollamaReq.Format = formatBytes
	}

	// Convert messages
	if req.Instruction != nil {
		ollamaReq.Messages = append(ollamaReq.Messages, api.Message{
			Role:    "system",
			Content: req.Instruction.Text(),
		})
	}

	for _, msg := range req.Messages {
		ollamaMsg, err := m.messageToOllamaMessage(msg)
		if err != nil {
			return nil, fmt.Errorf("failed to convert message: %w", err)
		}
		ollamaReq.Messages = append(ollamaReq.Messages, ollamaMsg)
	}

	// Convert tools
	if len(req.Tools) > 0 {
		ollamaTools, err := m.toolsToOllamaTools(req.Tools)
		if err != nil {
			return nil, fmt.Errorf("failed to convert tools: %w", err)
		}
		ollamaReq.Tools = ollamaTools
	}

	return ollamaReq, nil
}

// messageToOllamaMessage converts a blades Message to Ollama Message.
func (m *chatModel) messageToOllamaMessage(msg *blades.Message) (api.Message, error) {
	ollamaMsg := api.Message{
		Role:    roleToOllamaRole(msg.Role),
		Content: msg.Text(),
	}

	// Handle images and other content parts
	for _, part := range msg.Parts {
		switch v := any(part).(type) {
		case blades.FilePart:
			if v.MIMEType.Type() == "image" {
				// Ollama expects image data as []ImageData
				if v.URI != "" && len(v.URI) > 4 {
					// Try to decode base64 image data
					imgData, err := base64ToImageData(v.URI)
					if err != nil {
						log.Printf("Failed to decode image data: %v", err)
						continue
					}
					ollamaMsg.Images = append(ollamaMsg.Images, imgData)
				}
			}
		case blades.DataPart:
			if v.MIMEType.Type() == "image" {
				imgData := api.ImageData(v.Bytes)
				ollamaMsg.Images = append(ollamaMsg.Images, imgData)
			}
		case blades.ToolPart:
			if msg.Role == blades.RoleTool {
				ollamaMsg.ToolName = v.Name
				ollamaMsg.Content = v.Response
			} else {
				// This is a tool call
				ollamaMsg.ToolCalls = append(ollamaMsg.ToolCalls, api.ToolCall{
					Function: api.ToolCallFunction{
						Name:      v.Name,
						Arguments: toolCallArgumentsToMap(v.Request),
					},
				})
			}
		}
	}

	return ollamaMsg, nil
}

// toolsToOllamaTools converts blades tools to Ollama tools.
func (m *chatModel) toolsToOllamaTools(tools []tools.Tool) (api.Tools, error) {
	ollamaTools := make(api.Tools, 0, len(tools))

	for _, tool := range tools {
		fn := api.ToolFunction{
			Name: tool.Name(),
		}

		if tool.Description() != "" {
			fn.Description = tool.Description()
		}

		if tool.InputSchema() != nil {
			// Convert JSON schema to Ollama ToolFunctionParameters
			fn.Parameters = m.convertJSONSchemaToOllamaParams(tool.InputSchema())
		}

		ollamaTools = append(ollamaTools, api.Tool{
			Type:     "function",
			Function: fn,
		})
	}

	return ollamaTools, nil
}

// chatResponseToModelResponse converts Ollama ChatResponse to blades ModelResponse.
func (m *chatModel) chatResponseToModelResponse(resp *api.ChatResponse) (*blades.ModelResponse, error) {
	msg := &blades.Message{
		Role:   blades.RoleAssistant,
		Status: blades.StatusCompleted,
		Metadata: map[string]any{
			"model":      resp.Model,
			"created_at": resp.CreatedAt,
		},
	}

	// Set finish reason if available
	if resp.DoneReason != "" {
		msg.FinishReason = resp.DoneReason
	}

	// Add content
	if resp.Message.Content != "" {
		msg.Parts = append(msg.Parts, blades.TextPart{Text: resp.Message.Content})
	}

	// Handle thinking content
	if resp.Message.Thinking != "" {
		msg.Metadata["thinking"] = resp.Message.Thinking
	}

	// Handle tool calls
	for _, toolCall := range resp.Message.ToolCalls {
		msg.Parts = append(msg.Parts, blades.ToolPart{
			ID:      toolCall.Function.Name, // Ollama doesn't provide IDs, use name
			Name:    toolCall.Function.Name,
			Request: mapToJSON(toolCall.Function.Arguments),
		})
	}

	// Add usage information if available
	if resp.Metrics.PromptEvalCount > 0 {
		msg.TokenUsage = blades.TokenUsage{
			PromptTokens:     int64(resp.Metrics.PromptEvalCount),
			CompletionTokens: int64(resp.Metrics.EvalCount),
			TotalTokens:      int64(resp.Metrics.PromptEvalCount + resp.Metrics.EvalCount),
		}
	}

	return &blades.ModelResponse{Message: msg}, nil
}

// chatChunkToModelResponse converts Ollama streaming ChatResponse to blades ModelResponse.
func (m *chatModel) chatChunkToModelResponse(resp *api.ChatResponse) (*blades.ModelResponse, error) {
	msg := &blades.Message{
		Role:   blades.RoleAssistant,
		Status: blades.StatusIncomplete,
		Metadata: map[string]any{
			"model":      resp.Model,
			"created_at": resp.CreatedAt,
		},
	}

	// Add streaming content
	if resp.Message.Content != "" {
		msg.Parts = append(msg.Parts, blades.TextPart{Text: resp.Message.Content})
	}

	// Handle streaming tool calls
	for _, toolCall := range resp.Message.ToolCalls {
		msg.Parts = append(msg.Parts, blades.ToolPart{
			ID:      toolCall.Function.Name,
			Name:    toolCall.Function.Name,
			Request: mapToJSON(toolCall.Function.Arguments),
		})
	}

	return &blades.ModelResponse{Message: msg}, nil
}

// Helper functions

func roleToOllamaRole(role blades.Role) string {
	switch role {
	case blades.RoleUser:
		return "user"
	case blades.RoleAssistant:
		return "assistant"
	case blades.RoleSystem:
		return "system"
	case blades.RoleTool:
		return "tool"
	default:
		return "user"
	}
}

func base64ToImageData(dataURI string) ([]byte, error) {
	// Remove data URL prefix if present
	if len(dataURI) > 5 && dataURI[:5] == "data:" {
		commaIdx := -1
		for i, c := range dataURI {
			if c == ',' {
				commaIdx = i
				break
			}
		}
		if commaIdx > 0 {
			dataURI = dataURI[commaIdx+1:]
		}
	}

	// Decode base64
	return json.Marshal(dataURI)
}

func toolCallArgumentsToMap(args string) map[string]any {
	var result map[string]any
	if err := json.Unmarshal([]byte(args), &result); err != nil {
		// If parsing fails, return as string
		return map[string]any{"args": args}
	}
	return result
}

func mapToJSON(args map[string]any) string {
	if len(args) == 0 {
		return "{}"
	}
	result, err := json.Marshal(args)
	if err != nil {
		return "{}"
	}
	return string(result)
}

// convertJSONSchemaToOllamaParams converts a JSONSchema to Ollama ToolFunctionParameters
func (m *chatModel) convertJSONSchemaToOllamaParams(schema *jsonschema.Schema) api.ToolFunctionParameters {
	params := api.ToolFunctionParameters{
		Type:       "object",
		Properties: make(map[string]api.ToolProperty),
	}

	if schema.Type != "" {
		params.Type = schema.Type
	}

	if len(schema.Required) > 0 {
		params.Required = schema.Required
	}

	if schema.Properties != nil {
		for key, prop := range schema.Properties {
			toolProp := api.ToolProperty{
				Type:        api.PropertyType{prop.Type},
				Description: prop.Description,
			}

			if prop.Enum != nil {
				toolProp.Enum = prop.Enum
			}

			params.Properties[key] = toolProp
		}
	}

	return params
}
