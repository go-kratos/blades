package openai

import (
	"strings"

	"github.com/go-kratos/blades"
)

func promptFromMessages(messages []*blades.Message) (string, error) {
	var sections []string
	for _, msg := range messages {
		sections = append(sections, msg.Text())
	}
	if len(sections) == 0 {
		return "", ErrPromptRequired
	}
	return strings.Join(sections, "\n"), nil
}
