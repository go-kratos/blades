package claude

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/tools"
)

// convertPartsToContent converts Blades Parts to Claude ContentBlockParamUnion.
func convertPartsToContent(parts []blades.Part) ([]anthropic.ContentBlockParamUnion, error) {
	var content []anthropic.ContentBlockParamUnion
	for _, part := range parts {
		switch p := part.(type) {
		case blades.TextPart:
			content = append(content, anthropic.NewTextBlock(p.Text))
		}
	}
	return content, nil
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
	var (
		parts     []blades.Part
		toolCalls []*blades.ToolCall
	)
	for _, block := range message.Content {
		switch b := block.AsAny().(type) {
		case anthropic.TextBlock:
			parts = append(parts, blades.TextPart{Text: b.Text})
		case anthropic.ToolUseBlock:
			args, err := json.Marshal(b.Input)
			if err != nil {
				return nil, fmt.Errorf("marshaling tool input: %w", err)
			}
			toolCalls = append(toolCalls, &blades.ToolCall{
				ID:        b.ID,
				Name:      b.Name,
				Arguments: string(args),
			})
		}
	}
	msg := &blades.Message{
		Role:      blades.RoleAssistant,
		Parts:     parts,
		ToolCalls: toolCalls,
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

func buildToolMesssage(ctx context.Context, message *anthropic.Message, tools []*tools.Tool) ([]anthropic.MessageParam, error) {
	var (
		toolMessages []anthropic.MessageParam
		toolResults  []anthropic.ContentBlockParamUnion
	)
	for _, block := range message.Content {
		switch variant := block.AsAny().(type) {
		case anthropic.ToolUseBlock:
			args := variant.JSON.Input.Raw()
			result, err := handleToolCall(ctx, tools, variant.Name, args)
			if err != nil {
				return nil, err
			}
			toolResults = append(toolResults, anthropic.NewToolResultBlock(variant.ID, result, false))
		}
	}
	toolMessages = append(toolMessages, message.ToParam())
	toolMessages = append(toolMessages, anthropic.NewUserMessage(toolResults...))
	return toolMessages, nil
}

// handleToolCall invokes a tool by name with the given arguments.
func handleToolCall(ctx context.Context, tools []*tools.Tool, name, arguments string) (string, error) {
	for _, tool := range tools {
		if tool.Name == name {
			return tool.Handler.Handle(ctx, arguments)
		}
	}
	return "", ErrToolNotFound
}
