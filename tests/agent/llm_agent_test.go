package agent_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/event"
	"github.com/go-kratos/blades/model"
	"github.com/go-kratos/blades/session"
	"github.com/go-kratos/blades/tests/dummyprovider"
	"github.com/go-kratos/blades/tests/testtools"
	"github.com/stretchr/testify/assert"
)

func TestLLMAgentExecutesCalculateTool(t *testing.T) {
	provider := dummyprovider.NewProvider(
		dummyprovider.AssistantResponse(
			[]content.Part{
				dummyprovider.Text("Let me calculate that."),
				dummyprovider.ToolUse("calc-1", "calculate", json.RawMessage(`{"expression":"123 * 456"}`)),
			},
			dummyprovider.WithStopReason(model.StopToolUse),
		),
		dummyprovider.TextResponse("The result is 56088."),
	)
	agent, err := blades.NewAgent(
		"calculator",
		blades.WithModel(provider),
		blades.WithTools(testtools.NewCalculateTool()),
	)
	assert.NoError(t, err)

	sess := session.NewSession()
	ctx, cancel := context.WithCancel(session.NewContext(context.Background(), sess))
	defer cancel()
	inputs := make(chan event.Input, 1)
	inputs <- event.NewPromptText("What is 123 * 456?")

	outputs, err := collectAgentOutputs(ctx, agent, inputs)
	cancel()
	assert.NoError(t, err)

	toolEnd, ok := findToolEnd(outputs, "calc-1")
	assert.True(t, ok)
	assert.Equal(t, "calculate", toolEnd.Name)
	assert.False(t, toolEnd.IsError)
	assert.Equal(t, "123 * 456 = 56088", textFromParts(toolEnd.Parts))

	turnEnd, ok := lastTurnEnd(outputs)
	assert.True(t, ok)
	assert.Equal(t, event.StopEnd, turnEnd.StopReason)
	assert.Equal(t, "The result is 56088.", textFromParts(turnEnd.Parts))
	assert.Equal(t, 2, provider.CallCount())

	messages, err := sess.Messages(ctx)
	assert.NoError(t, err)
	if assert.Len(t, messages, 4) {
		assert.Equal(t, model.RoleUser, messages[0].Role)
		assert.Equal(t, model.RoleAssistant, messages[1].Role)
		assert.Equal(t, model.RoleTool, messages[2].Role)
		assert.Equal(t, model.RoleAssistant, messages[3].Role)
		assert.Equal(t, "123 * 456 = 56088", toolResultText(messages[2].Parts))
		assert.Equal(t, "The result is 56088.", textFromParts(messages[3].Parts))
	}
}

func collectAgentOutputs(ctx context.Context, agent blades.Agent, inputs <-chan event.Input) ([]event.Output, error) {
	outputs, err := agent.Run(ctx, inputs)
	if err != nil {
		return nil, err
	}

	var collected []event.Output
	for output := range outputs {
		collected = append(collected, output)
		if _, ok := output.(event.TurnEnd); ok {
			return collected, nil
		}
	}
	return collected, nil
}

func findToolEnd(outputs []event.Output, id string) (event.ToolEnd, bool) {
	for _, output := range outputs {
		toolEnd, ok := output.(event.ToolEnd)
		if ok && toolEnd.ID == id {
			return toolEnd, true
		}
	}
	return event.ToolEnd{}, false
}

func lastTurnEnd(outputs []event.Output) (event.TurnEnd, bool) {
	var turnEnd event.TurnEnd
	found := false
	for _, output := range outputs {
		next, ok := output.(event.TurnEnd)
		if ok {
			turnEnd = next
			found = true
		}
	}
	return turnEnd, found
}

func textFromParts(parts []content.Part) string {
	var text string
	for _, part := range parts {
		if textPart, ok := part.(content.Text); ok {
			text += textPart.Text
		}
	}
	return text
}

func toolResultText(parts []content.Part) string {
	for _, part := range parts {
		toolResult, ok := part.(content.ToolResult)
		if ok {
			return textFromParts(toolResult.Parts)
		}
	}
	return ""
}
