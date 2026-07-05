package convert

import (
	"testing"

	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/model"
)

func TestResponseToTurnEndCarriesResponseID(t *testing.T) {
	t.Parallel()

	turnEnd := ResponseToTurnEnd(&model.Response{
		Message:    &model.Message{Role: model.RoleAssistant, Parts: []content.Part{content.Text{Text: "ok"}}},
		StopReason: model.StopEnd,
		ResponseID: "resp_123",
	})

	if turnEnd.ResponseID != "resp_123" {
		t.Fatalf("ResponseID = %q, want resp_123", turnEnd.ResponseID)
	}
}
