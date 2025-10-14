package gemini

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"strings"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/tools"
	"google.golang.org/genai"
)

// ConvertBladesToGenAI converts Blades ModelRequest to GenAI Content format
// Returns contents and system instruction separately for proper GenAI usage
func ConvertBladesToGenAI(req *blades.ModelRequest) ([]*genai.Content, *genai.Content, error) {
	if req == nil {
		return nil, nil, fmt.Errorf("request cannot be nil")
	}

	var systemInstruction *genai.Content
	contents := make([]*genai.Content, 0, len(req.Messages))

	for _, msg := range req.Messages {
		// Extract system messages for SystemInstruction
		if msg.Role == blades.RoleSystem {
			if systemInstruction == nil {
				// Convert first system message to system instruction
				sysContent, err := convertBladesMessageToGenAI(msg)
				if err != nil {
					return nil, nil, fmt.Errorf("converting system message: %w", err)
				}
				systemInstruction = sysContent
			}
			// Skip system messages from regular content (they go to SystemInstruction)
			continue
		}

		// Convert non-system messages to regular content
		content, err := convertBladesMessageToGenAI(msg)
		if err != nil {
			return nil, nil, fmt.Errorf("converting message: %w", err)
		}
		if content != nil {
			contents = append(contents, content)
		}
	}

	return contents, systemInstruction, nil
}

// convertBladesMessageToGenAI converts a Blades Message to GenAI Content
// For system messages used as SystemInstruction, the role should be empty
func convertBladesMessageToGenAI(msg *blades.Message) (*genai.Content, error) {
	if msg == nil {
		return nil, nil
	}

	parts := make([]*genai.Part, 0, len(msg.Parts)+len(msg.ToolCalls))
	for _, part := range msg.Parts {
		genaiPart, err := convertBladesPartToGenAI(part)
		if err != nil {
			return nil, fmt.Errorf("converting part: %w", err)
		}
		if genaiPart != nil {
			parts = append(parts, genaiPart)
		}
	}

	// Preserve function call parts for assistant role messages so Gemini can match responses
	if msg.Role == blades.RoleAssistant && len(msg.ToolCalls) > 0 {
		for _, tc := range msg.ToolCalls {
			genaiPart, err := convertToolCallToGenAIFunctionCall(tc)
			if err != nil {
				return nil, fmt.Errorf("converting tool call: %w", err)
			}
			if genaiPart != nil {
				parts = append(parts, genaiPart)
			}
		}
	}

	// Convert tool responses for tool role messages
	if msg.Role == blades.RoleTool && len(msg.ToolCalls) > 0 {
		for _, tc := range msg.ToolCalls {
			genaiPart, err := convertToolCallToGenAIFunctionResponse(tc)
			if err != nil {
				return nil, fmt.Errorf("converting tool call: %w", err)
			}
			if genaiPart != nil {
				parts = append(parts, genaiPart)
			}
		}
	}

	if len(parts) == 0 {
		return nil, nil
	}

	// Map Blades roles to GenAI roles
	var role string
	switch msg.Role {
	case blades.RoleUser:
		role = "user"
	case blades.RoleAssistant:
		role = "model"
	case blades.RoleSystem:
		role = "" // SystemInstruction should have empty role
	default:
		role = "user"
	}

	return &genai.Content{
		Parts: parts,
		Role:  role,
	}, nil
}

// convertBladesPartToGenAI converts a Blades Part to GenAI Part
func convertBladesPartToGenAI(part blades.Part) (*genai.Part, error) {
	switch p := part.(type) {
	case blades.TextPart:
		if strings.TrimSpace(p.Text) == "" {
			return nil, nil
		}
		return &genai.Part{Text: p.Text}, nil

	case blades.DataPart:
		return &genai.Part{
			InlineData: &genai.Blob{
				MIMEType: string(p.MimeType),
				Data:     p.Bytes,
			},
		}, nil

	case blades.FilePart:
		// For file parts, we need to determine if it's a data URL or file reference
		if strings.HasPrefix(p.URI, "data:") {
			// Parse data URL
			mimeType, data, err := parseDataURL(p.URI)
			if err != nil {
				return nil, fmt.Errorf("parsing data URL: %w", err)
			}
			return &genai.Part{
				InlineData: &genai.Blob{
					MIMEType: mimeType,
					Data:     data,
				},
			}, nil
		}

		// For file URIs, we might need to read the file content
		// For now, return an error as we need the actual file content
		return nil, fmt.Errorf("file URI conversion not implemented yet: %s", p.URI)

	default:
		return nil, fmt.Errorf("unsupported part type: %T", part)
	}
}

