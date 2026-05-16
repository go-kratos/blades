package compact

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

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

func (t *toolResultBudget) Compact(_ context.Context, req Request) ([]*model.Message, error) {
	msgs := req.Messages
	if t.maxBytes <= 0 {
		return msgs, nil
	}
	if _, err := messageGroups(msgs); err != nil {
		return nil, err
	}
	result := make([]*model.Message, len(msgs))
	for i, msg := range msgs {
		if msg == nil {
			result[i] = msg
			continue
		}
		if msg.Role != model.RoleTool {
			result[i] = msg
			continue
		}
		truncated := false
		newParts := make([]content.Part, len(msg.Parts))
		for j, p := range msg.Parts {
			if tr, ok := p.(content.ToolResult); ok {
				if toolResultSize(tr.Parts) > t.maxBytes {
					tr.Parts = truncateToolResultParts(tr.Parts, t.maxBytes)
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

func toolResultSize(parts []content.Part) int64 {
	text := content.TextFromParts(parts)
	if text != "" {
		return int64(len([]byte(text)))
	}
	data, _ := json.Marshal(parts)
	return int64(len(data))
}

func truncateToolResultParts(parts []content.Part, maxBytes int64) []content.Part {
	text := content.TextFromParts(parts)
	if text == "" {
		return []content.Part{content.Text{Text: "[truncated: result exceeded budget]"}}
	}
	return []content.Part{content.Text{Text: truncateText(text, maxBytes)}}
}

func truncateText(text string, maxBytes int64) string {
	const marker = "\n[truncated: result exceeded budget]\n"
	if maxBytes <= int64(len(marker))+8 {
		return strings.TrimSpace(marker)
	}
	limit := int(maxBytes) - len(marker)
	headBytes := limit / 2
	tailBytes := limit - headBytes
	head := safePrefix(text, headBytes)
	tail := safeSuffix(text, tailBytes)
	return head + marker + tail
}

func safePrefix(text string, maxBytes int) string {
	if len(text) <= maxBytes {
		return text
	}
	for maxBytes > 0 && !utf8.ValidString(text[:maxBytes]) {
		maxBytes--
	}
	return text[:maxBytes]
}

func safeSuffix(text string, maxBytes int) string {
	if len(text) <= maxBytes {
		return text
	}
	start := len(text) - maxBytes
	for start < len(text) && !utf8.ValidString(text[start:]) {
		start++
	}
	return text[start:]
}
