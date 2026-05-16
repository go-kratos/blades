package event

import (
	"testing"

	"github.com/go-kratos/blades/content"
)

func TestTurnEndText(t *testing.T) {
	t.Parallel()

	turnEnd := TurnEnd{
		Parts: []content.Part{
			content.Text{Text: "hello"},
			content.DataPart{Bytes: []byte("ignored")},
			content.Text{Text: " world"},
		},
	}

	if got := turnEnd.Text(); got != "hello world" {
		t.Fatalf("TurnEnd.Text() = %q, want hello world", got)
	}
}
