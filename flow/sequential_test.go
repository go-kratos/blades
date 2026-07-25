package flow

import (
	"testing"

	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/event"
)

func TestDefaultBridgeUsesLastTurn(t *testing.T) {
	t.Parallel()

	output := make(chan event.Output, 4)
	output <- event.TurnEnd{Parts: []content.Part{content.Text{Text: "intermediate"}}}
	output <- event.AssistantMessageEnd{Parts: []content.Part{content.Text{Text: "final"}}}
	output <- event.TurnEnd{Parts: []content.Part{content.Text{Text: "final"}}}
	output <- event.Done{}
	close(output)

	input := defaultBridge(output)
	next, ok := <-input
	if !ok {
		t.Fatal("defaultBridge closed without an input")
	}
	prompt, ok := next.(event.Prompt)
	if !ok {
		t.Fatalf("defaultBridge output = %T, want event.Prompt", next)
	}
	if got := content.TextFromParts(prompt.Parts); got != "final" {
		t.Fatalf("defaultBridge text = %q, want final", got)
	}
	if _, ok := <-input; ok {
		t.Fatal("defaultBridge emitted more than one input")
	}
}
