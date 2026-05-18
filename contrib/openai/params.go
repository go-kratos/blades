package openai

import (
	"strings"

	"github.com/go-kratos/blades/model"
)

func promptFromMessages(messages []*model.Message) string {
	var sections []string
	for _, msg := range messages {
		sections = append(sections, textFromParts(msg.Parts))
	}
	return strings.Join(sections, "\n")
}
