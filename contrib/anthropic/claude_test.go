package anthropic

import (
	"testing"

	"github.com/go-kratos/blades"
)

func TestToClaudeParamsAssistantRole(t *testing.T) {
	t.Parallel()

	model := &Claude{model: "claude-test"}
	params, err := model.toClaudeParams(&blades.ModelRequest{
		Messages: []*blades.Message{
			blades.UserMessage("hello"),
			blades.AssistantMessage("world"),
		},
	})
	if err != nil {
		t.Fatalf("toClaudeParams returned error: %v", err)
	}
	if got, want := len(params.Messages), 2; got != want {
		t.Fatalf("messages len = %d, want %d", got, want)
	}
	if got, want := string(params.Messages[0].Role), "user"; got != want {
		t.Fatalf("first role = %q, want %q", got, want)
	}
	if got, want := string(params.Messages[1].Role), "assistant"; got != want {
		t.Fatalf("second role = %q, want %q", got, want)
	}
}
