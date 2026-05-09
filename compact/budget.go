package compact

import (
	"context"
	"encoding/json"

	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/model"
)

// NewToolResultBudget creates a compactor that truncates oversized tool results.
func NewToolResultBudget(maxBytes int64) Compactor {
	return &toolResultBudget{maxBytes: maxBytes}
}

type toolResultBudget struct {
	maxBytes int64
}

func (t *toolResultBudget) Compact(_ context.Context, msgs []*model.Message) ([]*model.Message, error) {
	if t.maxBytes <= 0 {
		return msgs, nil
	}
	result := make([]*model.Message, len(msgs))
	for i, msg := range msgs {
		if msg.Role != model.RoleTool {
			result[i] = msg
			continue
		}
		truncated := false
		newParts := make([]content.Part, len(msg.Parts))
		for j, p := range msg.Parts {
			if tr, ok := p.(content.ToolResult); ok {
				data, _ := json.Marshal(tr.Parts)
				if int64(len(data)) > t.maxBytes {
					tr.Parts = []content.Part{content.Text{Text: "[truncated: result exceeded budget]"}}
					truncated = true
				}
				newParts[j] = tr
			} else {
				newParts[j] = p
			}
		}
		if truncated {
			cp := *msg
			cp.Parts = newParts
			result[i] = &cp
		} else {
			result[i] = msg
		}
	}
	return result, nil
}
