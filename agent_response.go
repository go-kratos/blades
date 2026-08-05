package blades

import "github.com/go-kratos/blades/content"

// thinkingOnlyAsText converts a non-empty, thinking-only part list to text.
func thinkingOnlyAsText(parts []content.Part) ([]content.Part, bool) {
	if len(parts) == 0 {
		return parts, false
	}

	converted := make([]content.Part, len(parts))
	for i, part := range parts {
		thinking, ok := part.(content.Thinking)
		if !ok {
			return parts, false
		}
		converted[i] = content.Text{Text: thinking.Text}
	}
	return content.Coalesce(converted), true
}
