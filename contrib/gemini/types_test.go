package gemini

import (
	"testing"

	"github.com/go-kratos/blades"
	"google.golang.org/genai"
)

func TestConvertMessageToGenAI_AssistantRole(t *testing.T) {
	t.Parallel()

	_, contents, err := convertMessageToGenAI(&blades.ModelRequest{
		Messages: []*blades.Message{
			blades.UserMessage("hello"),
			blades.AssistantMessage("world"),
		},
	})
	if err != nil {
		t.Fatalf("convertMessageToGenAI returned error: %v", err)
	}
	if got, want := len(contents), 2; got != want {
		t.Fatalf("contents len = %d, want %d", got, want)
	}
	if got, want := contents[0].Role, genai.RoleUser; got != want {
		t.Fatalf("first role = %q, want %q", got, want)
	}
	if got, want := contents[1].Role, genai.RoleModel; got != want {
		t.Fatalf("second role = %q, want %q", got, want)
	}
}
