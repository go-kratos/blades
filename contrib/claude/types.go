package claude

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/go-kratos/blades"
)

// ConvertBladesToClaude converts a Blades ModelRequest to Claude MessageNewParams
func ConvertBladesToClaude(req *blades.ModelRequest, opt blades.ModelOptions) (anthropic.MessageNewParams, error) {
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(req.Model),
		MaxTokens: 4096, // Default max tokens
	}

	// Apply model options
	if opt.MaxOutputTokens > 0 {
		params.MaxTokens = int64(opt.MaxOutputTokens)
	}
	if opt.Temperature > 0 {
		params.Temperature = anthropic.Float(opt.Temperature)
	}
	if opt.TopP > 0 {
		params.TopP = anthropic.Float(opt.TopP)
	}

	// Convert messages
	var systemMessages []string
	var messages []anthropic.MessageParam

	for _, msg := range req.Messages {
		switch msg.Role {
		case blades.RoleSystem:
			// Extract system messages separately
			for _, part := range msg.Parts {
				if textPart, ok := part.(blades.TextPart); ok {
					systemMessages = append(systemMessages, textPart.Text)
				}
			}

		case blades.RoleUser:
			// Convert user message
			content, err := convertPartsToContent(msg.Parts)
			if err != nil {
				return params, err
			}
			messages = append(messages, anthropic.NewUserMessage(content...))

		case blades.RoleAssistant:
			// Convert assistant message
			content, err := convertPartsToContent(msg.Parts)
			if err != nil {
				return params, err
			}

			// Handle tool calls
			if len(msg.ToolCalls) > 0 {
				for _, tc := range msg.ToolCalls {
					// Parse tool input as map for proper JSON handling
					var input map[string]interface{}
					if err := json.Unmarshal([]byte(tc.Arguments), &input); err != nil {
						return params, fmt.Errorf("parsing tool arguments: %w", err)
					}

					toolUse := anthropic.NewToolUseBlock(tc.ID, input, tc.Name)
					content = append(content, toolUse)
				}
			}

			messages = append(messages, anthropic.NewAssistantMessage(content...))

		case blades.RoleTool:
			// Convert tool result to user message with tool_result blocks
			// This is the key difference from Gemini - Claude requires tool results in user messages
			var toolResults []anthropic.ContentBlockParamUnion
			for _, tc := range msg.ToolCalls {
				toolResult := anthropic.NewToolResultBlock(tc.ID, tc.Result, false)
				toolResults = append(toolResults, toolResult)
			}
			if len(toolResults) > 0 {
				messages = append(messages, anthropic.NewUserMessage(toolResults...))
			}
		}
	}

	// Set system message if any
	if len(systemMessages) > 0 {
		params.System = []anthropic.TextBlockParam{
			{Text: strings.Join(systemMessages, "\n\n")},
		}
	}

	params.Messages = messages

	// Convert tools if provided
	if len(req.Tools) > 0 {
		tools, err := convertBladesToolsToClaude(req.Tools)
		if err != nil {
			return params, fmt.Errorf("converting tools: %w", err)
		}
		params.Tools = tools
	}

	// Configure extended thinking if requested
	if opt.ThinkingBudget != nil {
		params.Thinking = anthropic.ThinkingConfigParamOfEnabled(int64(*opt.ThinkingBudget))
	}

	return params, nil
}

// convertPartsToContent converts Blades Parts to Claude ContentBlockParamUnion
func convertPartsToContent(parts []blades.Part) ([]anthropic.ContentBlockParamUnion, error) {
	var content []anthropic.ContentBlockParamUnion

	for _, part := range parts {
		switch p := part.(type) {
		case blades.TextPart:
			textBlock := anthropic.NewTextBlock(p.Text)
			content = append(content, textBlock)

		// Note: Image and Document parts are not yet defined in blades, skipping for now
		// Will be added when base types are ready
		}
	}

	return content, nil
}

// convertBladesToolsToClaude converts Blades Tools to Claude ToolParams
func convertBladesToolsToClaude(tools []*blades.Tool) ([]anthropic.ToolUnionParam, error) {
	var claudeTools []anthropic.ToolUnionParam

	for _, tool := range tools {
		toolParam := anthropic.ToolParam{
			Name:        tool.Name,
			Description: anthropic.String(tool.Description),
		}

		// Convert InputSchema if provided
		if tool.InputSchema != nil {
			// Marshal jsonschema.Schema to JSON and unmarshal to ToolInputSchemaParam
			schemaBytes, err := json.Marshal(tool.InputSchema)
			if err != nil {
				return nil, fmt.Errorf("marshaling tool schema: %w", err)
			}

			var inputSchema anthropic.ToolInputSchemaParam
			if err := json.Unmarshal(schemaBytes, &inputSchema); err != nil {
				return nil, fmt.Errorf("unmarshaling tool schema: %w", err)
			}

			toolParam.InputSchema = inputSchema
		} else {
			// Default schema if none provided
			toolParam.InputSchema = anthropic.ToolInputSchemaParam{
				Type: "object",
				Properties: map[string]interface{}{
					"input": map[string]interface{}{
						"type":        "string",
						"description": tool.Description,
					},
				},
			}
		}

		claudeTools = append(claudeTools, anthropic.ToolUnionParam{
			OfTool: &toolParam,
		})
	}

	return claudeTools, nil
}

// ConvertClaudeToBlades converts a Claude Message to Blades ModelResponse
func ConvertClaudeToBlades(message *anthropic.Message) (*blades.ModelResponse, error) {
	var messages []*blades.Message

	// Convert content blocks to Blades parts
	var parts []blades.Part
	var toolCalls []*blades.ToolCall

	for _, block := range message.Content {
		switch b := block.AsAny().(type) {
		case anthropic.TextBlock:
			parts = append(parts, blades.TextPart{Text: b.Text})

		case anthropic.ToolUseBlock:
			// Convert tool use to tool call
			argsJSON, err := json.Marshal(b.Input)
			if err != nil {
				return nil, fmt.Errorf("marshaling tool input: %w", err)
			}

			toolCalls = append(toolCalls, &blades.ToolCall{
				ID:        b.ID,
				Name:      b.Name,
				Arguments: string(argsJSON),
			})
		}
	}

	msg := &blades.Message{
		Role:      blades.RoleAssistant,
		Parts:     parts,
		ToolCalls: toolCalls,
	}

	messages = append(messages, msg)

	return &blades.ModelResponse{
		Messages: messages,
	}, nil
}

// ConvertStreamDeltaToBlades converts a Claude ContentBlockDeltaEvent to Blades ModelResponse
func ConvertStreamDeltaToBlades(event anthropic.ContentBlockDeltaEvent) (*blades.ModelResponse, error) {
	switch delta := event.Delta.AsAny().(type) {
	case anthropic.TextDelta:
		// Text content delta
		msg := &blades.Message{
			Role: blades.RoleAssistant,
			Parts: []blades.Part{
				blades.TextPart{Text: delta.Text},
			},
		}
		return &blades.ModelResponse{
			Messages: []*blades.Message{msg},
		}, nil

	case anthropic.InputJSONDelta:
		// Tool input delta - accumulate but don't stream
		return nil, nil
	}

	return nil, nil
}
