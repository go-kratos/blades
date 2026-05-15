package anthropic

import (
	"encoding/json"
	"fmt"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/model"
	"github.com/go-kratos/blades/tools"
)

// convertPartsToContent converts Blades parts to Claude ContentBlockParamUnion.
func convertPartsToContent(parts []content.Part) []anthropic.ContentBlockParamUnion {
	var out []anthropic.ContentBlockParamUnion
	for _, part := range parts {
		switch p := part.(type) {
		case content.Text:
			out = append(out, anthropic.NewTextBlock(p.Text))
		case content.Thinking:
			out = append(out, anthropic.NewThinkingBlock(string(p.Signature), p.Text))
		case content.ToolUse:
			out = append(out, anthropic.NewToolUseBlock(p.ID, decodeToolInput(p.Input), p.Name))
		case content.ToolResult:
			out = append(out, anthropic.NewToolResultBlock(p.ID, textFromParts(p.Parts), p.IsError))
		}
	}
	return out
}

// convertBladesToolsToClaude converts Blades tool specs to Claude ToolParams.
func convertBladesToolsToClaude(toolSpecs []tools.ToolSpec) ([]anthropic.ToolUnionParam, error) {
	var claudeTools []anthropic.ToolUnionParam
	for _, spec := range toolSpecs {
		var inputSchema anthropic.ToolInputSchemaParam
		if spec.InputSchema != nil {
			schemaBytes, err := json.Marshal(spec.InputSchema)
			if err != nil {
				return nil, fmt.Errorf("marshaling tool schema: %w", err)
			}
			if err := json.Unmarshal(schemaBytes, &inputSchema); err != nil {
				return nil, fmt.Errorf("unmarshaling tool schema: %w", err)
			}
		}
		toolParam := anthropic.ToolParam{
			Name:        spec.Name,
			InputSchema: inputSchema,
		}
		if spec.Description != "" {
			toolParam.Description = anthropic.String(spec.Description)
		}
		claudeTools = append(claudeTools, anthropic.ToolUnionParam{OfTool: &toolParam})
	}
	return claudeTools, nil
}

// convertClaudeToBlades converts a Claude Message to Blades model.Response.
func convertClaudeToBlades(message *anthropic.Message) (*model.Response, error) {
	resp := &model.Response{
		Message: &model.Message{Role: model.RoleAssistant},
		Usage: model.Usage{
			InputTokens:  message.Usage.InputTokens,
			OutputTokens: message.Usage.OutputTokens,
		},
		StopReason: mapClaudeStopReason(message.StopReason),
	}
	for _, block := range message.Content {
		switch b := block.AsAny().(type) {
		case anthropic.TextBlock:
			resp.Message.Parts = append(resp.Message.Parts, content.Text{Text: b.Text})
		case anthropic.ThinkingBlock:
			resp.Message.Parts = append(resp.Message.Parts, content.Thinking{Text: b.Thinking, Signature: []byte(b.Signature)})
		case anthropic.ToolUseBlock:
			resp.Message.Parts = append(resp.Message.Parts, content.ToolUse{
				ID:    b.ID,
				Name:  b.Name,
				Input: json.RawMessage(b.Input),
			})
		}
	}
	if resp.StopReason == "" {
		resp.StopReason = model.StopEnd
	}
	return resp, nil
}

// convertStreamDeltaToChunk converts a Claude ContentBlockDeltaEvent to a model chunk.
func convertStreamDeltaToChunk(event anthropic.ContentBlockDeltaEvent) *model.Chunk {
	chunk := &model.Chunk{}
	switch delta := event.Delta.AsAny().(type) {
	case anthropic.TextDelta:
		chunk.Parts = append(chunk.Parts, content.Text{Text: delta.Text})
	case anthropic.ThinkingDelta:
		chunk.Parts = append(chunk.Parts, content.Thinking{Text: delta.Thinking})
	}
	return chunk
}

func mapClaudeStopReason(reason anthropic.StopReason) model.StopReason {
	switch reason {
	case anthropic.StopReasonToolUse:
		return model.StopToolUse
	case anthropic.StopReasonMaxTokens:
		return model.StopMaxTokens
	case anthropic.StopReasonRefusal:
		return model.StopSafety
	default:
		return model.StopEnd
	}
}

func decodeToolInput(input json.RawMessage) any {
	var decoded any
	if err := json.Unmarshal(input, &decoded); err == nil {
		return decoded
	}
	return string(input)
}

func textFromParts(parts []content.Part) string {
	var text string
	for _, part := range parts {
		if textPart, ok := part.(content.Text); ok {
			text += textPart.Text
		}
	}
	return text
}
