package openai

import (
	"strings"

	"github.com/go-kratos/blades"
)

func promptFromMessages(messages []*blades.Message) (string, error) {
	if len(messages) == 0 {
		return "", ErrPromptRequired
	}
	var sections []string
	for _, msg := range messages {
		switch msg.Role {
		case blades.RoleSystem, blades.RoleUser:
			var textParts []string
			for _, part := range msg.Parts {
				switch v := part.(type) {
				case blades.TextPart:
					if strings.TrimSpace(v.Text) != "" {
						textParts = append(textParts, v.Text)
					}
				}
			}
			if len(textParts) > 0 {
				sections = append(sections, strings.Join(textParts, "\n"))
			}
		}
	}
	if len(sections) == 0 {
		return "", ErrPromptRequired
	}
	return strings.Join(sections, "\n\n"), nil
}
