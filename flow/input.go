package flow

import (
	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/event"
)

func promptInput(parts []content.Part) <-chan event.Input {
	input := make(chan event.Input, 1)
	input <- event.Prompt{Parts: parts}
	close(input)
	return input
}