func convertToolCallToGenAIFunctionCall(tc *blades.ToolCall) (*genai.Part, error) {
	if tc == nil {
		return nil, nil
	}

	args := map[string]any{}
	if strings.TrimSpace(tc.Arguments) != "" {
		if err := json.Unmarshal([]byte(tc.Arguments), &args); err != nil {
			args["input"] = tc.Arguments
		}
	}

	return &genai.Part{
		FunctionCall: &genai.FunctionCall{
			ID:   tc.ID,
			Name: tc.Name,
			Args: args,
		},
	}, nil
}

func convertToolCallToGenAIFunctionResponse(tc *blades.ToolCall) (*genai.Part, error) {
	if tc == nil {
		return nil, nil
	}

	response := map[string]any{}
	if strings.TrimSpace(tc.Result) != "" {
		var raw any
		if err := json.Unmarshal([]byte(tc.Result), &raw); err == nil {
			switch v := raw.(type) {
			case map[string]any:
				response = v
			default:
				response["output"] = v
			}
		} else {
			response["output"] = tc.Result
		}
	}

	return &genai.Part{
		FunctionResponse: &genai.FunctionResponse{
			ID:       tc.ID,
			Name:     tc.Name,
			Response: response,
		},
	}, nil
}

// parseDataURL parses a data URL and returns MIME type and decoded data
func parseDataURL(dataURL string) (string, []byte, error) {
	if !strings.HasPrefix(dataURL, "data:") {
		return "", nil, fmt.Errorf("invalid data URL")
	}

	// Format: data:[<mediatype>][;base64],<data>
	parts := strings.SplitN(dataURL[5:], ",", 2)
	if len(parts) != 2 {
		return "", nil, fmt.Errorf("invalid data URL format")
	}

	header := parts[0]
	data := parts[1]

	// Parse MIME type and encoding
	var mimeType string
	var isBase64 bool

	if header == "" {
		mimeType = "text/plain"
	} else {
		headerParts := strings.Split(header, ";")
		mimeType = headerParts[0]
		if mimeType == "" {
			mimeType = "text/plain"
		}

		for _, part := range headerParts[1:] {
			if part == "base64" {
				isBase64 = true
				break
			}
		}
	}

	// Decode data
	var decodedData []byte
	var err error

	if isBase64 {
		decodedData, err = base64.StdEncoding.DecodeString(data)
		if err != nil {
			return "", nil, fmt.Errorf("decoding base64 data: %w", err)
		}
	} else {
		decodedData = []byte(data)
	}

	return mimeType, decodedData, nil
}

// ConvertBladesToolsToGenAI converts Blades Tools to GenAI Tools
func ConvertBladesToolsToGenAI(tools []*tools.Tool) ([]*genai.Tool, error) {
	genaiTools := make([]*genai.Tool, 0, len(tools))

	for _, tool := range tools {
		genaiTool, err := convertBladesToolToGenAI(tool)
		if err != nil {
			return nil, fmt.Errorf("converting tool %s: %w", tool.Name, err)
		}
		if genaiTool != nil {
			genaiTools = append(genaiTools, genaiTool)
		}
	}

	return genaiTools, nil
}

// convertBladesToolToGenAI converts a single Blades Tool to GenAI Tool
func convertBladesToolToGenAI(tool *tools.Tool) (*genai.Tool, error) {
	if tool == nil {
		return nil, nil
	}

	// Convert tool function declaration
	funcDecl := &genai.FunctionDeclaration{
		Name:        tool.Name,
		Description: tool.Description,
	}

	// Convert input schema if present - use ParametersJsonSchema directly
	if tool.InputSchema != nil {
		// Marshal the jsonschema.Schema directly to JSON
		jsonBytes, err := tool.InputSchema.MarshalJSON()
		if err != nil {
			return nil, fmt.Errorf("marshaling input schema to JSON: %w", err)
		}

		var schema any
		if err := json.Unmarshal(jsonBytes, &schema); err != nil {
			return nil, fmt.Errorf("unmarshaling input schema JSON: %w", err)
		}

		funcDecl.ParametersJsonSchema = schema
	}

	return &genai.Tool{
		FunctionDeclarations: []*genai.FunctionDeclaration{funcDecl},
	}, nil
}

