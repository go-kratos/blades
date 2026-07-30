package gemini

import (
	"encoding/json"
	"fmt"

	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/model"
	"github.com/go-kratos/blades/tools"
	"google.golang.org/genai"
)

func convertMessageToGenAI(req *model.Request) (*genai.Content, []*genai.Content, error) {
	if req == nil {
		return nil, nil, nil
	}
	var (
		system   *genai.Content
		contents []*genai.Content
	)
	if req.System != "" {
		system = &genai.Content{Parts: []*genai.Part{genai.NewPartFromText(req.System)}}
	}
	for _, msg := range req.Messages {
		if msg == nil {
			continue
		}
		switch msg.Role {
		case model.RoleUser:
			contents = append(contents, &genai.Content{Role: genai.RoleUser, Parts: convertMessagePartsToGenAI(msg.Parts)})
		case model.RoleAssistant:
			contents = append(contents, &genai.Content{Role: genai.RoleModel, Parts: convertMessagePartsToGenAI(msg.Parts)})
		case model.RoleTool:
			parts := convertToolResultsToGenAI(msg.Parts)
			if len(parts) > 0 {
				contents = append(contents, &genai.Content{Role: genai.RoleUser, Parts: parts})
			}
		}
	}
	return system, contents, nil
}

func convertMessagePartsToGenAI(parts []content.Part) []*genai.Part {
	res := make([]*genai.Part, 0, len(parts))
	for _, part := range parts {
		switch v := part.(type) {
		case content.Text:
			res = append(res, genai.NewPartFromText(v.Text))
		case content.Thinking:
			res = append(res, &genai.Part{
				Text:             v.Text,
				Thought:          true,
				ThoughtSignature: v.Signature,
			})
		case content.DataPart:
			res = append(res, genai.NewPartFromBytes(v.Bytes, v.MIME))
		case content.FilePart:
			res = append(res, &genai.Part{
				FileData: &genai.FileData{
					FileURI:     v.URI,
					DisplayName: v.Filename,
					MIMEType:    v.MIME,
				},
			})
		case content.ToolUse:
			args := map[string]any{}
			if len(v.Input) > 0 {
				if err := json.Unmarshal(v.Input, &args); err != nil {
					args = map[string]any{"input": string(v.Input)}
				}
			}
			res = append(res, &genai.Part{
				FunctionCall: &genai.FunctionCall{
					ID:   v.ID,
					Name: v.Name,
					Args: args,
				},
			})
		}
	}
	return res
}

func convertToolResultsToGenAI(parts []content.Part) []*genai.Part {
	res := make([]*genai.Part, 0, len(parts))
	for _, part := range parts {
		result, ok := part.(content.ToolResult)
		if !ok {
			continue
		}
		response := map[string]any{}
		text := textFromParts(result.Parts)
		if text != "" {
			if err := json.Unmarshal([]byte(text), &response); err != nil {
				response["output"] = text
			}
		}
		if result.IsError {
			response["error"] = text
		}
		res = append(res, &genai.Part{
			FunctionResponse: &genai.FunctionResponse{
				ID:       result.ID,
				Name:     result.Name,
				Response: response,
			},
		})
	}
	return res
}

func convertBladesToolsToGenAI(toolSpecs []tools.ToolSpec) ([]*genai.Tool, error) {
	genaiTools := make([]*genai.Tool, 0, len(toolSpecs))
	for _, spec := range toolSpecs {
		genaiTool := &genai.Tool{
			FunctionDeclarations: []*genai.FunctionDeclaration{
				{
					Name:                 spec.Name,
					Description:          spec.Description,
					ParametersJsonSchema: spec.InputSchema,
					ResponseJsonSchema:   spec.OutputSchema,
				},
			},
		}
		genaiTools = append(genaiTools, genaiTool)
	}
	return genaiTools, nil
}

func convertGenAIToBlades(resp *genai.GenerateContentResponse) (*model.Response, error) {
	chunk, err := convertGenAIToChunk(resp)
	if err != nil {
		return nil, err
	}
	usage := model.Usage{}
	if chunk.Usage != nil {
		usage = *chunk.Usage
	}
	return &model.Response{
		Message:    &model.Message{Role: model.RoleAssistant, Parts: chunk.Parts},
		StopReason: chunk.StopReason,
		Usage:      usage,
	}, nil
}

func convertGenAIToChunk(resp *genai.GenerateContentResponse) (*model.Chunk, error) {
	if resp == nil {
		return &model.Chunk{}, nil
	}
	var (
		parts      []content.Part
		stopReason model.StopReason
	)
	for _, candidate := range resp.Candidates {
		if candidate == nil {
			continue
		}
		if candidate.FinishReason != "" {
			stopReason = mapGeminiStopReason(candidate.FinishReason)
		}
		if candidate.Content == nil {
			continue
		}
		for _, part := range candidate.Content.Parts {
			bladesPart, err := convertGenAIPartToBlades(part)
			if err != nil {
				return nil, err
			}
			if bladesPart != nil {
				parts = append(parts, bladesPart)
			}
		}
	}
	chunk := &model.Chunk{Parts: parts, StopReason: stopReason}
	if resp.UsageMetadata != nil {
		rawUsage, err := json.Marshal(resp.UsageMetadata)
		if err != nil {
			return nil, fmt.Errorf("marshal usage metadata: %w", err)
		}
		usage := geminiUsageToModel(resp.UsageMetadata, rawUsage)
		chunk.Usage = &usage
	}
	if hasToolUse(parts) {
		chunk.StopReason = model.StopToolUse
	}
	return chunk, nil
}

