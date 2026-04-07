package ollama

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/tools"
)

const defaultBaseURL = "http://127.0.0.1:11434"

// Config controls the Ollama provider behavior.
type Config struct {
	// BaseURL is the Ollama endpoint, defaults to http://127.0.0.1:11434.
	BaseURL string
	// HTTPClient defaults to http.DefaultClient.
	HTTPClient *http.Client
	// Headers are attached to every API request.
	Headers map[string]string
	// Options maps to Ollama's chat options field.
	Options map[string]any
	// KeepAlive maps to Ollama's keep_alive field.
	KeepAlive string
}

// Model implements blades.ModelProvider for Ollama /api/chat.
type Model struct {
	model      string
	baseURL    string
	httpClient *http.Client
	config     Config
}

// NewModel creates a provider backed by Ollama's chat API.
func NewModel(model string, config Config) blades.ModelProvider {
	baseURL := strings.TrimRight(config.BaseURL, "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	client := config.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	return &Model{model: model, baseURL: baseURL, httpClient: client, config: config}
}

// Name returns the model name.
func (m *Model) Name() string { return m.model }

// Generate executes a non-streaming Ollama chat request.
func (m *Model) Generate(ctx context.Context, req *blades.ModelRequest) (*blades.ModelResponse, error) {
	if req == nil {
		return nil, errors.New("nil model request")
	}
	payload, err := m.toChatRequest(req, false)
	if err != nil {
		return nil, err
	}
	body, err := m.doChat(ctx, payload)
	if err != nil {
		return nil, err
	}
	defer body.Close()

	var resp chatResponse
	if err := json.NewDecoder(body).Decode(&resp); err != nil {
		return nil, err
	}
	return fromChatResponse(resp), nil
}

// NewStreaming executes a streaming Ollama chat request and yields chunks.
func (m *Model) NewStreaming(ctx context.Context, req *blades.ModelRequest) blades.Generator[*blades.ModelResponse, error] {
	return func(yield func(*blades.ModelResponse, error) bool) {
		if req == nil {
			yield(nil, errors.New("nil model request"))
			return
		}
		payload, err := m.toChatRequest(req, true)
		if err != nil {
			yield(nil, err)
			return
		}
		body, err := m.doChat(ctx, payload)
		if err != nil {
			yield(nil, err)
			return
		}
		defer body.Close()

		decoder := json.NewDecoder(body)
		for {
			var resp chatResponse
			if err := decoder.Decode(&resp); err != nil {
				if err == io.EOF {
					return
				}
				yield(nil, err)
				return
			}
			if !yield(fromChatResponse(resp), nil) {
				return
			}
		}
	}
}

func (m *Model) doChat(ctx context.Context, payload chatRequest) (io.ReadCloser, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, m.baseURL+"/api/chat", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range m.config.Headers {
		httpReq.Header.Set(k, v)
	}
	resp, err := m.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama chat failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return resp.Body, nil
}

func (m *Model) toChatRequest(req *blades.ModelRequest, stream bool) (chatRequest, error) {
	messages := make([]chatMessage, 0, len(req.Messages)+1)
	if req.Instruction != nil {
		instruction := req.Instruction.Clone()
		instruction.Role = blades.RoleSystem
		msgs, err := toChatMessages(instruction)
		if err != nil {
			return chatRequest{}, err
		}
		messages = append(messages, msgs...)
	}
	for _, msg := range req.Messages {
		msgs, err := toChatMessages(msg)
		if err != nil {
			return chatRequest{}, err
		}
		messages = append(messages, msgs...)
	}
	toolsPayload, err := toTools(req.Tools)
	if err != nil {
		return chatRequest{}, err
	}
	payload := chatRequest{
		Model:     m.model,
		Messages:  messages,
		Tools:     toolsPayload,
		Stream:    stream,
		Options:   m.config.Options,
		KeepAlive: m.config.KeepAlive,
	}
	if req.OutputSchema != nil {
		payload.Format = req.OutputSchema
	}
	return payload, nil
}

func toChatMessages(msg *blades.Message) ([]chatMessage, error) {
	role := string(msg.Role)
	if role == "" {
		role = string(blades.RoleUser)
	}

	if msg.Role == blades.RoleTool {
		return toToolMessages(msg), nil
	}

	out := chatMessage{Role: role}
	for _, part := range msg.Parts {
		switch v := part.(type) {
		case blades.TextPart:
			if out.Content == "" {
				out.Content = v.Text
			} else {
				out.Content += "\n" + v.Text
			}
		case blades.FilePart:
			if v.MIMEType.Type() == "image" {
				encoded, err := encodeImageFromURI(v.URI)
				if err != nil {
					return nil, err
				}
				out.Images = append(out.Images, encoded)
			}
		case blades.DataPart:
			if v.MIMEType.Type() == "image" {
				out.Images = append(out.Images, base64.StdEncoding.EncodeToString(v.Bytes))
			}
		case blades.ToolPart:
			out.ToolCalls = append(out.ToolCalls, chatToolCall{
				ID: v.ID,
				Function: chatFunctionCall{
					Name:      v.Name,
					Arguments: rawJSON(v.Request),
				},
			})
		}
	}
	return []chatMessage{out}, nil
}

func toToolMessages(msg *blades.Message) []chatMessage {
	toolCalls := make([]chatToolCall, 0, len(msg.Parts))
	toolResponses := make([]chatMessage, 0, len(msg.Parts))
	for _, part := range msg.Parts {
		toolPart, ok := part.(blades.ToolPart)
		if !ok {
			continue
		}
		toolCalls = append(toolCalls, chatToolCall{
			ID: toolPart.ID,
			Function: chatFunctionCall{
				Name:      toolPart.Name,
				Arguments: rawJSON(toolPart.Request),
			},
		})
		if toolPart.Response == "" {
			continue
		}
		toolResponses = append(toolResponses, chatMessage{
			Role:       string(blades.RoleTool),
			Content:    toolPart.Response,
			ToolName:   toolPart.Name,
			ToolCallID: toolPart.ID,
		})
	}

	if len(toolCalls) > 0 {
		assistant := chatMessage{Role: string(blades.RoleAssistant), ToolCalls: toolCalls}
		return append([]chatMessage{assistant}, toolResponses...)
	}

	fallback := chatMessage{Role: string(blades.RoleTool)}
	for _, part := range msg.Parts {
		if text, ok := part.(blades.TextPart); ok {
			fallback.Content = text.Text
			break
		}
	}
	return []chatMessage{fallback}
}

func fromChatResponse(resp chatResponse) *blades.ModelResponse {
	status := blades.StatusIncomplete
	if resp.Done {
		status = blades.StatusCompleted
	}
	message := blades.NewAssistantMessage(status)
	if resp.DoneReason != "" {
		message.FinishReason = resp.DoneReason
	}
	message.TokenUsage = blades.TokenUsage{
		InputTokens:  resp.PromptEvalCount,
		OutputTokens: resp.EvalCount,
		TotalTokens:  resp.PromptEvalCount + resp.EvalCount,
	}
	if resp.Message.Content != "" {
		message.Parts = append(message.Parts, blades.TextPart{Text: resp.Message.Content})
	}
	for _, call := range resp.Message.ToolCalls {
		message.Role = blades.RoleTool
		message.Parts = append(message.Parts, blades.NewToolPart(call.ID, call.Function.Name, string(rawJSONFromMessage(call.Function.Arguments))))
	}
	return &blades.ModelResponse{Message: message}
}

func toTools(in []tools.Tool) ([]chatTool, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]chatTool, 0, len(in))
	for _, tool := range in {
		params := map[string]any{"type": "object"}
		if schema := tool.InputSchema(); schema != nil {
			b, err := json.Marshal(schema)
			if err != nil {
				return nil, err
			}
			if err := json.Unmarshal(b, &params); err != nil {
				return nil, err
			}
		}
		out = append(out, chatTool{Type: "function", Function: chatToolFunction{
			Name:        tool.Name(),
			Description: tool.Description(),
			Parameters:  params,
		}})
	}
	return out, nil
}