// ConvertOptionsToGenAI converts Blades model options to GenAI generation config
func ConvertOptionsToGenAI(options []blades.ModelOption) (*genai.GenerationConfig, error) {
	if len(options) == 0 {
		return nil, nil
	}

	// Apply options to extract values
	opts := &blades.ModelOptions{}
	for _, opt := range options {
		opt(opts)
	}

	config := &genai.GenerationConfig{}

	if opts.Temperature > 0 {
		temp := float32(opts.Temperature)
		config.Temperature = &temp
	}

	if opts.MaxOutputTokens > 0 {
		maxTokens := int32(opts.MaxOutputTokens)
		config.MaxOutputTokens = maxTokens
	}

	if opts.TopP > 0 {
		topP := float32(opts.TopP)
		config.TopP = &topP
	}

	return config, nil
}

// ConvertGenAIToBlades converts GenAI response to Blades ModelResponse
func ConvertGenAIToBlades(resp *genai.GenerateContentResponse) (*blades.ModelResponse, error) {
	if resp == nil {
		return nil, fmt.Errorf("response cannot be nil")
	}

	response := &blades.ModelResponse{}

	for _, candidate := range resp.Candidates {
		if candidate.Content == nil {
			continue
		}
		msg, err := convertGenAICandidateToBlades(candidate)
		if err != nil {
			return nil, fmt.Errorf("converting candidate: %w", err)
		}
		if msg != nil {
			response.Message = msg
		}
	}

	return response, nil
}

// convertGenAICandidateToBlades converts a GenAI Candidate to Blades Message
func convertGenAICandidateToBlades(candidate *genai.Candidate) (*blades.Message, error) {
	parts := make([]blades.Part, 0, len(candidate.Content.Parts))
	var toolCalls []*blades.ToolCall

	for _, part := range candidate.Content.Parts {
		// Handle function calls first
		if part.FunctionCall != nil {
			toolCall := &blades.ToolCall{
				ID:        part.FunctionCall.ID,
				Name:      part.FunctionCall.Name,
				Arguments: convertArgsToJSON(part.FunctionCall.Args),
				// Result will be set when function is executed
			}
			toolCalls = append(toolCalls, toolCall)
			continue
		}

		// Handle function responses
		if part.FunctionResponse != nil {
			// Function responses are handled separately in tool execution flow
			continue
		}

		// Handle regular content parts
		bladesPart, err := convertGenAIPartToBlades(part)
		if err != nil {
			return nil, fmt.Errorf("converting part: %w", err)
		}
		if bladesPart != nil {
			parts = append(parts, bladesPart)
		}
	}

	// Map GenAI roles back to Blades roles
	var role blades.Role
	switch candidate.Content.Role {
	case "model":
		role = blades.RoleAssistant
	case "user":
		role = blades.RoleUser
	default:
		role = blades.RoleAssistant
	}

	// If there are tool calls, set role to Tool
	if len(toolCalls) > 0 {
		role = blades.RoleTool
	}

	// Determine status based on finish reason
	status := blades.StatusCompleted
	switch candidate.FinishReason {
	case genai.FinishReasonMaxTokens:
		status = blades.StatusIncomplete
	case genai.FinishReasonSafety, genai.FinishReasonRecitation, genai.FinishReasonBlocklist:
		status = blades.StatusIncomplete
	}

	// Create metadata map for additional information
	metadata := make(map[string]string)

	// Add finish reason if available
	if candidate.FinishReason != "" {
		metadata["finish_reason"] = string(candidate.FinishReason)
	}

	// Add safety ratings as metadata
	if len(candidate.SafetyRatings) > 0 {
		for _, rating := range candidate.SafetyRatings {
			categoryKey := fmt.Sprintf("safety_%s", strings.ToLower(string(rating.Category)))
			metadata[categoryKey] = string(rating.Probability)
			if rating.Blocked {
				metadata[categoryKey+"_blocked"] = "true"
			}
		}
	}

	// Handle safety blocks or content filtering
	if candidate.FinishReason == genai.FinishReasonSafety {
		metadata["blocked_reason"] = "safety"
	} else if candidate.FinishReason == genai.FinishReasonRecitation {
		metadata["blocked_reason"] = "recitation"
	}

	msg := &blades.Message{
		Role:      role,
		Parts:     parts,
		Status:    status,
		ToolCalls: toolCalls,
		Metadata:  metadata,
	}

	return msg, nil
}

