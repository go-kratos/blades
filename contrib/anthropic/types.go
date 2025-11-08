package anthropic

import (
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/tools"
)

// convertPartsToContent converts Blades Parts to Claude ContentBlockParamUnion.
func convertPartsToContent(parts []blades.Part) []anthropic.ContentBlockParamUnion {
	var content []anthropic.ContentBlockParamUnion
	for _, part := range parts {
		switch p := part.(type) {
		case blades.TextPart:
			content = append(content, anthropic.NewTextBlock(p.Text))
		}
	}
	return content
}

// convertBladesToolsToClaude converts Blades Tools to Claude ToolParams.
func convertBladesToolsToClaude(tools []*tools.Tool) ([]anthropic.ToolUnionParam, error) {
	var claudeTools []anthropic.ToolUnionParam
	for _, tool := range tools {
		var inputSchema anthropic.ToolInputSchemaParam
		schemaBytes, err := json.Marshal(tool.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("marshaling tool schema: %w", err)
		}
		if err := json.Unmarshal(schemaBytes, &inputSchema); err != nil {
			return nil, fmt.Errorf("unmarshaling tool schema: %w", err)
		}
		toolParam := anthropic.ToolParam{
			Name:        tool.Name,
			InputSchema: inputSchema,
		}
		if tool.Description != "" {
			toolParam.Description = anthropic.String(tool.Description)
		}
		claudeTools = append(claudeTools, anthropic.ToolUnionParam{
			OfTool: &toolParam,
		})
	}
	return claudeTools, nil
}

// convertClaudeToBlades converts a Claude Message to Blades ModelResponse.
func convertClaudeToBlades(message *anthropic.Message) (*blades.ModelResponse, error) {
	msg := blades.NewMessage(blades.RoleAssistant)
	for _, block := range message.Content {
		switch b := block.AsAny().(type) {
		case anthropic.TextBlock:
			msg.Parts = append(msg.Parts, blades.TextPart{Text: b.Text})
		case anthropic.ToolUseBlock:
			input, err := json.Marshal(b.Input)
			if err != nil {
				return nil, err
			}
			msg.Parts = append(msg.Parts, blades.ToolPart{
				ID:      b.ID,
				Name:    b.Name,
				Request: string(input),
			})
		}
	}

	return &blades.ModelResponse{
		Message: msg,
	}, nil
}

// convertStreamDeltaToBlades converts a Claude ContentBlockDeltaEvent to Blades ModelResponse.
func convertStreamDeltaToBlades(event anthropic.ContentBlockDeltaEvent) (*blades.ModelResponse, error) {
	response := &blades.ModelResponse{}
	switch delta := event.Delta.AsAny().(type) {
	case anthropic.TextDelta:
		msg := &blades.Message{
			Role: blades.RoleAssistant,
			Parts: []blades.Part{
				blades.TextPart{Text: delta.Text},
			},
		}
		response.Message = msg
	}
	return response, nil
}