func encodeImageFromURI(uri string) (string, error) {
	if strings.HasPrefix(uri, "data:") {
		parts := strings.SplitN(uri, ",", 2)
		if len(parts) != 2 {
			return "", errors.New("invalid data URI")
		}
		if strings.Contains(parts[0], ";base64") {
			return parts[1], nil
		}
		decoded, err := url.QueryUnescape(parts[1])
		if err != nil {
			return "", err
		}
		return base64.StdEncoding.EncodeToString([]byte(decoded)), nil
	}

	path := strings.TrimPrefix(uri, "file://")
	if path == "" {
		path = uri
	}
	path = filepath.Clean(path)
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(content), nil
}

func rawJSON(v string) json.RawMessage {
	trimmed := strings.TrimSpace(v)
	if trimmed == "" {
		return json.RawMessage("{}")
	}
	if !json.Valid([]byte(trimmed)) {
		b, _ := json.Marshal(trimmed)
		return json.RawMessage(b)
	}
	return json.RawMessage(trimmed)
}

func rawJSONFromMessage(v json.RawMessage) json.RawMessage {
	if len(v) == 0 {
		return json.RawMessage("{}")
	}
	return v
}

// request/response models

type chatRequest struct {
	Model     string         `json:"model"`
	Messages  []chatMessage  `json:"messages"`
	Tools     []chatTool     `json:"tools,omitempty"`
	Format    any            `json:"format,omitempty"`
	Stream    bool           `json:"stream"`
	Options   map[string]any `json:"options,omitempty"`
	KeepAlive string         `json:"keep_alive,omitempty"`
}

type chatMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content,omitempty"`
	Images     []string       `json:"images,omitempty"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
	ToolName   string         `json:"tool_name,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

type chatTool struct {
	Type     string           `json:"type"`
	Function chatToolFunction `json:"function"`
}

type chatToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type chatToolCall struct {
	ID       string           `json:"id,omitempty"`
	Function chatFunctionCall `json:"function"`
}

type chatFunctionCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type chatResponse struct {
	Message         chatMessage `json:"message"`
	Done            bool        `json:"done"`
	DoneReason      string      `json:"done_reason,omitempty"`
	PromptEvalCount int64       `json:"prompt_eval_count,omitempty"`
	EvalCount       int64       `json:"eval_count,omitempty"`
}
