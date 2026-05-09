package convert

import (
	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/event"
	"github.com/go-kratos/blades/model"
)

// PromptToMessage converts an event.Prompt to a model.Message.
func PromptToMessage(p event.Prompt) *model.Message {
	return &model.Message{
		Role:  model.RoleUser,
		Parts: p.Parts,
	}
}

// SteerToMessage converts an event.Steer to a model.Message.
func SteerToMessage(s event.Steer) *model.Message {
	return &model.Message{
		Role:  model.RoleUser,
		Parts: s.Parts,
	}
}

// ToolResultToMessage converts tool results into a tool-role message.
func ToolResultToMessage(results []content.ToolResult) *model.Message {
	parts := make([]content.Part, len(results))
	for i, r := range results {
		parts[i] = r
	}
	return &model.Message{
		Role:  model.RoleTool,
		Parts: parts,
	}
}
