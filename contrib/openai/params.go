package openai

import (
	"strings"

	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/model"
)

func promptFromMessages(messages []*model.Message) string {
	var sections []string
	for _, msg := range messages {
		sections = append(sections, textFromContentParts(msg.Parts))
	}
	return strings.Join(sections, "\n")
}

func textFromContentParts(parts []content.Part) string {
	var text string
	for _, part := range parts {
		if textPart, ok := part.(content.Text); ok {
			text += textPart.Text
		}
	}
	return text
}
