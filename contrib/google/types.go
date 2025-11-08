package google

import (
	"encoding/json"
	"fmt"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/tools"
	"google.golang.org/genai"
)

func convertMessageToGenAI(req *blades.ModelRequest) (*genai.Content, []*genai.Content, error) {
	var (
		system   *genai.Content
		contents []*genai.Content
	)
	for _, msg := range req.Messages {
		switch msg.Role {
		case blades.RoleSystem:
			system = &genai.Content{Parts: convertMessagePartsToGenAI(msg.Parts)}
		case blades.RoleUser:
			contents = append(contents, &genai.Content{Role: genai.RoleUser, Parts: convertMessagePartsToGenAI(msg.Parts)})
		case blades.RoleAssistant:
			contents = append(contents, &genai.Content{Role: genai.RoleUser, Parts: convertMessagePartsToGenAI(msg.Parts)})
		case blades.RoleTool:
			var parts []*genai.Part
			for _, part := range msg.Parts {
				switch v := any(part).(type) {
				case blades.ToolPart:
					response := map[string]any{}
					if err := json.Unmarshal([]byte(v.Response), &response); err != nil {
						response["output"] = v.Response
					}
					parts = append(parts, genai.NewPartFromFunctionResponse(v.Name, response))
				}
			}
			contents = append(contents, &genai.Content{Role: genai.RoleUser, Parts: parts})
		}
	}
	return system, contents, nil
}

func convertMessagePartsToGenAI(parts []blades.Part) []*genai.Part {
	res := make([]*genai.Part, 0, len(parts))
	for _, part := range parts {
		switch v := part.(type) {
		case blades.TextPart:
			res = append(res, &genai.Part{Text: v.Text})
		case blades.DataPart:
			res = append(res, &genai.Part{
				InlineData: &genai.Blob{
					Data:        v.Bytes,
					DisplayName: v.Name,
					MIMEType:    string(v.MIMEType),
				},
			})
		case blades.FilePart:
			res = append(res, &genai.Part{
				FileData: &genai.FileData{
					FileURI:     v.URI,
					DisplayName: v.Name,
					MIMEType:    string(v.MIMEType),
				},
			})
		}
	}
	return res
}

func convertBladesToolsToGenAI(tools []*tools.Tool) ([]*genai.Tool, error) {
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

func convertBladesToolToGenAI(tool *tools.Tool) (*genai.Tool, error) {
	return &genai.Tool{
		FunctionDeclarations: []*genai.FunctionDeclaration{
			&genai.FunctionDeclaration{
				Name:                 tool.Name,
				Description:          tool.Description,
				ParametersJsonSchema: tool.InputSchema,
			},
		},
	}, nil
}

func convertGenAIToBlades(resp *genai.GenerateContentResponse) (*blades.ModelResponse, error) {
	message := &blades.Message{Status: blades.StatusIncomplete}
	for _, candidate := range resp.Candidates {
		if candidate.Content == nil {
			continue
		}
		for _, part := range candidate.Content.Parts {
			bladesPart, err := convertGenAIPartToBlades(part)
			if err != nil {
				return nil, err
			}
			message.Parts = append(message.Parts, bladesPart)
		}
	}
	return &blades.ModelResponse{Message: message}, nil
}

// convertGenAIPartToBlades converts a GenAI Part to Blades Part
func convertGenAIPartToBlades(part *genai.Part) (blades.Part, error) {
	if part.FileData != nil {
		return blades.FilePart{
			URI:      part.FileData.FileURI,
			Name:     part.FileData.DisplayName,
			MIMEType: blades.MIMEType(part.FileData.MIMEType),
		}, nil
	}
	if part.InlineData != nil {
		return blades.DataPart{
			Bytes:    part.InlineData.Data,
			Name:     part.InlineData.DisplayName,
			MIMEType: blades.MIMEType(part.InlineData.MIMEType),
		}, nil
	}
	return blades.TextPart{Text: part.Text}, nil
}