// convertGenAIPartToBlades converts a GenAI Part to Blades Part
func convertGenAIPartToBlades(part *genai.Part) (blades.Part, error) {
	if part == nil {
		return nil, nil
	}

	if part.Text != "" {
		return blades.TextPart{Text: part.Text}, nil
	}

	if part.InlineData != nil {
		// Determine MIME type and create appropriate part
		mimeType := blades.MimeType(part.InlineData.MIMEType)

		return blades.DataPart{
			Name:     generateNameFromMimeType(part.InlineData.MIMEType),
			Bytes:    part.InlineData.Data,
			MimeType: mimeType,
		}, nil
	}

	// Handle function calls and other parts as needed
	return nil, nil
}

// generateNameFromMimeType generates a filename based on MIME type
func generateNameFromMimeType(mimeType string) string {
	exts, err := mime.ExtensionsByType(mimeType)
	if err != nil || len(exts) == 0 {
		return "data"
	}
	return "data" + exts[0]
}

// convertArgsToJSON converts function call arguments map to JSON string
func convertArgsToJSON(args map[string]any) string {
	if args == nil {
		return "{}"
	}
	jsonBytes, err := json.Marshal(args)
	if err != nil {
		return "{}"
	}
	return string(jsonBytes)
}

// ConvertGenAIResponseToBlades converts GenAI response to Blades (alias for backward compatibility)
func ConvertGenAIResponseToBlades(resp *genai.GenerateContentResponse) (*blades.ModelResponse, error) {
	return ConvertGenAIToBlades(resp)
}

// ConvertStreamChunkToBlades converts a streaming response chunk to Blades ModelResponse
func ConvertStreamChunkToBlades(chunk *genai.GenerateContentResponse) (*blades.ModelResponse, error) {
	// Streaming chunks use the same format as regular responses
	return ConvertGenAIToBlades(chunk)
}

// ConvertSDKErrorToBlades converts SDK errors to Blades error types
func ConvertSDKErrorToBlades(err error) error {
	if err == nil {
		return nil
	}

	// This would need to inspect the actual error types from the SDK
	// For now, return a generic error wrapper
	return fmt.Errorf("SDK error: %w", err)
}

// Mock types for testing (these will be replaced by actual SDK types)
// These are used by the unit tests until the real implementation is complete

type GenAIResponse struct {
	Candidates    []Candidate    `json:"candidates"`
	UsageMetadata *UsageMetadata `json:"usageMetadata,omitempty"`
}

type VertexResponse struct {
	Candidates    []VertexCandidate    `json:"candidates"`
	UsageMetadata *VertexUsageMetadata `json:"usageMetadata,omitempty"`
}

type Candidate struct {
	Content      Content `json:"content"`
	FinishReason string  `json:"finishReason"`
}

type VertexCandidate struct {
	Content      VertexContent `json:"content"`
	FinishReason string        `json:"finishReason"`
}

type Content struct {
	Parts []Part `json:"parts"`
	Role  string `json:"role"`
}

type VertexContent struct {
	Parts []VertexPart `json:"parts"`
	Role  string       `json:"role"`
}

type Part struct {
	Text string `json:"text"`
}

type VertexPart struct {
	Text string `json:"text"`
}

type UsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

type VertexUsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

type StreamChunk struct {
	Candidates   []Candidate `json:"candidates"`
	FinishReason string      `json:"finishReason"`
}

// Error types for testing
type RateLimitError struct {
	Message string
}

func (e *RateLimitError) Error() string {
	return e.Message
}

type AuthError struct {
	Message string
}

func (e *AuthError) Error() string {
	return e.Message
}

type ServerError struct {
	Message string
}

func (e *ServerError) Error() string {
	return e.Message
}
