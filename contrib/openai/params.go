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
		sections = append(sections, msg.Text(), "\n")
	}
	if len(sections) == 0 {
		return "", ErrPromptRequired
	}
	return strings.Join(sections, "\n\n"), nil
}