func geminiUsageToModel(metadata *genai.GenerateContentResponseUsageMetadata, raw json.RawMessage) model.Usage {
	promptTokens := int64(metadata.PromptTokenCount)
	cachedTokens := int64(metadata.CachedContentTokenCount)
	toolTokens := int64(metadata.ToolUsePromptTokenCount)
	outputTokens := int64(metadata.CandidatesTokenCount)
	reasoningTokens := int64(metadata.ThoughtsTokenCount)
	totalInputTokens := promptTokens + toolTokens
	totalOutputTokens := outputTokens + reasoningTokens

	usage := model.Usage{
		InputCachedTokens:     cachedTokens,
		InputCacheMissTokens:  promptTokens - cachedTokens,
		InputToolTokens:       toolTokens,
		OutputReasoningTokens: reasoningTokens,
		TotalInputTokens:      totalInputTokens,
		TotalOutputTokens:     totalOutputTokens,
		TotalTokens:           int64(metadata.TotalTokenCount),
		Raw:                   raw,
	}
	addGeminiInputModalities(&usage, metadata.PromptTokensDetails, false)
	addGeminiInputModalities(&usage, metadata.CacheTokensDetails, true)
	addGeminiOutputModalities(&usage, metadata.CandidatesTokensDetails)
	return usage
}

func addGeminiInputModalities(usage *model.Usage, details []*genai.ModalityTokenCount, cached bool) {
	for _, detail := range details {
		tokens := int64(detail.TokenCount)
		switch detail.Modality {
		case genai.MediaModalityText:
			if cached {
				usage.InputCachedTextTokens += tokens
			} else {
				usage.InputTextTokens += tokens
			}
		case genai.MediaModalityImage:
			if cached {
				usage.InputCachedImageTokens += tokens
			} else {
				usage.InputImageTokens += tokens
			}
		case genai.MediaModalityAudio:
			if cached {
				usage.InputCachedAudioTokens += tokens
			} else {
				usage.InputAudioTokens += tokens
			}
		case genai.MediaModalityVideo:
			if cached {
				usage.InputCachedVideoTokens += tokens
			} else {
				usage.InputVideoTokens += tokens
			}
		}
	}
}

func addGeminiOutputModalities(usage *model.Usage, details []*genai.ModalityTokenCount) {
	for _, detail := range details {
		tokens := int64(detail.TokenCount)
		switch detail.Modality {
		case genai.MediaModalityText:
			usage.OutputTextTokens += tokens
		case genai.MediaModalityImage:
			usage.OutputImageTokens += tokens
		case genai.MediaModalityAudio:
			usage.OutputAudioTokens += tokens
		}
	}
}

// convertGenAIPartToBlades converts a GenAI Part to a shared Blades content Part.
func convertGenAIPartToBlades(part *genai.Part) (content.Part, error) {
	if part == nil {
		return nil, nil
	}
	if part.FunctionCall != nil {
		input := json.RawMessage("{}")
		if len(part.FunctionCall.Args) > 0 {
			args, err := json.Marshal(part.FunctionCall.Args)
			if err != nil {
				return nil, fmt.Errorf("marshal function call args: %w", err)
			}
			input = args
		}
		return content.ToolUse{
			ID:    part.FunctionCall.ID,
			Name:  part.FunctionCall.Name,
			Input: input,
		}, nil
	}
	if part.FileData != nil {
		return content.FilePart{
			URI:      part.FileData.FileURI,
			Filename: part.FileData.DisplayName,
			MIME:     part.FileData.MIMEType,
		}, nil
	}
	if part.InlineData != nil {
		return content.DataPart{
			Bytes:    part.InlineData.Data,
			Filename: part.InlineData.DisplayName,
			MIME:     part.InlineData.MIMEType,
		}, nil
	}
	if part.Thought {
		return content.Thinking{Text: part.Text, Signature: part.ThoughtSignature}, nil
	}
	if part.Text == "" {
		return nil, nil
	}
	return content.Text{Text: part.Text}, nil
}

func mapGeminiStopReason(reason genai.FinishReason) model.StopReason {
	switch reason {
	case genai.FinishReasonStop:
		return model.StopEnd
	case genai.FinishReasonMaxTokens:
		return model.StopMaxTokens
	case genai.FinishReasonSafety, genai.FinishReasonProhibitedContent, genai.FinishReasonImageSafety:
		return model.StopSafety
	default:
		return ""
	}
}

func hasToolUse(parts []content.Part) bool {
	for _, part := range parts {
		if _, ok := part.(content.ToolUse); ok {
			return true
		}
	}
	return false
}

func textFromParts(parts []content.Part) string {
	var text string
	for _, part := range parts {
		if t, ok := part.(content.Text); ok {
			text += t.Text
		}
	}
	return text
}
